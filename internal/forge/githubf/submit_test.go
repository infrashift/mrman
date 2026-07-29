package githubf

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
	body map[string]interface{}
}

func (c *reviewCapture) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /repos/octo/hello/pulls/42/reviews", func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if err := json.Unmarshal(raw, &c.body); err != nil {
			t.Errorf("unmarshal body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":987,"state":"PENDING",
			"html_url":"https://github.com/octo/hello/pull/42#pullrequestreview-987"}`)
	})
	return mux
}

func u32Ptr(v uint32) *uint32 { return &v }

func sidePtr(s submit.Side) *submit.Side { return &s }

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
		"commit_id": "abc1234",
		"body":      "body text",
		"event":     "COMMENT",
		"comments": []interface{}{map[string]interface{}{
			"path": "src/lib.rs",
			"body": "[ISSUE] boom",
			"line": float64(42),
			"side": "RIGHT",
		}},
	}
	if !reflect.DeepEqual(capture.body, want) {
		t.Errorf("payload = %#v\nwant %#v", capture.body, want)
	}

	if result.ReviewID != "987" || result.State != "PENDING" ||
		result.URL != "https://github.com/octo/hello/pull/42#pullrequestreview-987" {
		t.Errorf("result = %+v", result)
	}
	if result.Partial != nil {
		t.Error("atomic submit must not report partial failure")
	}
}

func TestCreateReviewDraftOmitsEvent(t *testing.T) {
	capture := &reviewCapture{}
	d := newTestDriver(t, capture.handler(t))

	_, err := d.CreateReview(context.Background(), testPR(), forge.CreateReviewRequest{
		Event:    forge.SubmitDraft,
		CommitID: "sha",
		Body:     "draft body",
	})
	if err != nil {
		t.Fatalf("CreateReview: %v", err)
	}
	if _, present := capture.body["event"]; present {
		t.Errorf("draft submit must omit event: %#v", capture.body)
	}
	if capture.body["body"] != "draft body" || capture.body["commit_id"] != "sha" {
		t.Errorf("payload = %#v", capture.body)
	}
}

func TestCreateReviewEventMapping(t *testing.T) {
	cases := map[forge.SubmitEvent]string{
		forge.SubmitApprove:        "APPROVE",
		forge.SubmitRequestChanges: "REQUEST_CHANGES",
	}
	for event, want := range cases {
		capture := &reviewCapture{}
		d := newTestDriver(t, capture.handler(t))
		if _, err := d.CreateReview(context.Background(), testPR(), forge.CreateReviewRequest{
			Event: event, CommitID: "sha",
		}); err != nil {
			t.Fatalf("CreateReview(%v): %v", event, err)
		}
		if capture.body["event"] != want {
			t.Errorf("event %v → %v, want %s", event, capture.body["event"], want)
		}
	}
}

func TestCreateReviewMultiLineAndOldSide(t *testing.T) {
	capture := &reviewCapture{}
	d := newTestDriver(t, capture.handler(t))

	_, err := d.CreateReview(context.Background(), testPR(), forge.CreateReviewRequest{
		Event:    forge.SubmitComment,
		CommitID: "sha",
		Comments: []submit.InlineComment{
			{
				Path:      "src/main.rs",
				Line:      20,
				Side:      submit.SideOld,
				StartLine: u32Ptr(15),
				StartSide: sidePtr(submit.SideOld),
				Body:      "ranged",
				CommentID: "local-2",
			},
			{
				Path:            "a.rs",
				Line:            7,
				Side:            submit.SideOld,
				CounterpartLine: u32Ptr(9), // GitHub encoding ignores counterparts
				Body:            "old side",
				CommentID:       "local-3",
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateReview: %v", err)
	}

	comments, ok := capture.body["comments"].([]interface{})
	if !ok || len(comments) != 2 {
		t.Fatalf("comments = %#v", capture.body["comments"])
	}
	ranged, ok := comments[0].(map[string]interface{})
	if !ok {
		t.Fatalf("ranged comment = %#v", comments[0])
	}
	if ranged["start_line"] != float64(15) || ranged["start_side"] != "LEFT" ||
		ranged["line"] != float64(20) || ranged["side"] != "LEFT" {
		t.Errorf("ranged comment = %#v", ranged)
	}
	single, ok := comments[1].(map[string]interface{})
	if !ok {
		t.Fatalf("single comment = %#v", comments[1])
	}
	if single["side"] != "LEFT" || single["line"] != float64(7) {
		t.Errorf("old-side comment = %#v", single)
	}
	if _, present := single["start_line"]; present {
		t.Errorf("single-line comment must not send start_line: %#v", single)
	}
	if _, present := single["start_side"]; present {
		t.Errorf("single-line comment must not send start_side: %#v", single)
	}
}

func TestCreateReviewRejectsUnknownEvent(t *testing.T) {
	d := newTestDriver(t, forbidNetwork(t))
	_, err := d.CreateReview(context.Background(), testPR(), forge.CreateReviewRequest{
		Event: forge.SubmitEvent(99),
	})
	wantForgeErr(t, err, forge.ErrorValidation)
}
