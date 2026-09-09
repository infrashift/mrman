package app

// scroll_behavior_test.go ports tuicr's src/app/tests/scroll_behavior_tests.rs
// and scroll_tests.rs in full.

import (
	"fmt"
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

// buildScrollApp builds a test App with a single file containing n context
// lines. Total rendered lines = 1 (review header) + 1 (file header) +
// 1 (hunk header) + n (diff lines) + 1 (spacing) = n + 4. The mock VCS
// reports zero-length files, so no EOF gap renders.
//
//nolint:unparam // mirrors the tuicr test builder; the ported suite always uses viewport 20
func buildScrollApp(n, viewport, scrollOffsetConfig int) *App {
	var lines []model.DiffLine
	for i := 1; i <= n; i++ {
		lines = append(lines, model.DiffLine{
			Origin:    model.OriginContext,
			Content:   fmt.Sprintf("line %d", i),
			OldLineno: new(uint32(i)),
			NewLineno: new(uint32(i)),
		})
	}

	hunk := model.DiffHunk{
		Header:   "@@ -1,N +1,N @@",
		Lines:    lines,
		OldStart: 1,
		OldCount: uint32(n),
		NewStart: 1,
		NewCount: uint32(n),
	}
	file := model.DiffFile{
		NewPath: new("test.rs"),
		Status:  model.StatusModified,
		Hunks:   []model.DiffHunk{hunk},
	}

	a := buildAppWithFiles([]model.DiffFile{file}, 0)
	a.DiffState.ViewportHeight = viewport
	a.DiffState.VisibleLineCount = viewport
	a.ScrollOffset = scrollOffsetConfig
	return a
}

func TestZzOnLastLineCentersCursor(t *testing.T) {
	// 40 diff lines + 4 overhead = 44 total. max_cursor = 42. Viewport = 20.
	a := buildScrollApp(40, 20, 5)
	assertEq(t, a.TotalLines(), 44, "total lines")
	last := a.MaxCursorLine() // 42

	a.DiffState.CursorLine = last
	a.CenterCursor()

	// scroll = cursor - viewport/2 = 42 - 10 = 32
	assertEq(t, a.DiffState.ScrollOffset, 32, "scroll after zz")
	assertEq(t, a.DiffState.CursorLine, 42, "cursor after zz")
}

func TestAfterZzOnLastLineJDoesNotChangeScroll(t *testing.T) {
	a := buildScrollApp(40, 20, 5)
	last := a.MaxCursorLine()

	a.DiffState.CursorLine = last
	a.CenterCursor()
	scrollAfterZz := a.DiffState.ScrollOffset

	// Press j — cursor is already at max, and it's centered.
	a.CursorDown(1)

	assertEq(t, a.DiffState.CursorLine, last, "cursor")
	assertEq(t, a.DiffState.ScrollOffset, scrollAfterZz,
		"j after zz on last line should not change scroll")
}

func TestAfterZzOnLastLineKDoesNotChangeScroll(t *testing.T) {
	a := buildScrollApp(40, 20, 5)
	last := a.MaxCursorLine()

	a.DiffState.CursorLine = last
	a.CenterCursor()
	scrollAfterZz := a.DiffState.ScrollOffset

	// Press k — cursor moves up 1, still in free zone.
	a.CursorUp(1)

	assertEq(t, a.DiffState.CursorLine, last-1, "cursor")
	assertEq(t, a.DiffState.ScrollOffset, scrollAfterZz,
		"k after zz on last line should not change scroll")
}

func TestAfterZzNoOscillationWithKThenJ(t *testing.T) {
	a := buildScrollApp(40, 20, 5)
	last := a.MaxCursorLine()

	a.DiffState.CursorLine = last
	a.CenterCursor()
	scrollAfterZz := a.DiffState.ScrollOffset

	// k then j should return to the same state.
	a.CursorUp(1)
	a.CursorDown(1)

	assertEq(t, a.DiffState.CursorLine, last, "cursor")
	assertEq(t, a.DiffState.ScrollOffset, scrollAfterZz,
		"k then j after zz should not cause oscillation")
}

func TestJScrollsOneLineAtATime(t *testing.T) {
	// Viewport 20, total 44. Start at the middle and scroll down.
	a := buildScrollApp(40, 20, 5)

	// Position cursor and scroll in steady state near the bottom margin.
	a.DiffState.CursorLine = 20
	a.DiffState.ScrollOffset = 6

	for range 10 {
		prevScroll := a.DiffState.ScrollOffset
		prevCursor := a.DiffState.CursorLine
		a.CursorDown(1)
		scrollDelta := a.DiffState.ScrollOffset - prevScroll
		cursorDelta := a.DiffState.CursorLine - prevCursor
		assertEq(t, cursorDelta, 1, "cursor should advance by exactly 1")
		if scrollDelta > 1 {
			t.Errorf("scroll should advance by at most 1, got %d", scrollDelta)
		}
	}
}

func TestJOnLastLineNearBottomDoesNotScroll(t *testing.T) {
	a := buildScrollApp(40, 20, 5)
	last := a.MaxCursorLine()

	// Put cursor at last line with it near the bottom of viewport.
	a.DiffState.CursorLine = last
	a.DiffState.ScrollOffset = satSub(last, 19) // cursor at bottom of viewport

	prevScroll := a.DiffState.ScrollOffset
	a.CursorDown(1)

	assertEq(t, a.DiffState.CursorLine, last, "cursor")
	assertEq(t, a.DiffState.ScrollOffset, prevScroll,
		"j on last line should never scroll the view")
}

func TestJOnLastLineCenteredDoesNotScroll(t *testing.T) {
	a := buildScrollApp(40, 20, 5)
	last := a.MaxCursorLine()

	a.DiffState.CursorLine = last
	a.CenterCursor()
	scrollAfterCenter := a.DiffState.ScrollOffset

	a.CursorDown(1)

	assertEq(t, a.DiffState.ScrollOffset, scrollAfterCenter,
		"j on last line when centered should not scroll")
}

func TestKReclaimsEmptySpaceBelow(t *testing.T) {
	a := buildScrollApp(40, 20, 5)
	last := a.MaxCursorLine()

	// Cursor at last line at top of view (maximum empty space below).
	a.DiffState.CursorLine = last
	a.DiffState.ScrollOffset = last // only 1 line visible

	// Press k — should immediately reclaim space (reduce scroll).
	a.CursorUp(1)

	assertEq(t, a.DiffState.CursorLine, last-1, "cursor")
	if a.DiffState.ScrollOffset >= last {
		t.Errorf("k should reclaim empty space below, scroll was %d expected less than %d",
			a.DiffState.ScrollOffset, last)
	}
}

func TestMaxScrollAllowsLastLineAtTop(t *testing.T) {
	a := buildScrollApp(40, 20, 5)
	total := a.TotalLines()

	assertEq(t, a.MaxScrollOffset(), total-1,
		"max scroll should allow last line at top of viewport")
}

func TestSmoothScrollToEndNoJumps(t *testing.T) {
	// Start at the beginning, scroll all the way down with j presses.
	a := buildScrollApp(40, 20, 5)
	last := a.MaxCursorLine()

	a.DiffState.CursorLine = 0
	a.DiffState.ScrollOffset = 0

	maxScrollDelta := 0
	for range last {
		prevScroll := a.DiffState.ScrollOffset
		a.CursorDown(1)
		delta := satSub(a.DiffState.ScrollOffset, prevScroll)
		if delta > maxScrollDelta {
			maxScrollDelta = delta
		}
	}

	assertEq(t, a.DiffState.CursorLine, last, "cursor at end")
	if maxScrollDelta > 1 {
		t.Errorf("scroll should never jump more than 1 line at a time, max was %d", maxScrollDelta)
	}
}

func TestKBelowMidpointOnlyMovesCursor(t *testing.T) {
	// After G, cursor is near the bottom of viewport. Pressing k should
	// only move the cursor, not also scroll the view.
	a := buildScrollApp(40, 20, 5)
	last := a.MaxCursorLine()

	// Simulate G: cursor at last line, scroll positions it at bottom.
	a.DiffState.CursorLine = last
	a.DiffState.ScrollOffset = satSub(last, 19)
	scrollBefore := a.DiffState.ScrollOffset

	a.CursorUp(1)
	assertEq(t, a.DiffState.CursorLine, last-1, "cursor")
	assertEq(t, a.DiffState.ScrollOffset, scrollBefore,
		"k when cursor is below midpoint should not change scroll")
}

func TestNoScrollWhenLastLineVisible(t *testing.T) {
	// When the last content line is visible, cursor should descend to it
	// without the view scrolling (no bottom margin near EOF).
	a := buildScrollApp(40, 20, 5)
	last := a.MaxCursorLine() // 42

	// Last line visible at viewport bottom: scroll=23, shows lines 23-42.
	a.DiffState.ScrollOffset = satSub(last, 19) // 23
	a.DiffState.CursorLine = last - 5           // 37, viewport position 14

	for i := range 5 {
		scrollBefore := a.DiffState.ScrollOffset
		a.CursorDown(1)
		if a.DiffState.ScrollOffset != scrollBefore {
			t.Errorf("scroll should not change on step %d (cursor near EOF with last line visible)", i)
		}
	}
	assertEq(t, a.DiffState.CursorLine, last, "cursor at end")
}

func TestCursorCannotGoPastLastContentLine(t *testing.T) {
	a := buildScrollApp(40, 20, 5)
	last := a.MaxCursorLine()
	total := a.TotalLines()

	// max cursor should be strictly less than total-1 (total-1 is the
	// trailing Spacing line).
	assertEq(t, last, total-2, "max cursor line")

	a.DiffState.CursorLine = last
	a.CursorDown(1)
	assertEq(t, a.DiffState.CursorLine, last, "cursor_down from last line should not advance")
}

func TestEffectiveScrollMarginPreventsOscillation(t *testing.T) {
	// With viewport 21 (odd), the margin must be at most 9 (= 21/2 - 1) so
	// that after centering at position 10 there's free space.
	state := DiffState{VisibleLineCount: 21, ViewportHeight: 21, WrapLines: true}
	margin := state.EffectiveScrollMargin(100)
	if margin >= 21/2 {
		t.Errorf("margin (%d) must be strictly less than half viewport (%d)", margin, 21/2)
	}
}

func TestScrollOffsetZeroMeansNoMargin(t *testing.T) {
	state := DiffState{VisibleLineCount: 20, ViewportHeight: 20, WrapLines: true}
	assertEq(t, state.EffectiveScrollMargin(0), 0, "margin should be 0 when scroll_offset is 0")
}

// --- scroll_tests.rs ---

// calcMaxScroll: max_scroll_offset is simply total_lines - 1 (last line can
// be at top).
func calcMaxScroll(totalLines int) int {
	return satSub(totalLines, 1)
}

func TestShouldCalculateMaxScroll(t *testing.T) {
	assertEq(t, calcMaxScroll(103), 102, "max scroll 103")
	assertEq(t, calcMaxScroll(20), 19, "max scroll 20")
}

func TestShouldHandleSmallContent(t *testing.T) {
	assertEq(t, calcMaxScroll(13), 12, "max scroll 13")
	assertEq(t, calcMaxScroll(1), 0, "max scroll 1")
}

func TestShouldHandleEmptyContent(t *testing.T) {
	assertEq(t, calcMaxScroll(0), 0, "max scroll 0")
}
