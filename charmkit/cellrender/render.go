// Package cellrender is a terminal-cell text layer for full-screen TUIs:
// styled spans, logical rows tagged with a caller-defined kind,
// display-width-accurate wrapping and truncation, horizontal scrolling that
// never splits a wide rune, background overlays that survive wrapping, and
// an ANSI emitter.
//
// It exists because lipgloss styles blocks, not cell streams: it offers no
// horizontal scroll, and no way to repaint a row's background so the paint
// stays aligned under word wrap. Anything that scrolls a wide document
// sideways — a diff, a log, a table — needs this layer underneath.
//
// Row semantics travel in RowKind on LogicalLine rather than in zero-width
// marker runes embedded in the text, so a row's meaning survives wrapping,
// truncation and re-styling.
package cellrender

import (
	"image/color"
	"strconv"

	"github.com/mattn/go-runewidth"
)

// widthCond is the pinned display-width oracle. EastAsianWidth is forced off
// (and StrictEmojiNeutral kept at the library default) so layout math is
// deterministic regardless of the user's locale, matching tuicr's use of the
// unicode-width crate.
var widthCond = &runewidth.Condition{EastAsianWidth: false, StrictEmojiNeutral: true}

// Style is a cell-level text style: a cheap comparable value type. Nil colors
// mean "terminal default". Colors stored here must have comparable dynamic
// types (color.RGBA and friends) so Style values can be compared with ==.
type Style struct {
	Fg        color.Color // foreground, nil = terminal default
	Bg        color.Color // background, nil = terminal default
	Bold      bool
	Italic    bool
	Underline bool
}

// Span is a run of text rendered with one Style.
type Span struct {
	Text  string
	Style Style
}

// ParseHexColor parses "#rrggbb" into an opaque color.RGBA. Anything else
// (including "") yields nil, meaning "inherit the terminal default".
func ParseHexColor(s string) color.Color {
	if len(s) != 7 || s[0] != '#' {
		return nil
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return nil
	}
	return color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xFF} //nolint:gosec // G115: masked to one byte
}

// StringWidth returns the display-column width of s under the pinned width
// condition (EastAsianWidth=false). Control characters, including tab, count
// as zero columns, matching the unicode-width crate used by tuicr.
func StringWidth(s string) int {
	return widthCond.StringWidth(s)
}

// SpanWidth returns the total display-column width of all span texts.
func SpanWidth(spans []Span) int {
	w := 0
	for _, sp := range spans {
		w += widthCond.StringWidth(sp.Text)
	}
	return w
}

// wrapCharWidth is the per-character width used by WrapSpans: tab counts as
// one column (tuicr: width(ch).unwrap_or(1) for '\t'), everything else uses
// the pinned condition.
func wrapCharWidth(ch rune) int {
	if ch == '\t' {
		return 1
	}
	return widthCond.RuneWidth(ch)
}
