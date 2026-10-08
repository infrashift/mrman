package azdof

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

// Shared fixture coordinates.
const (
	testProject  = "proj"
	testRepoName = "repo"
	testViewerID = "11111111-2222-3333-4444-555555555555"
)

// testRepo is the repository fixture shared across driver tests.
func testRepo() forgetypes.Repository {
	return forgetypes.Repository{
		Kind:    forgetypes.KindAzureDevOps,
		Host:    "dev.azure.com",
		Owner:   "org",
		Project: testProject,
		Name:    testRepoName,
	}
}

// testPR builds PR details pointing at the fixture repository, with the
// iteration payload thread creation consumes.
func testPR() *forge.PullRequestDetails {
	return &forge.PullRequestDetails{
		PullRequestSummary: forge.PullRequestSummary{
			Repository: testRepo(),
			Number:     42,
			URL:        "https://dev.azure.com/org/proj/_git/repo/pullrequest/42",
		},
		HeadSHA:      "headsha",
		BaseSHA:      "basesha",
		ForgePayload: []byte(`{"iteration_id":2,"change_tracking":{"/src/main.go":7}}`),
	}
}

// resourceLocations serves the SDK's OPTIONS /_apis discovery request with
// the route templates the driver's calls resolve through.
func resourceLocationsJSON() string {
	type loc struct{ id, resource, template string }
	locations := []loc{
		{"9946fd70-0d40-406e-b686-b4744cbbcc37", "pullRequests",
			"{project}/_apis/git/repositories/{repositoryId}/pullrequests/{pullRequestId}"},
		{"01a46dea-7d46-4d40-bc84-319e7c260d99", "pullRequests",
			"{project}/_apis/git/pullrequests/{pullRequestId}"},
		{"d43911ee-6958-46b0-a42b-8445b8a0d004", "iterations",
			"{project}/_apis/git/repositories/{repositoryId}/pullrequests/{pullRequestId}/iterations/{iterationId}"},
		{"4216bdcf-b6b1-4d59-8b82-c34cc183fc8b", "changes",
			"{project}/_apis/git/repositories/{repositoryId}/pullrequests/{pullRequestId}/iterations/{iterationId}/changes"},
		{"52823034-34a8-4576-922c-8d8b77e9e4c4", "commits",
			"{project}/_apis/git/repositories/{repositoryId}/pullrequests/{pullRequestId}/commits"},
		{"ab6e2e5d-a0b7-4153-b64a-a4efe0d49449", "threads",
			"{project}/_apis/git/repositories/{repositoryId}/pullrequests/{pullRequestId}/threads/{threadId}"},
		{"4b6702c7-aa35-4b89-9c96-b9abf6d3e540", "reviewers",
			"{project}/_apis/git/repositories/{repositoryId}/pullrequests/{pullRequestId}/reviewers/{reviewerId}"},
		{"fb93c0db-47ed-4a31-8c20-47552878fb44", "items",
			"{project}/_apis/git/repositories/{repositoryId}/items/{*path}"},
	}
	var entries []string
	for _, l := range locations {
		entries = append(entries, fmt.Sprintf(`{
			"id": %q,
			"area": "git",
			"resourceName": %q,
			"routeTemplate": %q,
			"minVersion": "1.0",
			"maxVersion": "7.1",
			"releasedVersion": "7.1",
			"resourceVersion": 2
		}`, l.id, l.resource, l.template))
	}
	return fmt.Sprintf(`{"count": %d, "value": [%s]}`, len(entries), strings.Join(entries, ","))
}

// connectionDataJSON is the canned identity response for the raw
// connectionData call.
func connectionDataJSON() string {
	return fmt.Sprintf(`{
		"authenticatedUser": {"id": %q, "providerDisplayName": "Vera Viewer"},
		"authorizedUser": {"id": %q, "providerDisplayName": "Vera Viewer"}
	}`, testViewerID, testViewerID)
}

// withDiscovery wraps handler so the SDK's OPTIONS /_apis resource
// discovery is always answered; every other request passes through.
func withDiscovery(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			writeJSON(w, http.StatusOK, resourceLocationsJSON())
			return
		}
		handler.ServeHTTP(w, r)
	})
}

// writeJSON writes body with a JSON content type.
func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// newTestDriver starts an httptest server around handler and points the
// driver's organization URL at it.
func newTestDriver(t *testing.T, handler http.Handler) *Driver {
	t.Helper()
	srv := httptest.NewServer(withDiscovery(handler))
	t.Cleanup(srv.Close)
	d, err := New(Options{
		OrgURL:  srv.URL,
		Project: testProject,
		Repo:    testRepoName,
		Token:   "pat",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return d
}

// newLocalDriver builds a driver with a local checkout fast path wired to a
// fake runner.
func newLocalDriver(t *testing.T, handler http.Handler, runner *fakeRunner) *Driver {
	t.Helper()
	srv := httptest.NewServer(withDiscovery(handler))
	t.Cleanup(srv.Close)
	d, err := New(Options{
		OrgURL:        srv.URL,
		Project:       testProject,
		Repo:          testRepoName,
		Token:         "pat",
		LocalCheckout: "/checkout",
		Runner:        runner,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return d
}

// forbidNetwork fails the test if any non-discovery HTTP request reaches
// the server.
func forbidNetwork(t *testing.T) http.Handler {
	return http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected API call %s %s", r.Method, r.URL)
	})
}

// fakeRunner is a canned vcs.Runner: results are keyed by the full command
// line, missing keys fail like a git error would. GetDiff fetches files
// concurrently, so Run must be safe to call from several goroutines.
type fakeRunner struct {
	out   map[string]string
	mu    sync.Mutex
	calls []string
}

// Run replays the canned result for the invoked command line.
func (r *fakeRunner) Run(_, name string, args ...string) ([]byte, []byte, error) {
	key := name + " " + strings.Join(args, " ")
	r.mu.Lock()
	r.calls = append(r.calls, key)
	r.mu.Unlock()
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
	if fe.ForgeID != forgetypes.KindAzureDevOps {
		t.Errorf("ForgeID = %q, want azure_devops", fe.ForgeID)
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
