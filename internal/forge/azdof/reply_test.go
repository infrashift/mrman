package azdof

import (
	"context"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
)

func TestReplyToThread(t *testing.T) {
	// Anyone's thread may be answered: the root's author is not checked.
	h := &resolveHandler{t: t, author: "99999999-0000-0000-0000-000000000000", status: "active"}
	d := newTestDriver(t, h)
	id, err := d.ReplyToThread(context.Background(), testPR(), "7", "2", "The answer.")
	if err != nil {
		t.Fatal(err)
	}
	if id != "3" || len(h.replies) != 1 || h.replies[0]["content"] != "The answer." ||
		h.replies[0]["parentCommentId"] != float64(2) || h.replies[0]["commentType"] != "text" {
		t.Errorf("id %q, replies %v", id, h.replies)
	}
	if len(h.patches) != 0 {
		t.Errorf("a reply must not change the thread: %v", h.patches)
	}
}

// Without a parent, the reply answers the thread's root comment.
func TestReplyToThread_DefaultsToTheRoot(t *testing.T) {
	h := &resolveHandler{t: t, author: testViewerID, status: "fixed"}
	d := newTestDriver(t, h)
	if _, err := d.ReplyToThread(context.Background(), testPR(), "7", "", "x"); err != nil {
		t.Fatal(err)
	}
	if len(h.replies) != 1 || h.replies[0]["parentCommentId"] != float64(1) {
		t.Errorf("replies %v", h.replies)
	}
}

func TestReplyToThread_Validation(t *testing.T) {
	d := newTestDriver(t, forbidNetwork(t))
	for _, tc := range []struct{ thread, parent, body string }{
		{"x", "", "body"}, {"7", "y", "body"}, {"7", "", "  "},
	} {
		_, err := d.ReplyToThread(context.Background(), testPR(), tc.thread, tc.parent, tc.body)
		if mustForgeErr(t, err).Kind != forge.ErrorValidation {
			t.Errorf("%+v: %v", tc, err)
		}
	}
}
