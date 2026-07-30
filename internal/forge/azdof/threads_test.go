package azdof

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"

	"github.com/infrashift/mrman/internal/forge"
)

// threadsJSON exercises every mapping rule at once: statuses, sides,
// multi-line spans, replies, system comments, deleted threads, and
// context-less threads.
const threadsJSON = `{"count": 8, "value": [
	{
		"id": 1,
		"status": "active",
		"threadContext": {
			"filePath": "/src/main.go",
			"rightFileStart": {"line": 4, "offset": 1},
			"rightFileEnd": {"line": 4, "offset": 10}
		},
		"pullRequestThreadContext": {"changeTrackingId": 7},
		"comments": [
			{"id": 100, "content": "root comment", "commentType": "text",
			 "author": {"displayName": "Alice"}, "publishedDate": "2026-07-02T10:00:00Z"},
			{"id": 101, "parentCommentId": 100, "content": "a reply", "commentType": "text",
			 "author": {"uniqueName": "bob@example.com"}}
		]
	},
	{
		"id": 2,
		"status": "fixed",
		"threadContext": {
			"filePath": "/src/old.go",
			"leftFileStart": {"line": 2, "offset": 1},
			"leftFileEnd": {"line": 5, "offset": 3}
		},
		"pullRequestThreadContext": {"changeTrackingId": 8},
		"comments": [{"id": 200, "content": "old side range", "commentType": "text"}]
	},
	{
		"id": 3,
		"status": "wontFix",
		"threadContext": {"filePath": "/x.go", "rightFileStart": {"line": 1, "offset": 1}},
		"comments": [{"id": 300, "content": "untracked thread", "commentType": "text"}]
	},
	{
		"id": 4,
		"status": "byDesign",
		"threadContext": {"filePath": "/y.go", "rightFileStart": {"line": 9, "offset": 1}, "rightFileEnd": {"line": 9, "offset": 1}},
		"pullRequestThreadContext": {},
		"comments": [{"id": 400, "content": "by design", "commentType": "text"}]
	},
	{
		"id": 5,
		"status": "closed",
		"threadContext": {"filePath": "/z.go", "rightFileEnd": {"line": 3, "offset": 1}},
		"pullRequestThreadContext": {},
		"comments": [{"id": 500, "content": "closed one", "commentType": "text"}]
	},
	{
		"id": 6,
		"isDeleted": true,
		"status": "active",
		"threadContext": {"filePath": "/gone.go", "rightFileStart": {"line": 1, "offset": 1}},
		"comments": [{"id": 600, "content": "deleted thread", "commentType": "text"}]
	},
	{
		"id": 7,
		"status": "active",
		"comments": [
			{"id": 700, "content": "overall summary body", "commentType": "text",
			 "author": {"displayName": "Sam"}, "publishedDate": "2026-07-03T09:00:00Z"}
		]
	},
	{
		"id": 8,
		"status": "active",
		"comments": [{"id": 800, "content": "PR was updated", "commentType": "system"}]
	}
]}`

func threadsHandler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != threadsPath || r.Method != http.MethodGet {
			t.Errorf("unexpected call %s %s", r.Method, r.URL)
			return
		}
		writeJSON(w, http.StatusOK, threadsJSON)
	})
}

func TestListReviewThreads(t *testing.T) {
	d := newTestDriver(t, threadsHandler(t))
	threads, err := d.ListReviewThreads(context.Background(), testPR())
	if err != nil {
		t.Fatalf("ListReviewThreads: %v", err)
	}
	if len(threads) != 5 {
		t.Fatalf("threads = %d, want 5 (file-anchored, live, human)", len(threads))
	}

	first := threads[0]
	if first.ID != "1" || first.Path != "src/main.go" {
		t.Errorf("first = %+v", first)
	}
	if first.Side != forge.SideNew || first.Line == nil || *first.Line != 4 || first.StartLine != nil {
		t.Errorf("first anchor = side=%v line=%v start=%v", first.Side, first.Line, first.StartLine)
	}
	if first.IsResolved || first.IsOutdated || !first.IsActive() {
		t.Errorf("first state = resolved=%v outdated=%v", first.IsResolved, first.IsOutdated)
	}
	if first.Disposition != "" {
		t.Errorf("active threads need no disposition badge, got %q", first.Disposition)
	}
	if len(first.Comments) != 2 {
		t.Fatalf("first comments = %d", len(first.Comments))
	}
	root := first.Root()
	if root.ID != "100" || root.Author != "Alice" || root.Body != "root comment" ||
		root.CreatedAt == nil || root.InReplyTo != "" {
		t.Errorf("root = %+v", root)
	}
	reply := first.Replies()[0]
	if reply.InReplyTo != "100" || reply.Author != "bob@example.com" {
		t.Errorf("reply = %+v", reply)
	}

	second := threads[1]
	if second.Side != forge.SideOld || second.Line == nil || *second.Line != 5 {
		t.Errorf("old-side anchor = %+v", second)
	}
	if second.StartLine == nil || *second.StartLine != 2 {
		t.Errorf("old-side range start = %v", second.StartLine)
	}
	if !second.IsResolved {
		t.Error("fixed threads are resolved")
	}
	if second.Disposition != "resolved" {
		t.Errorf("fixed disposition = %q, want \"resolved\"", second.Disposition)
	}

	third := threads[2]
	if third.Disposition != "won't fix" {
		t.Errorf("wontFix disposition = %q", third.Disposition)
	}
	if !third.IsResolved || !third.IsOutdated {
		t.Errorf("wontFix without tracking: resolved=%v outdated=%v (outdated approximation)",
			third.IsResolved, third.IsOutdated)
	}

	fourth := threads[3]
	if fourth.Disposition != "by design" {
		t.Errorf("byDesign disposition = %q", fourth.Disposition)
	}
	if !fourth.IsResolved || fourth.IsOutdated {
		t.Errorf("byDesign with tracking context: resolved=%v outdated=%v",
			fourth.IsResolved, fourth.IsOutdated)
	}

	fifth := threads[4]
	if fifth.Line == nil || *fifth.Line != 3 || fifth.StartLine != nil {
		t.Errorf("end-only position must anchor at end line: %+v", fifth)
	}
	if !fifth.IsResolved {
		t.Error("closed threads are resolved")
	}
	if fifth.Disposition != "closed" {
		t.Errorf("closed disposition = %q", fifth.Disposition)
	}
}

func TestListReviewSummariesApproximation(t *testing.T) {
	d := newTestDriver(t, threadsHandler(t))
	summaries, err := d.ListReviewSummaries(context.Background(), testPR())
	if err != nil {
		t.Fatalf("ListReviewSummaries: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("summaries = %d, want 1 (context-less human thread)", len(summaries))
	}
	s := summaries[0]
	if s.ID != "7" || s.Author != "Sam" || s.Body != "overall summary body" {
		t.Errorf("summary = %+v", s)
	}
	if s.State != forge.ReviewCommented {
		t.Errorf("state = %q, want commented (votes are not review states here)", s.State)
	}
	if s.CreatedAt == nil {
		t.Error("CreatedAt must map from publishedDate")
	}
}

func TestListReviewThreadsError(t *testing.T) {
	d := newTestDriver(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusForbidden, `{"message": "TF401027: no permission"}`)
	}))
	_, err := d.ListReviewThreads(context.Background(), testPR())
	fe := mustForgeErr(t, err)
	if fe.Kind != forge.ErrorForbidden {
		t.Fatalf("kind = %v", fe.Kind)
	}
	if !strings.Contains(fe.Hint, "Code (Read & Write)") {
		t.Errorf("hint = %q", fe.Hint)
	}
}

func TestReviewMetadataVotes(t *testing.T) {
	d := newTestDriver(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.EqualFold(r.URL.Path, "/_apis/connectionData"):
			writeJSON(w, http.StatusOK, connectionDataJSON())
		case r.URL.Path == prByIDPath:
			writeJSON(w, http.StatusOK, prByIDJSON("active"))
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL)
		}
	}))
	metadata, err := d.ReviewMetadata(context.Background(), testPR())
	if err != nil {
		t.Fatalf("ReviewMetadata: %v", err)
	}
	if metadata.ViewerLogin != "Vera Viewer" {
		t.Errorf("ViewerLogin = %q", metadata.ViewerLogin)
	}
	// Only reviewers with a non-zero vote count as review records; ADO
	// votes carry neither commit OIDs nor timestamps.
	if len(metadata.Reviews) != 2 {
		t.Fatalf("reviews = %d, want 2 (zero votes skipped)", len(metadata.Reviews))
	}
	if metadata.Reviews[0].Author != "Vera Viewer" || metadata.Reviews[1].Author != "Rex Reject" {
		t.Errorf("authors = %+v", metadata.Reviews)
	}
	for _, record := range metadata.Reviews {
		if record.CommitOID != "" || record.SubmittedAt != nil {
			t.Errorf("record %+v must have no commit OID or timestamp", record)
		}
	}
}

func TestReviewMetadataViewerError(t *testing.T) {
	d := newTestDriver(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusUnauthorized, `{"message": "bad PAT"}`)
	}))
	_, err := d.ReviewMetadata(context.Background(), testPR())
	wantForgeErr(t, err, forge.ErrorAuth)
}

// TestIsResolvedStatusCoversEveryDisposition pins the mapping from Azure
// DevOps' seven thread dispositions onto mrman's single resolved flag. The
// web UI lets a reviewer pick any of them, and four are terminal ("this
// thread is done") while Active and Pending are still open. Pending is the
// easy one to get wrong: it reads like a resolution but means "waiting on the
// author", so it must stay unresolved and keep showing by default.
func TestIsResolvedStatusCoversEveryDisposition(t *testing.T) {
	cases := []struct {
		status       git.CommentThreadStatus
		wantResolved bool
	}{
		{git.CommentThreadStatusValues.Active, false},
		{git.CommentThreadStatusValues.Pending, false},
		{git.CommentThreadStatusValues.Unknown, false},
		{git.CommentThreadStatusValues.Fixed, true},
		{git.CommentThreadStatusValues.WontFix, true},
		{git.CommentThreadStatusValues.Closed, true},
		{git.CommentThreadStatusValues.ByDesign, true},
	}
	if len(cases) != 7 {
		t.Fatalf("the enum has 7 values; the table covers %d", len(cases))
	}
	for _, tc := range cases {
		t.Run(string(tc.status), func(t *testing.T) {
			status := tc.status
			if got := isResolvedStatus(&status); got != tc.wantResolved {
				t.Errorf("isResolvedStatus(%q) = %v, want %v", tc.status, got, tc.wantResolved)
			}
		})
	}
	// A nil status is not hypothetical: setting a thread to "unknown" makes
	// Azure DevOps omit the status field from the response entirely rather
	// than store the string, so this is the shape that actually arrives.
	// Absent state must never hide a comment from the reviewer.
	if isResolvedStatus(nil) {
		t.Error("isResolvedStatus(nil) = true, want false")
	}
}

// TestDispositionLabelCoversEveryDisposition pins the badge wording for all
// seven Azure DevOps thread states. Fixed deliberately reads as "resolved" so
// the common case matches every other forge; the other three terminal states
// get their own wording because "won't fix" is not the same statement as
// "fixed". Active and an absent status stay unlabelled.
func TestDispositionLabelCoversEveryDisposition(t *testing.T) {
	cases := []struct {
		status git.CommentThreadStatus
		want   string
	}{
		{git.CommentThreadStatusValues.Active, ""},
		{git.CommentThreadStatusValues.Unknown, ""},
		{git.CommentThreadStatusValues.Pending, "pending"},
		{git.CommentThreadStatusValues.Fixed, "resolved"},
		{git.CommentThreadStatusValues.WontFix, "won't fix"},
		{git.CommentThreadStatusValues.Closed, "closed"},
		{git.CommentThreadStatusValues.ByDesign, "by design"},
	}
	if len(cases) != 7 {
		t.Fatalf("the enum has 7 values; the table covers %d", len(cases))
	}
	for _, tc := range cases {
		t.Run(string(tc.status), func(t *testing.T) {
			status := tc.status
			if got := dispositionLabel(&status); got != tc.want {
				t.Errorf("dispositionLabel(%q) = %q, want %q", tc.status, got, tc.want)
			}
		})
	}
	// Absent status is what Azure DevOps actually returns for "unknown".
	if got := dispositionLabel(nil); got != "" {
		t.Errorf("dispositionLabel(nil) = %q, want empty", got)
	}
	// Every resolved status must carry a label, or the badge would silently
	// lose the wording it had before dispositions existed.
	for _, tc := range cases {
		status := tc.status
		if isResolvedStatus(&status) && dispositionLabel(&status) == "" {
			t.Errorf("%q is resolved but has no disposition label", tc.status)
		}
	}
}
