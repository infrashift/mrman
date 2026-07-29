package app

// visual_selection_test.go ports tuicr's
// src/app/tests/visual_selection_tests.rs plus behavior tests for the
// visual-mode transitions in visual.go.

import (
	"testing"

	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
)

func p(idx, off int) SelPoint {
	return SelPoint{AnnotationIdx: idx, CharOffset: off, Side: model.LineSideNew}
}

func TestCollapsedStartsAtPoint(t *testing.T) {
	sel := CollapsedSelection(p(5, 3))
	assertEq(t, sel.Anchor, p(5, 3), "anchor")
	assertEq(t, sel.Head, p(5, 3), "head")
}

func TestOrderedReturnsAnchorHeadWhenAlreadyInOrder(t *testing.T) {
	sel := VisualSelection{Anchor: p(1, 0), Head: p(4, 8)}
	start, end := sel.Ordered()
	assertEq(t, start, p(1, 0), "start")
	assertEq(t, end, p(4, 8), "end")
}

func TestOrderedSwapsWhenHeadBeforeAnchorByIdx(t *testing.T) {
	sel := VisualSelection{Anchor: p(4, 0), Head: p(1, 0)}
	start, end := sel.Ordered()
	assertEq(t, start, p(1, 0), "start")
	assertEq(t, end, p(4, 0), "end")
}

func TestOrderedBreaksTiesOnIdxByCharOffset(t *testing.T) {
	sel := VisualSelection{Anchor: p(7, 20), Head: p(7, 5)}
	start, end := sel.Ordered()
	assertEq(t, start, p(7, 5), "start")
	assertEq(t, end, p(7, 20), "end")
}

func TestCharRangeCoversMiddleAnnotationsFully(t *testing.T) {
	sel := VisualSelection{Anchor: p(1, 3), Head: p(4, 2)}
	lo, hi := sel.CharRange(2, 10)
	assertEq(t, lo, 0, "middle lo")
	assertEq(t, hi, 10, "middle hi")
	lo, hi = sel.CharRange(1, 10)
	assertEq(t, lo, 3, "start lo")
	assertEq(t, hi, 10, "start hi")
	lo, hi = sel.CharRange(4, 10)
	assertEq(t, lo, 0, "end lo")
	assertEq(t, hi, 2, "end hi")
}

func TestEnterVisualModeSelectsWholeCursorLine(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	a.DiffState.CursorLine = hunkDiffLine(t, a, 0, 0)

	a.EnterVisualModeAtCursor()

	assertEq(t, a.InputMode, input.ModeVisualSelect, "mode")
	if a.VisualSelection == nil {
		t.Fatal("expected a selection")
	}
	assertEq(t, a.VisualSelection.Anchor.AnnotationIdx, a.DiffState.CursorLine, "anchor idx")
	assertEq(t, a.VisualSelection.Anchor.CharOffset, 0, "anchor offset")
	assertEq(t, a.VisualSelection.Head.CharOffset, len("hunk line 1"), "head offset is line length")

	a.ExitVisualMode()
	assertEq(t, a.InputMode, input.ModeNormal, "mode after exit")
	if a.VisualSelection != nil {
		t.Fatal("selection must be dropped on exit")
	}
}

func TestExtendVisualToCursorDownAndUp(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	first := hunkDiffLine(t, a, 0, 0)
	a.DiffState.CursorLine = first
	a.EnterVisualModeAtCursor()

	a.DiffState.CursorLine = first + 2
	a.ExtendVisualToCursor()
	start, end := a.VisualSelection.Ordered()
	assertEq(t, start.AnnotationIdx, first, "start row")
	assertEq(t, start.CharOffset, 0, "start offset")
	assertEq(t, end.AnnotationIdx, first+2, "end row")
	assertEq(t, end.CharOffset, len("hunk line 3"), "end offset")

	// Extending back above the anchor flips the offsets.
	a.DiffState.CursorLine = first
	a.ExtendVisualToCursor()
	start, end = a.VisualSelection.Ordered()
	assertEq(t, start.AnnotationIdx, first, "flipped start row")
	assertEq(t, end.AnnotationIdx, first, "flipped end row")
}

func TestVisualSelectionLineRange(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	first := hunkDiffLine(t, a, 0, 0)
	a.DiffState.CursorLine = first
	a.EnterVisualModeAtCursor()
	a.DiffState.CursorLine = first + 1
	a.ExtendVisualToCursor()

	rng, side, ok := a.VisualSelectionLineRange()
	if !ok {
		t.Fatal("expected a line range")
	}
	assertEq(t, rng, model.NewLineRange(1, 2), "range")
	assertEq(t, side, model.LineSideNew, "side")
}

func TestVisualSelectionLineRangeFailsOffDiffLines(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	a.DiffState.CursorLine = 0 // review comments header
	a.EnterVisualModeAtCursor()

	if _, _, ok := a.VisualSelectionLineRange(); ok {
		t.Fatal("header rows must not produce a line range")
	}
}

func TestCopyVisualSelectionExtractsSourceText(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	first := hunkDiffLine(t, a, 0, 0)
	a.DiffState.CursorLine = first
	a.EnterVisualModeAtCursor()
	a.DiffState.CursorLine = first + 1
	a.ExtendVisualToCursor()

	text, count, err := a.CopyVisualSelection()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertEq(t, text, "hunk line 1\nhunk line 2", "copied text")
	assertEq(t, count, len([]rune("hunk line 1\nhunk line 2")), "char count")
}

func TestCopyVisualSelectionIncludesAtomicHunkHeader(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 2), makeHunk(10, 1)})
	a := buildAppWithFiles([]model.DiffFile{file}, 0)
	// Select from the last line of hunk 0 across the second hunk header.
	start := hunkDiffLine(t, a, 0, 0) + 1
	a.DiffState.CursorLine = start
	a.EnterVisualModeAtCursor()
	a.DiffState.CursorLine = hunkDiffLine(t, a, 0, 1)
	a.ExtendVisualToCursor()

	text, _, err := a.CopyVisualSelection()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "hunk line 2\n@@ -10,1 +10,1 @@\nhunk line 10"
	assertEq(t, text, want, "copied text spans expander-free rows")
}

func TestCopyVisualSelectionWithoutSelection(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	text, count, err := a.CopyVisualSelection()
	if err != nil || text != "" || count != 0 {
		t.Fatalf("no selection must copy nothing: %q %d %v", text, count, err)
	}
}

func TestEnterCommentFromVisualAnchorsRange(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	first := hunkDiffLine(t, a, 0, 0)
	a.DiffState.CursorLine = first
	a.EnterVisualModeAtCursor()
	a.DiffState.CursorLine = first + 2
	a.ExtendVisualToCursor()

	a.EnterCommentFromVisual()

	assertEq(t, a.InputMode, input.ModeComment, "mode")
	if a.CommentLineRange == nil {
		t.Fatal("expected a range anchor")
	}
	assertEq(t, a.CommentLineRange.Range, model.NewLineRange(1, 3), "range")
	if a.CommentLine == nil || a.CommentLine.Line != 3 {
		t.Fatal("comment line must anchor at range end")
	}
	if a.VisualSelection != nil {
		t.Fatal("selection must be consumed")
	}
}

func TestEnterCommentFromVisualInvalidSelectionWarns(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	a.DiffState.CursorLine = 0 // header row: no line range
	a.EnterVisualModeAtCursor()

	a.EnterCommentFromVisual()

	assertEq(t, a.InputMode, input.ModeNormal, "mode reset")
	assertEq(t, a.Message.Content, "Invalid visual selection", "warning")
	assertEq(t, a.Message.Type, MessageWarning, "warning type")
}
