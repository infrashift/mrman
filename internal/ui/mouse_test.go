package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/input"
)

// mouseModel returns a rendered model with mouse support on, so the layout
// rectangles exist to hit-test against.
func mouseModel(t *testing.T) *Model {
	t.Helper()
	m := testModel(t)
	m.height = 40
	m.mouseEnabled = true
	m.syncViewport()
	_ = m.View() // records the pane rectangles
	return m
}

func mouse(x, y int, button tea.MouseButton) tea.Mouse {
	return tea.Mouse{X: x, Y: y, Button: button}
}

// paneOf returns the recorded rectangle for a panel.
func paneOf(t *testing.T, m *Model, panel app.FocusedPanel) paneRect {
	t.Helper()
	rect, ok := m.layout.find(panel)
	if !ok {
		t.Fatalf("no rectangle recorded for %v", panel)
	}
	return rect
}

func TestMouseModeFollowsTheConfig(t *testing.T) {
	m := testModel(t)
	if m.View().MouseMode != tea.MouseModeNone {
		t.Error("mouse must be off until the config enables it")
	}
	m.mouseEnabled = true
	if m.View().MouseMode != tea.MouseModeCellMotion {
		t.Error("mouse = true must request cell-motion tracking")
	}
}

func TestMouseEventsIgnoredWhenDisabled(t *testing.T) {
	m := mouseModel(t)
	m.mouseEnabled = false
	before := m.App.DiffState.ScrollOffset

	rect := paneOf(t, m, app.PanelDiff)
	m.Update(tea.MouseWheelMsg(mouse(rect.X+1, rect.Y+1, tea.MouseWheelDown)))

	if m.App.DiffState.ScrollOffset != before {
		t.Error("a disabled mouse must not move anything")
	}
}

func TestPaneRectanglesCoverTheRenderedPanes(t *testing.T) {
	m := testModel(t)
	m.height = 40
	m.mouseEnabled = true
	addReviewComment(m, "note")
	addCommits(m, 3)
	m.syncViewport()
	_ = m.View()

	for _, panel := range []app.FocusedPanel{
		app.PanelDiff, app.PanelFileList, app.PanelComments, app.PanelCommitSelector,
	} {
		rect := paneOf(t, m, panel)
		if rect.W <= 0 || rect.H <= 0 {
			t.Errorf("%v has an empty rectangle: %+v", panel, rect)
		}
		if rect.Y+rect.H > m.height {
			t.Errorf("%v runs past the bottom of the screen: %+v", panel, rect)
		}
	}

	// The panes must not overlap, or a click is ambiguous.
	for i, a := range m.layout.panes {
		for _, b := range m.layout.panes[i+1:] {
			if a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H {
				t.Errorf("%v and %v overlap: %+v vs %+v", a.Panel, b.Panel, a, b)
			}
		}
	}
}

func TestSplitLeftColumnEndsLevelWithTheDiff(t *testing.T) {
	m := testModel(t)
	m.height = 40
	m.mouseEnabled = true
	addReviewComment(m, "note")
	m.syncViewport()
	_ = m.View()

	nav := paneOf(t, m, app.PanelComments)
	diff := paneOf(t, m, app.PanelDiff)
	// The navigator's bottom border is one row below its body; the diff's
	// is likewise. Both columns must therefore end on the same row.
	if nav.Y+nav.H != diff.Y+diff.H {
		t.Errorf("the split column ends at %d but the diff at %d",
			nav.Y+nav.H, diff.Y+diff.H)
	}
}

func TestWheelScrollsThePaneUnderThePointer(t *testing.T) {
	m := mouseModel(t)
	diff := paneOf(t, m, app.PanelDiff)

	m.Update(tea.MouseWheelMsg(mouse(diff.X+1, diff.Y+1, tea.MouseWheelDown)))
	scrolled := m.App.DiffState.ScrollOffset
	if scrolled == 0 {
		t.Fatal("the wheel must scroll the diff")
	}
	// The wheel drives the view, not the cursor: the cursor only follows
	// far enough to stay on screen (vim's Ctrl-E).
	if m.App.DiffState.CursorLine < scrolled {
		t.Errorf("cursor %d fell off the top of the viewport at offset %d",
			m.App.DiffState.CursorLine, scrolled)
	}
	if m.App.VisualSelection != nil {
		t.Error("scrolling is not selecting")
	}

	m.Update(tea.MouseWheelMsg(mouse(diff.X+1, diff.Y+1, tea.MouseWheelUp)))
	if m.App.DiffState.ScrollOffset != 0 {
		t.Error("scrolling back up must return to the top")
	}
}

func TestWheelOverTheFileListScrollsTheTreeNotTheDiff(t *testing.T) {
	m := mouseModel(t)
	tree := paneOf(t, m, app.PanelFileList)
	diffBefore := m.App.DiffState.ScrollOffset

	m.Update(tea.MouseWheelMsg(mouse(tree.X+1, tree.Y+1, tea.MouseWheelDown)))
	if m.App.DiffState.ScrollOffset != diffBefore {
		t.Error("the wheel must act on the pane under the pointer only")
	}
}

func TestWheelOutsideEveryPaneDoesNothing(t *testing.T) {
	m := mouseModel(t)
	before := m.App.DiffState.ScrollOffset
	m.Update(tea.MouseWheelMsg(mouse(0, 0, tea.MouseWheelDown))) // the header
	if m.App.DiffState.ScrollOffset != before {
		t.Error("chrome rows are not scrollable")
	}
}

func TestClickingADiffLineMovesTheCursorAndFocus(t *testing.T) {
	m := mouseModel(t)
	m.App.FocusPanel(app.PanelFileList)
	diff := paneOf(t, m, app.PanelDiff)

	// Row 3 of the stream is a diff line in the shared fixture.
	m.Update(tea.MouseClickMsg(mouse(diff.X+10, diff.Y+3, tea.MouseLeft)))

	if m.App.FocusedPanel != app.PanelDiff {
		t.Error("clicking a pane must focus it")
	}
	if m.App.DiffState.CursorLine != 3 {
		t.Errorf("cursor = %d, want the clicked row 3", m.App.DiffState.CursorLine)
	}
}

func TestClickingAFileOpensIt(t *testing.T) {
	m := mouseModel(t)
	tree := paneOf(t, m, app.PanelFileList)
	items := m.App.BuildVisibleItems()

	// Find the first file row (the fixture starts with a directory).
	fileRow := -1
	for i := range items {
		if !items[i].IsDir {
			fileRow = i
			break
		}
	}
	if fileRow < 0 {
		t.Fatal("expected a file row in the tree")
	}

	m.Update(tea.MouseClickMsg(mouse(tree.X+2, tree.Y+fileRow, tea.MouseLeft)))
	if m.App.FocusedPanel != app.PanelDiff {
		t.Error("clicking a file must jump to it and focus the diff")
	}
}

func TestClickingADirectoryTogglesIt(t *testing.T) {
	m := mouseModel(t)
	tree := paneOf(t, m, app.PanelFileList)
	items := m.App.BuildVisibleItems()
	if len(items) == 0 || !items[0].IsDir {
		t.Skip("fixture has no directory row")
	}
	before := len(items)

	m.Update(tea.MouseClickMsg(mouse(tree.X+1, tree.Y, tea.MouseLeft)))
	if len(m.App.BuildVisibleItems()) == before {
		t.Error("clicking a directory must expand or collapse it")
	}
	if m.App.FocusedPanel != app.PanelFileList {
		t.Error("toggling a directory must keep focus in the tree")
	}
}

func TestClickingBelowTheContentIsIgnored(t *testing.T) {
	m := mouseModel(t)
	diff := paneOf(t, m, app.PanelDiff)
	before := m.App.DiffState.CursorLine

	// Far below the last annotation but still inside the pane.
	m.Update(tea.MouseClickMsg(mouse(diff.X+1, diff.Y+diff.H-1, tea.MouseLeft)))
	if m.App.DiffState.CursorLine != before {
		t.Error("clicking empty space must not move the cursor")
	}
}

func TestRightClickIsIgnored(t *testing.T) {
	m := mouseModel(t)
	diff := paneOf(t, m, app.PanelDiff)
	before := m.App.DiffState.CursorLine

	m.Update(tea.MouseClickMsg(mouse(diff.X+1, diff.Y+3, tea.MouseRight)))
	if m.App.DiffState.CursorLine != before {
		t.Error("only the left button acts")
	}
}

func TestDragSelectsARangeAndYankCopiesIt(t *testing.T) {
	m := mouseModel(t)
	diff := paneOf(t, m, app.PanelDiff)

	m.Update(tea.MouseClickMsg(mouse(diff.X+8, diff.Y+3, tea.MouseLeft)))
	if m.App.VisualSelection == nil {
		t.Fatal("a press must start a selection")
	}
	if !m.dragging {
		t.Error("a press must arm the drag")
	}

	m.Update(tea.MouseMotionMsg(mouse(diff.X+8, diff.Y+4, tea.MouseLeft)))
	sel := m.App.VisualSelection
	if sel.Head.AnnotationIdx == sel.Anchor.AnnotationIdx {
		t.Error("dragging must extend the selection past its anchor")
	}

	m.Update(tea.MouseReleaseMsg(mouse(diff.X+8, diff.Y+4, tea.MouseLeft)))
	if m.dragging {
		t.Error("release must end the drag")
	}
	if m.App.VisualSelection == nil {
		t.Fatal("the selection must survive release so y can copy it")
	}

	// y copies the highlighted text rather than exporting the review.
	pressRune(m, 'y')
	if m.App.VisualSelection != nil {
		t.Error("yanking must clear the selection")
	}
	if m.App.Message == nil || !strings.Contains(m.App.Message.Content, "Yanked") {
		t.Errorf("y over a drag selection must yank it, got %+v", m.App.Message)
	}
}

func TestMotionWithoutAPressDoesNotSelect(t *testing.T) {
	m := mouseModel(t)
	diff := paneOf(t, m, app.PanelDiff)

	m.Update(tea.MouseMotionMsg(mouse(diff.X+8, diff.Y+4, tea.MouseNone)))
	if m.App.VisualSelection != nil {
		t.Error("hovering must not start a selection")
	}
}

func TestYankWithoutASelectionStillExportsTheReview(t *testing.T) {
	m := mouseModel(t)
	m.export.ToStdout = true
	addReviewComment(m, "something to export")
	pressRune(m, 'y')
	if m.PendingStdout == "" {
		t.Error("y with nothing highlighted must export the review")
	}
}

func TestClickingACommitTogglesTheSelection(t *testing.T) {
	m := testModel(t)
	m.height = 40
	m.mouseEnabled = true
	addCommits(m, 3)
	m.syncViewport()
	_ = m.View()

	strip := paneOf(t, m, app.PanelCommitSelector)
	before := m.App.CommitSelectionRange

	m.Update(tea.MouseClickMsg(mouse(strip.X+2, strip.Y+1, tea.MouseLeft)))
	if m.App.FocusedPanel != app.PanelCommitSelector {
		t.Error("clicking the strip must focus it")
	}
	if m.App.CommitSelectionRange == before {
		t.Error("clicking a commit must change the selection")
	}
}

func TestClickingANavigatorRowJumps(t *testing.T) {
	m := testModel(t)
	m.height = 40
	m.mouseEnabled = true
	addReviewComment(m, "first")
	addReviewComment(m, "second")
	m.syncViewport()
	_ = m.View()

	nav := paneOf(t, m, app.PanelComments)
	m.Update(tea.MouseClickMsg(mouse(nav.X+1, nav.Y+1, tea.MouseLeft)))

	if m.App.CommentNav.Cursor != 1 {
		t.Errorf("the clicked row must become the cursor, got %d", m.App.CommentNav.Cursor)
	}
	if m.App.FocusedPanel != app.PanelDiff {
		t.Error("selecting a comment must jump to it in the diff")
	}
}

func TestMouseIgnoredInModalStates(t *testing.T) {
	m := mouseModel(t)
	diff := paneOf(t, m, app.PanelDiff)
	before := m.App.DiffState.ScrollOffset

	for _, mode := range []struct {
		name string
		set  func()
	}{
		{"help", func() { m.App.ToggleHelp() }},
		{"selector", func() { m.App.InputMode = input.ModeCommitSelect }},
	} {
		m.App.InputMode = input.ModeNormal
		mode.set()
		m.Update(tea.MouseWheelMsg(mouse(diff.X+1, diff.Y+1, tea.MouseWheelDown)))
		if m.App.DiffState.ScrollOffset != before {
			t.Errorf("%s owns the frame; clicking through it must do nothing", mode.name)
		}
	}
}
