package gitlabf

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gitlab "gitlab.com/gitlab-org/api/client-go"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

// testRepo is the repository fixture shared across driver tests. The owner
// contains a subgroup slash so every test exercises GitLab's url-encoded
// project path.
func testRepo() forgetypes.Repository {
	return forgetypes.Repository{
		Kind:  forgetypes.KindGitLab,
		Host:  "gitlab.com",
		Owner: "group/sub",
		Name:  "repo",
	}
}

// projectPrefix is the escaped API path prefix for testRepo's MR 42.
const projectPrefix = "/api/v4/projects/group%2Fsub%2Frepo/merge_requests/42"

// testPR builds MR details pointing at the fixture repository, including
// the start_sha ForgePayload GetPullRequest would have stashed.
func testPR() *forge.PullRequestDetails {
	return &forge.PullRequestDetails{
		PullRequestSummary: forge.PullRequestSummary{
			Repository: testRepo(),
			Number:     42,
			URL:        "https://gitlab.com/group/sub/repo/-/merge_requests/42",
		},
		HeadSHA:      "headsha1",
		BaseSHA:      "basesha1",
		ForgePayload: json.RawMessage(`{"start_sha":"startsha1"}`),
	}
}

// fixtureMux dispatches on "METHOD escaped-path" so project paths and file
// paths containing %2F stay distinguishable (http.ServeMux would decode
// them). Unmatched requests fail the test.
type fixtureMux struct {
	t        *testing.T
	handlers map[string]http.HandlerFunc
}

func newFixtureMux(t *testing.T) *fixtureMux {
	return &fixtureMux{t: t, handlers: map[string]http.HandlerFunc{}}
}

// Handle registers a handler for "METHOD /escaped/path".
func (m *fixtureMux) Handle(pattern string, handler http.HandlerFunc) {
	m.handlers[pattern] = handler
}

// JSON registers a handler answering with a canned JSON body.
func (m *fixtureMux) JSON(pattern, body string) {
	m.Handle(pattern, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
}

func (m *fixtureMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := r.Method + " " + r.URL.EscapedPath()
	handler, ok := m.handlers[key]
	if !ok {
		m.t.Errorf("unexpected API call %s", key)
		http.NotFound(w, r)
		return
	}
	handler(w, r)
}

// newTestDriver starts an httptest server around handler and points the
// driver (REST and GraphQL) at it.
func newTestDriver(t *testing.T, handler http.Handler) *Driver {
	t.Helper()
	return newDriver(t, handler, Options{})
}

// newLocalDriver builds a driver with a local checkout fast path wired to a
// fake runner.
func newLocalDriver(t *testing.T, handler http.Handler, runner *fakeRunner) *Driver {
	t.Helper()
	return newDriver(t, handler, Options{LocalCheckout: "/repo", Runner: runner})
}

// newDriver builds a driver against a fixture server. The SDK client is
// rebuilt without retries so 5xx and 429 fixtures fail fast instead of
// backing off.
func newDriver(t *testing.T, handler http.Handler, opts Options) *Driver {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	opts.APIBase = srv.URL
	opts.Token = "test-token"
	d, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	client, err := gitlab.NewClient(opts.Token,
		gitlab.WithBaseURL(srv.URL),
		gitlab.WithoutRetries(),
	)
	if err != nil {
		t.Fatalf("gitlab.NewClient: %v", err)
	}
	d.client = client
	return d
}

// forbidNetwork fails the test if any HTTP request reaches the server.
func forbidNetwork(t *testing.T) http.Handler {
	return http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected API call %s %s", r.Method, r.URL)
	})
}

// fakeRunner is a canned vcs.Runner: results are keyed by the full command
// line, missing keys fail like a git error would.
type fakeRunner struct {
	out   map[string]string
	calls []string
}

// Run replays the canned result for the invoked command line.
func (r *fakeRunner) Run(_, name string, args ...string) ([]byte, []byte, error) {
	key := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, key)
	out, ok := r.out[key]
	if !ok {
		return nil, []byte("fatal: not a valid object name"), errors.New("exit status 128")
	}
	return []byte(out), nil, nil
}

// mustForgeErr asserts err carries a *forge.Error with identity fields set
// and returns it for inspection.
func mustForgeErr(t *testing.T, err error) *forge.Error {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var fe *forge.Error
	if !errors.As(err, &fe) {
		t.Fatalf("expected *forge.Error, got %T: %v", err, err)
	}
	if fe.ForgeID != forgetypes.KindGitLab {
		t.Errorf("ForgeID = %q, want gitlab", fe.ForgeID)
	}
	if fe.Op == "" {
		t.Error("Op must be set on forge errors")
	}
	if fe.Host == "" {
		t.Error("Host must be set on forge errors")
	}
	return fe
}

// wantForgeErr asserts err is a *forge.Error of the wanted kind.
func wantForgeErr(t *testing.T, err error, kind forge.ErrorKind) {
	t.Helper()
	fe := mustForgeErr(t, err)
	if fe.Kind != kind {
		t.Fatalf("error kind = %v, want %v (err: %v)", fe.Kind, kind, fe)
	}
}
