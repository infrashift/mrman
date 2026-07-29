package render

import "unicode/utf8"

// ApplyHorizontalScroll skips scrollX display columns from a row's content
// while preserving spans[0] (the cursor-indicator column) untouched, the Go
// port of tuicr's apply_horizontal_scroll. Unlike the Rust original, which
// skipped character counts, this skips display columns; a wide rune is never
// split — when the scroll boundary falls inside one, the whole rune is
// skipped.
func ApplyHorizontalScroll(spans []Span, scrollX int) []Span {
	if scrollX <= 0 || len(spans) == 0 {
		return spans
	}
	out := make([]Span, 0, len(spans))
	out = append(out, spans[0])
	skip := scrollX
	for _, sp := range spans[1:] {
		if skip <= 0 {
			out = append(out, sp)
			continue
		}
		w := widthCond.StringWidth(sp.Text)
		if w <= skip {
			// Skip this span entirely.
			skip -= w
			continue
		}
		// Partially skip: consume whole runes until the columns are used up.
		i := 0
		for i < len(sp.Text) && skip > 0 {
			r, sz := utf8.DecodeRuneInString(sp.Text[i:])
			skip -= widthCond.RuneWidth(r)
			i += sz
		}
		skip = 0
		if i < len(sp.Text) {
			out = append(out, Span{Text: sp.Text[i:], Style: sp.Style})
		}
	}
	return out
}
