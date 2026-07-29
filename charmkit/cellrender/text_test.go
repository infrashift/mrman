package cellrender

import (
	"image/color"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

var (
	red  = Style{Fg: color.RGBA{R: 255, A: 255}}
	blue = Style{Fg: color.RGBA{B: 255, A: 255}}
)

func requireRows(t *testing.T, rows [][]Span, want int) {
	t.Helper()
	if len(rows) != want {
		t.Fatalf("got %d rows, want %d: %#v", len(rows), want, rows)
	}
}

func requireSpans(t *testing.T, got, want []Span) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("spans mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestTruncateStrReturnsStringUnchangedWhenWithinMaxLen(t *testing.T) {
	if got := TruncateStr("hello", 10); got != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestTruncateStrTruncatesASCIIStringWithEllipsis(t *testing.T) {
	if got := TruncateStr("hello world this is long", 10); got != "hello w..." {
		t.Fatalf("got %q", got)
	}
}

func TestTruncateStrHandlesMultibyteCharsWithoutSlicingMidRune(t *testing.T) {
	// The exact string from tuicr's bug report.
	s := `Resolve "SD : Envoi en validation manuelle après 3 rejet de la fiche employé"`
	got := TruncateStr(s, 47)
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("missing ellipsis: %q", got)
	}
	if len(got) > 47 {
		t.Fatalf("byte length %d exceeds 47: %q", len(got), got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("invalid UTF-8: %q", got)
	}
}

func TestTruncateStrHandlesStringOfOnlyMultibyteChars(t *testing.T) {
	got := TruncateStr("ééééééééé", 5)
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("missing ellipsis: %q", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("invalid UTF-8: %q", got)
	}
}

func TestTruncateOrPadPadsShortStringToWidth(t *testing.T) {
	if got := TruncateOrPad("abc", 6); got != "abc   " {
		t.Fatalf("got %q", got)
	}
}

func TestTruncateOrPadTruncatesLongStringByRuneCount(t *testing.T) {
	if got := TruncateOrPad("ééééééééé", 6); got != "ééé..." {
		t.Fatalf("got %q", got)
	}
}

func TestTruncateOrPadReturnsExactFitUnchanged(t *testing.T) {
	if got := TruncateOrPad("hello", 5); got != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestTruncateOrPadSpansTruncatesWithBaseStyledEllipsis(t *testing.T) {
	spans := []Span{{Text: "hello", Style: red}, {Text: " world", Style: blue}}
	got := TruncateOrPadSpans(spans, 8, Style{})
	requireSpans(t, got, []Span{{Text: "hello", Style: red}, {Text: "...", Style: Style{}}})
	if SpanWidth(got) != 8 {
		t.Fatalf("width %d, want 8", SpanWidth(got))
	}
}

func TestTruncateOrPadSpansDropsWideCharCrossingBoundary(t *testing.T) {
	// "中文測試" is 8 columns; width 6 leaves 3 for content, so only 中 (2
	// cols) fits — 文 would cross the boundary and is dropped whole.
	spans := []Span{{Text: "中文測試", Style: red}}
	got := TruncateOrPadSpans(spans, 6, blue)
	requireSpans(t, got, []Span{{Text: "中", Style: red}, {Text: "...", Style: blue}})
}

func TestTruncateOrPadSpansReturnsExactFitCopy(t *testing.T) {
	spans := []Span{{Text: "abc", Style: red}}
	got := TruncateOrPadSpans(spans, 3, Style{})
	requireSpans(t, got, spans)
}

func TestTruncateOrPadSpansPadsWideContentByDisplayWidth(t *testing.T) {
	spans := []Span{{Text: "中文", Style: red}} // 4 columns
	got := TruncateOrPadSpans(spans, 6, blue)
	requireSpans(t, got, []Span{{Text: "中文", Style: red}, {Text: "  ", Style: blue}})
}

func TestWrapSpansReturnsSingleRowWhenInputFits(t *testing.T) {
	rows := WrapSpans([]Span{{Text: "hello"}}, 10)
	requireRows(t, rows, 1)
	requireSpans(t, rows[0], []Span{{Text: "hello"}})
}

func TestWrapSpansReturnsSingleRowOnExactFit(t *testing.T) {
	rows := WrapSpans([]Span{{Text: "hello"}}, 5)
	requireRows(t, rows, 1)
	requireSpans(t, rows[0], []Span{{Text: "hello"}})
}

func TestWrapSpansReturnsSingleEmptyRowForEmptyInput(t *testing.T) {
	rows := WrapSpans(nil, 10)
	requireRows(t, rows, 1)
	if len(rows[0]) != 0 {
		t.Fatalf("row not empty: %#v", rows[0])
	}
}

func TestWrapSpansReturnsSingleEmptyRowForAllEmptySpanContents(t *testing.T) {
	rows := WrapSpans([]Span{{Text: "", Style: red}, {Text: ""}}, 10)
	requireRows(t, rows, 1)
	if len(rows[0]) != 0 {
		t.Fatalf("row not empty: %#v", rows[0])
	}
}

func TestWrapSpansReturnsInputUnchangedWhenWidthIsZero(t *testing.T) {
	rows := WrapSpans([]Span{{Text: "hello world"}}, 0)
	requireRows(t, rows, 1)
	requireSpans(t, rows[0], []Span{{Text: "hello world"}})
}

func TestWrapSpansWrapsAtWhitespaceWordBoundary(t *testing.T) {
	rows := WrapSpans([]Span{{Text: "hello world foo"}}, 8)
	requireRows(t, rows, 3)
	requireSpans(t, rows[0], []Span{{Text: "hello "}})
	requireSpans(t, rows[1], []Span{{Text: "world "}})
	requireSpans(t, rows[2], []Span{{Text: "foo"}})
}

func TestWrapSpansHardSplitsWordLongerThanWidth(t *testing.T) {
	rows := WrapSpans([]Span{{Text: "aaaaaaaaaa"}}, 4)
	requireRows(t, rows, 3)
	if rows[0][0].Text != "aaaa" || rows[1][0].Text != "aaaa" || rows[2][0].Text != "aa" {
		t.Fatalf("bad split: %#v", rows)
	}
}

func TestWrapSpansPreservesStylesAcrossWrapBoundary(t *testing.T) {
	rows := WrapSpans([]Span{{Text: "hello ", Style: red}, {Text: "world", Style: blue}}, 6)
	requireRows(t, rows, 2)
	requireSpans(t, rows[0], []Span{{Text: "hello ", Style: red}})
	requireSpans(t, rows[1], []Span{{Text: "world", Style: blue}})
}

func TestWrapSpansSplitsSpanThatCrossesWrapPoint(t *testing.T) {
	rows := WrapSpans([]Span{{Text: "hello world", Style: red}}, 6)
	requireRows(t, rows, 2)
	requireSpans(t, rows[0], []Span{{Text: "hello ", Style: red}})
	requireSpans(t, rows[1], []Span{{Text: "world", Style: red}})
}

func TestWrapSpansMergesSameStyleSpansIntoOneWhenWrapping(t *testing.T) {
	rows := WrapSpans([]Span{{Text: "he", Style: red}, {Text: "llo", Style: red}}, 3)
	requireRows(t, rows, 2)
	requireSpans(t, rows[0], []Span{{Text: "hel", Style: red}})
	requireSpans(t, rows[1], []Span{{Text: "lo", Style: red}})
}

func TestWrapSpansWrapsCJKCharsByDisplayWidth(t *testing.T) {
	rows := WrapSpans([]Span{{Text: "中文測試"}}, 5)
	requireRows(t, rows, 2)
	if rows[0][0].Text != "中文" || rows[1][0].Text != "測試" {
		t.Fatalf("bad CJK wrap: %#v", rows)
	}
}

func TestWrapSpansWrapsEmojiByDisplayWidth(t *testing.T) {
	rows := WrapSpans([]Span{{Text: "🙂🙂🙂"}}, 4)
	requireRows(t, rows, 2)
	if rows[0][0].Text != "🙂🙂" || rows[1][0].Text != "🙂" {
		t.Fatalf("bad emoji wrap: %#v", rows)
	}
}

func TestWrapSpansEmitsOversizedCharAloneWhenWidthIsOneAndCharIsWide(t *testing.T) {
	rows := WrapSpans([]Span{{Text: "中"}}, 1)
	requireRows(t, rows, 1)
	if rows[0][0].Text != "中" {
		t.Fatalf("got %#v", rows)
	}
}

func TestWrapSpansTreatsTabAsWhitespaceBreak(t *testing.T) {
	rows := WrapSpans([]Span{{Text: "hello\tworld"}}, 6)
	requireRows(t, rows, 2)
	if rows[0][0].Text != "hello\t" || rows[1][0].Text != "world" {
		t.Fatalf("bad tab wrap: %#v", rows)
	}
}

func TestWrapSpansPreservesLeadingWhitespaceOnContinuationRows(t *testing.T) {
	rows := WrapSpans([]Span{{Text: "foo   bar baz"}}, 7)
	requireRows(t, rows, 2)
	if rows[0][0].Text != "foo   " || rows[1][0].Text != "bar baz" {
		t.Fatalf("bad whitespace wrap: %#v", rows)
	}
}

func TestWrapSpansHandlesMultibyteUTF8CharBoundariesSafely(t *testing.T) {
	rows := WrapSpans([]Span{{Text: "ab中文cd"}}, 4)
	requireRows(t, rows, 2)
	for i, row := range rows {
		if w := SpanWidth(row); w > 4 {
			t.Fatalf("row %d width %d exceeds 4", i, w)
		}
	}
	if rows[0][0].Text != "ab中" || rows[1][0].Text != "文cd" {
		t.Fatalf("bad mixed wrap: %#v", rows)
	}
}
