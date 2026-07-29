package forgejof

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/submit"
)

// reviewCapture records the JSON body posted to the create-review endpoint
// and replies with a canned successful review.
type reviewCapture struct {
	body     map[string]interface{}
	response string
}

func (c *reviewCapture) handler(t *testing.T) http.Handler {
	if c.response == "" {
		c.response = `{"id":987,"state":"COMMENT",
			"html_url":"https://codeberg.org/octo/hello/pulls/42#issuecomment-987"}`
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/repos/octo/hello/pulls/42/reviews", func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if err := json.Unmarshal(raw, &c.body); err != nil {
			t.Errorf("unmarshal body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, c.response)
	})
	return mux
}

func u32Ptr(v uint32) *uint32 { return &v }

func TestCreateReviewGoldenCommentPayload(t *testing.T) {
	capture := &reviewCapture{}
	d := newTestDriver(t, capture.handler(t))

	result, err := d.CreateReview(context.Background(), testPR(), forge.CreateReviewRequest{
		Event:    forge.SubmitComment,
		CommitID: "abc1234",
		Body:     "body text",
		Comments: []submit.InlineComment{{
			Path:      "src/lib.rs",
			Line:      42,
			Side:      submit.SideNew,
			Body:      "[ISSUE] boom",
			CommentID: "local-1",
		}},
	})
	if err != nil {
		t.Fatalf("CreateReview: %v", err)
	}

	want := map[string]interface{}{
		"event":     "COMMENT",
		"commit_id": "abc1234",
		"body":      "body text",
		"comments": []interface{}{map[string]interface{}{
			"path":         "src/lib.rs",
			"body":         "[ISSUE] boom",
			"old_position": float64(0),
			"new_position": float64(42),
		}},
	}
	if !reflect.DeepEqual(capture.body, want) {
		t.Errorf("payload = %#v\nwant %#v", capture.body, want)
	}

	if result.ReviewID != "987" || result.State != "COMMENT" ||
		result.URL != "https://codeberg.org/octo/hello/pulls/42#issuecomment-987" {
		t.Errorf("result = %+v", result)
	}
	if result.Partial != nil {
		t.Error("atomic submit must not report partial failure")
	}
}

func TestCreateReviewDraftSendsPending(t *testing.T) {
	capture := &reviewCapture{response: `{"id":5,"state":"PENDING"}`}
	d := newTestDriver(t, capture.handler(t))

	result, err := d.CreateReview(context.Background(), testPR(), forge.CreateReviewRequest{
		Event:    forge.SubmitDraft,
		CommitID: "sha",
		Body:     "draft body",
	})
	if err != nil {
		t.Fatalf("CreateReview: %v", err)
	}
	if capture.body["event"] != "PENDING" {
		t.Errorf("draft submit must send PENDING: %#v", capture.body)
	}
	if capture.body["body"] != "draft body" || capture.body["commit_id"] != "sha" {
		t.Errorf("payload = %#v", capture.body)
	}
	// No html_url in the response: fall back to the PR URL.
	if result.URL != testPR().URL {
		t.Errorf("URL = %q, want PR URL fallback", result.URL)
	}
	if result.ReviewID != "5" || result.State != "PENDING" {
		t.Errorf("result = %+v", result)
	}
}

func TestCreateReviewEventMapping(t *testing.T) {
	cases := map[forge.SubmitEvent]string{
		forge.SubmitApprove:        "APPROVED",
		forge.SubmitRequestChanges: "REQUEST_CHANGES",
	}
	for event, want := range cases {
		capture := &reviewCapture{}
		d := newTestDriver(t, capture.handler(t))
		if _, err := d.CreateReview(context.Background(), testPR(), forge.CreateReviewRequest{
			Event: event, CommitID: "sha", Body: "summary",
		}); err != nil {
			t.Fatalf("CreateReview(%v): %v", event, err)
		}
		if capture.body["event"] != want {
			t.Errorf("event %v → %v, want %s", event, capture.body["event"], want)
		}
	}
}

func TestCreateReviewOldSidePlacement(t *testing.T) {
	capture := &reviewCapture{}
	d := newTestDriver(t, capture.handler(t))

	_, err := d.CreateReview(context.Background(), testPR(), forge.CreateReviewRequest{
		Event:    forge.SubmitComment,
		CommitID: "sha",
		Body:     "b",
		Comments: []submit.InlineComment{{
			Path:            "a.rs",
			Line:            7,
			Side:            submit.SideOld,
			CounterpartLine: u32Ptr(9), // no Forgejo encoding; must not be sent
			Body:            "old side",
			CommentID:       "local-2",
		}},
	})
	if err != nil {
		t.Fatalf("CreateReview: %v", err)
	}
	comments, ok := capture.body["comments"].([]interface{})
	if !ok || len(comments) != 1 {
		t.Fatalf("comments = %#v", capture.body["comments"])
	}
	comment, ok := comments[0].(map[string]interface{})
	if !ok {
		t.Fatalf("comment = %#v", comments[0])
	}
	if comment["old_position"] != float64(7) || comment["new_position"] != float64(0) {
		t.Errorf("old-side comment = %#v", comment)
	}
	if len(comment) != 4 { // path, body, old_position, new_position only
		t.Errorf("unexpected keys in comment: %#v", comment)
	}
}

func TestCreateReviewRejectsMultilineComments(t *testing.T) {
	d := newTestDriver(t, forbidNetwork(t))
	startSide := submit.SideNew
	_, err := d.CreateReview(context.Background(), testPR(), forge.CreateReviewRequest{
		Event:    forge.SubmitComment,
		CommitID: "sha",
		Body:     "b",
		Comments: []submit.InlineComment{{
			Path:      "a.rs",
			Line:      20,
			Side:      submit.SideNew,
			StartLine: u32Ptr(15),
			StartSide: &startSide,
			Body:      "ranged",
			CommentID: "local-3",
		}},
	})
	wantForgeErr(t, err, forge.ErrorValidation)
}

func TestCreateReviewRejectsUnknownEvent(t *testing.T) {
	d := newTestDriver(t, forbidNetwork(t))
	_, err := d.CreateReview(context.Background(), testPR(), forge.CreateReviewRequest{
		Event: forge.SubmitEvent(99),
	})
	wantForgeErr(t, err, forge.ErrorValidation)
}

func TestCreateReviewRejectsEmptyPayloadBeforeNetwork(t *testing.T) {
	// A COMMENT review with no body and no comments fails the SDK's own
	// validation; the driver must classify it instead of calling out.
	d := newTestDriver(t, forbidNetwork(t))
	_, err := d.CreateReview(context.Background(), testPR(), forge.CreateReviewRequest{
		Event: forge.SubmitComment, CommitID: "sha",
	})
	wantForgeErr(t, err, forge.ErrorValidation)
}

func TestCreateReviewErrorTable(t *testing.T) {
	cases := []struct {
		status int
		kind   forge.ErrorKind
		hint   string
	}{
		{http.StatusUnauthorized, forge.ErrorAuth, hintAuth},
		{http.StatusForbidden, forge.ErrorForbidden, hintReviewForbidden},
		{http.StatusNotFound, forge.ErrorNotFound, hintNotFound},
		{http.StatusUnprocessableEntity, forge.ErrorValidation, hintReviewRejected},
		{http.StatusInternalServerError, forge.ErrorServer, ""},
	}
	for _, tc := range cases {
		mux := http.NewServeMux()
		mux.HandleFunc("POST /api/v1/repos/octo/hello/pulls/42/reviews", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"message":"rejected"}`, tc.status)
		})
		d := newTestDriver(t, mux)
		_, err := d.CreateReview(context.Background(), testPR(), forge.CreateReviewRequest{
			Event: forge.SubmitComment, CommitID: "sha", Body: "b",
		})
		fe := mustForgeErr(t, err)
		if fe.Kind != tc.kind || fe.Status != tc.status || fe.Hint != tc.hint {
			t.Errorf("status %d: kind=%v status=%d hint=%q", tc.status, fe.Kind, fe.Status, fe.Hint)
		}
	}
}
