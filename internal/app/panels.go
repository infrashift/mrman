// panels.go owns which panes exist and which one has focus, porting the
// FocusedPanel half of tuicr's src/app/mod.rs plus the comment navigator's
// cursor state.
//
// Focus lives here rather than in the UI layer because whether a pane can
// take focus depends on state the UI does not own — whether the file list
// is shown, whether the review has more than one commit, whether any
// comment exists to navigate.
package app

// Panel layout constants, matching tuicr's src/ui/app_layout.rs.
const (
	// CommitStripMaxHeight caps the inline commit selector, including its
	// two border rows.
	CommitStripMaxHeight = 8
	// CommentNavMaxHeight and CommentNavMinHeight bound the comment
	// navigator inside the left column, borders included.
	CommentNavMaxHeight = 12
	CommentNavMinHeight = 4
	// CommentNavMinTotalHeight is the terminal height below which the left
	// column is too short to split and the navigator is dropped.
	CommentNavMinTotalHeight = 8
	// FileTreeMinHeight is the room the file tree keeps when the navigator
	// shares its column.
	FileTreeMinHeight = 4
)

// focusRing is the panel order Tab walks, top to bottom then left to
// right, matching the visual layout.
var focusRing = []FocusedPanel{
	PanelCommitSelector,
	PanelFileList,
	PanelComments,
	PanelDiff,
}

// PanelVisible reports whether a panel is currently on screen and can
// therefore take focus.
func (a *App) PanelVisible(panel FocusedPanel) bool {
	switch panel {
	case PanelDiff:
		return true
	case PanelFileList:
		return a.ShowFileList
	case PanelComments:
		// The navigator lives inside the file-list column, so it goes away
		// with it — and with nothing to navigate.
		return a.ShowFileList && a.HasCommentNavigatorItems()
	case PanelCommitSelector:
		return a.HasInlineCommitSelector()
	}
	return false
}

// CyclePanelFocus moves focus to the next visible panel in the ring,
// wrapping. dir is +1 forward (Tab) or -1 backward (Shift-Tab).
func (a *App) CyclePanelFocus(dir int) {
	a.moveFocus(dir, true)
}

// MovePanelFocus moves focus one visible panel up or down the ring without
// wrapping, for the leader j/k chords: walking off either end should stop,
// not teleport across the layout.
func (a *App) MovePanelFocus(dir int) {
	a.moveFocus(dir, false)
}

func (a *App) moveFocus(dir int, wrap bool) {
	if dir == 0 {
		return
	}
	start := focusRingIndex(a.FocusedPanel)
	n := len(focusRing)
	for step := 1; step <= n; step++ {
		idx := start + dir*step
		if wrap {
			idx = ((idx % n) + n) % n
		} else if idx < 0 || idx >= n {
			return // walked off the end: stay put
		}
		if a.PanelVisible(focusRing[idx]) {
			a.FocusPanel(focusRing[idx])
			return
		}
	}
}

// focusRingIndex is the ring position of a panel, 0 when unknown.
func focusRingIndex(panel FocusedPanel) int {
	for i, p := range focusRing {
		if p == panel {
			return i
		}
	}
	return 0
}

// FocusPanel focuses a panel, falling back to the diff when it is not
// visible so focus never lands somewhere the user cannot see.
func (a *App) FocusPanel(panel FocusedPanel) {
	if !a.PanelVisible(panel) {
		a.FocusedPanel = PanelDiff
		return
	}
	a.FocusedPanel = panel
	if panel == PanelComments {
		a.clampCommentNavCursor()
	}
}

// --- Comment navigator cursor ---

// CommentNavState is the comment navigator pane's cursor and scroll window.
type CommentNavState struct {
	Cursor int
	Offset int
	// ViewportHeight is the pane's visible row count, set by the renderer.
	ViewportHeight int
}

// clampCommentNavCursor keeps the cursor and scroll window inside the item
// list, which changes whenever a comment is added, deleted or filtered out.
func (a *App) clampCommentNavCursor() {
	count := len(a.BuildCommentNavigatorItems())
	if count == 0 {
		a.CommentNav = CommentNavState{ViewportHeight: a.CommentNav.ViewportHeight}
		return
	}
	a.CommentNav.Cursor = min(max(a.CommentNav.Cursor, 0), count-1)
	if a.CommentNav.Offset > a.CommentNav.Cursor {
		a.CommentNav.Offset = a.CommentNav.Cursor
	}
	if h := a.CommentNav.ViewportHeight; h > 0 && a.CommentNav.Cursor >= a.CommentNav.Offset+h {
		a.CommentNav.Offset = a.CommentNav.Cursor - h + 1
	}
}

// CommentNavDown moves the navigator cursor down one item.
func (a *App) CommentNavDown() {
	count := len(a.BuildCommentNavigatorItems())
	if a.CommentNav.Cursor < count-1 {
		a.CommentNav.Cursor++
	}
	a.clampCommentNavCursor()
}

// CommentNavUp moves the navigator cursor up one item.
func (a *App) CommentNavUp() {
	if a.CommentNav.Cursor > 0 {
		a.CommentNav.Cursor--
	}
	a.clampCommentNavCursor()
}

// CommentNavSelect jumps the diff cursor to the highlighted comment. Focus
// follows the jump to the diff, which is where the user wants to be once
// they have found the comment.
func (a *App) CommentNavSelect() {
	items := a.BuildCommentNavigatorItems()
	if a.CommentNav.Cursor < 0 || a.CommentNav.Cursor >= len(items) {
		return
	}
	a.jumpToCommentItem(&items[a.CommentNav.Cursor])
}
