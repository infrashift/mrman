package forgejof

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

// testToken is the fixture token every test driver sends.
const testToken = "secret-token"

// testRepo is the repository fixture shared across driver tests.
func testRepo() forgetypes.Repository {
	return forgetypes.Repository{
		Kind:  forgetypes.KindForgejo,
		Host:  "codeberg.org",
		Owner: "octo",
		Name:  "hello",
	}
}

// testPR builds PR details pointing at the fixture repository.
func testPR() *forge.PullRequestDetails {
	return &forge.PullRequestDetails{
		PullRequestSummary: forge.PullRequestSummary{
			Repository: testRepo(),
			Number:     42,
			URL:        "https://codeberg.org/octo/hello/pulls/42",
		},
		HeadSHA: "headsha",
		BaseSHA: "basesha",
	}
}

// newTestDriver starts an httptest server around handler and points the
// driver at it. Fixture handlers register paths under /api/v1/… because the
// SDK appends that prefix to the base URL.
func newTestDriver(t *testing.T, handler http.Handler) *Driver {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	d, err := New(Options{APIBase: srv.URL, Token: testToken})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return d
}

// newLocalDriver builds a driver with a local checkout fast path wired to a
// fake runner.
func newLocalDriver(t *testing.T, handler http.Handler, runner *fakeRunner) *Driver {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	d, err := New(Options{APIBase: srv.URL, Token: testToken, LocalCheckout: "/repo", Runner: runner})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
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
	if fe.ForgeID != forgetypes.KindForgejo {
		t.Errorf("ForgeID = %q, want forgejo", fe.ForgeID)
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
