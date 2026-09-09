package app

// find_source_line_test.go ports tuicr's
// src/app/tests/find_source_line_tests.rs in full.

import (
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

func fslDiffLine(fileIdx int, newLineno *uint32) AnnotatedLine {
	return AnnotatedLine{Kind: AnnDiffLine, FileIdx: fileIdx, NewLineno: newLineno}
}

func fslDiffLineWithOld(fileIdx int, oldLineno, newLineno *uint32) AnnotatedLine {
	return AnnotatedLine{Kind: AnnDiffLine, FileIdx: fileIdx, OldLineno: oldLineno, NewLineno: newLineno}
}

//nolint:unparam // mirrors the tuicr test helper; the ported tests only search file 0
func fslSbsLine(fileIdx int, newLineno *uint32) AnnotatedLine {
	return AnnotatedLine{Kind: AnnSideBySideLine, FileIdx: fileIdx, NewLineno: newLineno}
}

func exact(idx int) FindSourceLineResult { return FindSourceLineResult{Kind: FindExact, Index: idx} }
func nearest(idx int) FindSourceLineResult {
	return FindSourceLineResult{Kind: FindNearest, Index: idx}
}

func TestShouldFindExactMatch(t *testing.T) {
	annotations := []AnnotatedLine{
		{Kind: AnnFileHeader, FileIdx: 0},
		fslDiffLine(0, new(uint32(10))),
		fslDiffLine(0, new(uint32(11))),
		fslDiffLine(0, new(uint32(12))),
	}

	result := findSourceLine(annotations, 0, 11, model.LineSideNew)
	assertEq(t, result, exact(2), "exact match")
}

func TestShouldFindNearestWhenNoExactMatch(t *testing.T) {
	annotations := []AnnotatedLine{
		fslDiffLine(0, new(uint32(10))),
		fslDiffLine(0, new(uint32(15))),
		fslDiffLine(0, new(uint32(20))),
	}

	// Target 12 is closest to line 10 (dist=2) vs 15 (dist=3) vs 20 (dist=8).
	result := findSourceLine(annotations, 0, 12, model.LineSideNew)
	assertEq(t, result, nearest(0), "nearest below")
}

func TestShouldFindNearestAboveTarget(t *testing.T) {
	annotations := []AnnotatedLine{
		fslDiffLine(0, new(uint32(10))),
		fslDiffLine(0, new(uint32(15))),
		fslDiffLine(0, new(uint32(20))),
	}

	// Target 18 is closest to line 20 (dist=2) vs 15 (dist=3) vs 10 (dist=8).
	result := findSourceLine(annotations, 0, 18, model.LineSideNew)
	assertEq(t, result, nearest(2), "nearest above")
}

func TestShouldReturnNotFoundForEmptyAnnotations(t *testing.T) {
	result := findSourceLine(nil, 0, 42, model.LineSideNew)
	assertEq(t, result.Kind, FindNotFound, "empty annotations")
}

func TestShouldReturnNotFoundWhenNoLinesInCurrentFile(t *testing.T) {
	annotations := []AnnotatedLine{fslDiffLine(1, new(uint32(10))), fslDiffLine(1, new(uint32(20)))}

	// File 0 has no lines.
	result := findSourceLine(annotations, 0, 10, model.LineSideNew)
	assertEq(t, result.Kind, FindNotFound, "wrong file only")
}

func TestShouldSkipLinesFromOtherFiles(t *testing.T) {
	annotations := []AnnotatedLine{
		fslDiffLine(0, new(uint32(100))), // file 0, line 100
		fslDiffLine(1, new(uint32(42))),  // file 1, exact match but wrong file
		fslDiffLine(0, new(uint32(50))),  // file 0, line 50
	}

	// Searching file 0 for line 42 — nearest (50, dist=8), not file 1's exact.
	result := findSourceLine(annotations, 0, 42, model.LineSideNew)
	assertEq(t, result, nearest(2), "skip other files")
}

func TestShouldSkipNonDiffLineAnnotations(t *testing.T) {
	annotations := []AnnotatedLine{
		{Kind: AnnFileHeader, FileIdx: 0},
		{Kind: AnnHunkHeader, FileIdx: 0, HunkIdx: 0},
		{Kind: AnnSpacing},
		fslDiffLine(0, new(uint32(42))),
	}

	result := findSourceLine(annotations, 0, 42, model.LineSideNew)
	assertEq(t, result, exact(3), "skip non-diff annotations")
}

func TestShouldSkipDiffLinesWithNoNewLineno(t *testing.T) {
	// Deletion-only lines have new_lineno = nil.
	annotations := []AnnotatedLine{fslDiffLine(0, nil), fslDiffLine(0, new(uint32(20)))}

	result := findSourceLine(annotations, 0, 5, model.LineSideNew)
	assertEq(t, result, nearest(1), "skip nil new lineno")
}

func TestShouldWorkWithSideBySideLines(t *testing.T) {
	annotations := []AnnotatedLine{
		fslSbsLine(0, new(uint32(10))),
		fslSbsLine(0, new(uint32(20))),
		fslSbsLine(0, new(uint32(30))),
	}

	result := findSourceLine(annotations, 0, 20, model.LineSideNew)
	assertEq(t, result, exact(1), "sbs exact")
}

func TestShouldHandleMixedDiffAndSbsLines(t *testing.T) {
	annotations := []AnnotatedLine{
		fslDiffLine(0, new(uint32(10))),
		fslSbsLine(0, new(uint32(20))),
		fslDiffLine(0, new(uint32(30))),
	}

	// Nearest is line 20 (dist=5) or line 30 (dist=5), first match wins.
	result := findSourceLine(annotations, 0, 25, model.LineSideNew)
	assertEq(t, result, nearest(1), "mixed nearest tie")
}

func TestShouldReturnNotFoundWhenOnlyNonLineAnnotations(t *testing.T) {
	annotations := []AnnotatedLine{
		{Kind: AnnFileHeader, FileIdx: 0},
		{Kind: AnnSpacing},
		{Kind: AnnHunkHeader, FileIdx: 0, HunkIdx: 0},
	}

	result := findSourceLine(annotations, 0, 42, model.LineSideNew)
	assertEq(t, result.Kind, FindNotFound, "only non-line annotations")
}

func TestShouldPreferExactMatchOverEarlierNearest(t *testing.T) {
	annotations := []AnnotatedLine{
		fslDiffLine(0, new(uint32(41))), // dist=1 from target 42
		fslDiffLine(0, new(uint32(42))), // exact match
		fslDiffLine(0, new(uint32(43))), // dist=1 from target 42
	}

	result := findSourceLine(annotations, 0, 42, model.LineSideNew)
	assertEq(t, result, exact(1), "prefer exact")
}

func TestShouldFindNearestForTargetZero(t *testing.T) {
	// target = 0 is out-of-range (lines are 1-indexed) but should still
	// return the nearest line rather than panicking.
	annotations := []AnnotatedLine{fslDiffLine(0, new(uint32(1))), fslDiffLine(0, new(uint32(5)))}

	result := findSourceLine(annotations, 0, 0, model.LineSideNew)
	assertEq(t, result, nearest(0), "target zero")
}

func TestShouldTieBreakNearestByIterationOrder(t *testing.T) {
	// When two lines are equidistant, the first one encountered wins.
	annotations := []AnnotatedLine{
		fslDiffLine(0, new(uint32(30))),
		fslDiffLine(0, new(uint32(50))),
		fslDiffLine(0, new(uint32(10))),
	}

	result := findSourceLine(annotations, 0, 20, model.LineSideNew)
	assertEq(t, result, nearest(0), "tie-break by iteration order")
}

func TestShouldMatchOldLinenoWhenSideIsOld(t *testing.T) {
	// Deletion-only lines carry old_lineno but no new_lineno. :o<n> must
	// match those.
	annotations := []AnnotatedLine{
		fslDiffLineWithOld(0, new(uint32(5)), nil),
		fslDiffLineWithOld(0, new(uint32(10)), nil),
		fslDiffLine(0, new(uint32(50))), // new-side line — ignored when side=Old
	}

	assertEq(t, findSourceLine(annotations, 0, 10, model.LineSideOld), exact(1), "old exact")
	assertEq(t, findSourceLine(annotations, 0, 7, model.LineSideOld), nearest(0), "old nearest")
}

func TestShouldNotMatchNewLinenoWhenSideIsOld(t *testing.T) {
	// A pure-addition line has no old_lineno; searching old-side should not
	// fall back to its new_lineno.
	annotations := []AnnotatedLine{fslDiffLine(0, new(uint32(42)))}

	result := findSourceLine(annotations, 0, 42, model.LineSideOld)
	assertEq(t, result.Kind, FindNotFound, "no old fallback to new")
}
