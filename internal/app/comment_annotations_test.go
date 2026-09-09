package app

// comment_annotations_test.go covers the M4 comment-emission paths of
// RebuildAnnotations in both view modes, verifying the height math stays in
// lockstep with the annotation stream (tuicr's core invariant).

import (
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

// makeMixedHunk builds a hunk with a context line, a deletion run, an
// addition run, and a trailing context line so side-by-side pairing is
// exercised.
func makeMixedHunk() model.DiffHunk {
	return model.DiffHunk{
		Header: "@@ -1,4 +1,5 @@",
		Lines: []model.DiffLine{
			{Origin: model.OriginContext, Content: "ctx one", OldLineno: new(uint32(1)), NewLineno: new(uint32(1))},
			{Origin: model.OriginDeletion, Content: "old two", OldLineno: new(uint32(2))},
			{Origin: model.OriginDeletion, Content: "old three", OldLineno: new(uint32(3))},
			{Origin: model.OriginAddition, Content: "new two", NewLineno: new(uint32(2))},
			{Origin: model.OriginAddition, Content: "new three", NewLineno: new(uint32(3))},
			{Origin: model.OriginAddition, Content: "new four", NewLineno: new(uint32(4))},
			{Origin: model.OriginContext, Content: "ctx five", OldLineno: new(uint32(4)), NewLineno: new(uint32(5))},
		},
		OldStart: 1, OldCount: 4, NewStart: 1, NewCount: 5,
	}
}

func seedSideScopedComments(a *App) {
	review := a.Session.File("test.rs")
	oldSide, newSide := model.LineSideOld, model.LineSideNew
	review.AddLineComment(2, newSessionComment("on deleted line", "note", &oldSide))
	review.AddLineComment(4, newSessionComment("on added line", "note", &newSide))
	review.AddLineComment(5, newSessionComment("on context line", "note", &newSide))
	a.RebuildAnnotations()
}

func TestLineCommentsInterleaveInUnifiedMode(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeMixedHunk()})
	a := buildAppWithFiles([]model.DiffFile{file}, 0)
	seedSideScopedComments(a)

	// Each single-line comment renders 3 rows.
	got := countAnnotations(a, func(ann *AnnotatedLine) bool { return ann.Kind == AnnLineComment })
	assertEq(t, got, 9, "three comments, three rows each")
	assertEq(t, a.TotalLines(), len(a.LineAnnotations), "unified lockstep")

	// The old-side comment follows the deleted line, not an added one.
	for i := range a.LineAnnotations {
		ann := &a.LineAnnotations[i]
		if ann.Kind == AnnLineComment && ann.Side == model.LineSideOld {
			prev := &a.LineAnnotations[i-1]
			assertEq(t, prev.Kind, AnnDiffLine, "old-side comment attaches after its diff line")
			assertLineno(t, prev.OldLineno, 2, "anchored to old line 2")
			break
		}
	}
}

func TestLineCommentsInterleaveInSideBySideMode(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeMixedHunk()})
	a := buildAppWithFiles([]model.DiffFile{file}, 0)
	a.DiffViewMode = ViewSideBySide
	seedSideScopedComments(a)

	got := countAnnotations(a, func(ann *AnnotatedLine) bool { return ann.Kind == AnnLineComment })
	assertEq(t, got, 9, "three comments, three rows each")
	assertEq(t, a.TotalLines(), len(a.LineAnnotations), "side-by-side lockstep")

	// The file height helpers agree with the emitted rows.
	assertEq(t, a.FileRenderHeight(0, &a.DiffFiles[0]), len(a.LineAnnotations)-1, "file height excludes review header")
}

func TestContentForSidePicksPaneInSideBySide(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeMixedHunk()})
	a := buildAppWithFiles([]model.DiffFile{file}, 0)
	a.DiffViewMode = ViewSideBySide
	a.RebuildAnnotations()

	// First paired row: del "old two" / add "new two".
	pairRow := -1
	for i := range a.LineAnnotations {
		ann := &a.LineAnnotations[i]
		if ann.Kind == AnnSideBySideLine && ann.DelLineIdx != nil && ann.AddLineIdx != nil && ann.OldLineno != nil && *ann.OldLineno == 2 {
			pairRow = i
			break
		}
	}
	if pairRow < 0 {
		t.Fatal("missing paired row")
	}
	content, ok := a.ContentForSide(pairRow, model.LineSideNew)
	if !ok || content != "new two" {
		t.Fatalf("new side: got %q %v", content, ok)
	}
	content, ok = a.ContentForSide(pairRow, model.LineSideOld)
	if !ok || content != "old two" {
		t.Fatalf("old side: got %q %v", content, ok)
	}

	// A standalone addition row falls back to the add pane for Old.
	addRow := -1
	for i := range a.LineAnnotations {
		ann := &a.LineAnnotations[i]
		if ann.Kind == AnnSideBySideLine && ann.DelLineIdx == nil && ann.AddLineIdx != nil {
			addRow = i
			break
		}
	}
	if addRow < 0 {
		t.Fatal("missing addition row")
	}
	content, ok = a.ContentForSide(addRow, model.LineSideOld)
	if !ok || content != "new four" {
		t.Fatalf("old side falls back to add pane: got %q %v", content, ok)
	}

	// Non-content rows have no selectable content.
	if _, ok := a.ContentForSide(0, model.LineSideNew); ok {
		t.Fatal("header rows have no content")
	}
}

func TestReviewCommentEmissionKeepsOverviewLockstep(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	a.Session.ReviewComments = append(a.Session.ReviewComments,
		newSessionComment("first", "note", nil),
		newSessionComment("multi\nline", "note", nil))
	a.RebuildAnnotations()

	got := countAnnotations(a, func(ann *AnnotatedLine) bool { return ann.Kind == AnnReviewComment })
	assertEq(t, got, 3+4, "review comment rows")
	assertEq(t, a.TotalLines(), len(a.LineAnnotations), "overview lockstep")
	if !a.IsCursorInOverview() {
		t.Fatal("cursor 0 sits in the overview block")
	}

	// The review-level inline input box adds three rows to the height math
	// while composing a new review comment.
	before := a.TotalLines()
	a.EnterReviewCommentMode()
	assertEq(t, a.TotalLines(), before+3, "input box rows")
	a.ExitCommentMode()
	assertEq(t, a.TotalLines(), before, "rows released on exit")
}

func TestSingleFileViewSkipsOtherFilesComments(t *testing.T) {
	fileA := makeFileWithHunks("a.rs", []model.DiffHunk{makeHunk(1, 2)})
	fileB := makeFileWithHunks("b.rs", []model.DiffHunk{makeHunk(1, 2)})
	a := buildAppWithFiles([]model.DiffFile{fileA, fileB}, 20)
	a.Session.File("b.rs").AddFileComment(newSessionComment("other file", "note", nil))
	a.RebuildAnnotations()

	a.IsSingleFileView = true
	a.DiffState.CurrentFileIdx = 0
	a.RebuildAnnotations()

	if anyAnnotation(a, func(ann *AnnotatedLine) bool { return ann.Kind == AnnFileComment }) {
		t.Fatal("single-file view must not annotate the other file's comments")
	}
	assertEq(t, a.TotalLines(), len(a.LineAnnotations), "single-file lockstep")
}
