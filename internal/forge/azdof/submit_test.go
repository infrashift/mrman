package azdof

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/submit"
)

// threadCreatedJSON is the minimal CreateThread success response.
const threadCreatedJSON = `{"id": 900}`

// postedThread captures one recorded CreateThread body.
type postedThread struct {
	path string
	body map[string]any
}

// submitHandler records thread posts and reviewer votes.
type submitHandler struct {
	t           *testing.T
	threads     []postedThread
	votes       []map[string]any
	votePaths   []string
	failThread  int // 1-based index of the thread post to fail, 0 = never
	failVote    bool
	threadCount int
}

func (h *submitHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.EqualFold(r.URL.Path, "/_apis/connectionData"):
		writeJSON(w, http.StatusOK, connectionDataJSON())
	case r.Method == http.MethodPost && r.URL.Path == threadsPath:
		h.threadCount++
		if h.failThread == h.threadCount {
			writeJSON(w, http.StatusInternalServerError, `{"message": "boom"}`)
			return
		}
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			h.t.Errorf("thread body is not JSON: %v", err)
		}
		h.threads = append(h.threads, postedThread{path: r.URL.Path, body: body})
		writeJSON(w, http.StatusOK, threadCreatedJSON)
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, threadsPathPrefix+"/reviewers/"):
		if h.failVote {
			writeJSON(w, http.StatusForbidden, `{"message": "cannot vote"}`)
			return
		}
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			h.t.Errorf("vote body is not JSON: %v", err)
		}
		h.votes = append(h.votes, body)
		h.votePaths = append(h.votePaths, r.URL.Path)
		writeJSON(w, http.StatusOK, `{"vote": 10}`)
	default:
		h.t.Errorf("unexpected call %s %s", r.Method, r.URL)
	}
}

const threadsPathPrefix = "/proj/_apis/git/repositories/repo/pullrequests/42"

// mustJSON parses a JSON literal into the comparison shape.
func mustJSON(t *testing.T, s string) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("bad fixture JSON: %v", err)
	}
	return v
}

func TestCreateReviewCommentSequence(t *testing.T) {
	h := &submitHandler{t: t}
	d := newTestDriver(t, h)
	old := "old/path.go"
	req := forge.CreateReviewRequest{
		Event:    forge.SubmitComment,
		CommitID: "headsha",
		Body:     "Overall verdict",
		Comments: []submit.InlineComment{
			{
				Path: "src/main.go", Line: 4, Side: submit.SideNew,
				Body: "single line", CommentID: "c1",
			},
			{
				Path: "src/main.go", Line: 9, Side: submit.SideNew,
				StartLine: new(uint32(6)), StartSide: new(submit.SideNew),
				Body: "range comment", CommentID: "c2",
			},
			{
				Path: "renamed.go", OldPath: &old, Line: 2, Side: submit.SideOld,
				Body: "old side", CommentID: "c3",
			},
		},
	}
	pr := testPR()
	pr.ForgePayload = []byte(`{"iteration_id":2,"change_tracking":{"/src/main.go":7,"/renamed.go":9}}`)
	result, err := d.CreateReview(context.Background(), pr, req)
	if err != nil {
		t.Fatalf("CreateReview: %v", err)
	}
	if result.Partial != nil {
		t.Fatalf("Partial = %+v, want nil", result.Partial)
	}
	if result.ReviewID != "" {
		t.Errorf("ReviewID = %q, want empty (ADO has no review object)", result.ReviewID)
	}
	if result.State != "commented" || result.URL != testPR().URL {
		t.Errorf("result = %+v", result)
	}
	if len(h.threads) != 4 {
		t.Fatalf("thread posts = %d, want 4 (body + 3 comments)", len(h.threads))
	}
	if len(h.votes) != 0 {
		t.Errorf("comment submits must not vote")
	}

	// Golden 1: the body thread is context-less and active.
	want := mustJSON(t, `{
		"status": "active",
		"comments": [{"parentCommentId": 0, "content": "Overall verdict", "commentType": "text"}]
	}`)
	if !reflect.DeepEqual(h.threads[0].body, want) {
		t.Errorf("body thread = %v, want %v", h.threads[0].body, want)
	}

	// Golden 2: single-line new-side comment with iteration context and
	// change-tracking id from the ForgePayload, offset 1 on both ends.
	want = mustJSON(t, `{
		"status": "active",
		"comments": [{"parentCommentId": 0, "content": "single line", "commentType": "text"}],
		"threadContext": {
			"filePath": "/src/main.go",
			"rightFileStart": {"line": 4, "offset": 1},
			"rightFileEnd": {"line": 4, "offset": 1}
		},
		"pullRequestThreadContext": {
			"changeTrackingId": 7,
			"iterationContext": {"firstComparingIteration": 1, "secondComparingIteration": 2}
		}
	}`)
	if !reflect.DeepEqual(h.threads[1].body, want) {
		t.Errorf("inline thread = %v, want %v", h.threads[1].body, want)
	}

	// Golden 3: multi-line range spans start..end lines; no tracking id is
	// sent for a path missing from the change-tracking map.
	want = mustJSON(t, `{
		"status": "active",
		"comments": [{"parentCommentId": 0, "content": "range comment", "commentType": "text"}],
		"threadContext": {
			"filePath": "/src/main.go",
			"rightFileStart": {"line": 6, "offset": 1},
			"rightFileEnd": {"line": 9, "offset": 1}
		},
		"pullRequestThreadContext": {
			"changeTrackingId": 7,
			"iterationContext": {"firstComparingIteration": 1, "secondComparingIteration": 2}
		}
	}`)
	if !reflect.DeepEqual(h.threads[2].body, want) {
		t.Errorf("range thread = %v, want %v", h.threads[2].body, want)
	}

	// Golden 4: an old-side comment on a renamed file anchors left
	// positions under the file's current path, which is the form Azure
	// DevOps stores and the key its change-tracking ids are filed under.
	// The base-side path found no tracking id (verified live, PR 2).
	want = mustJSON(t, `{
		"status": "active",
		"comments": [{"parentCommentId": 0, "content": "old side", "commentType": "text"}],
		"threadContext": {
			"filePath": "/renamed.go",
			"leftFileStart": {"line": 2, "offset": 1},
			"leftFileEnd": {"line": 2, "offset": 1}
		},
		"pullRequestThreadContext": {
			"iterationContext": {"firstComparingIteration": 1, "secondComparingIteration": 2},
			"changeTrackingId": 9
		}
	}`)
	if !reflect.DeepEqual(h.threads[3].body, want) {
		t.Errorf("old-side thread = %v, want %v", h.threads[3].body, want)
	}
}

func TestCreateReviewWithoutPayloadOmitsIterationContext(t *testing.T) {
	h := &submitHandler{t: t}
	d := newTestDriver(t, h)
	pr := testPR()
	pr.ForgePayload = nil
	req := forge.CreateReviewRequest{
		Event:    forge.SubmitComment,
		Comments: []submit.InlineComment{{Path: "a.go", Line: 1, Side: submit.SideNew, Body: "hi", CommentID: "c1"}},
	}
	if _, err := d.CreateReview(context.Background(), pr, req); err != nil {
		t.Fatalf("CreateReview: %v", err)
	}
	if _, has := h.threads[0].body["pullRequestThreadContext"]; has {
		t.Errorf("no payload → no pullRequestThreadContext: %v", h.threads[0].body)
	}
}

func TestCreateReviewApprove(t *testing.T) {
	h := &submitHandler{t: t}
	d := newTestDriver(t, h)
	result, err := d.CreateReview(context.Background(), testPR(),
		forge.CreateReviewRequest{Event: forge.SubmitApprove})
	if err != nil {
		t.Fatalf("CreateReview: %v", err)
	}
	if result.State != "approved" {
		t.Errorf("state = %q", result.State)
	}
	if len(h.votes) != 1 {
		t.Fatalf("votes = %d", len(h.votes))
	}
	want := mustJSON(t, `{"id": "`+testViewerID+`", "vote": 10}`)
	if !reflect.DeepEqual(h.votes[0], want) {
		t.Errorf("vote body = %v, want %v", h.votes[0], want)
	}
	if !strings.HasSuffix(h.votePaths[0], "/reviewers/"+testViewerID) {
		t.Errorf("vote path = %q must target the viewer", h.votePaths[0])
	}
}

func TestCreateReviewRequestChanges(t *testing.T) {
	h := &submitHandler{t: t}
	d := newTestDriver(t, h)
	result, err := d.CreateReview(context.Background(), testPR(),
		forge.CreateReviewRequest{Event: forge.SubmitRequestChanges, Body: "needs work"})
	if err != nil {
		t.Fatalf("CreateReview: %v", err)
	}
	if result.State != "changes_requested" {
		t.Errorf("state = %q", result.State)
	}
	want := mustJSON(t, `{"id": "`+testViewerID+`", "vote": -10}`)
	if !reflect.DeepEqual(h.votes[0], want) {
		t.Errorf("vote body = %v, want %v", h.votes[0], want)
	}
	if len(h.threads) != 1 {
		t.Errorf("body thread posts = %d, want 1", len(h.threads))
	}
}

func TestCreateReviewDraftUnsupported(t *testing.T) {
	d := newTestDriver(t, forbidNetwork(t))
	_, err := d.CreateReview(context.Background(), testPR(),
		forge.CreateReviewRequest{Event: forge.SubmitDraft, Body: "draft"})
	fe := mustForgeErr(t, err)
	if fe.Kind != forge.ErrorUnsupported {
		t.Fatalf("kind = %v", fe.Kind)
	}
	if !strings.Contains(fe.Hint, "draft") {
		t.Errorf("hint = %q", fe.Hint)
	}
}

func TestCreateReviewPartialFailureMidComments(t *testing.T) {
	h := &submitHandler{t: t, failThread: 3} // body, c1 succeed; c2 fails
	d := newTestDriver(t, h)
	req := forge.CreateReviewRequest{
		Event: forge.SubmitComment,
		Body:  "body",
		Comments: []submit.InlineComment{
			{Path: "a.go", Line: 1, Side: submit.SideNew, Body: "one", CommentID: "c1"},
			{Path: "b.go", Line: 2, Side: submit.SideNew, Body: "two", CommentID: "c2"},
			{Path: "c.go", Line: 3, Side: submit.SideNew, Body: "three", CommentID: "c3"},
		},
	}
	result, err := d.CreateReview(context.Background(), testPR(), req)
	if err != nil {
		t.Fatalf("partial failures must not error: %v", err)
	}
	if result.Partial == nil {
		t.Fatal("Partial must be set")
	}
	if result.Partial.FailedAt != 1 {
		t.Errorf("FailedAt = %d, want 1", result.Partial.FailedAt)
	}
	if !reflect.DeepEqual(result.Partial.SucceededCommentIDs, []string{"c1"}) {
		t.Errorf("SucceededCommentIDs = %v", result.Partial.SucceededCommentIDs)
	}
	wantForgeErr(t, result.Partial.Cause, forge.ErrorServer)
}

func TestCreateReviewVoteFailureAfterComments(t *testing.T) {
	h := &submitHandler{t: t, failVote: true}
	d := newTestDriver(t, h)
	req := forge.CreateReviewRequest{
		Event: forge.SubmitApprove,
		Comments: []submit.InlineComment{
			{Path: "a.go", Line: 1, Side: submit.SideNew, Body: "one", CommentID: "c1"},
		},
	}
	result, err := d.CreateReview(context.Background(), testPR(), req)
	if err != nil {
		t.Fatalf("vote failure after posted comments must not error: %v", err)
	}
	if result.Partial == nil || result.Partial.FailedAt != 1 {
		t.Fatalf("Partial = %+v", result.Partial)
	}
	if !reflect.DeepEqual(result.Partial.SucceededCommentIDs, []string{"c1"}) {
		t.Errorf("SucceededCommentIDs = %v", result.Partial.SucceededCommentIDs)
	}
	wantForgeErr(t, result.Partial.Cause, forge.ErrorForbidden)
}

func TestCreateReviewPureVoteFailure(t *testing.T) {
	h := &submitHandler{t: t, failVote: true}
	d := newTestDriver(t, h)
	_, err := d.CreateReview(context.Background(), testPR(),
		forge.CreateReviewRequest{Event: forge.SubmitApprove})
	wantForgeErr(t, err, forge.ErrorForbidden)
}

func TestCreateReviewBodyFailure(t *testing.T) {
	h := &submitHandler{t: t, failThread: 1}
	d := newTestDriver(t, h)
	_, err := d.CreateReview(context.Background(), testPR(),
		forge.CreateReviewRequest{Event: forge.SubmitComment, Body: "body"})
	wantForgeErr(t, err, forge.ErrorServer)
}
