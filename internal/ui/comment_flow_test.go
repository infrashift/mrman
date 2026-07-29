package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
)

// moveToDiffLine puts the cursor on the first diff line.
func moveToDiffLine(t *testing.T, m *Model) {
	t.Helper()
	for i := range m.App.LineAnnotations {
		if m.App.LineAnnotations[i].Kind == app.AnnDiffLine {
			m.App.MoveCursorToAnnotation(i)
			return
		}
	}
	t.Fatal("no diff line found")
}

func TestCommentComposeAndSave(t *testing.T) {
	_, m := testLifecycle(t)
	moveToDiffLine(t, m)

	pressRune(m, 'c')
	if m.App.InputMode != input.ModeComment {
		t.Fatal("c must enter comment mode")
	}
	for _, r := range "needs a test" {
		pressRune(m, r)
	}
	if m.App.CommentBuffer != "needs a test" {
		t.Fatalf("buffer = %q", m.App.CommentBuffer)
	}
	// Tab cycles the comment type (default set: none only unless configured;
	// cycling with one entry is a no-op — just must not crash).
	press(m, "", tea.KeyTab, 0)
	press(m, "", tea.KeyEnter, 0)

	if m.App.InputMode != input.ModeNormal {
		t.Fatal("save must return to normal mode")
	}
	file := m.App.Session.File("src/x.go")
	if file == nil || file.CommentCount() != 1 {
		t.Fatalf("comment not stored: %+v", file)
	}
	// Autosaved: reload from disk and find it.
	reloaded, err := m.session.store.LoadSession(m.session.path)
	if err != nil || reloaded.File("src/x.go").CommentCount() != 1 {
		t.Fatalf("comment not autosaved: %v", err)
	}
	// The comment renders in the annotation stream as box rows.
	found := false
	for i := range m.App.LineAnnotations {
		if m.App.LineAnnotations[i].Kind == app.AnnLineComment {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("comment annotation missing")
	}
	// And in the rendered pane.
	lines := strings.Join(m.diffPane.BuildLines(m.App, 80, 40), "\n")
	if !strings.Contains(lines, "needs a test") || !strings.Contains(lines, "╭") {
		t.Fatal("comment box not rendered")
	}
}

func TestCommentEscapeCancels(t *testing.T) {
	_, m := testLifecycle(t)
	moveToDiffLine(t, m)
	pressRune(m, 'c')
	pressRune(m, 'x')
	press(m, "", tea.KeyEscape, 0)
	if m.App.InputMode != input.ModeNormal {
		t.Fatal("esc must cancel")
	}
	if m.App.Session.File("src/x.go").CommentCount() != 0 {
		t.Fatal("cancelled comment must not persist")
	}
}

func TestVimCommentFlow(t *testing.T) {
	_, m := testLifecycle(t)
	m.CommentVimMode = true
	moveToDiffLine(t, m)

	pressRune(m, 'c')
	if m.vim == nil {
		t.Fatal("vim wrapper must activate")
	}
	// Starts in INSERT; type, Esc to normal, :w to save.
	for _, r := range "vim note" {
		pressRune(m, r)
	}
	press(m, "", tea.KeyEscape, 0)
	if m.vim.editor.ModeLabel() != "NORMAL" {
		t.Fatalf("mode = %s", m.vim.editor.ModeLabel())
	}
	pressRune(m, ':')
	pressRune(m, 'w')
	press(m, "", tea.KeyEnter, 0)

	if m.App.InputMode != input.ModeNormal {
		t.Fatal(":w must save and exit")
	}
	if m.App.Session.File("src/x.go").CommentCount() != 1 {
		t.Fatal("vim comment not stored")
	}
}

func TestVimDoublePressEnterSaves(t *testing.T) {
	_, m := testLifecycle(t)
	m.CommentVimMode = true
	moveToDiffLine(t, m)
	pressRune(m, 'c')
	for _, r := range "double" {
		pressRune(m, r)
	}
	press(m, "", tea.KeyEscape, 0) // to NORMAL
	press(m, "", tea.KeyEnter, 0)  // arm
	if m.vim == nil || m.vim.pending != vimPendingSave {
		t.Fatal("first enter must arm save")
	}
	press(m, "", tea.KeyEnter, 0) // confirm
	if m.App.Session.File("src/x.go").CommentCount() != 1 {
		t.Fatal("double-press enter must save")
	}
}

func TestVisualRangeComment(t *testing.T) {
	_, m := testLifecycle(t)
	// Start on the first ADDITION line so extending stays on the new side
	// (a context→deletion range is mixed-side and correctly rejected).
	for i := range m.App.LineAnnotations {
		ann := &m.App.LineAnnotations[i]
		if ann.Kind == app.AnnDiffLine {
			file := &m.App.DiffFiles[ann.FileIdx]
			if file.Hunks[ann.HunkIdx].Lines[ann.LineIdx].Origin == model.OriginAddition {
				m.App.MoveCursorToAnnotation(i)
				break
			}
		}
	}

	pressRune(m, 'v')
	if m.App.InputMode != input.ModeVisualSelect {
		t.Fatal("v must enter visual mode")
	}
	pressRune(m, 'j') // extend one line
	pressRune(m, 'c') // comment on range
	if m.App.InputMode != input.ModeComment {
		t.Fatal("c in visual must open range comment")
	}
	for _, r := range "range note" {
		pressRune(m, r)
	}
	press(m, "", tea.KeyEnter, 0)

	file := m.App.Session.File("src/x.go")
	if file.CommentCount() != 1 {
		t.Fatal("range comment not stored")
	}
	for _, comments := range file.LineComments {
		if comments[0].LineRange == nil {
			t.Fatal("range comment must carry a line range")
		}
	}
}

func TestToggleReviewedCollapsesAndSaves(t *testing.T) {
	_, m := testLifecycle(t)
	moveToDiffLine(t, m)
	pressRune(m, 'r')
	if !m.App.Session.IsFileReviewed("src/x.go") {
		t.Fatal("r must mark reviewed")
	}
	reloaded, err := m.session.store.LoadSession(m.session.path)
	if err != nil || !reloaded.IsFileReviewed("src/x.go") {
		t.Fatal("reviewed state must autosave")
	}
	// The file body collapses out of the annotation stream.
	for i := range m.App.LineAnnotations {
		if m.App.LineAnnotations[i].Kind == app.AnnDiffLine {
			t.Fatal("reviewed file body must collapse")
		}
	}
}

func TestDeleteCommentWithDD(t *testing.T) {
	_, m := testLifecycle(t)
	moveToDiffLine(t, m)
	pressRune(m, 'c')
	for _, r := range "temp" {
		pressRune(m, r)
	}
	press(m, "", tea.KeyEnter, 0)
	if m.App.Session.File("src/x.go").CommentCount() != 1 {
		t.Fatal("setup comment missing")
	}
	// Move onto the comment box and dd it.
	for i := range m.App.LineAnnotations {
		if m.App.LineAnnotations[i].Kind == app.AnnLineComment {
			m.App.MoveCursorToAnnotation(i)
			break
		}
	}
	pressRune(m, 'd')
	pressRune(m, 'd')
	if m.App.Session.File("src/x.go").CommentCount() != 0 {
		t.Fatal("dd must delete the comment")
	}
}

func TestExportAfterComment(t *testing.T) {
	_, m := testLifecycle(t)
	moveToDiffLine(t, m)
	pressRune(m, 'c')
	for _, r := range "exported finding" {
		pressRune(m, r)
	}
	press(m, "", tea.KeyEnter, 0)

	text, err := renderExport(m.App, exportOptions{ShowLegend: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## Local mrman Comments", "exported finding", "src/x.go:"} {
		if !strings.Contains(text, want) {
			t.Errorf("export missing %q:\n%s", want, text)
		}
	}
	_ = model.ClearCommentsOnly // keep model import for clarity of intent
}
