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

// editHandler serves one thread (id 7), context-less unless filePath is
// set, and records the comment edits.
type editHandler struct {
	t        *testing.T
	author   string // the root comment's author id
	filePath string
	edits    []map[string]any
}

func (h *editHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	const thread = threadsPathPrefix + "/threads/7"
	path := strings.ToLower(r.URL.Path)
	switch {
	case strings.EqualFold(r.URL.Path, "/_apis/connectionData"):
		writeJSON(w, http.StatusOK, connectionDataJSON())
	case r.Method == http.MethodGet && path == strings.ToLower(thread):
		context := ""
		if h.filePath != "" {
			context = fmt.Sprintf(`"threadContext": {"filePath": %q},`, h.filePath)
		}
		writeJSON(w, http.StatusOK, fmt.Sprintf(`{"id": 7, "status": "active", %s
			"comments": [
				{"id": 4, "content": "system", "commentType": "system", "author": {"id": "system"}},
				{"id": 5, "content": "status", "commentType": "text", "author": {"id": %q, "displayName": "Bot"}}
			]}`, context, h.author))
	case r.Method == http.MethodPatch && path == strings.ToLower(thread+"/comments/5"):
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			h.t.Errorf("body is not JSON: %v", err)
		}
		h.edits = append(h.edits, body)
		writeJSON(w, http.StatusOK, `{"id": 5}`)
	default:
		h.t.Errorf("unexpected call %s %s", r.Method, r.URL)
	}
}

func TestEditOwnSummary(t *testing.T) {
	h := &editHandler{t: t, author: strings.ToUpper(testViewerID)}
	d := newTestDriver(t, h)
	if err := d.EditOwnSummary(context.Background(), testPR(), "7", "New status."); err != nil {
		t.Fatal(err)
	}
	// Only the content changes, on the first non-system comment.
	if len(h.edits) != 1 || h.edits[0]["content"] != "New status." || len(h.edits[0]) != 1 {
		t.Errorf("edits %v", h.edits)
	}
}

// Someone else's comment is never edited, whatever its display name.
func TestEditOwnSummary_RefusesOthersComments(t *testing.T) {
	h := &editHandler{t: t, author: "99999999-0000-0000-0000-000000000000"}
	d := newTestDriver(t, h)
	err := d.EditOwnSummary(context.Background(), testPR(), "7", "x")
	if fe := mustForgeErr(t, err); fe.Kind != forge.ErrorForbidden {
		t.Errorf("kind %v, want forbidden", fe.Kind)
	}
	if len(h.edits) != 0 {
		t.Errorf("edited another's comment: %v", h.edits)
	}
}

// A file-anchored thread is a review thread, not a summary.
func TestEditOwnSummary_RefusesFileThreads(t *testing.T) {
	h := &editHandler{t: t, author: testViewerID, filePath: "/calc/calc.go"}
	d := newTestDriver(t, h)
	err := d.EditOwnSummary(context.Background(), testPR(), "7", "x")
	if fe := mustForgeErr(t, err); fe.Kind != forge.ErrorValidation {
		t.Errorf("kind %v, want validation", fe.Kind)
	}
	if len(h.edits) != 0 {
		t.Errorf("edited a file thread: %v", h.edits)
	}
}

func TestEditOwnSummary_Validation(t *testing.T) {
	d := newTestDriver(t, forbidNetwork(t))
	for _, tc := range []struct{ id, body string }{{"x", "body"}, {"0", "body"}, {"7", " \n"}} {
		if err := d.EditOwnSummary(context.Background(), testPR(), tc.id, tc.body); mustForgeErr(t, err).Kind != forge.ErrorValidation {
			t.Errorf("%+v: %v", tc, err)
		}
	}
}
