package app

// hunkcomment_test.go covers commenting on a whole hunk: c on a @@ header,
// and ] / [ extending a visual selection hunk by hunk. Both produce ordinary
// range comments — there is no hunk scope in the model, by design.

import (
	"testing"

	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
)

// mixedHunk is a hunk of the usual shape: a context line, a deletion, its
// replacement, and a trailing context line. Old and new numbering are kept
// equal at the start so the new-side span is easy to read off.
func mixedHunk(start uint32) model.DiffHunk {
	return model.DiffHunk{
		Header: "@@ mixed @@",
		Lines: []model.DiffLine{
			{Origin: model.OriginContext, Content: "ctx", OldLineno: new(start), NewLineno: new(start)},
			{Origin: model.OriginDeletion, Content: "gone", OldLineno: new(start + 1)},
			{Origin: model.OriginAddition, Content: "new", NewLineno: new(start + 1)},
			{Origin: model.OriginContext, Content: "ctx", OldLineno: new(start + 2), NewLineno: new(start + 2)},
		},
		OldStart: start, OldCount: 3,
		NewStart: start, NewCount: 3,
	}
}

// appWithTwoMixedHunks is the fixture most of these tests share.
func appWithTwoMixedHunks(t *testing.T) *App {
	t.Helper()
	file := makeFileWithHunks("test.rs", []model.DiffHunk{mixedHunk(10), mixedHunk(50)})
	return buildAppWithFiles([]model.DiffFile{file}, 200)
}

// --- c on a hunk header ---

func TestCommentOnHunkHeaderAnchorsTheWholeHunk(t *testing.T) {
	a := appWithTwoMixedHunks(t)
	a.DiffState.CursorLine = mustHunkHeaderLine(t, a, 0, 0)

	a.EnterCommentMode(false)

	assertEq(t, a.InputMode, input.ModeComment, "header row opens the comment box")
	if a.CommentLineRange == nil {
		t.Fatal("a hunk comment must carry a range")
	}
	// The new side spans 10..12; the deleted old line 11 is not part of it.
	assertEq(t, a.CommentLineRange.Range, model.NewLineRange(10, 12), "range")
	assertEq(t, a.CommentLineRange.Side, model.LineSideNew, "side")
	if a.CommentLine == nil {
		t.Fatal("the range end doubles as the display anchor")
	}
	assertEq(t, a.CommentLine.Line, uint32(12), "display anchor is the range end")
}

func TestCommentOnDiffLineStillAnchorsOneLine(t *testing.T) {
	a := appWithTwoMixedHunks(t)
	a.DiffState.CursorLine = hunkDiffLine(t, a, 0, 0)

	a.EnterCommentMode(false)

	if a.CommentLineRange != nil {
		t.Fatal("c on a diff line must stay a line comment")
	}
	if a.CommentLine == nil {
		t.Fatal("expected a line anchor")
	}
	assertEq(t, a.CommentLine.Line, uint32(10), "line")
}

func TestCommentOnFileHeaderStillWarns(t *testing.T) {
	a := appWithTwoMixedHunks(t)
	a.DiffState.CursorLine = 0 // file header, not a hunk header

	a.EnterCommentMode(false)

	assertEq(t, a.InputMode, input.ModeNormal, "mode unchanged")
	assertEq(t, a.Message.Content,
		"Move cursor to a diff line to add a line comment", "message")
}

// TestSaveHunkCommentStoresARange walks the whole path: the comment lands in
// the session as a range comment keyed by its end line, which is what makes it
// submit as a genuine multi-line thread with no new mapping code.
func TestSaveHunkCommentStoresARange(t *testing.T) {
	a := appWithTwoMixedHunks(t)
	a.DiffState.CursorLine = mustHunkHeaderLine(t, a, 0, 1)

	a.EnterCommentMode(false)
	for _, r := range "this hunk needs a test" {
		a.InsertCommentChar(r)
	}
	saved := a.SaveComment()

	if saved == nil {
		t.Fatal("expected a saved comment")
	}
	if saved.LineRange == nil {
		t.Fatal("a hunk comment is stored as a range comment")
	}
	assertEq(t, *saved.LineRange, model.NewLineRange(50, 52), "stored range")
	assertEq(t, model.SideOf(saved), model.LineSideNew, "stored side")
	// Ranges are keyed by their end line.
	comments := a.Session.File("test.rs").LineComments[52]
	if len(comments) != 1 || comments[0] != saved {
		t.Fatal("comment must be stored under the range end")
	}
	assertEq(t, a.Message.Content, "Comment added to lines 50-52", "message")
}

func TestSaveHunkCommentOnAPureDeletionUsesTheOldSide(t *testing.T) {
	deletion := model.DiffHunk{
		Header: "@@ deletion @@",
		Lines: []model.DiffLine{
			{Origin: model.OriginDeletion, Content: "gone", OldLineno: new(uint32(30))},
			{Origin: model.OriginDeletion, Content: "gone", OldLineno: new(uint32(31))},
		},
		OldStart: 30, OldCount: 2, NewStart: 29, NewCount: 0,
	}
	file := makeFileWithHunks("test.rs", []model.DiffHunk{deletion})
	a := buildAppWithFiles([]model.DiffFile{file}, 200)
	a.DiffState.CursorLine = mustHunkHeaderLine(t, a, 0, 0)

	a.EnterCommentMode(false)
	a.InsertCommentChar('x')
	saved := a.SaveComment()

	if saved == nil {
		t.Fatal("expected a saved comment")
	}
	assertEq(t, model.SideOf(saved), model.LineSideOld, "pure deletion anchors on the old side")
	if saved.LineRange == nil {
		t.Fatal("expected a range")
	}
	assertEq(t, *saved.LineRange, model.NewLineRange(30, 31), "stored range")
}

// TestHunkLookupsRejectNonHunkRows covers the guards: a row that belongs to no
// hunk, and a cursor parked past the end of the annotation stream, which a
// reload can leave behind.
func TestHunkLookupsRejectNonHunkRows(t *testing.T) {
	a := appWithTwoMixedHunks(t)

	if _, _, ok := a.hunkLineBounds(0); ok {
		t.Error("the file header belongs to no hunk")
	}
	if _, _, ok := a.hunkLineBounds(-1); ok {
		t.Error("a negative index has no bounds")
	}
	if _, _, ok := a.hunkLineBounds(len(a.LineAnnotations)); ok {
		t.Error("an out-of-range index has no bounds")
	}

	a.DiffState.CursorLine = len(a.LineAnnotations)
	if _, _, ok := a.hunkCommentRangeAtCursor(); ok {
		t.Error("a cursor past the last annotation anchors nothing")
	}
	a.EnterCommentMode(false)
	assertEq(t, a.InputMode, input.ModeNormal, "no comment box opens off the end")
}

// --- Visual mode: ] and [ ---

func TestEnterVisualOnAHunkHeaderSnapsToItsFirstLine(t *testing.T) {
	a := appWithTwoMixedHunks(t)
	header := mustHunkHeaderLine(t, a, 0, 0)
	a.DiffState.CursorLine = header

	a.EnterVisualModeAtCursor()

	// Anchoring on the header itself would leave the selection with no source
	// line to project onto, and c would reject it.
	if a.DiffState.CursorLine != header+1 {
		t.Fatalf("cursor: got %d, want %d (the hunk's first line)",
			a.DiffState.CursorLine, header+1)
	}
	if _, _, ok := a.VisualSelectionLineRange(); !ok {
		t.Fatal("a selection started on a header must still project onto lines")
	}
}

func TestExtendVisualToNextHunkGrowsAHunkAtATime(t *testing.T) {
	a := appWithTwoMixedHunks(t)
	a.DiffState.CursorLine = hunkDiffLine(t, a, 0, 0)
	a.EnterVisualModeAtCursor()

	a.ExtendVisualToNextHunk()

	rng, side, ok := a.VisualSelectionLineRange()
	if !ok {
		t.Fatal("expected a projectable selection")
	}
	assertEq(t, side, model.LineSideNew, "side")
	assertEq(t, rng, model.NewLineRange(10, 12), "first ] covers the current hunk")

	a.ExtendVisualToNextHunk()

	rng, _, ok = a.VisualSelectionLineRange()
	if !ok {
		t.Fatal("expected a projectable selection")
	}
	assertEq(t, rng, model.NewLineRange(10, 52), "second ] reaches through the next hunk")
}

func TestExtendVisualToNextHunkStopsAtTheLastHunk(t *testing.T) {
	a := appWithTwoMixedHunks(t)
	a.DiffState.CursorLine = hunkDiffLine(t, a, 0, 1)
	a.EnterVisualModeAtCursor()

	a.ExtendVisualToNextHunk()
	a.ExtendVisualToNextHunk() // nothing further to reach

	rng, _, ok := a.VisualSelectionLineRange()
	if !ok {
		t.Fatal("expected a projectable selection")
	}
	assertEq(t, rng, model.NewLineRange(50, 52), "selection stays on the last hunk")
}

func TestExtendVisualToPrevHunkShrinksBack(t *testing.T) {
	a := appWithTwoMixedHunks(t)
	// Anchor on the last line of the second hunk and walk backwards.
	a.DiffState.CursorLine = hunkDiffLine(t, a, 0, 1) + 3
	a.EnterVisualModeAtCursor()

	a.ExtendVisualToPrevHunk()

	rng, _, ok := a.VisualSelectionLineRange()
	if !ok {
		t.Fatal("expected a projectable selection")
	}
	assertEq(t, rng, model.NewLineRange(50, 52), "[ reaches the start of this hunk")

	a.ExtendVisualToPrevHunk()

	rng, _, ok = a.VisualSelectionLineRange()
	if !ok {
		t.Fatal("expected a projectable selection")
	}
	assertEq(t, rng, model.NewLineRange(10, 52), "[ again reaches back into the previous hunk")
}

// TestVisualHunkWalkThenComment is the documented flow end to end: park on a
// header, select, grow by a hunk, comment.
func TestVisualHunkWalkThenComment(t *testing.T) {
	a := appWithTwoMixedHunks(t)
	a.DiffState.CursorLine = mustHunkHeaderLine(t, a, 0, 0)

	a.EnterVisualModeAtCursor()
	a.ExtendVisualToNextHunk()
	a.ExtendVisualToNextHunk()
	a.EnterCommentFromVisual()

	assertEq(t, a.InputMode, input.ModeComment, "comment box opens")
	if a.CommentLineRange == nil {
		t.Fatal("expected a range anchor")
	}
	assertEq(t, a.CommentLineRange.Range, model.NewLineRange(10, 52), "spans both hunks")
}

func TestExtendVisualHunkIsInertWithoutASelection(t *testing.T) {
	a := appWithTwoMixedHunks(t)
	a.DiffState.CursorLine = hunkDiffLine(t, a, 0, 0)
	before := a.DiffState.CursorLine

	a.ExtendVisualToNextHunk()
	a.ExtendVisualToPrevHunk()

	assertEq(t, a.DiffState.CursorLine, before, "cursor must not move outside visual mode")
	if a.VisualSelection != nil {
		t.Fatal("no selection should have been created")
	}
}

// TestExtendVisualToNextHunkSkipsACollapsedHunk covers the interaction with R:
// a reviewed hunk renders its header and no lines, so there is nothing there to
// extend a selection over.
func TestExtendVisualToNextHunkSkipsACollapsedHunk(t *testing.T) {
	file := makeFileWithHunks("test.rs",
		[]model.DiffHunk{mixedHunk(10), mixedHunk(50), mixedHunk(90)})
	a := buildAppWithFiles([]model.DiffFile{file}, 300)

	// Mark the middle hunk reviewed, which folds its body away.
	a.DiffState.CursorLine = mustHunkHeaderLine(t, a, 0, 1)
	a.ToggleHunkReviewed()
	if !a.IsHunkReviewed(0, 1) {
		t.Fatal("setup: middle hunk should be reviewed")
	}

	a.DiffState.CursorLine = hunkDiffLine(t, a, 0, 0)
	a.EnterVisualModeAtCursor()
	a.ExtendVisualToNextHunk() // to the end of hunk 0
	a.ExtendVisualToNextHunk() // over the folded hunk, into hunk 2

	rng, _, ok := a.VisualSelectionLineRange()
	if !ok {
		t.Fatal("expected a projectable selection")
	}
	assertEq(t, rng, model.NewLineRange(10, 92), "selection reaches the third hunk")
}
