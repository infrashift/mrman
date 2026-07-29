package render

import (
	"image/color"
	"testing"

	"github.com/infrashift/mrman/internal/syntax"
)

func TestFromSyntaxSpansConvertsHexColorsAndAttributes(t *testing.T) {
	in := []syntax.Span{
		{Text: "let", Style: syntax.Style{FG: "#ff0000", Bold: true}},
		{Text: " x", Style: syntax.Style{FG: "#0000ff", BG: "#00ff00", Italic: true, Underline: true}},
		{Text: ";", Style: syntax.Style{}},
	}
	got := FromSyntaxSpans(in)
	want := []Span{
		{Text: "let", Style: Style{Fg: color.RGBA{R: 255, A: 255}, Bold: true}},
		{Text: " x", Style: Style{
			Fg:     color.RGBA{B: 255, A: 255},
			Bg:     color.RGBA{G: 255, A: 255},
			Italic: true, Underline: true,
		}},
		{Text: ";", Style: Style{}},
	}
	requireSpans(t, got, want)
}

func TestFromSyntaxSpansTreatsInvalidHexAsNil(t *testing.T) {
	in := []syntax.Span{
		{Text: "a", Style: syntax.Style{FG: "red"}},
		{Text: "b", Style: syntax.Style{FG: "#fff"}},
		{Text: "c", Style: syntax.Style{FG: "#zzzzzz"}},
		{Text: "d", Style: syntax.Style{FG: ""}},
	}
	for i, sp := range FromSyntaxSpans(in) {
		if sp.Style.Fg != nil {
			t.Fatalf("span %d: expected nil Fg, got %#v", i, sp.Style.Fg)
		}
	}
}

func TestStringWidthUsesPinnedNonEastAsianWidths(t *testing.T) {
	cases := []struct {
		s    string
		want int
	}{
		{"hello", 5},
		{"中文", 4},
		{"🙂", 2},
		{"\t", 0}, // control chars are zero columns, matching unicode-width
		{"héllo", 5},
		{"", 0},
	}
	for _, c := range cases {
		if got := StringWidth(c.s); got != c.want {
			t.Fatalf("StringWidth(%q) = %d, want %d", c.s, got, c.want)
		}
	}
}

func TestSpanWidthSumsAllSpans(t *testing.T) {
	spans := []Span{{Text: "ab", Style: red}, {Text: "中", Style: blue}, {Text: ""}}
	if got := SpanWidth(spans); got != 4 {
		t.Fatalf("got %d, want 4", got)
	}
}
