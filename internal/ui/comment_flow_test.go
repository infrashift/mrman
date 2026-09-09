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

// TestExportLegendListsUsedCommentTypes covers the "Comment types:" legend.
// It never rendered: export_legend defaults to on, but renderExport did not
// pass the app's resolved types into ExportOptions, so usedLegendEntries was
// always handed nil and every definition written in [[comment_types]] was
// silently dropped.
//
// The legend lists only the types a session actually used, so a review with
// one NOTE must not advertise ISSUE, SUGGESTION and PRAISE.
func TestExportLegendListsUsedCommentTypes(t *testing.T) {
	_, m := testLifecycle(t)
	moveToDiffLine(t, m)
	pressRune(m, 'c')
	for _, r := range "a plain remark" {
		pressRune(m, r)
	}
	press(m, "", tea.KeyEnter, 0)

	// Export as the author who wrote it, so the type tag stands alone and this
	// test stays about the legend. The author badge has its own tests.
	m.App.Username = model.DefaultAuthor

	// The built-in default type is NOTE, so this comment is a note.
	text, err := renderExport(m.App, exportOptions{ShowLegend: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Comment types: NOTE (worth knowing") {
		t.Errorf("export is missing the NOTE legend entry:\n%s", text)
	}
	if !strings.Contains(text, "**[NOTE]**") {
		t.Errorf("export is missing the [NOTE] tag:\n%s", text)
	}
	for _, unused := range []string{"ISSUE", "SUGGESTION", "PRAISE"} {
		if strings.Contains(text, unused) {
			t.Errorf("legend advertises unused type %s:\n%s", unused, text)
		}
	}

	// export_legend = false drops the line but keeps the per-comment tag.
	plain, err := renderExport(m.App, exportOptions{ShowLegend: false})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain, "Comment types:") {
		t.Errorf("ShowLegend=false must drop the legend line:\n%s", plain)
	}
	if !strings.Contains(plain, "**[NOTE]**") {
		t.Errorf("the tag must survive ShowLegend=false:\n%s", plain)
	}
}

// TestExportDiffQuotesTheCommentedHunk covers the export_diff path end to end:
// the app's own diff has to reach the exporter, which it never had to before
// — renderExport was the one export that took the session alone.
func TestExportDiffQuotesTheCommentedHunk(t *testing.T) {
	_, m := testLifecycle(t)
	moveToDiffLine(t, m)
	pressRune(m, 'c')
	for _, r := range "this hunk needs a test" {
		pressRune(m, r)
	}
	press(m, "", tea.KeyEnter, 0)

	text, err := renderExport(m.App, exportOptions{IncludeDiff: true})
	if err != nil {
		t.Fatal(err)
	}
	want := "```diff\n" +
		"@@ -1,3 +1,4 @@\n" +
		" package x\n" +
		"-var A = 1\n" +
		"+var A = 10\n" +
		"+var C = 3\n" +
		"```"
	if !strings.Contains(text, want) {
		t.Errorf("export is missing the quoted hunk:\n%s", text)
	}

	// Off is the default, and it must leave the notes exactly as they were.
	plain, err := renderExport(m.App, exportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain, "```") {
		t.Errorf("IncludeDiff=false must quote nothing:\n%s", plain)
	}
	if !strings.Contains(plain, "this hunk needs a test") {
		t.Errorf("the comment must survive IncludeDiff=false:\n%s", plain)
	}
}

// TestCommentBoxTitleAdvertisesTabOnlyWhenCyclable pins the conditional hint.
// The box used to offer "Tab:type" unconditionally, including when the cycle
// held a single entry and Tab did nothing — which reads as a broken key rather
// than an absent feature.
func TestCommentBoxTitleAdvertisesTabOnlyWhenCyclable(t *testing.T) {
	t.Run("built-in types are cyclable", func(t *testing.T) {
		_, m := testLifecycle(t)
		moveToDiffLine(t, m)
		pressRune(m, 'c')
		title := viewString(m)
		if !strings.Contains(title, "Tab:type") {
			t.Errorf("four built-in types must advertise Tab:\n%s", commentBoxTitle(t, title))
		}
	})

	t.Run("a lone configured type is not", func(t *testing.T) {
		_, m := testLifecycle(t)
		m.App.CommentTypes = app.ResolveCommentTypes([]app.CommentTypeDef{{ID: "none"}})
		moveToDiffLine(t, m)
		pressRune(m, 'c')
		title := viewString(m)
		if strings.Contains(title, "Tab:type") {
			t.Errorf("a single-entry cycle must not advertise Tab:\n%s", commentBoxTitle(t, title))
		}
		// The rest of the hint stays intact.
		for _, want := range []string{"Enter:save", "Esc:cancel"} {
			if !strings.Contains(title, want) {
				t.Errorf("hint lost %q:\n%s", want, commentBoxTitle(t, title))
			}
		}
	})
}

// commentBoxTitle extracts the comment box header row for failure messages.
func commentBoxTitle(t *testing.T, view string) string {
	t.Helper()
	for line := range strings.SplitSeq(view, "\n") {
		if strings.Contains(line, "comment ") && strings.Contains(line, "╭") {
			return strings.TrimSpace(ansiSequence.ReplaceAllString(line, ""))
		}
	}
	return "<no comment box header found>"
}

// TestVimCommentBoxTitleAdvertisesTabByMode keeps the vim box honest about
// Tab. It never mentioned Tab at all, even though Normal mode cycles the
// comment type with it — but Insert mode consumes Tab to insert
// comment_tab_width spaces, so the hint has to follow the mode rather than
// simply matching the plain box.
func TestVimCommentBoxTitleAdvertisesTabByMode(t *testing.T) {
	_, m := testLifecycle(t)
	m.CommentVimMode = true
	moveToDiffLine(t, m)
	pressRune(m, 'c')

	// The box opens in Insert mode, where Tab inserts spaces.
	if title := commentBoxTitle(t, viewString(m)); strings.Contains(title, "Tab:type") {
		t.Errorf("Insert mode must not advertise Tab:type:\n%s", title)
	}

	// Esc to Normal mode: Tab now reaches the type cycle.
	press(m, "", tea.KeyEscape, 0)
	normal := commentBoxTitle(t, viewString(m))
	if !strings.Contains(normal, "Tab:type") {
		t.Errorf("Normal mode cycles the type with Tab and must say so:\n%s", normal)
	}
	// The vim-specific hints survive the addition.
	for _, want := range []string{"i:insert", "Ctrl-S:save"} {
		if !strings.Contains(normal, want) {
			t.Errorf("vim hint lost %q:\n%s", want, normal)
		}
	}

	// Back to Insert mode and the claim goes away again.
	pressRune(m, 'i')
	if title := commentBoxTitle(t, viewString(m)); strings.Contains(title, "Tab:type") {
		t.Errorf("returning to Insert mode must drop Tab:type:\n%s", title)
	}
}

// TestVimCommentBoxTitleWithSingleType covers both conditions at once: Normal
// mode, but a cycle with nothing to cycle to.
func TestVimCommentBoxTitleWithSingleType(t *testing.T) {
	_, m := testLifecycle(t)
	m.CommentVimMode = true
	m.App.CommentTypes = app.ResolveCommentTypes([]app.CommentTypeDef{{ID: "none"}})
	moveToDiffLine(t, m)
	pressRune(m, 'c')
	press(m, "", tea.KeyEscape, 0)

	title := commentBoxTitle(t, viewString(m))
	if !strings.Contains(title, "NORMAL") {
		t.Fatalf("expected Normal mode, got:\n%s", title)
	}
	if strings.Contains(title, "Tab:type") {
		t.Errorf("a single-entry cycle must not advertise Tab even in Normal mode:\n%s", title)
	}
}

// TestExportBadgesTheAuthorEndToEnd is the wiring this whole feature was
// missing: the badge rule and the username both live on the app, and the
// export never asked for either, so a session reviewed by a person and an
// agent exported as one anonymous list.
func TestExportBadgesTheAuthorEndToEnd(t *testing.T) {
	_, m := testLifecycle(t)
	m.App.Username = "ryan"
	moveToDiffLine(t, m)
	pressRune(m, 'c')
	for _, r := range "mine" {
		pressRune(m, r)
	}
	press(m, "", tea.KeyEnter, 0)

	// A second comment from someone else, as an agent writing through the CLI
	// would leave it.
	theirs := model.NewComment("theirs", model.CommentTypeFromID("note"), nil)
	theirs.Author = "claude"
	m.App.Session.ReviewComments = append(m.App.Session.ReviewComments, theirs)

	text, err := renderExport(m.App, exportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "**[NOTE @claude]**") {
		t.Errorf("someone else's comment must be badged:\n%s", text)
	}
	if strings.Contains(text, "@ryan") {
		t.Errorf("your own comment must not be badged:\n%s", text)
	}

	// show_own_author attributes everything, in the export as in the TUI.
	m.App.ShowOwnAuthor = true
	all, err := renderExport(m.App, exportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(all, "**[NOTE @ryan]**") || !strings.Contains(all, "**[NOTE @claude]**") {
		t.Errorf("show_own_author must attribute every comment:\n%s", all)
	}
}
