package ui

import (
	"strings"
	"testing"

	render "github.com/infrashift/mrman/charmkit/cellrender"
	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/vcs"
)

// stripModel adds a multi-commit inline selector.
func stripModel(m *Model, n int) {
	rows := make([]vcs.CommitInfo, n)
	for i := range rows {
		id := string(rune('a' + i))
		rows[i] = vcs.CommitInfo{ID: id, ShortID: "sha" + id, Summary: "commit " + id}
	}
	m.App.ReviewCommits = rows
	m.App.CommitList = append([]vcs.CommitInfo(nil), rows...)
	m.App.VisibleCommitCount = n
	m.App.ShowCommitSelector = true
	m.App.DiffSource = app.DiffSource{Kind: app.DiffSourceCommitRange}
}

// TestFrameFillsTheTerminal pins the invariant that every rendered frame is
// exactly as tall as the terminal.
//
// A frame that is short does not look broken on its own — the missing rows
// simply keep whatever the *previous* view drew there. It showed up as the
// selector's commit rows and footer still visible behind a diff, which
// reads as corruption rather than as a height bug.
func TestFrameFillsTheTerminal(t *testing.T) {
	for _, size := range [][2]int{{120, 30}, {100, 24}, {200, 50}, {80, 12}} {
		for _, tc := range []struct {
			name  string
			setup func(*Model)
		}{
			{"diff", func(*Model) {}},
			{"commit strip", func(m *Model) { stripModel(m, 3) }},
			{"strip capped", func(m *Model) { stripModel(m, 40) }},
			{"comment navigator", func(m *Model) { addReviewComment(m, "note") }},
			{"strip and navigator", func(m *Model) {
				stripModel(m, 3)
				addReviewComment(m, "note")
			}},
			{"no file list", func(m *Model) { m.App.ShowFileList = false }},
			{"single file view", func(m *Model) { m.App.IsSingleFileView = true }},
			{"selector", func(m *Model) { m.App.InputMode = input.ModeCommitSelect }},
			{"help", func(m *Model) { m.App.InputMode = input.ModeHelp }},
		} {
			t.Run(tc.name, func(t *testing.T) {
				m := testModel(t)
				m.width, m.height = size[0], size[1]
				tc.setup(m)
				m.syncViewport()

				rows := strings.Split(viewString(m), "\n")
				if len(rows) != m.height {
					t.Errorf("%dx%d: frame is %d rows, terminal is %d — the "+
						"difference keeps whatever the previous view drew there",
						size[0], size[1], len(rows), m.height)
				}
			})
		}
	}
}

// TestFrameRowsFillTheWidth catches the same class of staleness sideways: a
// row narrower than the terminal leaves the old frame's tail visible.
func TestFrameRowsFillTheWidth(t *testing.T) {
	m := testModel(t)
	m.width, m.height = 120, 30
	stripModel(m, 3)
	addReviewComment(m, "note")
	m.syncViewport()

	for i, row := range strings.Split(plainView(m), "\n") {
		if w := len([]rune(row)); w > m.width {
			t.Errorf("row %d is %d columns wide, terminal is %d", i, w, m.width)
		}
	}
}

// TestDiffRowsFitTheirPane pins the width arithmetic in both view modes.
//
// A row wider than its pane is not clipped — lipgloss wraps it, and the
// wrapped tail lands outside the column it belongs to. In side-by-side that
// destroys the alignment the mode exists for, from a single column of
// overflow.
func TestDiffRowsFitTheirPane(t *testing.T) {
	for _, mode := range []struct {
		name string
		view app.DiffViewMode
	}{
		{"unified", app.ViewUnified},
		{"side-by-side", app.ViewSideBySide},
	} {
		for _, width := range []int{80, 100, 140, 201} { // 201: odd, so the halves cannot be equal
			t.Run(mode.name, func(t *testing.T) {
				m := testModel(t)
				m.width, m.height = width, 30
				m.App.DiffViewMode = mode.view
				m.syncViewport()

				pane := m.diffInnerWidth()
				for i, line := range m.diffPane.BuildLines(m.App, pane, 20) {
					got := render.StringWidth(ansiSequence.ReplaceAllString(line, ""))
					if got > pane {
						t.Errorf("%s at %d cols: row %d is %d wide, pane is %d",
							mode.name, width, i, got, pane)
					}
				}
			})
		}
	}
}

// TestEscapeFromAnEmptyReviewIsNotADeadEnd covers the state a user reaches
// by pressing Esc out of the startup target selector: normal mode with no
// diff. Every review key is a no-op there, so if Esc does nothing too the
// only ways out are three commands the status bar never mentions.
func TestEscapeFromAnEmptyReviewIsNotADeadEnd(t *testing.T) {
	m := testModel(t)
	m.App.VCS = &selectorBackend{
		stubBackend: stubBackend{info: vcs.Info{RootPath: "/repo", HeadCommit: "abc", Type: vcs.TypeGit}},
		commits:     []vcs.CommitInfo{makeCommit("aaa1111", "first")},
	}
	m.App.DiffFiles = nil
	m.App.InputMode = input.ModeNormal

	m.dispatchNormal(input.Action{Kind: input.ExitMode})

	if m.App.InputMode != input.ModeCommitSelect {
		t.Fatalf("esc with nothing loaded must reopen the selector, mode is %v", m.App.InputMode)
	}
}

// TestEscapeInALoadedReviewLeavesItAlone is the other half: Esc must not
// yank a reviewer out of the diff they are reading.
func TestEscapeInALoadedReviewLeavesItAlone(t *testing.T) {
	m := testModel(t)
	if len(m.App.DiffFiles) == 0 {
		t.Fatal("fixture must load a diff")
	}
	count := 12
	m.App.PendingCount = &count

	m.dispatchNormal(input.Action{Kind: input.ExitMode})
	if m.App.PendingCount != nil {
		t.Error("esc must discard a half-typed count")
	}
	if m.App.InputMode != input.ModeNormal {
		t.Errorf("esc in a loaded review must stay in normal mode, got %v", m.App.InputMode)
	}

	m.dispatchNormal(input.Action{Kind: input.ExitMode})
	if m.App.InputMode != input.ModeNormal {
		t.Errorf("a second esc must still not leave the diff, got %v", m.App.InputMode)
	}
}

// TestCommentBoxIsAClosedBox pins the overlay's two rules to the same width.
// The top one used to stop after the hint text, so the box trailed off in
// mid-air; at a narrow width the hint wrapped into the diff behind it.
func TestCommentBoxIsAClosedBox(t *testing.T) {
	for _, width := range []int{40, 60, 100, 160} {
		m := testModel(t)
		m.width, m.height = width, 30
		m.syncViewport()
		m.App.InputMode = input.ModeComment
		m.App.CommentLine = &app.CommentAnchor{Line: 11}

		rows := m.diffPane.commentInputOverlay(m.App, nil, width)
		if len(rows) < 2 {
			t.Fatalf("width %d: overlay is %d rows", width, len(rows))
		}
		top := render.StringWidth(ansiSequence.ReplaceAllString(rows[0], ""))
		bottom := render.StringWidth(ansiSequence.ReplaceAllString(rows[len(rows)-1], ""))
		if top > width {
			t.Errorf("width %d: top rule is %d wide and will wrap", width, top)
		}
		if top != bottom {
			t.Errorf("width %d: top rule %d, bottom rule %d — the box does not close", width, top, bottom)
		}
	}
}

// TestHelpTakesACountPrefix covers the one mode where a typed count was
// dropped. The popup is a scrollable document, so "20j" must move 20 rows,
// not one.
func TestHelpTakesACountPrefix(t *testing.T) {
	m := testModel(t)
	m.App.ToggleHelp()
	m.helpView() // primes TotalLines/ViewportHeight
	if m.App.InputMode != input.ModeHelp {
		t.Fatal("help must be open")
	}

	for _, r := range "20j" {
		pressRune(m, r)
	}
	if got := m.App.HelpState.ScrollOffset; got != 20 {
		t.Errorf("20j scrolled %d rows, want 20", got)
	}
	if m.App.PendingCount != nil {
		t.Error("the count must be consumed, not left pending")
	}

	for _, r := range "5k" {
		pressRune(m, r)
	}
	if got := m.App.HelpState.ScrollOffset; got != 15 {
		t.Errorf("5k left offset %d, want 15", got)
	}
}

// TestCountInHelpDoesNotJumpToASourceLine: {N}G means "to the end" in the
// popup. Routing it to GoToSourceLine would move the diff cursor behind an
// open help window and leave a "line not in diff" message under it.
func TestCountInHelpDoesNotJumpToASourceLine(t *testing.T) {
	m := testModel(t)
	m.App.ToggleHelp()
	m.helpView() // primes TotalLines/ViewportHeight
	before := m.App.DiffState.CursorLine

	for _, r := range "42G" {
		pressRune(m, r)
	}
	if m.App.DiffState.CursorLine != before {
		t.Error("a count in help must not move the diff cursor")
	}
	if m.App.HelpState.ScrollOffset == 0 {
		t.Error("42G in help must still scroll to the end")
	}
}

// TestSubmitModalsAreBoxed pins every submit modal to a closed frame at the
// pane width. Printed bare, their rows read as diff content that had gone
// strange rather than as a dialog about to push a review to a forge.
func TestSubmitModalsAreBoxed(t *testing.T) {
	modes := []input.Mode{
		input.ModeSubmitActionPicker,
		input.ModeSubmitResolver,
		input.ModeSubmitConfirm,
	}
	for _, mode := range modes {
		for _, width := range []int{60, 100, 160} {
			m, _ := prModel(t)
			m.width = width
			m.App.InputMode = mode
			m.App.Submit = &app.SubmitState{Event: forge.SubmitComment}

			rows := m.submitModalView(width)
			if len(rows) < 3 {
				t.Fatalf("%v at %d: got %d rows, want a head, a body and a foot",
					mode, width, len(rows))
			}
			head := ansiSequence.ReplaceAllString(rows[0], "")
			foot := ansiSequence.ReplaceAllString(rows[len(rows)-1], "")
			if !strings.HasPrefix(head, "    ╭──") {
				t.Errorf("%v at %d: head is %q", mode, width, head)
			}
			if !strings.HasPrefix(foot, "    ╰──") {
				t.Errorf("%v at %d: foot is %q", mode, width, foot)
			}
			if hw, fw := render.StringWidth(head), render.StringWidth(foot); hw != fw {
				t.Errorf("%v at %d: head %d, foot %d — the box does not close", mode, width, hw, fw)
			}
			for i, row := range rows {
				if w := render.StringWidth(ansiSequence.ReplaceAllString(row, "")); w > width {
					t.Errorf("%v at %d: row %d is %d wide and will wrap", mode, width, i, w)
				}
			}
		}
	}
}
