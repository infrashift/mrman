package render

import (
	"strconv"
	"strings"
)

// Emitter serializes span rows to ANSI escape sequences. It owns a reusable
// buffer, so one Emitter per render loop avoids per-row allocations. The zero
// value is ready to use. Not safe for concurrent use.
type Emitter struct {
	buf strings.Builder
}

// Line serializes one row of spans: an SGR sequence per style change
// (adjacent equal styles are coalesced into one run), truecolor 38;2/48;2
// codes for non-nil colors (nil emits no fg/bg code), and a trailing reset
// whenever the row ends with attributes active. Empty-text spans are skipped.
func (e *Emitter) Line(spans []Span) string {
	e.buf.Reset()
	var cur Style
	started := false
	for _, sp := range spans {
		if sp.Text == "" {
			continue
		}
		if !started || sp.Style != cur {
			e.writeSGR(sp.Style, started && cur != Style{})
			cur = sp.Style
			started = true
		}
		e.buf.WriteString(sp.Text)
	}
	if started && cur != (Style{}) {
		e.buf.WriteString("\x1b[0m")
	}
	return e.buf.String()
}

// writeSGR emits the SGR sequence that switches to st. needReset says
// attributes are currently active and must be cleared first; a default style
// with nothing to reset emits no sequence at all.
func (e *Emitter) writeSGR(st Style, needReset bool) {
	if st == (Style{}) {
		if needReset {
			e.buf.WriteString("\x1b[0m")
		}
		return
	}
	e.buf.WriteString("\x1b[")
	first := true
	sep := func() {
		if !first {
			e.buf.WriteByte(';')
		}
		first = false
	}
	if needReset {
		sep()
		e.buf.WriteByte('0')
	}
	if st.Bold {
		sep()
		e.buf.WriteByte('1')
	}
	if st.Italic {
		sep()
		e.buf.WriteByte('3')
	}
	if st.Underline {
		sep()
		e.buf.WriteByte('4')
	}
	if st.Fg != nil {
		sep()
		e.writeColor(38, st.Fg)
	}
	if st.Bg != nil {
		sep()
		e.writeColor(48, st.Bg)
	}
	e.buf.WriteByte('m')
}

// writeColor emits "38;2;r;g;b" / "48;2;r;g;b" for a truecolor color.
func (e *Emitter) writeColor(base int, c interface{ RGBA() (r, g, b, a uint32) }) {
	r, g, b, _ := c.RGBA()
	e.buf.WriteString(strconv.Itoa(base))
	e.buf.WriteString(";2;")
	e.buf.WriteString(strconv.Itoa(int(r >> 8)))
	e.buf.WriteByte(';')
	e.buf.WriteString(strconv.Itoa(int(g >> 8)))
	e.buf.WriteByte(';')
	e.buf.WriteString(strconv.Itoa(int(b >> 8)))
}
