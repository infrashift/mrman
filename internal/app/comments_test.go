package app

// comments_test.go ports the comment-cycling tests from tuicr's
// src/app/tests/expand_gap_tests.rs, the comment-type cycle tests from
// target_selector_tests.rs, and adds behavior tests for the comment-mode
// transitions, save/edit/delete, and the comment navigator.

import (
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
)

func newSessionComment(content, typeID string, side *model.LineSide) *model.Comment {
	return model.NewComment(content, model.CommentTypeFromID(typeID), side)
}

// seedThreeScopedComments installs one review, one file, and one line
// comment (line 2, new side), mirroring the tuicr cycle tests.
func seedThreeScopedComments(a *App, fileBody string) {
	a.Session.ReviewComments = append(a.Session.ReviewComments,
		newSessionComment("review", "note", nil))
	review := a.Session.File("test.rs")
	review.AddFileComment(newSessionComment(fileBody, "suggestion", nil))
	side := model.LineSideNew
	review.AddLineComment(2, newSessionComment("line", "issue", &side))
	a.RebuildAnnotations()
}

func TestShouldCycleForwardThroughReviewFileAndLineComments(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	a.DiffState.ViewportHeight = 4
	seedThreeScopedComments(a, "file")

	a.DiffState.CursorLine = 0

	a.NextComment()
	firstComment := a.DiffState.CursorLine
	ann := &a.LineAnnotations[a.DiffState.CursorLine]
	assertEq(t, ann.Kind, AnnReviewComment, "first jump kind")
	assertEq(t, ann.CommentIdx, 0, "first jump idx")

	a.NextComment()
	ann = &a.LineAnnotations[a.DiffState.CursorLine]
	assertEq(t, ann.Kind, AnnFileComment, "second jump kind")
	assertEq(t, ann.FileIdx, 0, "second jump file")
	assertEq(t, ann.CommentIdx, 0, "second jump idx")

	a.NextComment()
	ann = &a.LineAnnotations[a.DiffState.CursorLine]
	assertEq(t, ann.Kind, AnnLineComment, "third jump kind")
	assertEq(t, ann.Line, uint32(2), "third jump line")
	assertEq(t, ann.Side, model.LineSideNew, "third jump side")
	assertEq(t, ann.CommentIdx, 0, "third jump idx")

	a.NextComment()
	assertEq(t, a.DiffState.CursorLine, firstComment, "wraps to first comment")
}

func TestShouldCycleBackwardAndSkipCurrentMultilineComment(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	seedThreeScopedComments(a, "file\nbody")

	fileComment := -1
	for i := range a.LineAnnotations {
		ann := &a.LineAnnotations[i]
		if ann.Kind == AnnFileComment && ann.FileIdx == 0 && ann.CommentIdx == 0 {
			fileComment = i
			break
		}
	}
	if fileComment < 0 {
		t.Fatal("missing annotation")
	}
	a.DiffState.CursorLine = fileComment + 2

	a.PrevComment()

	ann := &a.LineAnnotations[a.DiffState.CursorLine]
	assertEq(t, ann.Kind, AnnReviewComment, "prev jump kind")
	assertEq(t, ann.CommentIdx, 0, "prev jump idx")
}

func TestShouldWrapBackwardToLastComment(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	review := a.Session.File("test.rs")
	review.AddFileComment(newSessionComment("file", "suggestion", nil))
	side := model.LineSideNew
	review.AddLineComment(2, newSessionComment("line", "issue", &side))
	a.RebuildAnnotations()
	a.DiffState.CursorLine = 0

	a.PrevComment()

	ann := &a.LineAnnotations[a.DiffState.CursorLine]
	assertEq(t, ann.Kind, AnnLineComment, "wrap kind")
	assertEq(t, ann.Line, uint32(2), "wrap line")
	assertEq(t, ann.Side, model.LineSideNew, "wrap side")
	assertEq(t, ann.CommentIdx, 0, "wrap idx")
}

func TestShouldReportWhenNoCommentsExist(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)

	a.NextComment()
	assertEq(t, a.Message.Content, "No comments", "next message")

	a.PrevComment()
	assertEq(t, a.Message.Content, "No comments", "prev message")
}

func TestNextCommentReportsPosition(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	seedThreeScopedComments(a, "file")
	a.DiffState.CursorLine = 0

	a.NextComment()
	assertEq(t, a.Message.Content, "Comment 1/3", "position message")
	assertEq(t, a.FocusedPanel, PanelDiff, "focus follows jump")
}

// --- Comment type cycling (target_selector_tests.rs) ---

func TestDefaultCommentTypeIsNoneWithoutConfig(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	a.DiffState.CursorLine = hunkDiffLine(t, a, 0, 0)
	a.EnterCommentMode(false)
	assertEq(t, a.InputMode, input.ModeComment, "mode")
	// Out of the box the only type is None — untyped, no prefix.
	if !a.CommentType.IsNone() {
		t.Fatal("default type must be none")
	}
	assertEq(t, a.CommentType.ID(), "none", "type id")

	// With a single type there is nothing to cycle to; stays on None.
	a.CycleCommentType()
	if !a.CommentType.IsNone() {
		t.Fatal("cycle with one type must stay on none")
	}
	assertEq(t, a.Message.Content, "Only one comment type configured", "message")
}

func TestShouldCycleCommentTypeOnTabAction(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	// Configuring types overrides the None default (first configured type
	// becomes the default) but None stays available, appended to the cycle.
	a.SetCommentTypes([]CommentTypeDef{{ID: "note"}, {ID: "suggestion"}})
	a.DiffState.CursorLine = hunkDiffLine(t, a, 0, 0)
	a.EnterCommentMode(false)
	assertEq(t, a.InputMode, input.ModeComment, "mode")
	assertEq(t, a.CommentType.ID(), "note", "default type")

	a.CycleCommentType()
	assertEq(t, a.CommentType.ID(), "suggestion", "next type")

	// None is appended and reachable by cycling.
	a.CycleCommentType()
	assertEq(t, a.CommentType.ID(), "none", "none reachable")
	if !a.CommentType.IsNone() {
		t.Fatal("expected none")
	}
	assertEq(t, a.Message.Content, "Comment type: none", "announcement falls back to id")

	// Wraps back around to the first configured type.
	a.CycleCommentType()
	assertEq(t, a.CommentType.ID(), "note", "wraps")
	assertEq(t, a.Message.Content, "Comment type: NOTE", "announcement uses label")

	// Reverse cycling walks the same ring backwards.
	a.CycleCommentTypeReverse()
	assertEq(t, a.CommentType.ID(), "none", "reverse wraps to none")
	a.CycleCommentTypeReverse()
	assertEq(t, a.CommentType.ID(), "suggestion", "reverse steps back")
}

// --- Comment mode transitions and save ---

func TestEnterCommentModeRequiresDiffLine(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	a.DiffState.CursorLine = 0 // header row

	a.EnterCommentMode(false)

	assertEq(t, a.InputMode, input.ModeNormal, "mode unchanged")
	assertEq(t, a.Message.Content, "Move cursor to a diff line to add a line comment", "message")
}

func TestSaveLineCommentInsertsThroughSharedPrimitive(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	a.Username = "alice"
	a.DiffState.CursorLine = hunkDiffLine(t, a, 0, 0) + 1 // line 2

	a.EnterCommentMode(false)
	for _, r := range "needs a test" {
		a.InsertCommentChar(r)
	}
	saved := a.SaveComment()

	if saved == nil {
		t.Fatal("expected a saved comment")
	}
	assertEq(t, saved.Content, "needs a test", "content")
	assertEq(t, saved.Author, "alice", "author stamped from Username")
	if saved.CommitID != nil {
		t.Fatal("no selector => no commit id")
	}
	comments := a.Session.File("test.rs").LineComments[2]
	if len(comments) != 1 || comments[0] != saved {
		t.Fatal("comment must be stored under line 2")
	}
	if !a.Dirty {
		t.Fatal("save must mark dirty")
	}
	assertEq(t, a.Message.Content, "Comment added to line 2", "message")
	assertEq(t, a.InputMode, input.ModeNormal, "comment mode exited")
	if !anyAnnotation(a, func(ann *AnnotatedLine) bool { return ann.Kind == AnnLineComment }) {
		t.Fatal("annotations must include the new comment")
	}
	assertEq(t, a.TotalLines(), len(a.LineAnnotations), "heights stay in lockstep")
}

func TestSaveReviewAndFileComments(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)

	a.EnterReviewCommentMode()
	a.InsertCommentText("overall looks good")
	if a.SaveComment() == nil {
		t.Fatal("expected review comment")
	}
	assertEq(t, a.Message.Content, "Review comment added", "review message")
	assertEq(t, len(a.Session.ReviewComments), 1, "review comment stored")

	a.EnterCommentMode(true)
	a.InsertCommentText("file scope")
	if a.SaveComment() == nil {
		t.Fatal("expected file comment")
	}
	assertEq(t, a.Message.Content, "File comment added", "file message")
	assertEq(t, len(a.Session.File("test.rs").FileComments), 1, "file comment stored")
	assertEq(t, a.TotalLines(), len(a.LineAnnotations), "heights stay in lockstep")
}

func TestSaveRangeCommentFromVisual(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	first := hunkDiffLine(t, a, 0, 0)
	a.DiffState.CursorLine = first
	a.EnterVisualModeAtCursor()
	a.DiffState.CursorLine = first + 2
	a.ExtendVisualToCursor()
	a.EnterCommentFromVisual()

	a.InsertCommentText("range note")
	saved := a.SaveComment()

	if saved == nil || saved.LineRange == nil {
		t.Fatal("expected a range comment")
	}
	assertEq(t, *saved.LineRange, model.NewLineRange(1, 3), "range")
	assertEq(t, a.Message.Content, "Comment added to lines 1-3", "message")
	// Range comments are keyed by their end line.
	if len(a.Session.File("test.rs").LineComments[3]) != 1 {
		t.Fatal("range comment must be keyed by end line")
	}
}

func TestSaveCommentRejectsEmptyBuffer(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	a.EnterReviewCommentMode()
	a.InsertCommentText("   ")

	if a.SaveComment() != nil {
		t.Fatal("whitespace-only comment must not save")
	}
	assertEq(t, a.Message.Content, "Comment cannot be empty", "message")
	assertEq(t, a.InputMode, input.ModeComment, "stays in comment mode")
}

func TestEnterEditModeAndSaveUpdatesInPlace(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	side := model.LineSideNew
	a.Session.File("test.rs").AddLineComment(2, newSessionComment("orig", "note", &side))
	a.RebuildAnnotations()

	// Park the cursor on the comment's first row.
	for i := range a.LineAnnotations {
		if a.LineAnnotations[i].Kind == AnnLineComment {
			a.DiffState.CursorLine = i
			break
		}
	}

	if !a.EnterEditMode(true) {
		t.Fatal("expected to enter edit mode")
	}
	assertEq(t, a.CommentBuffer, "orig", "buffer loads content")
	assertEq(t, a.CommentCursor, len("orig"), "cursor at end")
	if a.EditingCommentID == nil {
		t.Fatal("editing id must be set")
	}

	a.InsertCommentText(" v2")
	saved := a.SaveComment()
	if saved == nil {
		t.Fatal("expected updated comment")
	}
	assertEq(t, saved.Content, "orig v2", "updated content")
	assertEq(t, a.Message.Content, "Comment on line 2 updated", "message")
	assertEq(t, len(a.Session.File("test.rs").LineComments[2]), 1, "no duplicate insert")
}

func TestEnterEditModeCursorAtStart(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	a.Session.ReviewComments = append(a.Session.ReviewComments,
		newSessionComment("first\nsecond", "note", nil))
	a.RebuildAnnotations()

	// Row 0 is the header; rows 1..4 are the comment box (top border,
	// "first", "second", bottom border). Cursor on the "second" row.
	a.DiffState.CursorLine = 3
	if !a.EnterEditMode(false) {
		t.Fatal("expected to enter edit mode")
	}
	assertEq(t, a.CommentBuffer, "first\nsecond", "buffer")
	assertEq(t, a.CommentCursor, len("first\n"), "cursor at start of second line")
	if !a.CommentIsReviewLevel {
		t.Fatal("review scope flag")
	}

	a.ExitCommentMode()
	assertEq(t, a.CommentBuffer, "", "buffer cleared")
	if a.EditingCommentID != nil {
		t.Fatal("editing id cleared")
	}
}

func TestEnterEditModeFailsOffComments(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	a.DiffState.CursorLine = hunkDiffLine(t, a, 0, 0)
	if a.EnterEditMode(true) {
		t.Fatal("diff lines are not editable comments")
	}
}

// --- Delete at cursor ---

func TestDeleteCommentAtCursorMessages(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	a.DiffState.CursorLine = hunkDiffLine(t, a, 0, 0)

	if a.DeleteCommentAtCursor() {
		t.Fatal("nothing to delete")
	}
	assertEq(t, a.Message.Content, "No comment at cursor", "no-comment message")

	side := model.LineSideNew
	locked := newSessionComment("pushed", "note", &side)
	locked.LifecycleState = model.LifecyclePushedDraft
	a.Session.File("test.rs").AddLineComment(2, locked)
	a.RebuildAnnotations()
	for i := range a.LineAnnotations {
		if a.LineAnnotations[i].Kind == AnnLineComment {
			a.DiffState.CursorLine = i
			break
		}
	}
	if !a.CursorOnLockedComment() {
		t.Fatal("cursor must report a locked comment")
	}
	if a.DeleteCommentAtCursor() {
		t.Fatal("locked comments must not delete")
	}
	assertEq(t, a.Message.Content, "Comment already pushed — read only", "locked message")
	assertEq(t, len(a.Session.File("test.rs").LineComments[2]), 1, "comment retained")
}

func TestDeleteCommentAtCursorRemovesEachScope(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	seedThreeScopedComments(a, "file")

	find := func(kind AnnKind) int {
		for i := range a.LineAnnotations {
			if a.LineAnnotations[i].Kind == kind {
				return i
			}
		}
		t.Fatalf("missing %v annotation", kind)
		return -1
	}

	a.DiffState.CursorLine = find(AnnLineComment)
	if !a.DeleteCommentAtCursor() {
		t.Fatal("line comment delete failed")
	}
	assertEq(t, a.Message.Content, "Comment on line 2 deleted", "line message")
	if len(a.Session.File("test.rs").LineComments) != 0 {
		t.Fatal("empty line-comment key must be removed")
	}

	a.DiffState.CursorLine = find(AnnFileComment)
	if !a.DeleteCommentAtCursor() {
		t.Fatal("file comment delete failed")
	}
	assertEq(t, a.Message.Content, "Comment deleted", "file message")

	a.DiffState.CursorLine = find(AnnReviewComment)
	if !a.DeleteCommentAtCursor() {
		t.Fatal("review comment delete failed")
	}
	assertEq(t, a.Message.Content, "Review comment deleted", "review message")
	if a.Session.HasComments() {
		t.Fatal("all comments deleted")
	}
	assertEq(t, a.TotalLines(), len(a.LineAnnotations), "heights stay in lockstep")
}

func TestClearCommentsReportsCounts(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)

	a.ClearComments(model.ClearCommentsOnly)
	assertEq(t, a.Message.Content, "No comments to clear", "empty message")

	seedThreeScopedComments(a, "file")
	a.Session.File("test.rs").Reviewed = true
	a.ClearComments(model.ClearCommentsAndReviewed)
	assertEq(t, a.Message.Content, "Cleared 3 comments, unreviewed 1 files", "clear message")
	if a.Session.HasComments() {
		t.Fatal("comments must be gone")
	}
}

// --- Display-line math and navigator plumbing ---

func TestCommentDisplayLinesWrapsToViewport(t *testing.T) {
	c := newSessionComment("short", "note", nil)
	assertEq(t, CommentDisplayLines(c, 80), 3, "single line: borders + 1")

	c = newSessionComment("a\nb\nc", "note", nil)
	assertEq(t, CommentDisplayLines(c, 80), 5, "three lines")

	// Width 20 => content area 10; 25 chars wrap to 3 segments.
	c = newSessionComment(strings.Repeat("x", 25), "note", nil)
	assertEq(t, CommentDisplayLines(c, 20), 5, "wrapped long line")

	// Zero width disables wrapping (annotation build before first render).
	assertEq(t, CommentDisplayLines(c, 0), 3, "zero width")
}

func TestWrapSegmentsRespectsCJKWidth(t *testing.T) {
	segs := WrapSegments("中文测试", 4)
	assertEq(t, len(segs), 2, "two segments")
	assertEq(t, segs[0], "中文", "first")
	assertEq(t, segs[1], "测试", "second")

	segs = WrapSegments("中a", 1)
	assertEq(t, segs[0], "中", "oversized char emitted alone")

	segs = WrapSegments("", 10)
	assertEq(t, len(segs), 1, "empty text single segment")
}

func TestBuildCommentNavigatorItemsCollapsesMultilineComments(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	seedThreeScopedComments(a, "file\nwith body")

	items := a.BuildCommentNavigatorItems()
	assertEq(t, len(items), 3, "one item per comment")
	assertEq(t, items[0].Key.Scope, NavScopeReview, "review first")
	assertEq(t, items[1].Key.Scope, NavScopeFile, "file second")
	if items[1].Path == nil || *items[1].Path != "test.rs" {
		t.Fatal("file item path")
	}
	assertEq(t, items[2].Key.Scope, NavScopeLine, "line third")
	if items[2].Line == nil || *items[2].Line != 2 {
		t.Fatal("line item lineno")
	}
	if !a.HasCommentNavigatorItems() {
		t.Fatal("navigator has items")
	}
}
