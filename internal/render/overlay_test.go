package render

import (
	"image/color"
	"testing"
)

var green = color.RGBA{G: 255, A: 255}

func withBg(s Style, bg color.Color) Style {
	s.Bg = bg
	return s
}

func TestOverrideBgSplitsSpanMidRange(t *testing.T) {
	got := OverrideBg([]Span{{Text: "hello", Style: red}}, 1, 3, green)
	requireSpans(t, got, []Span{
		{Text: "h", Style: red},
		{Text: "el", Style: withBg(red, green)},
		{Text: "lo", Style: red},
	})
}

func TestOverrideBgSpansMultipleInputSpans(t *testing.T) {
	got := OverrideBg([]Span{{Text: "abc", Style: red}, {Text: "def", Style: blue}}, 2, 4, green)
	requireSpans(t, got, []Span{
		{Text: "ab", Style: red},
		{Text: "c", Style: withBg(red, green)},
		{Text: "d", Style: withBg(blue, green)},
		{Text: "ef", Style: blue},
	})
}

func TestOverrideBgExtendsToWholeWideRuneAtLowerBoundary(t *testing.T) {
	// Columns: a=0, 中=1..2, b=3. Range [2,4) starts inside 中, so the
	// whole rune gets the override.
	got := OverrideBg([]Span{{Text: "a中b", Style: red}}, 2, 4, green)
	requireSpans(t, got, []Span{
		{Text: "a", Style: red},
		{Text: "中b", Style: withBg(red, green)},
	})
}

func TestOverrideBgExtendsToWholeWideRuneAtUpperBoundary(t *testing.T) {
	// Range [0,2) ends inside 中 (columns 1..2): the whole rune extends.
	got := OverrideBg([]Span{{Text: "a中b", Style: red}}, 0, 2, green)
	requireSpans(t, got, []Span{
		{Text: "a中", Style: withBg(red, green)},
		{Text: "b", Style: red},
	})
}

func TestOverrideBgEmptyRangeReturnsInputUnchanged(t *testing.T) {
	spans := []Span{{Text: "abc", Style: red}}
	got := OverrideBg(spans, 3, 3, green)
	requireSpans(t, got, spans)
}

func TestOverrideBgRangeBeyondContentLeavesSpansUnstyled(t *testing.T) {
	got := OverrideBg([]Span{{Text: "ab", Style: red}}, 5, 9, green)
	requireSpans(t, got, []Span{{Text: "ab", Style: red}})
}

func TestOverrideBgPassesEmptySpansThrough(t *testing.T) {
	got := OverrideBg([]Span{{Text: "", Style: red}, {Text: "ab", Style: blue}}, 0, 2, green)
	requireSpans(t, got, []Span{
		{Text: "", Style: red},
		{Text: "ab", Style: withBg(blue, green)},
	})
}

func TestFillBgSetsBackgroundOnlyOnNilBgSpans(t *testing.T) {
	spans := []Span{
		{Text: "a", Style: red},
		{Text: "b", Style: withBg(blue, color.RGBA{A: 255})},
	}
	got := FillBg(spans, green)
	requireSpans(t, got, []Span{
		{Text: "a", Style: withBg(red, green)},
		{Text: "b", Style: withBg(blue, color.RGBA{A: 255})},
	})
}

func TestPadToWidthAppendsStyledSpacer(t *testing.T) {
	got := PadToWidth([]Span{{Text: "中文", Style: red}}, 7, blue)
	requireSpans(t, got, []Span{
		{Text: "中文", Style: red},
		{Text: "   ", Style: blue},
	})
	if SpanWidth(got) != 7 {
		t.Fatalf("width %d, want 7", SpanWidth(got))
	}
}

func TestPadToWidthReturnsInputWhenAlreadyWideEnough(t *testing.T) {
	spans := []Span{{Text: "hello", Style: red}}
	got := PadToWidth(spans, 5, blue)
	requireSpans(t, got, spans)
}
