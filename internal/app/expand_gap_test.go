package app

// expand_gap_test.go ports tuicr's src/app/tests/expand_gap_tests.rs —
// every test in the read-only scope. The next/prev-comment cycle tests live
// in comments_test.go and the toggle_hunk_reviewed entry-point tests in
// reviewedmut_test.go (M4); the annotation effects are also covered below
// via direct session state. Still skipped: the remote-thread regression
// tests (M6).

import (
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

// markHunkReviewed flips a hunk's reviewed state directly on the session,
// standing in for toggle_hunk_reviewed (a later-milestone mutation).
func markHunkReviewed(t *testing.T, a *App, fileIdx, hunkIdx int) {
	t.Helper()
	path := a.DiffFiles[fileIdx].DisplayPath()
	key, ok := a.DiffFiles[fileIdx].HunkReviewKey(hunkIdx)
	if !ok {
		t.Fatal("missing hunk review key")
	}
	a.Session.File(path).ToggleHunkReviewed(key)
	a.RebuildAnnotations()
}

func TestShouldFoldReviewedHunkBody(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3), makeHunk(10, 2)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	before := a.TotalLines()

	markHunkReviewed(t, a, 0, 0)

	assertEq(t, a.TotalLines(), before-4, "total lines after fold")
	if anyAnnotation(a, func(l *AnnotatedLine) bool {
		return l.Kind == AnnDiffLine && l.FileIdx == 0 && l.HunkIdx == 0
	}) {
		t.Error("reviewed hunk body should not render")
	}
	if anyAnnotation(a, func(l *AnnotatedLine) bool {
		return (l.Kind == AnnExpander || l.Kind == AnnHiddenLines) &&
			l.GapID == (GapID{FileIdx: 0, HunkIdx: 1})
	}) {
		t.Error("gap controls adjoining a reviewed hunk should not render")
	}
	if !anyAnnotation(a, func(l *AnnotatedLine) bool {
		return l.Kind == AnnDiffLine && l.FileIdx == 0 && l.HunkIdx == 1
	}) {
		t.Error("unreviewed hunk body should still render")
	}
}

func TestShouldKeepFileAndHunkReviewedStateIndependent(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	path := a.DiffFiles[0].DisplayPath()
	key, _ := a.DiffFiles[0].HunkReviewKey(0)

	markHunkReviewed(t, a, 0, 0)
	a.Session.File(path).Reviewed = true
	a.RebuildAnnotations()

	if !a.Session.IsFileReviewed(path) {
		t.Error("file should be reviewed")
	}
	if !a.Session.IsHunkReviewed(path, key) {
		t.Error("hunk should stay reviewed independently")
	}
}

func TestShouldExpandUpFromFirstHunk(t *testing.T) {
	// given: file with 50-line gap before first hunk (hunk starts at line 51)
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(51, 5)})
	a := buildAppWithFiles([]model.DiffFile{file}, 100)
	gapID := GapID{FileIdx: 0, HunkIdx: 0}

	// when: expand Up with limit 20 (reveals lines closest to hunk)
	if err := a.ExpandGap(gapID, ExpandUp, new(20)); err != nil {
		t.Fatal(err)
	}

	// then: 20 lines expanded from the bottom of the gap (lines 31-50)
	content := a.ExpandedBottom[gapID]
	assertEq(t, len(content), 20, "expanded bottom length")
	assertLineno(t, content[0].NewLineno, 31, "first expanded line")
	assertLineno(t, content[19].NewLineno, 50, "last expanded line")
}

func TestShouldExpandAllLinesWithBothDirection(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(51, 5)})
	a := buildAppWithFiles([]model.DiffFile{file}, 100)
	gapID := GapID{FileIdx: 0, HunkIdx: 0}

	if err := a.ExpandGap(gapID, ExpandBoth, nil); err != nil {
		t.Fatal(err)
	}

	content := a.ExpandedTop[gapID]
	assertEq(t, len(content), 50, "expanded top length")
	assertLineno(t, content[0].NewLineno, 1, "first expanded line")
	assertLineno(t, content[49].NewLineno, 50, "last expanded line")
}

func TestShouldExpandDownFromUpperHunk(t *testing.T) {
	// given: two hunks, gap of 24 lines (6..29) between them
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 5), makeHunk(30, 5)})
	a := buildAppWithFiles([]model.DiffFile{file}, 100)
	gapID := GapID{FileIdx: 0, HunkIdx: 1}

	if err := a.ExpandGap(gapID, ExpandDown, new(10)); err != nil {
		t.Fatal(err)
	}

	content := a.ExpandedTop[gapID]
	assertEq(t, len(content), 10, "expanded top length")
	assertLineno(t, content[0].NewLineno, 6, "first expanded line")
	assertLineno(t, content[9].NewLineno, 15, "last expanded line")
}

func TestShouldExpandUpFromLowerHunk(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 5), makeHunk(30, 5)})
	a := buildAppWithFiles([]model.DiffFile{file}, 100)
	gapID := GapID{FileIdx: 0, HunkIdx: 1}

	if err := a.ExpandGap(gapID, ExpandUp, new(10)); err != nil {
		t.Fatal(err)
	}

	content := a.ExpandedBottom[gapID]
	assertEq(t, len(content), 10, "expanded bottom length")
	assertLineno(t, content[0].NewLineno, 20, "first expanded line")
	assertLineno(t, content[9].NewLineno, 29, "last expanded line")
}

func TestShouldAppendOnSubsequentDownExpand(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 5), makeHunk(50, 5)})
	a := buildAppWithFiles([]model.DiffFile{file}, 100)
	gapID := GapID{FileIdx: 0, HunkIdx: 1}
	if err := a.ExpandGap(gapID, ExpandDown, new(20)); err != nil {
		t.Fatal(err)
	}

	if err := a.ExpandGap(gapID, ExpandDown, new(20)); err != nil {
		t.Fatal(err)
	}

	content := a.ExpandedTop[gapID]
	assertEq(t, len(content), 40, "expanded top length")
	assertLineno(t, content[0].NewLineno, 6, "first expanded line")
	assertLineno(t, content[39].NewLineno, 45, "last expanded line")
}

func TestShouldPrependOnSubsequentUpExpand(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 5), makeHunk(50, 5)})
	a := buildAppWithFiles([]model.DiffFile{file}, 100)
	gapID := GapID{FileIdx: 0, HunkIdx: 1}
	if err := a.ExpandGap(gapID, ExpandUp, new(10)); err != nil {
		t.Fatal(err)
	}

	if err := a.ExpandGap(gapID, ExpandUp, new(10)); err != nil {
		t.Fatal(err)
	}

	// then: 20 lines total in bottom, in ascending order
	content := a.ExpandedBottom[gapID]
	assertEq(t, len(content), 20, "expanded bottom length")
	assertLineno(t, content[0].NewLineno, 30, "first expanded line")
	assertLineno(t, content[19].NewLineno, 49, "last expanded line")
}

func TestShouldCapAtGapBoundaries(t *testing.T) {
	// given: 50-line gap, already expanded 40 up
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(51, 5)})
	a := buildAppWithFiles([]model.DiffFile{file}, 100)
	gapID := GapID{FileIdx: 0, HunkIdx: 0}
	if err := a.ExpandGap(gapID, ExpandUp, new(40)); err != nil {
		t.Fatal(err)
	}

	// when: expand Up 20 more (only 10 remain)
	if err := a.ExpandGap(gapID, ExpandUp, new(20)); err != nil {
		t.Fatal(err)
	}

	content := a.ExpandedBottom[gapID]
	assertEq(t, len(content), 50, "expanded bottom length")
	assertLineno(t, content[0].NewLineno, 1, "first expanded line")
}

func TestShouldShowUpExpanderForTopOfFilePartial(t *testing.T) {
	// given: 50-line gap, expanded 20 lines up
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(51, 5)})
	a := buildAppWithFiles([]model.DiffFile{file}, 100)
	gapID := GapID{FileIdx: 0, HunkIdx: 0}
	if err := a.ExpandGap(gapID, ExpandUp, new(20)); err != nil {
		t.Fatal(err)
	}

	expanderCount := countAnnotations(a, func(l *AnnotatedLine) bool {
		return l.Kind == AnnExpander && l.GapID == gapID && l.Direction == ExpandUp
	})
	assertEq(t, expanderCount, 1, "up expander count")

	hiddenCount := countAnnotations(a, func(l *AnnotatedLine) bool {
		return l.Kind == AnnHiddenLines && l.GapID == gapID
	})
	assertEq(t, hiddenCount, 1, "should show hidden lines count")

	expandedCount := countAnnotations(a, func(l *AnnotatedLine) bool {
		return l.Kind == AnnExpandedContext && l.GapID == gapID
	})
	assertEq(t, expandedCount, 20, "expanded context count")
}

func TestShouldNotShowExpanderWhenFullyExpanded(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(51, 5)})
	a := buildAppWithFiles([]model.DiffFile{file}, 100)
	gapID := GapID{FileIdx: 0, HunkIdx: 0}
	if err := a.ExpandGap(gapID, ExpandBoth, nil); err != nil {
		t.Fatal(err)
	}

	expanderCount := countAnnotations(a, func(l *AnnotatedLine) bool {
		return l.Kind == AnnExpander && l.GapID == gapID
	})
	assertEq(t, expanderCount, 0, "expander count when fully expanded")
}

func TestShouldShowMergedExpanderForSmallBetweenHunkGap(t *testing.T) {
	// given: two hunks and a 15-line gap between them
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 5), makeHunk(21, 5)})
	a := buildAppWithFiles([]model.DiffFile{file}, 100)
	gapID := GapID{FileIdx: 0, HunkIdx: 1}

	bothCount := countAnnotations(a, func(l *AnnotatedLine) bool {
		return l.Kind == AnnExpander && l.GapID == gapID && l.Direction == ExpandBoth
	})
	assertEq(t, bothCount, 1, "small gap should show merged expander")
}

func TestShouldShowSplitExpandersForLargeBetweenHunkGap(t *testing.T) {
	// given: two hunks and a 30-line gap between them
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 5), makeHunk(36, 5)})
	a := buildAppWithFiles([]model.DiffFile{file}, 100)
	gapID := GapID{FileIdx: 0, HunkIdx: 1}

	downCount := countAnnotations(a, func(l *AnnotatedLine) bool {
		return l.Kind == AnnExpander && l.GapID == gapID && l.Direction == ExpandDown
	})
	upCount := countAnnotations(a, func(l *AnnotatedLine) bool {
		return l.Kind == AnnExpander && l.GapID == gapID && l.Direction == ExpandUp
	})
	hiddenCount := countAnnotations(a, func(l *AnnotatedLine) bool {
		return l.Kind == AnnHiddenLines && l.GapID == gapID
	})
	assertEq(t, downCount, 1, "down expander count")
	assertEq(t, upCount, 1, "up expander count")
	assertEq(t, hiddenCount, 1, "hidden lines count")
}

func TestShouldExpandGapInCorrectFileNotAdjacentFile(t *testing.T) {
	file0 := makeFileWithHunks("a.rs", []model.DiffHunk{makeHunk(31, 5)})
	file1 := makeFileWithHunks("b.rs", []model.DiffHunk{makeHunk(21, 5)})
	a := buildAppWithFiles([]model.DiffFile{file0, file1}, 100)

	gapIDFile1 := GapID{FileIdx: 1, HunkIdx: 0}

	if err := a.ExpandGap(gapIDFile1, ExpandUp, new(10)); err != nil {
		t.Fatal(err)
	}

	content := a.ExpandedBottom[gapIDFile1]
	assertEq(t, len(content), 10, "expanded bottom length")
	assertLineno(t, content[9].NewLineno, 20, "last expanded line")

	gapIDFile0 := GapID{FileIdx: 0, HunkIdx: 0}
	_, inTop := a.ExpandedTop[gapIDFile0]
	_, inBottom := a.ExpandedBottom[gapIDFile0]
	if inTop || inBottom {
		t.Error("file0's gap should not be expanded")
	}
}

func TestShouldNoopWhenAlreadyFullyExpanded(t *testing.T) {
	// given: 10-line gap, fully expanded
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(11, 5)})
	a := buildAppWithFiles([]model.DiffFile{file}, 100)
	gapID := GapID{FileIdx: 0, HunkIdx: 0}
	if err := a.ExpandGap(gapID, ExpandBoth, nil); err != nil {
		t.Fatal(err)
	}
	lenBefore := len(a.ExpandedTop[gapID])

	if err := a.ExpandGap(gapID, ExpandUp, new(20)); err != nil {
		t.Fatal(err)
	}

	assertEq(t, len(a.ExpandedTop[gapID]), lenBefore, "no change when fully expanded")
}

func TestShouldExpandSmallGapFullyEvenWithLargeLimit(t *testing.T) {
	// given: 5-line gap
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(6, 5)})
	a := buildAppWithFiles([]model.DiffFile{file}, 100)
	gapID := GapID{FileIdx: 0, HunkIdx: 0}

	if err := a.ExpandGap(gapID, ExpandUp, new(20)); err != nil {
		t.Fatal(err)
	}

	assertEq(t, len(a.ExpandedBottom[gapID]), 5, "all 5 lines expanded")

	expanderCount := countAnnotations(a, func(l *AnnotatedLine) bool {
		return l.Kind == AnnExpander && l.GapID == gapID
	})
	assertEq(t, expanderCount, 0, "no expander remaining")
}

func TestShouldMergeToBothWhenRemainingDropsBelowBatch(t *testing.T) {
	// given: 30-line between-hunk gap, expand 20 down => 10 remaining
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 5), makeHunk(36, 5)})
	a := buildAppWithFiles([]model.DiffFile{file}, 100)
	gapID := GapID{FileIdx: 0, HunkIdx: 1}
	if err := a.ExpandGap(gapID, ExpandDown, new(20)); err != nil {
		t.Fatal(err)
	}

	bothCount := countAnnotations(a, func(l *AnnotatedLine) bool {
		return l.Kind == AnnExpander && l.GapID == gapID && l.Direction == ExpandBoth
	})
	assertEq(t, bothCount, 1, "should merge to merged expander when <20 remaining")
}

func TestShouldExpandCollapsedGapWhenJumpingIntoIt(t *testing.T) {
	// given: 50-line gap before the first hunk (hunk @ line 51)
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(51, 5)})
	a := buildAppWithFiles([]model.DiffFile{file}, 100)
	gapID := GapID{FileIdx: 0, HunkIdx: 0}
	if _, ok := a.ExpandedTop[gapID]; ok {
		t.Fatal("gap should start collapsed")
	}
	if _, ok := a.ExpandedBottom[gapID]; ok {
		t.Fatal("gap should start collapsed")
	}

	// when: jump to line 30, which lives inside the collapsed gap
	a.GoToSourceLine(30, model.LineSideNew)

	// then: the gap was expanded and the cursor sits on new line 30
	assertLineno(t, cursorNewLineno(a), 30, "cursor new lineno")
}

func TestShouldNotExpandWhenLineIsAlreadyInAHunk(t *testing.T) {
	// Line 52 lives inside the hunk's own range, not in a gap.
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(51, 5)})
	a := buildAppWithFiles([]model.DiffFile{file}, 100)
	gapID := GapID{FileIdx: 0, HunkIdx: 0}

	a.GoToSourceLine(52, model.LineSideNew)

	if _, ok := a.ExpandedTop[gapID]; ok {
		t.Error("no top expansion expected")
	}
	if _, ok := a.ExpandedBottom[gapID]; ok {
		t.Error("no bottom expansion expected")
	}
}

func TestShouldExpandOldSideGapWhenJumpingWithOPrefix(t *testing.T) {
	// Symmetric gap (offset = 0): the side=Old path of GoToSourceLine works
	// end-to-end — the gap auto-expands and the cursor lands on old line 30.
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(51, 5)})
	a := buildAppWithFiles([]model.DiffFile{file}, 100)

	a.GoToSourceLine(30, model.LineSideOld)

	assertLineno(t, cursorOldLineno(a), 30, "cursor old lineno")
}

func TestShouldExpandUpWhenCursorIsBelowTheGap(t *testing.T) {
	// Two hunks: hunk0 at lines 1-5, hunk1 at lines 50-54. Gap spans new
	// lines 6..=49. Cursor on hunk1, jump to line 30 — expansion should
	// come from the bottom of the gap (Up).
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 5), makeHunk(50, 5)})
	a := buildAppWithFiles([]model.DiffFile{file}, 100)
	gapID := GapID{FileIdx: 0, HunkIdx: 1}

	hunk1HeaderIdx := mustHunkHeaderLine(t, a, 0, 1)
	a.DiffState.CursorLine = hunk1HeaderIdx + 1

	a.GoToSourceLine(30, model.LineSideNew)

	assertLineno(t, cursorNewLineno(a), 30, "cursor new lineno")
	if _, ok := a.ExpandedTop[gapID]; ok {
		t.Error("no top expansion when cursor is below the gap")
	}
	// Gap covers new lines 6..=49 (44 lines). Up expansion to line 30
	// reveals lines 30..=49 = 20 lines.
	assertEq(t, len(a.ExpandedBottom[gapID]), 20, "bottom expansion length")
	hasDownExpander := anyAnnotation(a, func(l *AnnotatedLine) bool {
		return l.Kind == AnnExpander && l.GapID == gapID && l.Direction == ExpandDown
	})
	if !hasDownExpander {
		t.Error("remaining hidden lines need a down expander above the cursor")
	}
}

func TestShouldExpandOnlyUpToTargetLineNotFullGap(t *testing.T) {
	// Gap before hunk spans new lines 1..=50. Jumping to line 20 should
	// reveal lines 1..=20 and leave 21..=50 collapsed behind an up
	// expander.
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(51, 5)})
	a := buildAppWithFiles([]model.DiffFile{file}, 100)
	gapID := GapID{FileIdx: 0, HunkIdx: 0}

	a.GoToSourceLine(20, model.LineSideNew)

	assertLineno(t, cursorNewLineno(a), 20, "cursor new lineno")
	assertEq(t, len(a.ExpandedTop[gapID]), 20, "only lines up to the target should expand")
	if _, ok := a.ExpandedBottom[gapID]; ok {
		t.Error("no bottom expansion should happen for a downward jump")
	}
	hasUpExpander := anyAnnotation(a, func(l *AnnotatedLine) bool {
		return l.Kind == AnnExpander && l.GapID == gapID && l.Direction == ExpandUp
	})
	if !hasUpExpander {
		t.Error("remaining hidden lines need an up expander")
	}
}

func TestShouldShowEndOfFileExpander(t *testing.T) {
	// given: hunk at lines 1-5 and total 100 lines (95 lines after hunk)
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 5)})
	a := buildAppWithFiles([]model.DiffFile{file}, 100)
	eofGapID := GapID{FileIdx: 0, HunkIdx: 1} // len(hunks) == 1

	expanderCount := countAnnotations(a, func(l *AnnotatedLine) bool {
		return l.Kind == AnnExpander && l.GapID == eofGapID && l.Direction == ExpandDown
	})
	assertEq(t, expanderCount, 1, "should show down expander at end of file")

	hiddenCount := countAnnotations(a, func(l *AnnotatedLine) bool {
		return l.Kind == AnnHiddenLines && l.GapID == eofGapID && l.Count == 95
	})
	assertEq(t, hiddenCount, 1, "should show hidden lines count")

	assertEq(t, a.renderedHeight(), len(a.LineAnnotations),
		"file render height sum must match annotation count")
}

func TestShouldExpandDownAtEndOfFile(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 5)})
	a := buildAppWithFiles([]model.DiffFile{file}, 100)
	eofGapID := GapID{FileIdx: 0, HunkIdx: 1}

	if err := a.ExpandGap(eofGapID, ExpandDown, new(20)); err != nil {
		t.Fatal(err)
	}

	// then: 20 lines expanded from start of EOF gap (lines 6-25)
	content := a.ExpandedTop[eofGapID]
	assertEq(t, len(content), 20, "expanded top length")
	assertLineno(t, content[0].NewLineno, 6, "first expanded line")
	assertLineno(t, content[19].NewLineno, 25, "last expanded line")
}

func TestShouldNotShowEofGapWhenHunkEndsAtFileEnd(t *testing.T) {
	// given: hunk at lines 1-100 and total 100 lines (no gap)
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 100)})
	a := buildAppWithFiles([]model.DiffFile{file}, 100)
	eofGapID := GapID{FileIdx: 0, HunkIdx: 1}

	expanderCount := countAnnotations(a, func(l *AnnotatedLine) bool {
		return l.Kind == AnnExpander && l.GapID == eofGapID
	})
	assertEq(t, expanderCount, 0, "no EOF expander when hunk covers entire file")
}

func TestShouldHandleSubsequentEofExpansions(t *testing.T) {
	// given: hunk at lines 1-5, total 50 lines, already expanded 20
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 5)})
	a := buildAppWithFiles([]model.DiffFile{file}, 50)
	eofGapID := GapID{FileIdx: 0, HunkIdx: 1}
	if err := a.ExpandGap(eofGapID, ExpandDown, new(20)); err != nil {
		t.Fatal(err)
	}

	if err := a.ExpandGap(eofGapID, ExpandDown, new(20)); err != nil {
		t.Fatal(err)
	}

	// then: 40 lines total (lines 6-45)
	content := a.ExpandedTop[eofGapID]
	assertEq(t, len(content), 40, "expanded top length")
	assertLineno(t, content[0].NewLineno, 6, "first expanded line")
	assertLineno(t, content[39].NewLineno, 45, "last expanded line")
}

func TestShouldShowSmallEofGapExpanderWithoutHiddenLines(t *testing.T) {
	// given: hunk at lines 1-90 and total 100 lines (10-line gap)
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 90)})
	a := buildAppWithFiles([]model.DiffFile{file}, 100)
	eofGapID := GapID{FileIdx: 0, HunkIdx: 1}

	expanderCount := countAnnotations(a, func(l *AnnotatedLine) bool {
		return l.Kind == AnnExpander && l.GapID == eofGapID && l.Direction == ExpandDown
	})
	assertEq(t, expanderCount, 1, "down expander count")

	hiddenCount := countAnnotations(a, func(l *AnnotatedLine) bool {
		return l.Kind == AnnHiddenLines && l.GapID == eofGapID
	})
	assertEq(t, hiddenCount, 0, "small EOF gap should not show hidden lines")
}

func TestTotalLinesMustMatchAnnotationsWithEofGaps(t *testing.T) {
	// Multiple files, each with EOF gaps of different sizes.
	files := []model.DiffFile{
		makeFileWithHunks("a.rs", []model.DiffHunk{makeHunk(10, 5)}),                 // gap before (9) + gap after
		makeFileWithHunks("b.rs", []model.DiffHunk{makeHunk(1, 5), makeHunk(30, 5)}), // between gap + EOF gap
		makeFileWithHunks("c.rs", []model.DiffHunk{makeHunk(1, 100)}),                // no EOF gap
	}
	a := buildAppWithFiles(files, 100)

	assertEq(t, a.renderedHeight(), len(a.LineAnnotations),
		"TotalLines must equal len(LineAnnotations)")
}

func TestShouldNotShowEofGapForDeletedFiles(t *testing.T) {
	// given: a deleted file with hunks (old-side content)
	hunks := []model.DiffHunk{makeHunk(1, 5)}
	file := model.DiffFile{
		OldPath:     new("deleted.rs"),
		Status:      model.StatusDeleted,
		Hunks:       hunks,
		ContentHash: model.ComputeContentHash(hunks),
	}
	a := buildAppWithFiles([]model.DiffFile{file}, 100)
	eofGapID := GapID{FileIdx: 0, HunkIdx: 1}

	expanderCount := countAnnotations(a, func(l *AnnotatedLine) bool {
		return l.Kind == AnnExpander && l.GapID == eofGapID
	})
	assertEq(t, expanderCount, 0, "deleted files should not have EOF expander")

	assertEq(t, a.renderedHeight(), len(a.LineAnnotations), "total lines must match annotations")
}

// TestGapRemainingCountsWhatIsStillHidden: the expander label promised the
// whole gap ("expand (20 lines)") after part of it was already shown, and
// the merged expander could promise 20 lines and reveal 5.
func TestGapRemainingCountsWhatIsStillHidden(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 5), makeHunk(36, 5)})
	a := buildAppWithFiles([]model.DiffFile{file}, 100)
	gapID := GapID{FileIdx: 0, HunkIdx: 1}
	size, _ := a.GapSize(gapID)

	if err := a.ExpandGap(gapID, ExpandDown, new(20)); err != nil {
		t.Fatal(err)
	}
	if err := a.ExpandGap(gapID, ExpandUp, new(5)); err != nil {
		t.Fatal(err)
	}
	remaining, ok := a.GapRemaining(gapID)
	if !ok || remaining != size-25 {
		t.Fatalf("GapRemaining = %d, %v; want %d (size %d less 25 shown)", remaining, ok, size-25, size)
	}
}

// TestExpandGapWithAZeroLimitFetchesNothing: a zero limit wrapped around in
// the unsigned arithmetic and expanded the whole gap upwards.
func TestExpandGapWithAZeroLimitFetchesNothing(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 5), makeHunk(50, 5)})
	a := buildAppWithFiles([]model.DiffFile{file}, 100)
	gapID := GapID{FileIdx: 0, HunkIdx: 1}
	for _, dir := range []ExpandDirection{ExpandUp, ExpandDown} {
		if err := a.ExpandGap(gapID, dir, new(0)); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(a.ExpandedTop[gapID]) + len(a.ExpandedBottom[gapID]); n != 0 {
		t.Fatalf("a zero limit expanded %d lines", n)
	}
}
