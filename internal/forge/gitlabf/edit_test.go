package gitlabf

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
)

// summaryEditFixture serves the current user, discussion testDiscussion as a
// general note (or a positioned one) and the note edit, recording the edits.
type summaryEditFixture struct {
	viewer     string
	author     string
	positioned bool
	edits      []string
}

func (f *summaryEditFixture) mux(t *testing.T) *fixtureMux {
	m := newFixtureMux(t)
	m.JSON("GET /api/v4/user", fmt.Sprintf(`{"id": 9, "username": %q}`, f.viewer))
	position := "null"
	if f.positioned {
		position = `{"position_type": "text", "new_path": "calc/calc.go", "new_line": 3}`
	}
	m.JSON("GET "+discussionPath, fmt.Sprintf(`{"id": %q, "individual_note": %t, "notes": [
		{"id": 100, "body": "status", "author": {"id": 3, "username": %q},
		 "system": false, "position": %s}
	]}`, testDiscussion, !f.positioned, f.author, position))
	m.Handle("PUT "+discussionPath+"/notes/100", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("note body is not JSON: %v", err)
		}
		f.edits = append(f.edits, fmt.Sprint(body["body"]))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id": 100, "body": "ok", "author": {"username": "vizzini-bot"}}`))
	})
	return m
}

func TestEditOwnSummary(t *testing.T) {
	f := &summaryEditFixture{viewer: "vizzini-bot", author: "Vizzini-Bot"}
	d := newTestDriver(t, f.mux(t))
	if err := d.EditOwnSummary(context.Background(), testPR(), testDiscussion, "New status."); err != nil {
		t.Fatal(err)
	}
	if len(f.edits) != 1 || f.edits[0] != "New status." {
		t.Errorf("edits %v", f.edits)
	}
}

func TestEditOwnSummaryRefusesOthersNotes(t *testing.T) {
	f := &summaryEditFixture{viewer: "vizzini-bot", author: "alice"}
	d := newTestDriver(t, f.mux(t))
	wantForgeErr(t, d.EditOwnSummary(context.Background(), testPR(), testDiscussion, "x"), forge.ErrorForbidden)
	if len(f.edits) != 0 {
		t.Errorf("edited another's note: %v", f.edits)
	}
}

// A positioned discussion is a review thread, not a summary.
func TestEditOwnSummaryRefusesPositionedDiscussions(t *testing.T) {
	f := &summaryEditFixture{viewer: "vizzini-bot", author: "vizzini-bot", positioned: true}
	d := newTestDriver(t, f.mux(t))
	wantForgeErr(t, d.EditOwnSummary(context.Background(), testPR(), testDiscussion, "x"), forge.ErrorValidation)
	if len(f.edits) != 0 {
		t.Errorf("edited a review thread: %v", f.edits)
	}
}

func TestEditOwnSummaryValidates(t *testing.T) {
	d := newTestDriver(t, forbidNetwork(t))
	for _, id := range []string{"", "abc/../../x", "abc?x=1"} {
		wantForgeErr(t, d.EditOwnSummary(context.Background(), testPR(), id, "body"), forge.ErrorValidation)
	}
	wantForgeErr(t, d.EditOwnSummary(context.Background(), testPR(), testDiscussion, " \n"), forge.ErrorValidation)
}
