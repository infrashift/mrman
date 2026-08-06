package submit

import (
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

func u32(v uint32) *uint32 { return &v }

func strPtr(s string) *string { return &s }

func sidePtr(s model.LineSide) *model.LineSide { return &s }

func line(origin model.LineOrigin, newLn, oldLn *uint32) model.DiffLine {
	return model.DiffLine{Origin: origin, OldLineno: oldLn, NewLineno: newLn}
}

func hunk(lines ...model.DiffLine) model.DiffHunk {
	return model.DiffHunk{Header: "@@", OldStart: 1, NewStart: 1, Lines: lines}
}

func fileWithHunks(hunks ...model.DiffHunk) *model.DiffFile {
	return &model.DiffFile{
		OldPath: strPtr("src/lib.rs"),
		NewPath: strPtr("src/lib.rs"),
		Status:  model.StatusModified,
		Hunks:   hunks,
	}
}

func typicalFile() *model.DiffFile {
	return fileWithHunks(hunk(
		line(model.OriginContext, u32(10), u32(10)),
		line(model.OriginDeletion, nil, u32(11)),
		line(model.OriginAddition, u32(11), nil),
		line(model.OriginContext, u32(12), u32(12)),
	))
}

func commentWithLine(side model.LineSide, newLn, oldLn *uint32) *model.Comment {
	c := model.NewComment("needs work", model.CommentTypeFromID("issue"), sidePtr(side))
	c.LineContext = &model.LineContext{NewLine: newLn, OldLine: oldLn}
	return c
}

func commentRange(side model.LineSide, r model.LineRange) *model.Comment {
	return model.NewCommentWithRange("ranged", model.CommentTypeFromID("note"), sidePtr(side), r)
}

func commentFileLevel() *model.Comment {
	return model.NewComment("module is messy", model.CommentTypeFromID("note"), nil)
}

// anchorFrom infers the CommentAnchor from a test fixture's comment shape,
// mirroring the tuicr test helper: production callers build the anchor from
// session structure instead.
func anchorFrom(c *model.Comment) CommentAnchor {
	if c.LineRange != nil {
		return RangeAnchor()
	}
	side := model.LineSideNew
	if c.Side != nil {
		side = *c.Side
	}
	var ln *uint32
	if c.LineContext != nil {
		if side == model.LineSideNew {
			ln = c.LineContext.NewLine
		} else {
			ln = c.LineContext.OldLine
		}
	}
	if ln != nil {
		return LineAnchor(*ln, side)
	}
	return FileLevelAnchor()
}

func mustInline(t *testing.T, m MappedComment) *InlineComment {
	t.Helper()
	if m.Inline == nil {
		t.Fatalf("expected Inline, got Unmappable %+v", m.Unmappable)
	}
	return m.Inline
}

func mustUnmappable(t *testing.T, m MappedComment) *UnmappableItem {
	t.Helper()
	if m.Unmappable == nil {
		t.Fatalf("expected Unmappable, got Inline %+v", m.Inline)
	}
	return m.Unmappable
}

// Single-line mapping

func TestMapsSingleAdditionLineToNewSide(t *testing.T) {
	c := commentWithLine(model.LineSideNew, u32(11), nil)
	inline := mustInline(t, MapComment(c, anchorFrom(c), typicalFile(), true))
	if inline.Line != 11 || inline.Side != SideNew {
		t.Fatalf("line/side = %d/%s", inline.Line, inline.Side)
	}
	if inline.StartLine != nil || inline.StartSide != nil {
		t.Fatalf("unexpected start fields: %+v", inline)
	}
	if !strings.HasPrefix(inline.Body, "[ISSUE] ") {
		t.Fatalf("body = %q", inline.Body)
	}
}

func TestMapsSingleContextLineToNewSide(t *testing.T) {
	c := commentWithLine(model.LineSideNew, u32(10), u32(10))
	inline := mustInline(t, MapComment(c, anchorFrom(c), typicalFile(), true))
	if inline.Line != 10 || inline.Side != SideNew {
		t.Fatalf("line/side = %d/%s", inline.Line, inline.Side)
	}
}

func TestPopulatesCounterpartLineForContextLine(t *testing.T) {
	c := commentWithLine(model.LineSideNew, u32(10), u32(10))
	inline := mustInline(t, MapComment(c, anchorFrom(c), typicalFile(), true))
	if inline.CounterpartLine == nil || *inline.CounterpartLine != 10 {
		t.Fatalf("counterpart = %v", inline.CounterpartLine)
	}
}

func TestNoCounterpartLineForAdditionLine(t *testing.T) {
	c := commentWithLine(model.LineSideNew, u32(11), nil)
	inline := mustInline(t, MapComment(c, anchorFrom(c), typicalFile(), true))
	if inline.CounterpartLine != nil {
		t.Fatalf("counterpart = %v", *inline.CounterpartLine)
	}
}

func TestMapsSingleDeletionLineToOldSide(t *testing.T) {
	c := commentWithLine(model.LineSideOld, nil, u32(11))
	inline := mustInline(t, MapComment(c, anchorFrom(c), typicalFile(), true))
	if inline.Line != 11 || inline.Side != SideOld {
		t.Fatalf("line/side = %d/%s", inline.Line, inline.Side)
	}
}

func TestMarksLineOutsideDiffAsUnmappable(t *testing.T) {
	c := commentWithLine(model.LineSideNew, u32(99), nil)
	item := mustUnmappable(t, MapComment(c, anchorFrom(c), typicalFile(), true))
	if item.Reason != LineNotInDiff {
		t.Fatalf("reason = %v", item.Reason)
	}
	if item.File != "src/lib.rs" {
		t.Fatalf("file = %q", item.File)
	}
}

// Range mapping

func TestMapsNewSideRangeToStartAndEnd(t *testing.T) {
	file := fileWithHunks(hunk(
		line(model.OriginAddition, u32(10), nil),
		line(model.OriginAddition, u32(11), nil),
		line(model.OriginAddition, u32(12), nil),
	))
	c := commentRange(model.LineSideNew, model.NewLineRange(10, 12))
	inline := mustInline(t, MapComment(c, anchorFrom(c), file, true))
	if inline.Line != 12 || inline.Side != SideNew {
		t.Fatalf("line/side = %d/%s", inline.Line, inline.Side)
	}
	if inline.StartLine == nil || *inline.StartLine != 10 {
		t.Fatalf("start = %v", inline.StartLine)
	}
	if inline.StartSide == nil || *inline.StartSide != SideNew {
		t.Fatalf("start side = %v", inline.StartSide)
	}
}

func TestMapsOldSideRangeToStartAndEnd(t *testing.T) {
	file := fileWithHunks(hunk(
		line(model.OriginDeletion, nil, u32(20)),
		line(model.OriginDeletion, nil, u32(21)),
		line(model.OriginDeletion, nil, u32(22)),
	))
	c := commentRange(model.LineSideOld, model.NewLineRange(20, 22))
	inline := mustInline(t, MapComment(c, anchorFrom(c), file, true))
	if inline.Line != 22 || inline.Side != SideOld {
		t.Fatalf("line/side = %d/%s", inline.Line, inline.Side)
	}
	if inline.StartLine == nil || *inline.StartLine != 20 {
		t.Fatalf("start = %v", inline.StartLine)
	}
	if inline.StartSide == nil || *inline.StartSide != SideOld {
		t.Fatalf("start side = %v", inline.StartSide)
	}
}

func TestFlattensSingleLineRangeToInlineWithoutStartFields(t *testing.T) {
	file := fileWithHunks(hunk(line(model.OriginAddition, u32(15), nil)))
	c := commentRange(model.LineSideNew, model.SingleLineRange(15))
	inline := mustInline(t, MapComment(c, anchorFrom(c), file, true))
	if inline.Line != 15 || inline.StartLine != nil || inline.StartSide != nil {
		t.Fatalf("inline = %+v", inline)
	}
}

func TestMarksMixedSideRangeAsUnmappable(t *testing.T) {
	// Range claims New side but the file only has Old-side lines 20-22.
	file := fileWithHunks(hunk(
		line(model.OriginDeletion, nil, u32(20)),
		line(model.OriginDeletion, nil, u32(21)),
		line(model.OriginDeletion, nil, u32(22)),
	))
	c := commentRange(model.LineSideNew, model.NewLineRange(20, 22))
	item := mustUnmappable(t, MapComment(c, anchorFrom(c), file, true))
	if item.Reason != MixedSideRange {
		t.Fatalf("reason = %v", item.Reason)
	}
}

func TestMarksRangeWithoutSideAsUnmappable(t *testing.T) {
	c := model.NewCommentWithRange("ranged", model.CommentTypeFromID("note"), nil, model.NewLineRange(10, 12))
	item := mustUnmappable(t, MapComment(c, RangeAnchor(), typicalFile(), true))
	if item.Reason != MixedSideRange {
		t.Fatalf("reason = %v", item.Reason)
	}
}

func TestMarksRangeAnchorWithoutLineRangeAsUnmappable(t *testing.T) {
	c := model.NewComment("ranged", model.CommentTypeFromID("note"), sidePtr(model.LineSideNew))
	item := mustUnmappable(t, MapComment(c, RangeAnchor(), typicalFile(), true))
	if item.Reason != MixedSideRange {
		t.Fatalf("reason = %v", item.Reason)
	}
}

// File-level mapping

func TestAnchorsFileLevelToFirstValidNewLine(t *testing.T) {
	c := commentFileLevel()
	inline := mustInline(t, MapComment(c, anchorFrom(c), typicalFile(), true))
	if inline.Line != 10 || inline.Side != SideNew {
		t.Fatalf("line/side = %d/%s", inline.Line, inline.Side)
	}
	if !strings.HasPrefix(inline.Body, "[NOTE] File-level: ") {
		t.Fatalf("body = %q", inline.Body)
	}
	if inline.CounterpartLine == nil || *inline.CounterpartLine != 10 {
		t.Fatalf("counterpart = %v", inline.CounterpartLine)
	}
}

func TestMarksFileLevelWithoutNewAnchorAsUnmappable(t *testing.T) {
	// Pure deletion file: nothing on the New side.
	file := fileWithHunks(hunk(line(model.OriginDeletion, nil, u32(5))))
	c := commentFileLevel()
	item := mustUnmappable(t, MapComment(c, anchorFrom(c), file, true))
	if item.Reason != FileLevelNoAnchor {
		t.Fatalf("reason = %v", item.Reason)
	}
}

func TestMarksBinaryFileCommentAsUnmappable(t *testing.T) {
	file := typicalFile()
	file.IsBinary = true
	c := commentWithLine(model.LineSideNew, u32(11), nil)
	item := mustUnmappable(t, MapComment(c, anchorFrom(c), file, true))
	if item.Reason != BinaryFile {
		t.Fatalf("reason = %v", item.Reason)
	}
}

func TestMarksTooLargeFileCommentAsUnmappable(t *testing.T) {
	file := typicalFile()
	file.IsTooLarge = true
	c := commentFileLevel()
	item := mustUnmappable(t, MapComment(c, anchorFrom(c), file, true))
	if item.Reason != TooLargeFile {
		t.Fatalf("reason = %v", item.Reason)
	}
}

// Renamed old path

func TestSetsOldPathForRenamedFile(t *testing.T) {
	file := typicalFile()
	file.Status = model.StatusRenamed
	file.OldPath = strPtr("src/old.rs")
	file.NewPath = strPtr("src/new.rs")
	c := commentWithLine(model.LineSideNew, u32(10), u32(10))
	inline := mustInline(t, MapComment(c, anchorFrom(c), file, true))
	if inline.OldPath == nil || *inline.OldPath != "src/old.rs" {
		t.Fatalf("old path = %v", inline.OldPath)
	}
	if inline.Path != "src/new.rs" {
		t.Fatalf("path = %q", inline.Path)
	}
}

func TestOmitsOldPathWhenRenameKeepsSameName(t *testing.T) {
	file := typicalFile()
	file.Status = model.StatusRenamed
	c := commentWithLine(model.LineSideNew, u32(10), u32(10))
	inline := mustInline(t, MapComment(c, anchorFrom(c), file, true))
	if inline.OldPath != nil {
		t.Fatalf("old path = %v", *inline.OldPath)
	}
}

func TestOmitsOldPathForModifiedFile(t *testing.T) {
	c := commentWithLine(model.LineSideNew, u32(10), u32(10))
	inline := mustInline(t, MapComment(c, anchorFrom(c), typicalFile(), true))
	if inline.OldPath != nil {
		t.Fatalf("old path = %v", *inline.OldPath)
	}
}

// Body prefix toggle

func TestOmitsTypePrefixWhenDisabled(t *testing.T) {
	c := commentWithLine(model.LineSideNew, u32(11), nil)
	inline := mustInline(t, MapComment(c, anchorFrom(c), typicalFile(), false))
	if inline.Body != "needs work" {
		t.Fatalf("body = %q", inline.Body)
	}
}

func TestCommentIDCarriesThrough(t *testing.T) {
	c := commentWithLine(model.LineSideNew, u32(11), nil)
	inline := mustInline(t, MapComment(c, anchorFrom(c), typicalFile(), true))
	if inline.CommentID != c.ID {
		t.Fatalf("comment id = %q want %q", inline.CommentID, c.ID)
	}
}

// UnmappableReason labels

func TestUnmappableReasonHumanLabels(t *testing.T) {
	cases := map[UnmappableReason]string{
		MixedSideRange:       "range spans both diff sides",
		FileLevelNoAnchor:    "no valid anchor line",
		BinaryFile:           "binary file",
		TooLargeFile:         "file too large",
		LineNotInDiff:        "line not in current diff",
		UnmappableReason(99): "unmappable",
	}
	for reason, want := range cases {
		if got := reason.HumanLabel(); got != want {
			t.Errorf("HumanLabel(%d) = %q want %q", reason, got, want)
		}
	}
}

func TestSideFromLineSide(t *testing.T) {
	if SideFromLineSide(model.LineSideOld) != SideOld {
		t.Fatal("old should map to SideOld")
	}
	if SideFromLineSide(model.LineSideNew) != SideNew {
		t.Fatal("new should map to SideNew")
	}
}

// TestMapsHunkDerivedRange guards the seam between DiffHunk.CommentSpan and
// this package: a hunk comment is nothing but a range whose bounds the hunk
// chose, so the origin filter CommentSpan selects lines with has to agree with
// the one rangeEndpointsPresent validates against. If they ever drift, every
// hunk comment silently becomes MixedSideRange at submit.
func TestMapsHunkDerivedRange(t *testing.T) {
	file := typicalFile()
	span, side, ok := file.Hunks[0].CommentSpan()
	if !ok {
		t.Fatal("the fixture hunk must yield a span")
	}
	if span != model.NewLineRange(10, 12) || side != model.LineSideNew {
		t.Fatalf("span: got %+v on %q, want {10 12} on new", span, side)
	}

	inline := mustInline(t, MapComment(commentRange(side, span), RangeAnchor(), file, true))

	if inline.StartLine == nil || *inline.StartLine != 10 {
		t.Fatalf("StartLine: got %v, want 10", inline.StartLine)
	}
	if inline.Line != 12 {
		t.Errorf("Line: got %d, want 12", inline.Line)
	}
	if inline.Side != SideNew {
		t.Errorf("Side: got %v, want %v", inline.Side, SideNew)
	}
	if inline.StartSide == nil || *inline.StartSide != SideNew {
		t.Fatalf("StartSide: got %v, want %v", inline.StartSide, SideNew)
	}
}

// TestMapsHunkDerivedRangeOnAPureDeletion is the old-side fallback: the hunk
// has no new-side line to anchor to, so the range must land on the old side and
// still map.
func TestMapsHunkDerivedRangeOnAPureDeletion(t *testing.T) {
	file := fileWithHunks(hunk(
		line(model.OriginDeletion, nil, u32(30)),
		line(model.OriginDeletion, nil, u32(31)),
	))
	span, side, ok := file.Hunks[0].CommentSpan()
	if !ok {
		t.Fatal("a pure-deletion hunk must still yield a span")
	}
	if side != model.LineSideOld {
		t.Fatalf("side: got %q, want old", side)
	}

	inline := mustInline(t, MapComment(commentRange(side, span), RangeAnchor(), file, true))

	if inline.StartLine == nil || *inline.StartLine != 30 || inline.Line != 31 {
		t.Fatalf("range: got start %v end %d, want 30..31", inline.StartLine, inline.Line)
	}
	if inline.Side != SideOld {
		t.Errorf("Side: got %v, want %v", inline.Side, SideOld)
	}
}
