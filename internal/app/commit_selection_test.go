package app

// commit_selection_test.go ports tuicr's
// src/app/tests/commit_selection_tests.rs in full, plus tests pinning the
// sweep-toggle, cycling, and paged load-more semantics of commits.go.

import (
	"slices"
	"testing"

	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs"
)

// buildCommitListApp mirrors the Rust suite's build_app: a working-tree app
// whose selector shows the given commit list in commit-select mode.
func buildCommitListApp(commits ...vcs.CommitInfo) *App {
	a := buildAppWithFiles(nil, 0)
	a.CommitList = commits
	a.VisibleCommitCount = len(commits)
	a.InputMode = input.ModeCommitSelect
	return a
}

func rangeOf(start, end int) *model.IndexRange {
	return &model.IndexRange{start, end}
}

func assertRange(t *testing.T, got *model.IndexRange, want *model.IndexRange, msg string) {
	t.Helper()
	switch {
	case want == nil && got != nil:
		t.Errorf("%s: got %v, want nil", msg, *got)
	case want != nil && got == nil:
		t.Errorf("%s: got nil, want %v", msg, *want)
	case want != nil && got != nil && *got != *want:
		t.Errorf("%s: got %v, want %v", msg, *got, *want)
	}
}

func TestSpecialCommitCountCountsLeadingSpecialEntries(t *testing.T) {
	a := buildCommitListApp(stagedCommitEntry(), unstagedCommitEntry(), commit("abc123"))
	assertEq(t, a.specialCommitCount(), 2, "leading special entries")
	assertEq(t, a.loadedHistoryCommitCount(), 1, "history commits")
}

func TestSpecialCommitCountIgnoresNonLeadingSpecialEntries(t *testing.T) {
	a := buildCommitListApp(commit("abc123"), stagedCommitEntry())
	assertEq(t, a.specialCommitCount(), 0, "non-leading special entries")
}

func TestToggleCommitSelectionFromAllSelectedSelectsOnlyCursor(t *testing.T) {
	for cursor := range 3 {
		a := buildCommitListApp(commit("abc123"), commit("def456"), commit("789abc"))
		a.CommitSelectionRange = rangeOf(0, 2)
		a.CommitListCursor = cursor

		a.ToggleCommitSelection()

		assertRange(t, a.CommitSelectionRange, rangeOf(cursor, cursor), "all-selected collapse")
	}
}

func TestToggleCommitSelectionKeepsPartialRangeShrinkBehavior(t *testing.T) {
	a := buildCommitListApp(commit("abc123"), commit("def456"), commit("789abc"))
	a.CommitSelectionRange = rangeOf(0, 1)
	a.CommitListCursor = 0

	a.ToggleCommitSelection()

	assertRange(t, a.CommitSelectionRange, rangeOf(1, 1), "start-edge shrink")
}

func TestInitialCommitRangeAllSelectsFullSpan(t *testing.T) {
	assertRange(t, InitialCommitRange(CommitSelectionAll, 4), rangeOf(0, 3), "all")
}

func TestInitialCommitRangeOldestSelectsLastIndex(t *testing.T) {
	// Commits are stored newest-first, so the oldest commit is the last
	// index regardless of the display order.
	assertRange(t, InitialCommitRange(CommitSelectionOldest, 4), rangeOf(3, 3), "oldest")
}

func TestInitialCommitRangeEmptyIsNone(t *testing.T) {
	assertRange(t, InitialCommitRange(CommitSelectionOldest, 0), nil, "oldest empty")
	assertRange(t, InitialCommitRange(CommitSelectionAll, 0), nil, "all empty")
}

func TestCommitDataIndexIsIdentityWhenDescending(t *testing.T) {
	a := buildCommitListApp(commit("a"), commit("b"), commit("c"))
	a.ReviewCommits = slices.Clone(a.CommitList)
	a.CommitOrder = CommitDescending
	for i := range 3 {
		assertEq(t, a.CommitDataIndex(i), i, "descending identity")
	}
}

func TestCommitDataIndexMirrorsAndRoundTripsWhenAscending(t *testing.T) {
	a := buildCommitListApp(commit("a"), commit("b"), commit("c"))
	a.ReviewCommits = slices.Clone(a.CommitList)
	a.CommitOrder = CommitAscending
	assertEq(t, a.CommitDataIndex(0), 2, "ascending mirror 0")
	assertEq(t, a.CommitDataIndex(2), 0, "ascending mirror 2")
	// The mapping is its own inverse (data <-> display row).
	for i := range 3 {
		assertEq(t, a.CommitDataIndex(a.CommitDataIndex(i)), i, "round trip")
	}
}

func TestToggleCommitSelectorFlipsVisibilityAndDropsFocus(t *testing.T) {
	a := buildCommitListApp(commit("a"), commit("b"))
	a.ShowCommitSelector = true
	a.FocusedPanel = PanelCommitSelector

	a.ToggleCommitSelector()
	assertEq(t, a.ShowCommitSelector, false, "selector hidden")
	// Hiding the focused pane returns focus to the diff.
	assertEq(t, a.FocusedPanel, PanelDiff, "focus falls back to diff")

	a.ToggleCommitSelector()
	assertEq(t, a.ShowCommitSelector, true, "selector visible again")
}

func TestHasReviewCommitsIgnoresVisibilityButRequiresMultipleNonWorktree(t *testing.T) {
	a := buildCommitListApp(commit("a"), commit("b"))
	a.ReviewCommits = slices.Clone(a.CommitList)
	a.DiffSource = DiffSource{Kind: DiffSourceCommitRange, Commits: []string{"a", "b"}}

	a.ShowCommitSelector = false
	assertEq(t, a.HasReviewCommits(), true, "hidden pane still has review commits")
	assertEq(t, a.HasInlineCommitSelector(), false, "hidden pane does not render")

	a.ShowCommitSelector = true
	assertEq(t, a.HasInlineCommitSelector(), true, "visible pane renders")

	// Working-tree reviews never cycle commits.
	a.DiffSource = DiffSource{Kind: DiffSourceWorkingTree}
	assertEq(t, a.HasReviewCommits(), false, "working tree has no review commits")
}

func TestCommitSelectionSummaryShowsPositionForSingleAndCountForRange(t *testing.T) {
	a := buildCommitListApp(commit("a"), commit("b"), commit("c"))
	a.ReviewCommits = slices.Clone(a.CommitList)

	// Whole range selected -> no summary (caller shows the plain total).
	a.CommitSelectionRange = rangeOf(0, 2)
	assertEq(t, a.CommitSelectionSummary(), "", "full range has no summary")

	// Single commit, descending: position == data index + 1, so cycling moves it.
	a.CommitOrder = CommitDescending
	a.CommitSelectionRange = rangeOf(0, 0)
	assertEq(t, a.CommitSelectionSummary(), "commit 1/3", "descending first")
	a.CommitSelectionRange = rangeOf(2, 2)
	assertEq(t, a.CommitSelectionSummary(), "commit 3/3", "descending last")

	// Single commit, ascending: display position is mirrored (top row == 1).
	a.CommitOrder = CommitAscending
	a.CommitSelectionRange = rangeOf(0, 0) // newest -> bottom row
	assertEq(t, a.CommitSelectionSummary(), "commit 3/3", "ascending newest")
	a.CommitSelectionRange = rangeOf(2, 2) // oldest -> top row
	assertEq(t, a.CommitSelectionSummary(), "commit 1/3", "ascending oldest")

	// Multi-commit subrange -> selected count.
	a.CommitOrder = CommitDescending
	a.CommitSelectionRange = rangeOf(0, 1)
	assertEq(t, a.CommitSelectionSummary(), "2 of 3 commits", "subrange count")
}

// --- Toggle semantics beyond the Rust suite (pinning the port) ---

func TestToggleCommitSelectionWithNoSelectionSelectsCursor(t *testing.T) {
	a := buildCommitListApp(commit("a"), commit("b"), commit("c"))
	a.CommitListCursor = 1
	a.ToggleCommitSelection()
	assertRange(t, a.CommitSelectionRange, rangeOf(1, 1), "first toggle selects cursor")
}

func TestToggleCommitSelectionSingleSelectedDeselectsAll(t *testing.T) {
	a := buildCommitListApp(commit("a"), commit("b"), commit("c"))
	a.CommitSelectionRange = rangeOf(1, 1)
	a.CommitListCursor = 1
	a.ToggleCommitSelection()
	assertRange(t, a.CommitSelectionRange, nil, "single selection clears")
}

func TestToggleCommitSelectionEndEdgeShrinksFromEnd(t *testing.T) {
	a := buildCommitListApp(commit("a"), commit("b"), commit("c"), commit("d"), commit("e"))
	a.CommitSelectionRange = rangeOf(1, 3)
	a.CommitListCursor = 3
	a.ToggleCommitSelection()
	assertRange(t, a.CommitSelectionRange, rangeOf(1, 2), "end-edge shrink")
}

func TestToggleCommitSelectionMiddlePressTruncates(t *testing.T) {
	a := buildCommitListApp(commit("a"), commit("b"), commit("c"), commit("d"), commit("e"))
	a.CommitSelectionRange = rangeOf(1, 4)
	a.CommitListCursor = 2
	a.ToggleCommitSelection()
	assertRange(t, a.CommitSelectionRange, rangeOf(1, 1), "middle press truncates to (start, cursor-1)")
}

func TestToggleCommitSelectionOutsideExtends(t *testing.T) {
	a := buildCommitListApp(commit("a"), commit("b"), commit("c"), commit("d"), commit("e"))
	a.CommitSelectionRange = rangeOf(1, 2)
	a.CommitListCursor = 4
	a.ToggleCommitSelection()
	assertRange(t, a.CommitSelectionRange, rangeOf(1, 4), "extend below")

	a.CommitListCursor = 0
	a.ToggleCommitSelection()
	assertRange(t, a.CommitSelectionRange, rangeOf(0, 4), "extend above")
}

func TestToggleCommitSelectionIgnoresCursorPastList(t *testing.T) {
	a := buildCommitListApp(commit("a"), commit("b"))
	a.CommitListCursor = 2 // e.g. the "show more" row
	a.ToggleCommitSelection()
	assertRange(t, a.CommitSelectionRange, nil, "cursor past list is a no-op")
}

func TestToggleCommitSelectionAndAdvanceSweeps(t *testing.T) {
	a := buildCommitListApp(commit("a"), commit("b"), commit("c"), commit("d"))

	a.ToggleCommitSelectionAndAdvance()
	assertRange(t, a.CommitSelectionRange, rangeOf(0, 0), "sweep step 1 range")
	assertEq(t, a.CommitListCursor, 1, "sweep step 1 cursor advanced")

	a.ToggleCommitSelectionAndAdvance()
	assertRange(t, a.CommitSelectionRange, rangeOf(0, 1), "sweep step 2 range")
	assertEq(t, a.CommitListCursor, 2, "sweep step 2 cursor advanced")
}

func TestToggleCommitSelectionAndAdvanceStaysOnDeselect(t *testing.T) {
	a := buildCommitListApp(commit("a"), commit("b"), commit("c"))
	a.CommitSelectionRange = rangeOf(0, 1)
	a.CommitListCursor = 0

	// An edge press deselects; not a sweep, so the cursor stays put.
	a.ToggleCommitSelectionAndAdvance()
	assertRange(t, a.CommitSelectionRange, rangeOf(1, 1), "edge press shrinks")
	assertEq(t, a.CommitListCursor, 0, "cursor stays on deselect")
}

func TestToggleCommitSelectionAndAdvanceStopsAtListEnd(t *testing.T) {
	a := buildCommitListApp(commit("a"), commit("b"))
	a.CommitListCursor = 1

	a.ToggleCommitSelectionAndAdvance()
	assertRange(t, a.CommitSelectionRange, rangeOf(1, 1), "last row selected")
	assertEq(t, a.CommitListCursor, 1, "cursor cannot advance past the end")
}

func TestIsCommitSelected(t *testing.T) {
	a := buildCommitListApp(commit("a"), commit("b"), commit("c"), commit("d"))
	assertEq(t, a.IsCommitSelected(0), false, "no selection")
	a.CommitSelectionRange = rangeOf(1, 2)
	assertEq(t, a.IsCommitSelected(0), false, "before range")
	assertEq(t, a.IsCommitSelected(1), true, "range start")
	assertEq(t, a.IsCommitSelected(2), true, "range end")
	assertEq(t, a.IsCommitSelected(3), false, "after range")
}

func TestIsStrictCommitSelection(t *testing.T) {
	assertEq(t, IsStrictCommitSelection(nil, 3), false, "nil range")
	assertEq(t, IsStrictCommitSelection(rangeOf(0, 2), 3), false, "full range")
	assertEq(t, IsStrictCommitSelection(rangeOf(0, 1), 3), true, "leading subrange")
	assertEq(t, IsStrictCommitSelection(rangeOf(1, 2), 3), true, "trailing subrange")
	assertEq(t, IsStrictCommitSelection(rangeOf(1, 1), 3), true, "single middle")
	assertEq(t, IsStrictCommitSelection(rangeOf(0, 0), 1), false, "single commit full")
	assertEq(t, IsStrictCommitSelection(rangeOf(2, 1), 3), false, "inverted range")
	assertEq(t, IsStrictCommitSelection(rangeOf(1, 3), 3), false, "end out of bounds")
	assertEq(t, IsStrictCommitSelection(rangeOf(0, 0), 0), false, "empty list")
}

// --- ( / ) cycling ---

func buildInlineSelectorApp(n int) *App {
	commits := make([]vcs.CommitInfo, 0, n)
	for i := range n {
		commits = append(commits, commit(string(rune('a'+i))))
	}
	a := buildCommitListApp(commits...)
	a.ReviewCommits = slices.Clone(commits)
	return a
}

func TestCycleCommitNextSemantics(t *testing.T) {
	a := buildInlineSelectorApp(3)

	// None selected -> select all.
	a.CycleCommitNext()
	assertRange(t, a.CommitSelectionRange, rangeOf(0, 2), "none -> all")

	// all -> last.
	a.CycleCommitNext()
	assertRange(t, a.CommitSelectionRange, rangeOf(2, 2), "all -> last")
	assertEq(t, a.CommitListCursor, 2, "cursor follows last")

	// last -> all.
	a.CycleCommitNext()
	assertRange(t, a.CommitSelectionRange, rangeOf(0, 2), "last -> all")

	// i -> i+1.
	a.CommitSelectionRange = rangeOf(0, 0)
	a.CycleCommitNext()
	assertRange(t, a.CommitSelectionRange, rangeOf(1, 1), "i -> i+1")
	assertEq(t, a.CommitListCursor, 1, "cursor follows i+1")

	// Multi-commit subrange -> last of that range.
	a.CommitSelectionRange = rangeOf(0, 1)
	a.CycleCommitNext()
	assertRange(t, a.CommitSelectionRange, rangeOf(1, 1), "subrange -> its last")
}

func TestCycleCommitPrevSemantics(t *testing.T) {
	a := buildInlineSelectorApp(3)

	// None selected -> select all.
	a.CycleCommitPrev()
	assertRange(t, a.CommitSelectionRange, rangeOf(0, 2), "none -> all")

	// all -> first.
	a.CycleCommitPrev()
	assertRange(t, a.CommitSelectionRange, rangeOf(0, 0), "all -> first")
	assertEq(t, a.CommitListCursor, 0, "cursor follows first")

	// first -> all.
	a.CycleCommitPrev()
	assertRange(t, a.CommitSelectionRange, rangeOf(0, 2), "first -> all")

	// i -> i-1.
	a.CommitSelectionRange = rangeOf(2, 2)
	a.CycleCommitPrev()
	assertRange(t, a.CommitSelectionRange, rangeOf(1, 1), "i -> i-1")
	assertEq(t, a.CommitListCursor, 1, "cursor follows i-1")

	// Multi-commit subrange -> first of that range.
	a.CommitSelectionRange = rangeOf(1, 2)
	a.CycleCommitPrev()
	assertRange(t, a.CommitSelectionRange, rangeOf(1, 1), "subrange -> its first")
}

func TestCycleCommitNoopWithoutReviewCommits(t *testing.T) {
	a := buildCommitListApp(commit("a"))
	a.CycleCommitNext()
	assertRange(t, a.CommitSelectionRange, nil, "next without review commits")
	a.CycleCommitPrev()
	assertRange(t, a.CommitSelectionRange, nil, "prev without review commits")
}

// --- Cursor movement / expand row ---

func TestCommitSelectUpDownMovesCursorAndScrolls(t *testing.T) {
	a := buildCommitListApp(commit("a"), commit("b"), commit("c"), commit("d"))
	a.CommitListViewportHeight = 2

	a.CommitSelectDown()
	a.CommitSelectDown()
	assertEq(t, a.CommitListCursor, 2, "cursor moved down")
	assertEq(t, a.CommitListScrollOffset, 1, "scrolled to keep cursor visible")

	a.CommitSelectDown()
	assertEq(t, a.CommitListCursor, 3, "cursor at last row")
	a.CommitSelectDown()
	assertEq(t, a.CommitListCursor, 3, "cursor clamped at last row")

	a.CommitListScrollOffset = 3
	a.CommitSelectUp()
	assertEq(t, a.CommitListCursor, 2, "cursor moved up")
	assertEq(t, a.CommitListScrollOffset, 2, "scrolled up with cursor")

	a.CommitSelectUp()
	a.CommitSelectUp()
	a.CommitSelectUp()
	assertEq(t, a.CommitListCursor, 0, "cursor clamped at top")
}

func TestCommitSelectDownReachesExpandRow(t *testing.T) {
	a := buildCommitListApp(commit("a"), commit("b"))
	a.HasMoreCommits = true

	assertEq(t, a.CanShowMoreCommits(), true, "more commits available")
	assertEq(t, a.CommitSelectRowCount(), 3, "rows include the show-more row")

	a.CommitSelectDown()
	a.CommitSelectDown()
	assertEq(t, a.CommitListCursor, 2, "cursor on the expand row")
	assertEq(t, a.IsOnExpandRow(), true, "expand row detected")

	a.CommitSelectDown()
	assertEq(t, a.CommitListCursor, 2, "cursor clamped on expand row")
}

func TestExpandCommitRevealsLoadedCommitsFirst(t *testing.T) {
	commits := make([]vcs.CommitInfo, 0, 25)
	for i := range 25 {
		commits = append(commits, commit(string(rune('a'+i))))
	}
	a := buildCommitListApp(commits...)
	a.VisibleCommitCount = 10

	if err := a.ExpandCommit(); err != nil {
		t.Fatal(err)
	}
	assertEq(t, a.VisibleCommitCount, 20, "first page revealed")
	if err := a.ExpandCommit(); err != nil {
		t.Fatal(err)
	}
	assertEq(t, a.VisibleCommitCount, 25, "final partial page clamps to the list")
}

func TestExpandCommitWithoutMoreWarns(t *testing.T) {
	a := buildCommitListApp(commit("a"))
	a.HasMoreCommits = false
	if err := a.ExpandCommit(); err != nil {
		t.Fatal(err)
	}
	if a.Message == nil || a.Message.Content != "No more commits" {
		t.Fatalf("expected 'No more commits' message, got %v", a.Message)
	}
}
