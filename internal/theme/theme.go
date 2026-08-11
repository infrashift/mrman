// Package theme provides the application color themes ported from tuicr's
// src/theme/mod.rs: the Theme color vocabulary, the built-in palettes, the
// appearance-resolution precedence, and the lipgloss style materialization
// used by the TUI chrome.
package theme

import (
	"fmt"
	"image/color"
	"sync"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"

	"github.com/infrashift/mrman/internal/syntax"
)

// Theme is the complete color theme for the application. Every slot is an
// image/color.Color; nil means "terminal default" (no color emitted), which
// is how transparent backgrounds are represented.
//
// Themes carry lazily-built state (the syntax highlighter), so they must be
// handled by pointer; the built-in constructors all return *Theme.
type Theme struct {
	// Name is the canonical theme name (e.g. "tokyo-night-storm").
	Name string

	// Base colors.
	PanelBg     color.Color
	BgHighlight color.Color
	FgPrimary   color.Color
	FgSecondary color.Color
	FgDim       color.Color

	// Diff colors.
	DiffAdd           color.Color
	DiffAddBg         color.Color
	DiffDel           color.Color
	DiffDelBg         color.Color
	DiffContext       color.Color
	DiffHunkHeader    color.Color
	ExpandedContextFg color.Color

	// Syntax-highlighting diff row backgrounds (for highlighted code).
	SyntaxAddBg color.Color
	SyntaxDelBg color.Color

	// File status colors.
	FileAdded    color.Color
	FileModified color.Color
	FileDeleted  color.Color
	FileRenamed  color.Color

	// Review status colors.
	Reviewed color.Color
	Pending  color.Color

	// Comment type colors.
	CommentNote       color.Color
	CommentSuggestion color.Color
	CommentIssue      color.Color
	CommentPraise     color.Color

	// UI element colors.
	BorderFocused   color.Color
	BorderUnfocused color.Color
	StatusBarBg     color.Color
	CursorColor     color.Color
	CursorLineBg    color.Color
	BranchName      color.Color
	HelpIndicator   color.Color

	// Message/update badge colors.
	MessageInfoFg    color.Color
	MessageInfoBg    color.Color
	MessageWarningFg color.Color
	MessageWarningBg color.Color
	MessageErrorFg   color.Color
	MessageErrorBg   color.Color
	UpdateBadgeFg    color.Color
	UpdateBadgeBg    color.Color

	// Mode indicator colors.
	ModeFg color.Color
	ModeBg color.Color

	// SyntaxStyle is the chroma style name used for code highlighting.
	SyntaxStyle string

	// customSyntax, when non-nil, overrides SyntaxStyle with a style loaded
	// from a chroma XML file (a local theme's syntax_style_file). Set by
	// LoadLocal; SyntaxStyle then carries the loaded style's display name.
	customSyntax *chroma.Style

	hlOnce sync.Once
	hl     *syntax.Highlighter
}

// rgbComponents extracts 8-bit RGB channels from a concrete RGB color.
// ok is false for nil and for non-RGB colors (ANSI palette colors,
// lipgloss.NoColor), mirroring tuicr's match on Color::Rgb.
func rgbComponents(c color.Color) (r, g, b uint8, ok bool) {
	switch v := c.(type) {
	case color.RGBA:
		return v.R, v.G, v.B, true
	case color.NRGBA:
		return v.R, v.G, v.B, true
	case lipgloss.RGBColor:
		return v.R, v.G, v.B, true
	default:
		return 0, 0, 0, false
	}
}

func clamp8(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// ShiftLightness shifts every channel of c by amount, additively: channels
// move up by amount when the channel average is below 128 (dark colors get
// lighter) and down by amount otherwise, clamped to 0..255. nil and non-RGB
// colors pass through unchanged. Exact port of tuicr's shift_lightness — do
// not substitute HSL-based lighten/darken.
func ShiftLightness(c color.Color, amount int) color.Color {
	r, g, b, ok := rgbComponents(c)
	if !ok {
		return c
	}
	avg := (int(r) + int(g) + int(b)) / 3
	amt := amount
	if avg >= 128 {
		amt = -amount
	}
	return color.RGBA{
		R: clamp8(int(r) + amt),
		G: clamp8(int(g) + amt),
		B: clamp8(int(b) + amt),
		A: 0xff,
	}
}

// Blend mixes accent into base with integer per-channel arithmetic:
// channel = (base*(100-p) + accent*p) / 100. accentPercent is clamped to
// 0..100. When either input is nil or non-RGB the accent is returned
// unchanged, matching tuicr's blend.
func Blend(base, accent color.Color, accentPercent int) color.Color {
	br, bg, bb, baseOK := rgbComponents(base)
	ar, ag, ab, accentOK := rgbComponents(accent)
	if !baseOK || !accentOK {
		return accent
	}
	p := accentPercent
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	inv := 100 - p
	mix := func(b, a uint8) uint8 {
		return uint8((int(b)*inv + int(a)*p) / 100)
	}
	return color.RGBA{R: mix(br, ar), G: mix(bg, ag), B: mix(bb, ab), A: 0xff}
}

// SectionHighlightBg is the subtle row tint for diff section markers (hunk
// headers, gap expanders) — a brightness shift from PanelBg so it adapts to
// any palette without per-theme tuning. Returns nil when PanelBg is nil.
func (t *Theme) SectionHighlightBg() color.Color {
	return ShiftLightness(t.PanelBg, 18)
}

// VisualSelectionBg is the background painted behind selected text in the
// diff, whether the selection came from visual mode or a mouse drag.
//
// It shifts away from BgHighlight rather than using it directly: several
// themes give BgHighlight and CursorLineBg the same value, and the cursor sits
// on the selection by construction — so painting the selection in BgHighlight
// would make it invisible on exactly the row it starts from. Returns nil when
// BgHighlight is nil.
func (t *Theme) VisualSelectionBg() color.Color {
	return ShiftLightness(t.BgHighlight, 18)
}

// Highlighter returns the theme's syntax highlighter, lazily built on first
// use and cached for the lifetime of the theme.
func (t *Theme) Highlighter() *syntax.Highlighter {
	t.hlOnce.Do(func() {
		if t.customSyntax != nil {
			t.hl = syntax.NewHighlighterWithStyle(t.customSyntax, colorToHex(t.SyntaxAddBg), colorToHex(t.SyntaxDelBg))
			return
		}
		t.hl = syntax.NewHighlighter(t.SyntaxStyle, colorToHex(t.SyntaxAddBg), colorToHex(t.SyntaxDelBg))
	})
	return t.hl
}

// ApplyTransparentBackground clears the panel background so the terminal's
// own background shows through (transparent_background=true).
func (t *Theme) ApplyTransparentBackground() {
	t.PanelBg = nil
}

// hexColor parses a "#rrggbb" (or "#rgb") hex string into an RGB color.
// Invalid input yields nil (terminal default).
func hexColor(s string) color.Color {
	if len(s) == 0 || s[0] != '#' {
		return nil
	}
	valid := true
	hexNibble := func(b byte) uint8 {
		switch {
		case b >= '0' && b <= '9':
			return b - '0'
		case b >= 'a' && b <= 'f':
			return b - 'a' + 10
		case b >= 'A' && b <= 'F':
			return b - 'A' + 10
		}
		valid = false
		return 0
	}
	c := color.RGBA{A: 0xff}
	switch len(s) {
	case 7:
		c.R = hexNibble(s[1])<<4 | hexNibble(s[2])
		c.G = hexNibble(s[3])<<4 | hexNibble(s[4])
		c.B = hexNibble(s[5])<<4 | hexNibble(s[6])
	case 4:
		c.R = hexNibble(s[1]) * 17
		c.G = hexNibble(s[2]) * 17
		c.B = hexNibble(s[3]) * 17
	default:
		return nil
	}
	if !valid {
		return nil
	}
	return c
}

// colorToHex renders a color as "#rrggbb" for APIs that speak hex strings
// (the syntax highlighter bridge). nil yields "".
func colorToHex(c color.Color) string {
	if c == nil {
		return ""
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", uint8(r>>8), uint8(g>>8), uint8(b>>8))
}
