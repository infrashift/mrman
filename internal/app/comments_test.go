package app

// comments_test.go ports the comment-cycling tests from tuicr's
// src/app/tests/expand_gap_tests.rs, the comment-type cycle tests from
// target_selector_tests.rs, and adds behavior tests for the comment-mode
// transitions, save/edit/delete, and the comment navigator.

import (
	"slices"
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

// TestDefaultCommentTypeWithoutConfig covers the built-in cycle. Unlike
// tuicr, an unconfigured mrman ships NOTE / ISSUE / SUGGESTION / PRAISE plus
// the typeless entry, and NOTE leads so an unclassified comment reads as a
// remark rather than a blocker.
func TestDefaultCommentTypeWithoutConfig(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	a.DiffState.CursorLine = hunkDiffLine(t, a, 0, 0)
	a.EnterCommentMode(false)
	assertEq(t, a.InputMode, input.ModeComment, "mode")
	assertEq(t, a.CommentType.ID(), "note", "default type id")
	if a.CommentType.IsNone() {
		t.Fatal("the default type must no longer be the typeless one")
	}

	// Tab walks the built-ins in order and ends on the typeless entry, which
	// stays reachable so a comment can still be left unclassified.
	for _, want := range []string{"issue", "suggestion", "praise", "none", "note"} {
		a.CycleCommentType()
		assertEq(t, a.CommentType.ID(), want, "cycled type id")
	}
}

// TestSingleConfiguredTypeStillReachesNone pins the "nothing to cycle" path
// that the built-ins no longer exercise: a config declaring only a "none"
// entry has a one-element cycle.
func TestSingleConfiguredTypeStillReachesNone(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	a.CommentTypes = ResolveCommentTypes([]CommentTypeDef{{ID: "none"}})
	a.DiffState.CursorLine = hunkDiffLine(t, a, 0, 0)
	a.EnterCommentMode(false)
	if !a.CommentType.IsNone() {
		t.Fatal("a lone none entry must be the default type")
	}
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

// TestBuiltinCommentTypes pins the shipped cycle. The four ids are not
// arbitrary: internal/ui maps exactly these onto the theme's comment_note /
// comment_issue / comment_suggestion / comment_praise slots, so renaming one
// silently drops it to the fallback colour.
func TestBuiltinCommentTypes(t *testing.T) {
	resolved := ResolveCommentTypes(nil)

	ids := make([]string, 0, len(resolved))
	for _, d := range resolved {
		ids = append(ids, d.ID)
	}
	want := []string{"note", "issue", "suggestion", "praise", "none"}
	if !slices.Equal(ids, want) {
		t.Fatalf("cycle = %v, want %v", ids, want)
	}

	// Every built-in carries a label and a legend definition; the typeless
	// entry deliberately carries neither.
	for _, d := range resolved[:len(resolved)-1] {
		if d.Label != strings.ToUpper(d.ID) {
			t.Errorf("%s: label = %q, want %q", d.ID, d.Label, strings.ToUpper(d.ID))
		}
		if d.Definition == nil || *d.Definition == "" {
			t.Errorf("%s: needs a definition, it appears in the exported legend", d.ID)
		}
		// Colour comes from the theme slot for the id, not a pinned literal.
		if d.Color != nil {
			t.Errorf("%s: Color = %q, want nil so the theme decides", d.ID, *d.Color)
		}
	}
}

// TestConfiguredCommentTypesReplaceBuiltins keeps the override contract:
// declaring types replaces the built-ins entirely rather than extending them,
// so a user who wants only their own two types gets only those (plus the
// typeless entry, which stays reachable).
func TestConfiguredCommentTypesReplaceBuiltins(t *testing.T) {
	resolved := ResolveCommentTypes([]CommentTypeDef{
		{ID: "blocker", Label: "BLOCKER"},
		{ID: "nit"},
	})

	ids := make([]string, 0, len(resolved))
	for _, d := range resolved {
		ids = append(ids, d.ID)
	}
	if !slices.Equal(ids, []string{"blocker", "nit", "none"}) {
		t.Fatalf("cycle = %v, want [blocker nit none] with no built-ins", ids)
	}
	// An omitted label still falls back to the id.
	if resolved[1].Label != "nit" {
		t.Errorf("label = %q, want the id as fallback", resolved[1].Label)
	}
}

// TestNavigatorKeepsCommentsWhenFileCollapsed is the behaviour a reviewer
// asked for: marking a file reviewed folds it, and the comment list used to
// empty out with it — losing the notes exactly when you had finished with the
// file and wanted to keep them.
func TestNavigatorKeepsCommentsWhenFileCollapsed(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	seedThreeScopedComments(a, "file")

	before := a.BuildCommentNavigatorItems()
	if len(before) != 3 {
		t.Fatalf("expected review, file and line comments, got %d", len(before))
	}

	a.Session.File("test.rs").Reviewed = true
	a.RebuildAnnotations()

	after := a.BuildCommentNavigatorItems()
	if len(after) != len(before) {
		t.Fatalf("collapsing the file dropped items: %d before, %d after", len(before), len(after))
	}
	// Order and identity survive; only the jump target is gone.
	for i := range before {
		if after[i].Key != before[i].Key {
			t.Errorf("item %d: key = %+v, want %+v (order must not shift)", i, after[i].Key, before[i].Key)
		}
		if after[i].CommentType != before[i].CommentType {
			t.Errorf("item %d: type = %v, want %v", i, after[i].CommentType, before[i].CommentType)
		}
	}
	// The review-scoped comment is not inside the file, so it keeps its row.
	if after[0].Key.Scope != NavScopeReview || after[0].TargetAnnotation == NoTargetAnnotation {
		t.Errorf("the review comment is outside the file and must stay navigable: %+v", after[0])
	}
	// The file's own comments have nowhere to jump to.
	for _, item := range after[1:] {
		if item.TargetAnnotation != NoTargetAnnotation {
			t.Errorf("a collapsed file's comment must have no target: %+v", item)
		}
	}
}

// TestNavigableCommentItemsExcludesCollapsed keeps m / M honest: they move the
// diff cursor, so they must skip comments with no row to land on. Without the
// filter, PrevComment matched NoTargetAnnotation (-1 < cursor) and jumped to a
// negative annotation index.
func TestNavigableCommentItemsExcludesCollapsed(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	seedThreeScopedComments(a, "file")
	a.Session.File("test.rs").Reviewed = true
	a.RebuildAnnotations()

	all := a.BuildCommentNavigatorItems()
	navigable := a.NavigableCommentItems()
	if len(all) != 3 || len(navigable) != 1 {
		t.Fatalf("all = %d (want 3), navigable = %d (want 1, the review comment)", len(all), len(navigable))
	}
	if navigable[0].Key.Scope != NavScopeReview {
		t.Errorf("navigable item = %+v, want the review-scoped comment", navigable[0])
	}

	// Both directions must survive a list whose only navigable row is the
	// review comment, without moving the cursor somewhere impossible.
	a.PrevComment()
	if a.DiffState.CursorLine < 0 {
		t.Fatalf("PrevComment moved the cursor to %d", a.DiffState.CursorLine)
	}
	a.NextComment()
	if a.DiffState.CursorLine < 0 {
		t.Fatalf("NextComment moved the cursor to %d", a.DiffState.CursorLine)
	}
}

// TestNavigatorKeepsCommentsWhenHunkCollapsed applies the same rule to R,
// which collapses a single hunk rather than the whole file.
func TestNavigatorKeepsCommentsWhenHunkCollapsed(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	side := model.LineSideNew
	a.Session.File("test.rs").AddLineComment(2, newSessionComment("in the hunk", "issue", &side))
	a.RebuildAnnotations()

	if got := len(a.BuildCommentNavigatorItems()); got != 1 {
		t.Fatalf("expected the line comment, got %d items", got)
	}
	// R acts on the hunk under the cursor.
	a.DiffState.CursorLine = hunkDiffLine(t, a, 0, 0)
	a.ToggleHunkReviewed()
	if !a.IsHunkReviewed(0, 0) {
		t.Fatal("setup: the hunk was not marked reviewed")
	}
	a.RebuildAnnotations()

	items := a.BuildCommentNavigatorItems()
	if len(items) != 1 {
		t.Fatalf("collapsing the hunk dropped the comment: %d items", len(items))
	}
	if items[0].TargetAnnotation != NoTargetAnnotation {
		t.Errorf("a collapsed hunk's comment must have no target: %+v", items[0])
	}
}

// TestCommentNavSelectPeeksWhenCollapsed covers the way back into a comment on
// a file you already finished: Enter jumps when there is a row to jump to, and
// opens the read-only peek panel when the file is folded. Toggling r off would
// work too, but it unfolds the file and moves the reviewer somewhere they did
// not ask to go.
func TestCommentNavSelectPeeksWhenCollapsed(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	side := model.LineSideNew
	a.Session.File("test.rs").AddLineComment(2, newSessionComment("needs a guard", "issue", &side))
	a.RebuildAnnotations()

	// Visible: Enter jumps and opens no panel.
	a.FocusPanel(PanelComments)
	a.CommentNav.Cursor = 0
	a.CommentNavSelect()
	if a.CommentPeek != nil {
		t.Fatal("a visible comment must be jumped to, not peeked")
	}

	// Folded: Enter peeks.
	a.Session.File("test.rs").Reviewed = true
	a.RebuildAnnotations()
	a.CommentNav.Cursor = 0
	a.CommentNavSelect()
	if a.CommentPeek == nil {
		t.Fatal("a collapsed comment must open the peek panel")
	}
	if a.InputMode != input.ModeCommentPeek {
		t.Errorf("InputMode = %v, want ModeCommentPeek", a.InputMode)
	}

	// The panel shows the comment and some file context around its anchor.
	var sawAnchor, sawBody bool
	for _, line := range a.CommentPeek.Lines {
		switch line.Kind {
		case PeekAnchor:
			sawAnchor = true
			if line.Lineno != 2 {
				t.Errorf("anchor line = %d, want 2", line.Lineno)
			}
		case PeekCommentBody:
			if line.Text == "needs a guard" {
				sawBody = true
			}
		}
	}
	if !sawAnchor {
		t.Error("the panel must mark the commented line")
	}
	if !sawBody {
		t.Errorf("the panel must show the comment body, got %+v", a.CommentPeek.Lines)
	}

	// Closing restores normal mode without touching the reviewed flag: the
	// file stays folded, which is the whole point.
	a.CloseCommentPeek()
	if a.CommentPeek != nil || a.InputMode != input.ModeNormal {
		t.Error("closing must clear the panel and return to normal mode")
	}
	if !a.Session.IsFileReviewed("test.rs") {
		t.Error("peeking must not unmark the file as reviewed")
	}
}

// TestPeekScrollClamps keeps the panel's own scrolling inside its content.
func TestPeekScrollClamps(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	side := model.LineSideNew
	a.Session.File("test.rs").AddLineComment(2, newSessionComment("a\nb\nc\nd\ne\nf", "note", &side))
	a.RebuildAnnotations()
	a.Session.File("test.rs").Reviewed = true
	a.RebuildAnnotations()

	a.CommentNav.Cursor = 0
	a.CommentNavSelect()
	p := a.CommentPeek
	if p == nil {
		t.Fatal("expected the peek panel")
	}
	p.ViewportHeight = 3

	a.PeekScroll(-5)
	if p.ScrollOffset != 0 {
		t.Errorf("scrolling above the top gave offset %d, want 0", p.ScrollOffset)
	}
	a.PeekScroll(1000)
	if want := len(p.Lines) - 3; p.ScrollOffset != want {
		t.Errorf("scrolling past the end gave offset %d, want %d", p.ScrollOffset, want)
	}
}

// TestSaveCommentSnapshotsLineContext covers the anchor snapshot. A comment is
// anchored by (path, line, side) and nothing else, so after a rebase or amend
// line 42 may be a different line — or gone. Recording the line's content at
// creation time is what lets a later session tell "still the same code" from
// "the line moved" from "the code is gone", instead of silently re-anchoring
// onto whatever now occupies that number.
func TestSaveCommentSnapshotsLineContext(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	a.DiffState.CursorLine = hunkDiffLine(t, a, 0, 0)

	a.EnterCommentMode(false)
	a.CommentBuffer = "needs a guard"
	saved := a.SaveComment()
	if saved == nil {
		t.Fatal("SaveComment returned nil")
	}
	if saved.LineContext == nil {
		t.Fatal("a line comment must carry a line-context snapshot")
	}
	if saved.LineContext.NewLine == nil {
		t.Fatalf("snapshot has no new-side line number: %+v", saved.LineContext)
	}
	// The snapshot must describe the line the comment is filed under.
	anchor := a.LineContextAt("test.rs", *saved.LineContext.NewLine, model.LineSideNew)
	if anchor == nil {
		t.Fatal("the anchored line is not in the diff")
	}
	if saved.LineContext.Content != anchor.Content {
		t.Errorf("snapshot content = %q, want the anchored line's %q",
			saved.LineContext.Content, anchor.Content)
	}
}

// TestFileAndReviewCommentsHaveNoLineContext keeps the snapshot to comments
// that have a line to go stale. A file- or review-scoped comment has none, so
// carrying an empty snapshot would only invite a false staleness verdict later.
func TestFileAndReviewCommentsHaveNoLineContext(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	a.DiffState.CursorLine = hunkDiffLine(t, a, 0, 0)

	a.EnterCommentMode(true) // file-level
	a.CommentBuffer = "this file does two jobs"
	if saved := a.SaveComment(); saved == nil {
		t.Fatal("SaveComment returned nil")
	} else if saved.LineContext != nil {
		t.Errorf("a file comment must carry no line context, got %+v", saved.LineContext)
	}

	a.EnterReviewCommentMode()
	a.CommentBuffer = "overall this looks right"
	if saved := a.SaveComment(); saved == nil {
		t.Fatal("SaveComment returned nil")
	} else if saved.LineContext != nil {
		t.Errorf("a review comment must carry no line context, got %+v", saved.LineContext)
	}
}

// TestLineContextAtMissingLine covers the unverifiable case: a line that is not
// in the diff yields no snapshot, so the comment is simply unverifiable rather
// than being wrongly recorded against the wrong content.
func TestLineContextAtMissingLine(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	if got := a.LineContextAt("test.rs", 9999, model.LineSideNew); got != nil {
		t.Errorf("LineContextAt for a line outside the diff = %+v, want nil", got)
	}
	if got := a.LineContextAt("nope.rs", 1, model.LineSideNew); got != nil {
		t.Errorf("LineContextAt for an unknown file = %+v, want nil", got)
	}
}
