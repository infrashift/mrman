// Package render holds the terminal-cell representation of the TUI's text
// content: styled spans, logical diff-stream rows tagged with semantic row
// kinds, width/wrap/truncate helpers ported from tuicr's text_utils.rs, and
// an ANSI emitter that serializes spans to escape sequences.
//
// The package deliberately does not port tuicr's zero-width marker trick;
// RowKind tags on LogicalLine carry row semantics instead.
package render

import (
	"image/color"
	"strconv"

	"github.com/mattn/go-runewidth"

	"github.com/infrashift/mrman/internal/syntax"
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

// FromSyntaxSpans converts highlighter spans (hex-string styles) into render
// spans (color.Color styles). Empty or malformed hex strings become nil
// (terminal default) colors.
func FromSyntaxSpans(spans []syntax.Span) []Span {
	out := make([]Span, len(spans))
	for i, sp := range spans {
		out[i] = Span{
			Text: sp.Text,
			Style: Style{
				Fg:        parseHexColor(sp.Style.FG),
				Bg:        parseHexColor(sp.Style.BG),
				Bold:      sp.Style.Bold,
				Italic:    sp.Style.Italic,
				Underline: sp.Style.Underline,
			},
		}
	}
	return out
}

// parseHexColor parses "#rrggbb" into an opaque color.RGBA. Anything else
// (including "") yields nil.
func parseHexColor(s string) color.Color {
	if len(s) != 7 || s[0] != '#' {
		return nil
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return nil
	}
	return color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xFF}
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
