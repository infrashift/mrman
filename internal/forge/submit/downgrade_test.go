package submit

import "testing"

func TestDowngradeCollapsesRangeToEndLineWithPrefix(t *testing.T) {
	startSide := SideNew
	in := []InlineComment{{
		Path:      "a.go",
		Line:      12,
		Side:      SideNew,
		StartLine: new(uint32(10)),
		StartSide: &startSide,
		Body:      "[NOTE] ranged",
		CommentID: "c1",
	}}
	out := DowngradeMultiline(in)
	if len(out) != 1 {
		t.Fatalf("len = %d", len(out))
	}
	got := out[0]
	if got.Body != "Lines 10–12: [NOTE] ranged" {
		t.Fatalf("body = %q", got.Body)
	}
	if got.Line != 12 || got.StartLine != nil || got.StartSide != nil {
		t.Fatalf("anchor fields = %+v", got)
	}
	// Input must not be mutated.
	if in[0].Body != "[NOTE] ranged" || in[0].StartLine == nil {
		t.Fatalf("input mutated: %+v", in[0])
	}
}

func TestDowngradeLeavesSingleLineCommentsUntouched(t *testing.T) {
	in := []InlineComment{{Path: "a.go", Line: 7, Side: SideOld, Body: "single", CommentID: "c2"}}
	out := DowngradeMultiline(in)
	if out[0].Body != "single" || out[0].Line != 7 || out[0].StartLine != nil {
		t.Fatalf("comment changed: %+v", out[0])
	}
}

func TestDowngradeDropsDegenerateRangeWithoutPrefix(t *testing.T) {
	// StartLine equal to the end line carries no information; clear it
	// silently instead of writing "Lines 5-5:".
	in := []InlineComment{{Path: "a.go", Line: 5, Side: SideNew, StartLine: new(uint32(5)), Body: "b"}}
	out := DowngradeMultiline(in)
	if out[0].Body != "b" || out[0].StartLine != nil {
		t.Fatalf("comment = %+v", out[0])
	}
}

func TestDowngradePreservesOrderAndCount(t *testing.T) {
	in := []InlineComment{
		{Path: "a.go", Line: 3, Side: SideNew, Body: "one"},
		{Path: "b.go", Line: 9, Side: SideNew, StartLine: new(uint32(4)), Body: "two"},
		{Path: "c.go", Line: 1, Side: SideOld, Body: "three"},
	}
	out := DowngradeMultiline(in)
	if len(out) != 3 {
		t.Fatalf("len = %d", len(out))
	}
	if out[0].Body != "one" || out[1].Body != "Lines 4–9: two" || out[2].Body != "three" {
		t.Fatalf("bodies = %q %q %q", out[0].Body, out[1].Body, out[2].Body)
	}
}

func TestDowngradeEmptyInput(t *testing.T) {
	if out := DowngradeMultiline(nil); len(out) != 0 {
		t.Fatalf("len = %d", len(out))
	}
}
