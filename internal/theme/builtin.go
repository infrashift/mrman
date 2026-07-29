package theme

import (
	"strings"

	lipgloss "charm.land/lipgloss/v2"
)

// ANSI palette slots used by the ported dark/light themes. tuicr uses
// ratatui named colors in a few slots; ratatui maps Black->0, White->15
// (bright white) and Cyan->6, so those exact indices are kept here.
var (
	ansiBlack = lipgloss.Black       // ANSI 0
	ansiCyan  = lipgloss.Cyan        // ANSI 6
	ansiWhite = lipgloss.BrightWhite // ANSI 15 (ratatui Color::White)
)

// Dark returns the built-in dark theme (tuicr's default), a port of
// Theme::dark.
func Dark() *Theme {
	return &Theme{
		Name: "dark",

		PanelBg:     hexColor("#18181c"), // Rgb(24, 24, 28)
		BgHighlight: hexColor("#464646"), // Rgb(70, 70, 70)
		FgPrimary:   ansiWhite,
		FgSecondary: hexColor("#d2d2d2"), // Rgb(210, 210, 210)
		FgDim:       hexColor("#a0a0a0"), // Rgb(160, 160, 160)

		DiffAdd:           hexColor("#50dc78"), // Rgb(80, 220, 120)
		DiffAddBg:         hexColor("#003c14"), // Rgb(0, 60, 20)
		DiffDel:           hexColor("#f05a5a"), // Rgb(240, 90, 90)
		DiffDelBg:         hexColor("#460000"), // Rgb(70, 0, 0)
		DiffContext:       hexColor("#c8c8c8"), // Rgb(200, 200, 200)
		DiffHunkHeader:    hexColor("#5ac8ff"), // Rgb(90, 200, 255)
		ExpandedContextFg: hexColor("#8c8c8c"), // Rgb(140, 140, 140)

		SyntaxAddBg: hexColor("#00230c"), // Rgb(0, 35, 12)
		SyntaxDelBg: hexColor("#2d0000"), // Rgb(45, 0, 0)

		FileAdded:    hexColor("#50dc78"),
		FileModified: hexColor("#ffd25a"), // Rgb(255, 210, 90)
		FileDeleted:  hexColor("#f05a5a"),
		FileRenamed:  hexColor("#ff8cdc"), // Rgb(255, 140, 220)

		Reviewed: hexColor("#50dc78"),
		Pending:  hexColor("#ffd25a"),

		CommentNote:       hexColor("#5aaaff"), // Rgb(90, 170, 255)
		CommentSuggestion: hexColor("#5adcf0"), // Rgb(90, 220, 240)
		CommentIssue:      hexColor("#f05a5a"),
		CommentPraise:     hexColor("#50dc78"),

		BorderFocused:   hexColor("#5ac8ff"),
		BorderUnfocused: hexColor("#6e6e6e"), // Rgb(110, 110, 110)
		StatusBarBg:     hexColor("#1e1e1e"), // Rgb(30, 30, 30)
		CursorColor:     hexColor("#ffd25a"),
		CursorLineBg:    hexColor("#28282d"), // Rgb(40, 40, 45)
		BranchName:      hexColor("#5adcf0"),
		HelpIndicator:   hexColor("#6e6e6e"),

		MessageInfoFg:    ansiBlack,
		MessageInfoBg:    ansiCyan,
		MessageWarningFg: ansiBlack,
		MessageWarningBg: hexColor("#ffd25a"),
		MessageErrorFg:   ansiWhite,
		MessageErrorBg:   hexColor("#f05a5a"),
		UpdateBadgeFg:    ansiBlack,
		UpdateBadgeBg:    hexColor("#ffd25a"),

		ModeFg: ansiBlack,
		ModeBg: hexColor("#5ac8ff"),

		// tuicr uses syntect Base16EightiesDark; chroma has no base16
		// eighties port, so the closest bundled base16 dark is used.
		SyntaxStyle: "base16-snazzy",
	}
}

// Light returns the built-in light theme, a port of Theme::light.
func Light() *Theme {
	return &Theme{
		Name: "light",

		PanelBg:     hexColor("#f5f3e8"), // Rgb(245, 243, 232)
		BgHighlight: hexColor("#c8c8dc"), // Rgb(200, 200, 220)
		FgPrimary:   hexColor("#000000"),
		FgSecondary: hexColor("#1e1e1e"), // Rgb(30, 30, 30)
		FgDim:       hexColor("#505050"), // Rgb(80, 80, 80)

		DiffAdd:           hexColor("#005000"), // Rgb(0, 80, 0)
		DiffAddBg:         hexColor("#dcffdc"), // Rgb(220, 255, 220)
		DiffDel:           hexColor("#780000"), // Rgb(120, 0, 0)
		DiffDelBg:         hexColor("#fff0f0"), // Rgb(255, 240, 240)
		DiffContext:       hexColor("#000000"),
		DiffHunkHeader:    hexColor("#003c8c"), // Rgb(0, 60, 140)
		ExpandedContextFg: hexColor("#3c3c3c"), // Rgb(60, 60, 60)

		SyntaxAddBg: hexColor("#dcffdc"),
		SyntaxDelBg: hexColor("#ffe6e6"), // Rgb(255, 230, 230)

		FileAdded:    hexColor("#006400"), // Rgb(0, 100, 0)
		FileModified: hexColor("#8c5000"), // Rgb(140, 80, 0)
		FileDeleted:  hexColor("#a00000"), // Rgb(160, 0, 0)
		FileRenamed:  hexColor("#640064"), // Rgb(100, 0, 100)

		Reviewed: hexColor("#006400"),
		Pending:  hexColor("#8c5000"),

		CommentNote:       hexColor("#003c8c"),
		CommentSuggestion: hexColor("#006478"), // Rgb(0, 100, 120)
		CommentIssue:      hexColor("#a00000"),
		CommentPraise:     hexColor("#006400"),

		BorderFocused:   hexColor("#003c8c"),
		BorderUnfocused: hexColor("#646464"), // Rgb(100, 100, 100)
		StatusBarBg:     hexColor("#d2d2dc"), // Rgb(210, 210, 220)
		CursorColor:     hexColor("#8c5000"),
		CursorLineBg:    hexColor("#e1e1eb"), // Rgb(225, 225, 235)
		BranchName:      hexColor("#006478"),
		HelpIndicator:   hexColor("#5a5a5a"), // Rgb(90, 90, 90)

		MessageInfoFg:    ansiBlack,
		MessageInfoBg:    hexColor("#8cdcff"), // Rgb(140, 220, 255)
		MessageWarningFg: ansiBlack,
		MessageWarningBg: hexColor("#f0d296"), // Rgb(240, 210, 150)
		MessageErrorFg:   ansiWhite,
		MessageErrorBg:   hexColor("#b43c3c"), // Rgb(180, 60, 60)
		UpdateBadgeFg:    ansiBlack,
		UpdateBadgeBg:    hexColor("#f0d296"),

		ModeFg: ansiWhite,
		ModeBg: hexColor("#0050a0"), // Rgb(0, 80, 160)

		// tuicr uses syntect Base16OceanLight; chroma has no base16 light
		// port, so the closest legible bundled light style is used.
		SyntaxStyle: "github",
	}
}

// TokyoNightStorm returns the Tokyo Night Storm theme
// (folke/tokyonight.nvim "storm" variant), a port of
// Theme::tokyo_night_storm.
func TokyoNightStorm() *Theme {
	bg := hexColor("#24283b")
	bgDark := hexColor("#1f2335")
	bgHighlight := hexColor("#292e42")
	terminalBlack := hexColor("#414868")
	fg := hexColor("#c0caf5")
	fgDark := hexColor("#a9b1d6")
	dark3 := hexColor("#545c7e")
	comment := hexColor("#565f89")
	blue := hexColor("#7aa2f7")
	cyan := hexColor("#7dcfff")
	magenta := hexColor("#bb9af7")
	orange := hexColor("#ff9e64")
	yellow := hexColor("#e0af68")
	green := hexColor("#9ece6a")
	red := hexColor("#f7768e")

	return &Theme{
		Name: "tokyo-night-storm",

		PanelBg:     bg,
		BgHighlight: bgHighlight,
		FgPrimary:   fg,
		FgSecondary: fgDark,
		FgDim:       dark3,

		DiffAdd:           green,
		DiffAddBg:         hexColor("#20303b"),
		DiffDel:           red,
		DiffDelBg:         hexColor("#37222c"),
		DiffContext:       fgDark,
		DiffHunkHeader:    blue,
		ExpandedContextFg: dark3,

		SyntaxAddBg: hexColor("#1c2a34"), // Rgb(28, 42, 52)
		SyntaxDelBg: hexColor("#2f1e26"), // Rgb(47, 30, 38)

		FileAdded:    green,
		FileModified: yellow,
		FileDeleted:  red,
		FileRenamed:  magenta,

		Reviewed: green,
		Pending:  yellow,

		CommentNote:       blue,
		CommentSuggestion: cyan,
		CommentIssue:      red,
		CommentPraise:     green,

		BorderFocused:   blue,
		BorderUnfocused: terminalBlack,
		StatusBarBg:     bgDark,
		CursorColor:     orange,
		CursorLineBg:    bgHighlight,
		BranchName:      cyan,
		HelpIndicator:   comment,

		MessageInfoFg:    bgDark,
		MessageInfoBg:    blue,
		MessageWarningFg: bgDark,
		MessageWarningBg: yellow,
		MessageErrorFg:   fg,
		MessageErrorBg:   red,
		UpdateBadgeFg:    bgDark,
		UpdateBadgeBg:    yellow,

		ModeFg: bgDark,
		ModeBg: blue,

		// chroma's native tokyonight style — better fidelity than tuicr's
		// base16 approximation (deliberate improvement).
		SyntaxStyle: "tokyonight-storm",
	}
}

// TokyoNightDay returns the Tokyo Night Day theme (folke/tokyonight.nvim
// "day" variant), a port of Theme::tokyo_night_day.
func TokyoNightDay() *Theme {
	bg := hexColor("#e1e2e7")
	bgDark := hexColor("#d0d5e3")
	bgHighlight := hexColor("#c4c8da")
	terminalBlack := hexColor("#6c6e75")
	fg := hexColor("#3760bf")
	fgDark := hexColor("#6172b0")
	dark3 := hexColor("#848cb5")
	comment := hexColor("#848cb5")
	blue := hexColor("#2e7de9")
	cyan := hexColor("#007197")
	magenta := hexColor("#7847bd")
	orange := hexColor("#b15c00")
	yellow := hexColor("#8c6c3e")
	green := hexColor("#587539")
	red := hexColor("#f52a65")

	return &Theme{
		Name: "tokyo-night-day",

		PanelBg:     bg,
		BgHighlight: bgHighlight,
		FgPrimary:   fg,
		FgSecondary: fgDark,
		FgDim:       dark3,

		DiffAdd:           green,
		DiffAddBg:         hexColor("#c5dde6"), // light blue-tinted
		DiffDel:           red,
		DiffDelBg:         hexColor("#f3c5cb"), // light rose
		DiffContext:       fg,
		DiffHunkHeader:    blue,
		ExpandedContextFg: dark3,

		SyntaxAddBg: hexColor("#d8e6ec"),
		SyntaxDelBg: hexColor("#f5d5d9"),

		FileAdded:    green,
		FileModified: yellow,
		FileDeleted:  red,
		FileRenamed:  magenta,

		Reviewed: green,
		Pending:  yellow,

		CommentNote:       blue,
		CommentSuggestion: cyan,
		CommentIssue:      red,
		CommentPraise:     green,

		BorderFocused:   blue,
		BorderUnfocused: terminalBlack,
		StatusBarBg:     bgDark,
		CursorColor:     orange,
		CursorLineBg:    bgHighlight,
		BranchName:      cyan,
		HelpIndicator:   comment,

		MessageInfoFg:    bg,
		MessageInfoBg:    blue,
		MessageWarningFg: bg,
		MessageWarningBg: yellow,
		MessageErrorFg:   bg,
		MessageErrorBg:   red,
		UpdateBadgeFg:    bg,
		UpdateBadgeBg:    yellow,

		ModeFg: bg,
		ModeBg: blue,

		// chroma's native tokyonight style — better fidelity than tuicr's
		// bundled .tmTheme (deliberate improvement).
		SyntaxStyle: "tokyonight-day",
	}
}

// builtinConstructors maps canonical names to theme constructors, in
// display order.
var builtinConstructors = []struct {
	name string
	ctor func() *Theme
}{
	{"dark", Dark},
	{"light", Light},
	{"tokyo-night-storm", TokyoNightStorm},
	{"tokyo-night-day", TokyoNightDay},
}

// BuiltinNames returns the canonical names of the bundled themes, in
// display order.
func BuiltinNames() []string {
	names := make([]string, len(builtinConstructors))
	for i, entry := range builtinConstructors {
		names[i] = entry.name
	}
	return names
}

// builtinNamesDisplay renders the bundled theme names for warnings and
// errors ("dark, light, ...").
func builtinNamesDisplay() string {
	return strings.Join(BuiltinNames(), ", ")
}

// Lookup resolves a theme name (case-insensitive, surrounding whitespace
// ignored) to a fresh built-in theme instance. ok is false for unknown
// names.
func Lookup(name string) (*Theme, bool) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	for _, entry := range builtinConstructors {
		if entry.name == normalized {
			return entry.ctor(), true
		}
	}
	return nil, false
}
