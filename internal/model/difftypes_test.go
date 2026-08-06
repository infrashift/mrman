package model

// difftypes_test.go covers DiffHunk.CommentSpan, the range a whole-hunk
// comment anchors to. The rules that matter are that it reads the lines the
// hunk actually carries rather than the @@ counts, and that it commits to one
// diff side — submit rejects a range straddling both.

import "testing"

func spanU32(v uint32) *uint32 { return &v }

// ctxLine, addLine and delLine build the three line origins with the side
// numbering a real diff would give them.
func ctxLine(oldNo, newNo uint32) DiffLine {
	return DiffLine{Origin: OriginContext, Content: "ctx", OldLineno: spanU32(oldNo), NewLineno: spanU32(newNo)}
}

func addLine(newNo uint32) DiffLine {
	return DiffLine{Origin: OriginAddition, Content: "add", NewLineno: spanU32(newNo)}
}

func delLine(oldNo uint32) DiffLine {
	return DiffLine{Origin: OriginDeletion, Content: "del", OldLineno: spanU32(oldNo)}
}

func TestCommentSpanPrefersNewSideOnAMixedHunk(t *testing.T) {
	// The shape of an ordinary edit: context, a deletion, its replacement,
	// more context. The deletion carries an old lineno only.
	h := &DiffHunk{Lines: []DiffLine{
		ctxLine(40, 42),
		delLine(41),
		addLine(43),
		ctxLine(42, 44),
	}}

	span, side, ok := h.CommentSpan()
	if !ok {
		t.Fatal("a hunk with new-side lines must yield a span")
	}
	if side != LineSideNew {
		t.Errorf("side: got %q, want %q", side, LineSideNew)
	}
	// 42..44 — the new-side lines only. The deleted old line 41 is not in
	// range, and must not drag the start down to 41.
	if span != (LineRange{Start: 42, End: 44}) {
		t.Errorf("span: got %+v, want {42 44}", span)
	}
}

func TestCommentSpanFallsBackToOldSideOnAPureDeletion(t *testing.T) {
	h := &DiffHunk{Lines: []DiffLine{delLine(10), delLine(11), delLine(12)}}

	span, side, ok := h.CommentSpan()
	if !ok {
		t.Fatal("a pure-deletion hunk must still yield a span")
	}
	if side != LineSideOld {
		t.Errorf("side: got %q, want %q", side, LineSideOld)
	}
	if span != (LineRange{Start: 10, End: 12}) {
		t.Errorf("span: got %+v, want {10 12}", span)
	}
}

func TestCommentSpanOnAPureAddition(t *testing.T) {
	h := &DiffHunk{Lines: []DiffLine{addLine(7), addLine(8)}}

	span, side, ok := h.CommentSpan()
	if !ok {
		t.Fatal("a pure-addition hunk must yield a span")
	}
	if side != LineSideNew {
		t.Errorf("side: got %q, want %q", side, LineSideNew)
	}
	if span != (LineRange{Start: 7, End: 8}) {
		t.Errorf("span: got %+v, want {7 8}", span)
	}
}

func TestCommentSpanOnASingleLineHunkIsSingle(t *testing.T) {
	h := &DiffHunk{Lines: []DiffLine{addLine(5)}}

	span, _, ok := h.CommentSpan()
	if !ok {
		t.Fatal("expected a span")
	}
	if !span.IsSingle() {
		t.Errorf("span %+v should be single, so submit collapses it to a line comment", span)
	}
}

// TestCommentSpanIgnoresTheHeaderCounts pins the choice to read Lines rather
// than NewStart/NewCount: submit requires both endpoints of a range to be
// present on the side it anchors to, and the counts can name lines the hunk
// does not carry.
func TestCommentSpanIgnoresTheHeaderCounts(t *testing.T) {
	h := &DiffHunk{
		NewStart: 1,
		NewCount: 999,
		Lines:    []DiffLine{ctxLine(60, 60), addLine(61)},
	}

	span, _, ok := h.CommentSpan()
	if !ok {
		t.Fatal("expected a span")
	}
	if span != (LineRange{Start: 60, End: 61}) {
		t.Errorf("span: got %+v, want {60 61} from the lines, not the counts", span)
	}
}

func TestCommentSpanOnAnEmptyHunk(t *testing.T) {
	h := &DiffHunk{}

	if _, _, ok := h.CommentSpan(); ok {
		t.Error("a hunk with no lines has nothing to anchor a comment to")
	}
}
