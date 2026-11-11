package azdof

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
)

// resolveHandler serves one thread (id 7) and records the reply and the
// status patch.
type resolveHandler struct {
	t        *testing.T
	author   string // the root comment's author id
	status   string
	replies  []map[string]any
	patches  []map[string]any
	failPost bool
}

func (h *resolveHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	const thread = threadsPathPrefix + "/threads/7"
	path := strings.ToLower(r.URL.Path)
	decode := func() map[string]any {
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			h.t.Errorf("body is not JSON: %v", err)
		}
		return body
	}
	switch {
	case strings.EqualFold(r.URL.Path, "/_apis/connectionData"):
		writeJSON(w, http.StatusOK, connectionDataJSON())
	case r.Method == http.MethodGet && path == strings.ToLower(thread):
		status := ""
		if h.status != "" {
			status = fmt.Sprintf(`"status": %q,`, h.status)
		}
		writeJSON(w, http.StatusOK, fmt.Sprintf(`{"id": 7, %s
			"comments": [
				{"id": 1, "content": "finding", "commentType": "text", "author": {"id": %q, "displayName": "Bot"}},
				{"id": 2, "content": "reply", "commentType": "text", "author": {"id": "someone-else"}}
			]}`, status, h.author))
	case r.Method == http.MethodPost && path == strings.ToLower(thread+"/comments"):
		if h.failPost {
			writeJSON(w, http.StatusInternalServerError, `{"message": "boom"}`)
			return
		}
		h.replies = append(h.replies, decode())
		writeJSON(w, http.StatusOK, `{"id": 3}`)
	case r.Method == http.MethodPatch && path == strings.ToLower(thread):
		h.patches = append(h.patches, decode())
		writeJSON(w, http.StatusOK, `{"id": 7, "status": "fixed"}`)
	default:
		h.t.Errorf("unexpected call %s %s", r.Method, r.URL)
	}
}

func TestResolveOwnThread(t *testing.T) {
	h := &resolveHandler{t: t, author: strings.ToUpper(testViewerID), status: "active"}
	d := newTestDriver(t, h)
	got, err := d.ResolveOwnThread(context.Background(), testPR(), "7", "Fixed in abc123.")
	if err != nil {
		t.Fatalf("ResolveOwnThread: %v", err)
	}
	if got != forge.ResolveResolved {
		t.Errorf("outcome %q", got)
	}
	if len(h.replies) != 1 || h.replies[0]["content"] != "Fixed in abc123." || h.replies[0]["parentCommentId"] != float64(1) {
		t.Errorf("reply %v", h.replies)
	}
	if len(h.patches) != 1 || h.patches[0]["status"] != "fixed" || len(h.patches[0]) != 1 {
		t.Errorf("patch %v, want only status fixed", h.patches)
	}
}

func TestResolveOwnThread_NoReply(t *testing.T) {
	h := &resolveHandler{t: t, author: testViewerID}
	d := newTestDriver(t, h)
	if _, err := d.ResolveOwnThread(context.Background(), testPR(), "7", ""); err != nil {
		t.Fatal(err)
	}
	if len(h.replies) != 0 || len(h.patches) != 1 {
		t.Errorf("replies %v patches %v", h.replies, h.patches)
	}
}

func TestResolveOwnThread_AlreadyResolvedIsLeftAlone(t *testing.T) {
	for _, status := range []string{"fixed", "wontFix", "closed", "byDesign"} {
		h := &resolveHandler{t: t, author: testViewerID, status: status}
		d := newTestDriver(t, h)
		got, err := d.ResolveOwnThread(context.Background(), testPR(), "7", "reply")
		if err != nil || got != forge.ResolveAlreadyResolved || len(h.replies)+len(h.patches) != 0 {
			t.Errorf("%s: %q %v, writes %v %v", status, got, err, h.replies, h.patches)
		}
	}
}

// Someone else's discussion is never closed, whatever its display name.
func TestResolveOwnThread_RefusesOthersThreads(t *testing.T) {
	h := &resolveHandler{t: t, author: "99999999-0000-0000-0000-000000000000", status: "active"}
	d := newTestDriver(t, h)
	_, err := d.ResolveOwnThread(context.Background(), testPR(), "7", "reply")
	if fe := mustForgeErr(t, err); fe.Kind != forge.ErrorForbidden {
		t.Errorf("kind %v, want forbidden", fe.Kind)
	}
	if len(h.replies)+len(h.patches) != 0 {
		t.Errorf("wrote to another's thread: %v %v", h.replies, h.patches)
	}
}

func TestResolveOwnThread_Errors(t *testing.T) {
	d := newTestDriver(t, forbidNetwork(t))
	if _, err := d.ResolveOwnThread(context.Background(), testPR(), "PRRT_x", ""); mustForgeErr(t, err).Kind != forge.ErrorValidation {
		t.Error("a non-numeric id must be a validation error")
	}
	h := &resolveHandler{t: t, author: testViewerID, status: "active", failPost: true}
	d = newTestDriver(t, h)
	if _, err := d.ResolveOwnThread(context.Background(), testPR(), "7", "reply"); err == nil || len(h.patches) != 0 {
		t.Errorf("a failed reply must not resolve: %v %v", err, h.patches)
	}
}
