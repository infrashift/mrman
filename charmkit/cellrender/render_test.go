package cellrender

import (
	"testing"
)

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
