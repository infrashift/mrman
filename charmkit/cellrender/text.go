package cellrender

import "strings"

// TruncateStr shortens s to at most maxLen bytes, replacing the tail with
// "..." when it does not fit. Port of tuicr's truncate_str: the cut point is
// the last rune-start byte index <= maxLen-3, so the result is always valid
// UTF-8 and never exceeds maxLen bytes.
func TruncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	truncateAt := max(maxLen-3, 0)
	end := 0
	for i := range s { // i visits rune-start byte offsets
		if i > truncateAt {
			break
		}
		end = i
	}
	return s[:end] + "..."
}

// TruncateOrPad forces s to exactly width characters (rune count, not display
// columns — port of tuicr's truncate_or_pad): longer strings are cut to
// width-3 runes plus "...", shorter ones are right-padded with spaces.
func TruncateOrPad(s string, width int) string {
	runes := []rune(s)
	if len(runes) > width {
		keep := max(width-3, 0)
		return string(runes[:keep]) + "..."
	}
	return s + strings.Repeat(" ", width-len(runes))
}

// TruncateOrPadSpans forces spans to exactly width display columns (port of
// tuicr's truncate_or_pad_spans). Overflowing input keeps width-3 columns of
// content and appends a base-styled "..."; short input is padded with a
// base-styled space run; exact fits are returned as a copy. Wide characters
// are never split: a character that would cross the truncation boundary is
// dropped entirely.
func TruncateOrPadSpans(spans []Span, width int, base Style) []Span {
	total := SpanWidth(spans)
	switch {
	case total > width:
		var result []Span
		remaining := max(width-3, 0)
		for _, sp := range spans {
			if remaining == 0 {
				break
			}
			w := widthCond.StringWidth(sp.Text)
			if w <= remaining {
				result = append(result, sp)
				remaining -= w
				continue
			}
			// Truncate this span character by character to fit.
			var b strings.Builder
			current := 0
			for _, c := range sp.Text {
				cw := widthCond.RuneWidth(c)
				if current+cw > remaining {
					break
				}
				b.WriteRune(c)
				current += cw
			}
			if b.Len() > 0 {
				result = append(result, Span{Text: b.String(), Style: sp.Style})
			}
			remaining = 0
		}
		return append(result, Span{Text: "...", Style: base})
	case total < width:
		result := append([]Span(nil), spans...)
		return append(result, Span{Text: strings.Repeat(" ", width-total), Style: base})
	default:
		return append([]Span(nil), spans...)
	}
}

// wrapItem is one character of wrap input with its style and display width.
type wrapItem struct {
	ch    rune
	style Style
	w     int
}

// wrapState carries the accumulating rows for WrapSpans' flushUnit, a direct
// port of tuicr's flush_unit.
type wrapState struct {
	rows     [][]wrapItem
	current  []wrapItem
	currentW int
	width    int
}

// flushUnit places one indivisible unit (a word, or a single whitespace
// character) onto the current row, starting a new row when it does not fit
// and hard-splitting units wider than the row.
func (st *wrapState) flushUnit(unit []wrapItem, unitW int) {
	if len(unit) == 0 {
		return
	}
	if st.currentW+unitW <= st.width {
		st.current = append(st.current, unit...)
		st.currentW += unitW
		return
	}
	if unitW <= st.width {
		st.rows = append(st.rows, st.current)
		st.current = append([]wrapItem(nil), unit...)
		st.currentW = unitW
		return
	}
	// Unit is wider than a full row: hard-split it.
	pos := 0
	for pos < len(unit) {
		remaining := max(st.width-st.currentW, 0)
		consumed, consumedW := 0, 0
		for _, it := range unit[pos:] {
			if consumedW+it.w > remaining {
				break
			}
			consumedW += it.w
			consumed++
		}
		if consumed == 0 {
			if len(st.current) == 0 {
				// Oversized single character: emit it alone.
				st.current = append(st.current, unit[pos])
				pos++
			}
			st.rows = append(st.rows, st.current)
			st.current = nil
			st.currentW = 0
		} else {
			st.current = append(st.current, unit[pos:pos+consumed]...)
			pos += consumed
			st.currentW += consumedW
			if pos < len(unit) {
				st.rows = append(st.rows, st.current)
				st.current = nil
				st.currentW = 0
			}
		}
	}
}

// WrapSpans wraps styled spans to the given display width, word-aware (port
// of tuicr's wrap_spans). Words break at spaces/tabs, each whitespace
// character is flushed as its own unit (so trailing separators stay on the
// row they follow), words wider than a row are hard-split, wide characters
// are never split across rows, and per-character styles are preserved with
// adjacent same-style characters re-coalesced into single spans.
//
// Degenerate cases mirror the Rust: width 0 returns the input as one row,
// empty or all-empty input returns one empty row, and input that already fits
// returns a single row.
func WrapSpans(spans []Span, width int) [][]Span {
	if width == 0 {
		return [][]Span{append([]Span(nil), spans...)}
	}
	allEmpty := true
	for _, sp := range spans {
		if sp.Text != "" {
			allEmpty = false
			break
		}
	}
	if len(spans) == 0 || allEmpty {
		return [][]Span{{}}
	}
	if SpanWidth(spans) <= width {
		return [][]Span{append([]Span(nil), spans...)}
	}

	st := &wrapState{width: width}
	var word []wrapItem
	wordW := 0
	for _, sp := range spans {
		for _, ch := range sp.Text {
			w := wrapCharWidth(ch)
			it := wrapItem{ch: ch, style: sp.Style, w: w}
			if ch == ' ' || ch == '\t' {
				st.flushUnit(word, wordW)
				word, wordW = word[:0], 0
				st.flushUnit([]wrapItem{it}, w)
			} else {
				word = append(word, it)
				wordW += w
			}
		}
	}
	st.flushUnit(word, wordW)
	if len(st.current) > 0 || len(st.rows) == 0 {
		st.rows = append(st.rows, st.current)
	}

	out := make([][]Span, 0, len(st.rows))
	for _, row := range st.rows {
		out = append(out, coalesceItems(row))
	}
	return out
}

// coalesceItems merges runs of same-style characters back into spans.
func coalesceItems(row []wrapItem) []Span {
	var out []Span
	var buf []rune
	var cur Style
	started := false
	for _, it := range row {
		if started && it.style == cur {
			buf = append(buf, it.ch)
			continue
		}
		if started {
			out = append(out, Span{Text: string(buf), Style: cur})
		}
		cur = it.style
		started = true
		buf = buf[:0]
		buf = append(buf, it.ch)
	}
	if started {
		out = append(out, Span{Text: string(buf), Style: cur})
	}
	return out
}
