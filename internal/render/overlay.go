package render

import (
	"image/color"
	"strings"
)

// OverrideBg returns spans with the background of the display-column range
// [lo, hi) replaced by bg. Spans are split at rune boundaries as needed; a
// wide rune that straddles either range edge is re-backgrounded as a whole
// (the override extends to cover the full rune). Zero-width runes keep their
// original background. An empty range returns the input unchanged.
func OverrideBg(spans []Span, lo, hi int, bg color.Color) []Span {
	if hi <= lo || len(spans) == 0 {
		return spans
	}
	out := make([]Span, 0, len(spans)+2)
	col := 0
	for _, sp := range spans {
		if sp.Text == "" {
			out = append(out, sp)
			continue
		}
		var seg strings.Builder
		segIn, started := false, false
		flush := func() {
			if seg.Len() == 0 {
				return
			}
			st := sp.Style
			if segIn {
				st.Bg = bg
			}
			out = append(out, Span{Text: seg.String(), Style: st})
			seg.Reset()
		}
		for _, r := range sp.Text {
			w := widthCond.RuneWidth(r)
			in := col < hi && col+w > lo
			if started && in != segIn {
				flush()
			}
			started, segIn = true, in
			seg.WriteRune(r)
			col += w
		}
		flush()
	}
	return out
}

// FillBg returns spans with bg set on every span whose background is nil
// (terminal default); spans that already carry a background keep it.
func FillBg(spans []Span, bg color.Color) []Span {
	out := make([]Span, len(spans))
	for i, sp := range spans {
		if sp.Style.Bg == nil {
			sp.Style.Bg = bg
		}
		out[i] = sp
	}
	return out
}

// PadToWidth appends a spacer span of style-styled spaces so the row reaches
// width display columns. Input at or beyond width is returned unchanged.
func PadToWidth(spans []Span, width int, style Style) []Span {
	w := SpanWidth(spans)
	if w >= width {
		return spans
	}
	out := append([]Span(nil), spans...)
	return append(out, Span{Text: strings.Repeat(" ", width-w), Style: style})
}
