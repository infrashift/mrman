package azdof

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
)

const (
	prByIDPath     = "/proj/_apis/git/pullrequests/42"
	iterationsPath = "/proj/_apis/git/repositories/repo/pullrequests/42/iterations"
	changesPath    = "/proj/_apis/git/repositories/repo/pullrequests/42/iterations/2/changes"
	commitsPath    = "/proj/_apis/git/repositories/repo/pullrequests/42/commits"
	threadsPath    = "/proj/_apis/git/repositories/repo/pullrequests/42/threads"
	itemsPath      = "/proj/_apis/git/repositories/repo/items"
)

// prByIDJSON is the canned GetPullRequestById response.
func prByIDJSON(status string) string {
	closedDate := ""
	if status != "active" {
		closedDate = `"closedDate": "2026-07-10T12:00:00Z",`
	}
	return fmt.Sprintf(`{
		"pullRequestId": 42,
		"title": "Add feature",
		"description": "The body",
		"status": %q,
		%s
		"isDraft": false,
		"createdBy": {"displayName": "Alice"},
		"creationDate": "2026-07-01T10:00:00Z",
		"sourceRefName": "refs/heads/feature",
		"targetRefName": "refs/heads/main",
		"lastMergeSourceCommit": {"commitId": "headsha"},
		"lastMergeTargetCommit": {"commitId": "basesha"},
		"reviewers": [
			{"id": %q, "displayName": "Vera Viewer", "vote": 10},
			{"displayName": "Rex Reject", "uniqueName": "rex@example.com", "vote": -10},
			{"displayName": "Nora Novote", "vote": 0}
		]
	}`, status, closedDate, testViewerID)
}

const iterationsJSON = `{"count":2,"value":[{"id":1},{"id":2}]}`

const changesJSON = `{
	"changeEntries": [
		{"changeTrackingId": 7, "changeType": "edit", "item": {"path": "/src/main.go"}},
		{"changeTrackingId": 9, "changeType": "add", "item": {"path": "/docs/new.md"}}
	],
	"nextSkip": 0,
	"nextTop": 0
}`

// prHandler serves the PR detail endpoints.
func prHandler(t *testing.T, status string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case prByIDPath:
			writeJSON(w, http.StatusOK, prByIDJSON(status))
		case iterationsPath:
			writeJSON(w, http.StatusOK, iterationsJSON)
		case changesPath:
			writeJSON(w, http.StatusOK, changesJSON)
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL)
		}
	})
}

func TestGetPullRequest(t *testing.T) {
	d := newTestDriver(t, prHandler(t, "active"))
	repo := testRepo()
	pr, err := d.GetPullRequest(context.Background(), forge.Target{Repository: &repo, Number: 42})
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if pr.HeadSHA != "headsha" || pr.BaseSHA != "basesha" {
		t.Errorf("SHAs = %q/%q", pr.HeadSHA, pr.BaseSHA)
	}
	if pr.Title != "Add feature" || pr.Body != "The body" || pr.Author != "Alice" {
		t.Errorf("fields = %q %q %q", pr.Title, pr.Body, pr.Author)
	}
	if pr.HeadRefName != "feature" || pr.BaseRefName != "main" {
		t.Errorf("refs = %q/%q", pr.HeadRefName, pr.BaseRefName)
	}
	if pr.Closed || pr.MergedAt != nil || pr.State != "open" {
		t.Errorf("state = %q closed=%v merged=%v", pr.State, pr.Closed, pr.MergedAt)
	}
	want := `{"iteration_id":2,"change_tracking":{"/docs/new.md":9,"/src/main.go":7}}`
	if string(pr.ForgePayload) != want {
		t.Errorf("ForgePayload = %s, want %s", pr.ForgePayload, want)
	}
}

// TestGetPullRequestDiffsFromTheMergeBase: lastMergeTargetCommit is the
// target branch's tip. Diffing head against it shows every change made on
// the target since the branch point, reversed, and numbers old-side lines
// against the wrong file. The latest iteration's commonRefCommit is the
// merge base, which is what Azure DevOps's own PR view compares against.
func TestGetPullRequestDiffsFromTheMergeBase(t *testing.T) {
	d := newTestDriver(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case prByIDPath:
			writeJSON(w, http.StatusOK, prByIDJSON("active"))
		case iterationsPath:
			writeJSON(w, http.StatusOK, `{"count":2,"value":[
				{"id":2,"commonRefCommit":{"commitId":"mergebase2"}},
				{"id":1,"commonRefCommit":{"commitId":"mergebase1"}}]}`)
		case changesPath:
			writeJSON(w, http.StatusOK, changesJSON)
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL)
		}
	}))
	repo := testRepo()
	pr, err := d.GetPullRequest(context.Background(), forge.Target{Repository: &repo, Number: 42})
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if pr.BaseSHA != "mergebase2" {
		t.Errorf("BaseSHA = %q, want the latest iteration's merge base", pr.BaseSHA)
	}
}

func TestGetPullRequestCompleted(t *testing.T) {
	d := newTestDriver(t, prHandler(t, "completed"))
	repo := testRepo()
	pr, err := d.GetPullRequest(context.Background(), forge.Target{Repository: &repo, Number: 42})
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if !pr.Closed || pr.State != "merged" {
		t.Errorf("completed PR: closed=%v state=%q", pr.Closed, pr.State)
	}
	if pr.MergedAt == nil {
		t.Fatal("MergedAt must approximate from closedDate for completed PRs")
	}
	if !pr.IsReadOnly() {
		t.Error("completed PR must be read-only")
	}
}

func TestGetPullRequestAbandoned(t *testing.T) {
	d := newTestDriver(t, prHandler(t, "abandoned"))
	repo := testRepo()
	pr, err := d.GetPullRequest(context.Background(), forge.Target{Repository: &repo, Number: 42})
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if !pr.Closed || pr.State != "closed" {
		t.Errorf("abandoned PR: closed=%v state=%q", pr.Closed, pr.State)
	}
	if pr.MergedAt != nil {
		t.Error("abandoned PRs are not merged")
	}
}

func TestGetPullRequestValidatesTarget(t *testing.T) {
	d := newTestDriver(t, forbidNetwork(t))
	repo := testRepo()

	_, err := d.GetPullRequest(context.Background(), forge.Target{Number: 42, Original: "42"})
	wantForgeErr(t, err, forge.ErrorValidation)

	_, err = d.GetPullRequest(context.Background(), forge.Target{Repository: &repo, Original: "x"})
	wantForgeErr(t, err, forge.ErrorValidation)
}

func TestGetPullRequestMissingSHAs(t *testing.T) {
	d := newTestDriver(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, `{"pullRequestId": 42, "status": "active"}`)
	}))
	repo := testRepo()
	_, err := d.GetPullRequest(context.Background(), forge.Target{Repository: &repo, Number: 42})
	wantForgeErr(t, err, forge.ErrorValidation)
}

func TestGetPullRequestNoIterations(t *testing.T) {
	d := newTestDriver(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case prByIDPath:
			writeJSON(w, http.StatusOK, prByIDJSON("active"))
		case iterationsPath:
			writeJSON(w, http.StatusOK, `{"count":0,"value":[]}`)
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL)
		}
	}))
	repo := testRepo()
	pr, err := d.GetPullRequest(context.Background(), forge.Target{Repository: &repo, Number: 42})
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if len(pr.ForgePayload) != 0 {
		t.Errorf("ForgePayload = %s, want empty without iterations", pr.ForgePayload)
	}
}

func TestListCommitsPagesAndReverses(t *testing.T) {
	var tokens []string
	d := newTestDriver(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != commitsPath {
			t.Errorf("unexpected call %s %s", r.Method, r.URL)
			return
		}
		tokens = append(tokens, r.URL.Query().Get("continuationToken"))
		if r.URL.Query().Get("continuationToken") == "" {
			w.Header().Set("X-MS-ContinuationToken", "tok-2")
			writeJSON(w, http.StatusOK, `{"count":2,"value":[
				{"commitId": "cccccccc1234", "comment": "third\n\ndetails", "author": {"name": "Carol", "date": "2026-07-03T00:00:00Z"}},
				{"commitId": "bbbbbbbb1234", "comment": "second", "author": {"email": "bob@example.com"}}
			]}`)
			return
		}
		writeJSON(w, http.StatusOK, `{"count":1,"value":[
			{"commitId": "aaaaaaaa1234", "comment": "first", "author": {}}
		]}`)
	}))

	commits, err := d.ListCommits(context.Background(), testPR())
	if err != nil {
		t.Fatalf("ListCommits: %v", err)
	}
	if len(commits) != 3 {
		t.Fatalf("commits = %d, want 3", len(commits))
	}
	// Oldest-first: the API returned newest-first across both pages.
	if commits[0].OID != "aaaaaaaa1234" || commits[2].OID != "cccccccc1234" {
		t.Errorf("order = %q,%q,%q", commits[0].OID, commits[1].OID, commits[2].OID)
	}
	if commits[0].ShortOID != "aaaaaaa" {
		t.Errorf("ShortOID = %q", commits[0].ShortOID)
	}
	if commits[0].Author != "unknown" {
		t.Errorf("author fallback = %q", commits[0].Author)
	}
	if commits[1].Author != "bob@example.com" {
		t.Errorf("email author = %q", commits[1].Author)
	}
	if commits[2].Author != "Carol" || commits[2].Summary != "third" {
		t.Errorf("newest = %+v", commits[2])
	}
	if commits[2].Timestamp == nil {
		t.Error("timestamp must map when present")
	}
	if len(tokens) != 2 || tokens[1] != "tok-2" {
		t.Errorf("continuation tokens = %v", tokens)
	}
}

func TestGetCommitRangeDiffUnsupported(t *testing.T) {
	d := newTestDriver(t, forbidNetwork(t))
	_, err := d.GetCommitRangeDiff(context.Background(), testPR(), "a", "b")
	fe := mustForgeErr(t, err)
	if fe.Kind != forge.ErrorUnsupported {
		t.Fatalf("kind = %v, want unsupported", fe.Kind)
	}
	if !strings.Contains(fe.Hint, "commit-range") {
		t.Errorf("hint = %q", fe.Hint)
	}
}
