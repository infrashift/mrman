package app

import (
	"testing"

	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs"
)

// withComment gives the app one review comment so the navigator has an item.
func withComment(a *App) {
	a.Session.ReviewComments = append(a.Session.ReviewComments,
		model.NewComment("look here", model.CommentTypeFromID("note"), nil))
	a.RebuildAnnotations()
}

// withCommits gives the app a multi-commit inline selector.
func withCommits(a *App, n int) {
	rows := make([]vcs.CommitInfo, n)
	for i := range rows {
		id := string(rune('a' + i))
		rows[i] = vcs.CommitInfo{ID: id, ShortID: id, Summary: "commit " + id}
	}
	a.ReviewCommits = rows
	a.CommitList = append([]vcs.CommitInfo(nil), rows...)
	a.VisibleCommitCount = n
	a.ShowCommitSelector = true
	a.DiffSource = DiffSource{Kind: DiffSourceCommitRange}
}

func TestPanelVisibilityGatesFocus(t *testing.T) {
	a := newTestApp(t)

	// Nothing to navigate and one commit: only tree and diff exist.
	if a.PanelVisible(PanelComments) {
		t.Error("the navigator must be hidden with no comments")
	}
	if a.PanelVisible(PanelCommitSelector) {
		t.Error("the commit strip must be hidden on a single-commit review")
	}
	if !a.PanelVisible(PanelFileList) || !a.PanelVisible(PanelDiff) {
		t.Error("the tree and diff are always available here")
	}

	withComment(a)
	withCommits(a, 3)
	if !a.PanelVisible(PanelComments) || !a.PanelVisible(PanelCommitSelector) {
		t.Error("both panes must appear once they have content")
	}

	// The navigator lives inside the file-list column, so it goes with it.
	a.ShowFileList = false
	if a.PanelVisible(PanelComments) || a.PanelVisible(PanelFileList) {
		t.Error("hiding the file list must hide the navigator with it")
	}
}

func TestCyclePanelFocusSkipsHiddenPanesAndWraps(t *testing.T) {
	a := newTestApp(t)
	a.FocusPanel(PanelDiff)

	// Only tree and diff are visible, so Tab toggles between them.
	a.CyclePanelFocus(1)
	if a.FocusedPanel != PanelFileList {
		t.Fatalf("Tab from the diff must reach the tree, got %v", a.FocusedPanel)
	}
	a.CyclePanelFocus(1)
	if a.FocusedPanel != PanelDiff {
		t.Fatalf("Tab must skip the hidden navigator, got %v", a.FocusedPanel)
	}

	withComment(a)
	withCommits(a, 3)
	a.FocusPanel(PanelCommitSelector)
	for _, want := range []FocusedPanel{PanelFileList, PanelComments, PanelDiff, PanelCommitSelector} {
		a.CyclePanelFocus(1)
		if a.FocusedPanel != want {
			t.Fatalf("ring order broke: got %v, want %v", a.FocusedPanel, want)
		}
	}

	// Backwards walks the ring in reverse.
	a.CyclePanelFocus(-1)
	if a.FocusedPanel != PanelDiff {
		t.Errorf("Shift-Tab from the strip must wrap to the diff, got %v", a.FocusedPanel)
	}
}

func TestMovePanelFocusStopsAtTheEnds(t *testing.T) {
	a := newTestApp(t)
	withComment(a)
	withCommits(a, 3)

	a.FocusPanel(PanelCommitSelector)
	a.MovePanelFocus(-1)
	if a.FocusedPanel != PanelCommitSelector {
		t.Error("leader-k at the top must stay put rather than wrap across the layout")
	}

	a.FocusPanel(PanelDiff)
	a.MovePanelFocus(1)
	if a.FocusedPanel != PanelDiff {
		t.Error("leader-j at the bottom must stay put")
	}

	a.FocusPanel(PanelFileList)
	a.MovePanelFocus(1)
	if a.FocusedPanel != PanelComments {
		t.Errorf("leader-j must step down the column, got %v", a.FocusedPanel)
	}
}

func TestFocusPanelFallsBackToTheDiffWhenHidden(t *testing.T) {
	a := newTestApp(t)
	a.ShowFileList = false
	a.FocusPanel(PanelFileList)
	if a.FocusedPanel != PanelDiff {
		t.Error("focusing a hidden pane must land somewhere the user can see")
	}
}

func TestCommentNavCursorMovesAndClamps(t *testing.T) {
	a := newTestApp(t)
	for i := 0; i < 3; i++ {
		a.Session.ReviewComments = append(a.Session.ReviewComments,
			model.NewComment("c", model.CommentTypeFromID("note"), nil))
	}
	a.RebuildAnnotations()
	a.CommentNav.ViewportHeight = 2

	a.CommentNavDown()
	a.CommentNavDown()
	if a.CommentNav.Cursor != 2 {
		t.Fatalf("cursor = %d, want 2", a.CommentNav.Cursor)
	}
	if a.CommentNav.Offset != 1 {
		t.Errorf("the window must follow the cursor, offset = %d", a.CommentNav.Offset)
	}

	// Walking past the end stops.
	for i := 0; i < 5; i++ {
		a.CommentNavDown()
	}
	if a.CommentNav.Cursor != 2 {
		t.Errorf("cursor must clamp to the last item, got %d", a.CommentNav.Cursor)
	}

	// Deleting comments underneath the cursor must not strand it.
	a.Session.ReviewComments = a.Session.ReviewComments[:1]
	a.RebuildAnnotations()
	a.clampCommentNavCursor()
	if a.CommentNav.Cursor != 0 {
		t.Errorf("cursor must clamp when items disappear, got %d", a.CommentNav.Cursor)
	}

	for i := 0; i < 3; i++ {
		a.CommentNavUp()
	}
	if a.CommentNav.Cursor != 0 || a.CommentNav.Offset != 0 {
		t.Error("walking up past the start must settle at the top")
	}
}

func TestCommentNavSelectJumpsAndFocusesTheDiff(t *testing.T) {
	a := newTestApp(t)
	withComment(a)
	a.FocusPanel(PanelComments)
	a.DiffState.CursorLine = 0

	a.CommentNavSelect()

	if a.FocusedPanel != PanelDiff {
		t.Error("selecting a comment must move focus to where it lives")
	}
	items := a.BuildCommentNavigatorItems()
	if a.DiffState.CursorLine != items[0].TargetAnnotation {
		t.Errorf("cursor = %d, want the comment's first row %d",
			a.DiffState.CursorLine, items[0].TargetAnnotation)
	}
}

func TestCommentNavSelectIsANoopWithNoItems(t *testing.T) {
	a := newTestApp(t)
	before := a.DiffState.CursorLine
	a.CommentNavSelect()
	if a.DiffState.CursorLine != before {
		t.Error("selecting with an empty navigator must do nothing")
	}
}
