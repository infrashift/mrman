package app

import (
	"fmt"
	"testing"

	"github.com/infrashift/mrman/internal/vcs"
)

// scrollTreeApp builds a flat tree with more rows than fit on screen.
func scrollTreeApp(files, viewport int) *App {
	paths := make([]string, files)
	for i := range paths {
		paths[i] = fmt.Sprintf("file%02d.go", i)
	}
	a := treeApp(paths...)
	a.FileListState.ViewportHeight = viewport
	return a
}

// TestFileTreeScrollsWithItsSelection is the property that made the tree
// unusable on a large review: only the mouse wheel ever moved the offset,
// so keyboard navigation walked the selection off the bottom of the pane
// and the tree appeared frozen at its first screenful.
func TestFileTreeScrollsWithItsSelection(t *testing.T) {
	const files, viewport = 40, 10
	a := scrollTreeApp(files, viewport)
	total := len(a.BuildVisibleItems())

	for step := 0; step < total+5; step++ {
		selected := a.FileListState.Selected()
		offset := a.FileListState.Offset()
		if selected < offset || selected >= offset+viewport {
			t.Fatalf("after %d downs the selection %d is outside the visible window [%d,%d)",
				step, selected, offset, offset+viewport)
		}
		a.FileListDown(1)
	}

	// The selection must have reached the end, or it never scrolled at all.
	if got := a.FileListState.Selected(); got != total-1 {
		t.Errorf("selection stopped at %d, want the last row %d", got, total-1)
	}
	if a.FileListState.Offset() == 0 {
		t.Error("the viewport never scrolled")
	}

	for step := 0; step < total+5; step++ {
		a.FileListUp(1)
		selected := a.FileListState.Selected()
		offset := a.FileListState.Offset()
		if selected < offset || selected >= offset+viewport {
			t.Fatalf("after %d ups the selection %d is outside the visible window [%d,%d)",
				step, selected, offset, offset+viewport)
		}
	}
	if a.FileListState.Offset() != 0 {
		t.Errorf("returning to the top must scroll back, offset is %d", a.FileListState.Offset())
	}
}

// TestFileTreeNeverScrollsPastTheEnd keeps the last screenful full rather
// than leaving blank rows below the final file.
func TestFileTreeNeverScrollsPastTheEnd(t *testing.T) {
	const files, viewport = 40, 10
	a := scrollTreeApp(files, viewport)
	total := len(a.BuildVisibleItems())

	a.FileListDown(1000)
	if want := total - viewport; a.FileListState.Offset() != want {
		t.Errorf("offset %d, want %d — the last page must be full", a.FileListState.Offset(), want)
	}
}

// TestShortTreeNeverScrolls: a tree that fits needs no offset at all.
func TestShortTreeNeverScrolls(t *testing.T) {
	a := scrollTreeApp(3, 20)
	a.FileListDown(1000)
	if a.FileListState.Offset() != 0 {
		t.Errorf("a tree that fits must not scroll, offset is %d", a.FileListState.Offset())
	}
}

// stripApp builds a review spanning n commits with a strip too short to
// show them all.
func stripApp(n, viewport int) *App {
	a := scrollTreeApp(2, 10)
	commits := make([]vcs.CommitInfo, n)
	for i := range commits {
		commits[i] = vcs.CommitInfo{ID: fmt.Sprintf("%040d", i), ShortID: fmt.Sprintf("c%02d", i)}
	}
	a.InstallReviewCommits(commits)
	a.CommitListViewportHeight = viewport
	return a
}

// TestCommitStripFollowsTheCycledCommit covers ( and ) on a review with
// more commits than the strip can display. They move the selection a whole
// commit at a time but never scrolled, so past the sixth commit the panel
// showed no selected row at all while its title counted up to 12/12.
func TestCommitStripFollowsTheCycledCommit(t *testing.T) {
	const commits, viewport = 12, 6
	a := stripApp(commits, viewport)

	a.CycleCommitPrev() // all → first
	for step := 0; step < commits; step++ {
		cursor, offset := a.CommitListCursor, a.CommitListScrollOffset
		if cursor < offset || cursor >= offset+viewport {
			t.Fatalf("after %d ) the cursor %d is outside the strip window [%d,%d)",
				step, cursor, offset, offset+viewport)
		}
		a.CycleCommitNext()
	}

	a.CycleCommitPrev() // off the "all" wrap, back onto the last commit
	for step := 0; step < commits; step++ {
		cursor, offset := a.CommitListCursor, a.CommitListScrollOffset
		if cursor < offset || cursor >= offset+viewport {
			t.Fatalf("after %d ( the cursor %d is outside the strip window [%d,%d)",
				step, cursor, offset, offset+viewport)
		}
		a.CycleCommitPrev()
	}
}

// TestShortCommitStripNeverScrolls: a strip that fits stays at the top.
func TestShortCommitStripNeverScrolls(t *testing.T) {
	a := stripApp(3, 8)
	for i := 0; i < 6; i++ {
		a.CycleCommitNext()
	}
	if a.CommitListScrollOffset != 0 {
		t.Errorf("a strip that fits must not scroll, offset is %d", a.CommitListScrollOffset)
	}
}
