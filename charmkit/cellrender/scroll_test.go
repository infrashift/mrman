package cellrender

import "testing"

var indicator = Span{Text: "▌", Style: red}

func TestApplyHorizontalScrollReturnsInputWhenScrollIsZero(t *testing.T) {
	spans := []Span{indicator, {Text: "hello"}}
	got := ApplyHorizontalScroll(spans, 0)
	requireSpans(t, got, spans)
}

func TestApplyHorizontalScrollReturnsEmptyInputUnchanged(t *testing.T) {
	got := ApplyHorizontalScroll(nil, 5)
	if len(got) != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestApplyHorizontalScrollPreservesIndicatorSpan(t *testing.T) {
	spans := []Span{indicator, {Text: "hello world"}}
	got := ApplyHorizontalScroll(spans, 6)
	requireSpans(t, got, []Span{indicator, {Text: "world"}})
}

func TestApplyHorizontalScrollSkipsAcrossMultipleSpans(t *testing.T) {
	spans := []Span{indicator, {Text: "abc", Style: red}, {Text: "def", Style: blue}}
	got := ApplyHorizontalScroll(spans, 4)
	requireSpans(t, got, []Span{indicator, {Text: "ef", Style: blue}})
}

func TestApplyHorizontalScrollDropsSpanExactlyConsumed(t *testing.T) {
	spans := []Span{indicator, {Text: "abc", Style: red}, {Text: "def", Style: blue}}
	got := ApplyHorizontalScroll(spans, 3)
	requireSpans(t, got, []Span{indicator, {Text: "def", Style: blue}})
}

func TestApplyHorizontalScrollSkipsWholeWideRuneWhenBoundaryFallsInside(t *testing.T) {
	// 中 spans columns 0-1; scrolling 1 column may not split it, so the
	// whole rune is skipped.
	spans := []Span{indicator, {Text: "中文", Style: red}}
	got := ApplyHorizontalScroll(spans, 1)
	requireSpans(t, got, []Span{indicator, {Text: "文", Style: red}})
}

func TestApplyHorizontalScrollWideRunePartialSkipStopsFollowingSpansFromShifting(t *testing.T) {
	// Skipping 2 columns of "a中" lands inside 中, so the whole rune goes;
	// the skip budget is then exhausted and the next span is kept whole.
	spans := []Span{indicator, {Text: "a中", Style: red}, {Text: "xy", Style: blue}}
	got := ApplyHorizontalScroll(spans, 2)
	requireSpans(t, got, []Span{indicator, {Text: "xy", Style: blue}})
}

func TestApplyHorizontalScrollBeyondContentLeavesOnlyIndicator(t *testing.T) {
	spans := []Span{indicator, {Text: "abc"}}
	got := ApplyHorizontalScroll(spans, 99)
	requireSpans(t, got, []Span{indicator})
}
