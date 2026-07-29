package theme

import "image/color"

// Flavor-generated theme families ported from tuicr's catppuccin_theme,
// gruvbox_theme, nord_theme, and everforest_theme generators. Each family
// defines a flavor struct holding the upstream palette and a generator that
// maps it onto the Theme slots, deriving the diff backgrounds with Blend.

// catppuccinFlavor is the Catppuccin palette subset the generator consumes
// (catppuccin.com flavor colors).
type catppuccinFlavor struct {
	dark     bool
	text     color.Color
	subtext1 color.Color
	overlay1 color.Color
	overlay0 color.Color
	surface2 color.Color
	surface1 color.Color
	base     color.Color
	mantle   color.Color
	crust    color.Color
	red      color.Color
	yellow   color.Color
	green    color.Color
	teal     color.Color
	blue     color.Color
	lavender color.Color
	peach    color.Color
	pink     color.Color
}

// catppuccinTheme maps a Catppuccin flavor onto the Theme slots, a port of
// tuicr's catppuccin_theme. The diff/syntax backgrounds are green/red
// accents blended into the base at 20%/16%.
func catppuccinTheme(name string, flavor catppuccinFlavor, syntaxStyle string) *Theme {
	accentFg := flavor.crust
	if flavor.dark {
		accentFg = flavor.base
	}

	return &Theme{
		Name: name,

		PanelBg:     flavor.base,
		BgHighlight: flavor.surface1,
		FgPrimary:   flavor.text,
		FgSecondary: flavor.subtext1,
		FgDim:       flavor.overlay0,

		DiffAdd:           flavor.green,
		DiffAddBg:         Blend(flavor.base, flavor.green, 20),
		DiffDel:           flavor.red,
		DiffDelBg:         Blend(flavor.base, flavor.red, 20),
		DiffContext:       flavor.text,
		DiffHunkHeader:    flavor.blue,
		ExpandedContextFg: flavor.overlay1,

		SyntaxAddBg: Blend(flavor.base, flavor.green, 16),
		SyntaxDelBg: Blend(flavor.base, flavor.red, 16),

		FileAdded:    flavor.green,
		FileModified: flavor.yellow,
		FileDeleted:  flavor.red,
		FileRenamed:  flavor.pink,

		Reviewed: flavor.green,
		Pending:  flavor.yellow,

		CommentNote:       flavor.blue,
		CommentSuggestion: flavor.teal,
		CommentIssue:      flavor.red,
		CommentPraise:     flavor.green,

		BorderFocused:   flavor.blue,
		BorderUnfocused: flavor.surface2,
		StatusBarBg:     flavor.mantle,
		CursorColor:     flavor.peach,
		CursorLineBg:    flavor.surface1,
		BranchName:      flavor.teal,
		HelpIndicator:   flavor.overlay0,

		MessageInfoFg:    accentFg,
		MessageInfoBg:    flavor.teal,
		MessageWarningFg: accentFg,
		MessageWarningBg: flavor.yellow,
		MessageErrorFg:   accentFg,
		MessageErrorBg:   flavor.red,
		UpdateBadgeFg:    accentFg,
		UpdateBadgeBg:    flavor.peach,

		ModeFg: accentFg,
		ModeBg: flavor.lavender,

		SyntaxStyle: syntaxStyle,
	}
}

// CatppuccinLatte returns the Catppuccin Latte (light) theme, a port of
// Theme::catppuccin_latte.
func CatppuccinLatte() *Theme {
	return catppuccinTheme("catppuccin-latte", catppuccinFlavor{
		dark:     false,
		text:     hexColor("#4c4f69"),
		subtext1: hexColor("#5c5f77"),
		overlay1: hexColor("#8c8fa1"),
		overlay0: hexColor("#9ca0b0"),
		surface2: hexColor("#acb0be"),
		surface1: hexColor("#bcc0cc"),
		base:     hexColor("#eff1f5"),
		mantle:   hexColor("#e6e9ef"),
		crust:    hexColor("#dce0e8"),
		red:      hexColor("#d20f39"),
		yellow:   hexColor("#df8e1d"),
		green:    hexColor("#40a02b"),
		teal:     hexColor("#179299"),
		blue:     hexColor("#1e66f5"),
		lavender: hexColor("#7287fd"),
		peach:    hexColor("#fe640b"),
		pink:     hexColor("#ea76cb"),
	}, "catppuccin-latte")
}

// CatppuccinFrappe returns the Catppuccin Frappé theme, a port of
// Theme::catppuccin_frappe.
func CatppuccinFrappe() *Theme {
	return catppuccinTheme("catppuccin-frappe", catppuccinFlavor{
		dark:     true,
		text:     hexColor("#c6d0f5"),
		subtext1: hexColor("#b5bfe2"),
		overlay1: hexColor("#838ba7"),
		overlay0: hexColor("#737994"),
		surface2: hexColor("#626880"),
		surface1: hexColor("#51576d"),
		base:     hexColor("#303446"),
		mantle:   hexColor("#292c3c"),
		crust:    hexColor("#232634"),
		red:      hexColor("#e78284"),
		yellow:   hexColor("#e5c890"),
		green:    hexColor("#a6d189"),
		teal:     hexColor("#81c8be"),
		blue:     hexColor("#8caaee"),
		lavender: hexColor("#babbf1"),
		peach:    hexColor("#ef9f76"),
		pink:     hexColor("#f4b8e4"),
	}, "catppuccin-frappe")
}

// CatppuccinMacchiato returns the Catppuccin Macchiato theme, a port of
// Theme::catppuccin_macchiato.
func CatppuccinMacchiato() *Theme {
	return catppuccinTheme("catppuccin-macchiato", catppuccinFlavor{
		dark:     true,
		text:     hexColor("#cad3f5"),
		subtext1: hexColor("#b8c0e0"),
		overlay1: hexColor("#8087a2"),
		overlay0: hexColor("#6e738d"),
		surface2: hexColor("#5b6078"),
		surface1: hexColor("#494d64"),
		base:     hexColor("#24273a"),
		mantle:   hexColor("#1e2030"),
		crust:    hexColor("#181926"),
		red:      hexColor("#ed8796"),
		yellow:   hexColor("#eed49f"),
		green:    hexColor("#a6da95"),
		teal:     hexColor("#8bd5ca"),
		blue:     hexColor("#8aadf4"),
		lavender: hexColor("#b7bdf8"),
		peach:    hexColor("#f5a97f"),
		pink:     hexColor("#f5bde6"),
	}, "catppuccin-macchiato")
}

// CatppuccinMocha returns the Catppuccin Mocha theme, a port of
// Theme::catppuccin_mocha.
func CatppuccinMocha() *Theme {
	return catppuccinTheme("catppuccin-mocha", catppuccinFlavor{
		dark:     true,
		text:     hexColor("#cdd6f4"),
		subtext1: hexColor("#bac2de"),
		overlay1: hexColor("#7f849c"),
		overlay0: hexColor("#6c7086"),
		surface2: hexColor("#585b70"),
		surface1: hexColor("#45475a"),
		base:     hexColor("#1e1e2e"),
		mantle:   hexColor("#181825"),
		crust:    hexColor("#11111b"),
		red:      hexColor("#f38ba8"),
		yellow:   hexColor("#f9e2af"),
		green:    hexColor("#a6e3a1"),
		teal:     hexColor("#94e2d5"),
		blue:     hexColor("#89b4fa"),
		lavender: hexColor("#b4befe"),
		peach:    hexColor("#fab387"),
		pink:     hexColor("#f5c2e7"),
	}, "catppuccin-mocha")
}

// gruvboxFlavor is the Gruvbox Material palette subset the generator
// consumes (sainnhe/gruvbox-material hard variants).
type gruvboxFlavor struct {
	dark       bool
	bg0        color.Color
	bg1        color.Color
	bg4        color.Color
	selectedBg color.Color
	fg0        color.Color
	fg1        color.Color
	grey0      color.Color
	grey1      color.Color
	red        color.Color
	orange     color.Color
	yellow     color.Color
	green      color.Color
	aqua       color.Color
	blue       color.Color
	purple     color.Color
	bgRed      color.Color
	bgGreen    color.Color
}

// gruvboxTheme maps a Gruvbox flavor onto the Theme slots, a port of
// tuicr's gruvbox_theme.
func gruvboxTheme(name string, flavor gruvboxFlavor) *Theme {
	syntaxStyle := "gruvbox-light"
	if flavor.dark {
		syntaxStyle = "gruvbox"
	}
	accentFg := flavor.fg1
	if flavor.dark {
		accentFg = flavor.bg0
	}

	return &Theme{
		Name: name,

		PanelBg:     flavor.bg0,
		BgHighlight: flavor.selectedBg,
		FgPrimary:   flavor.fg0,
		FgSecondary: flavor.fg1,
		FgDim:       flavor.grey0,

		DiffAdd:           flavor.green,
		DiffAddBg:         flavor.bgGreen,
		DiffDel:           flavor.red,
		DiffDelBg:         flavor.bgRed,
		DiffContext:       flavor.fg0,
		DiffHunkHeader:    flavor.blue,
		ExpandedContextFg: flavor.grey1,

		SyntaxAddBg: flavor.bgGreen,
		SyntaxDelBg: flavor.bgRed,

		FileAdded:    flavor.green,
		FileModified: flavor.yellow,
		FileDeleted:  flavor.red,
		FileRenamed:  flavor.purple,

		Reviewed: flavor.green,
		Pending:  flavor.yellow,

		CommentNote:       flavor.blue,
		CommentSuggestion: flavor.aqua,
		CommentIssue:      flavor.red,
		CommentPraise:     flavor.green,

		BorderFocused:   flavor.aqua,
		BorderUnfocused: flavor.bg4,
		StatusBarBg:     flavor.bg1,
		CursorColor:     flavor.orange,
		CursorLineBg:    flavor.selectedBg,
		BranchName:      flavor.aqua,
		HelpIndicator:   flavor.grey0,

		MessageInfoFg:    accentFg,
		MessageInfoBg:    flavor.aqua,
		MessageWarningFg: accentFg,
		MessageWarningBg: flavor.yellow,
		MessageErrorFg:   accentFg,
		MessageErrorBg:   flavor.red,
		UpdateBadgeFg:    accentFg,
		UpdateBadgeBg:    flavor.orange,

		ModeFg: accentFg,
		ModeBg: flavor.green,

		SyntaxStyle: syntaxStyle,
	}
}

// GruvboxDark returns the Gruvbox dark theme, a port of Theme::gruvbox_dark.
func GruvboxDark() *Theme {
	return gruvboxTheme("gruvbox-dark", gruvboxFlavor{
		dark:       true,
		bg0:        hexColor("#1d2021"),
		bg1:        hexColor("#282828"),
		bg4:        hexColor("#504945"),
		selectedBg: hexColor("#3c3836"),
		fg0:        hexColor("#d4be98"),
		fg1:        hexColor("#ddc7a1"),
		grey0:      hexColor("#7c6f64"),
		grey1:      hexColor("#928374"),
		red:        hexColor("#fb4934"),
		orange:     hexColor("#fe8019"),
		yellow:     hexColor("#fabd2f"),
		green:      hexColor("#b8bb26"),
		aqua:       hexColor("#8ec07c"),
		blue:       hexColor("#83a598"),
		purple:     hexColor("#d3869b"),
		bgRed:      hexColor("#402120"),
		bgGreen:    hexColor("#34381b"),
	})
}

// GruvboxLight returns the Gruvbox light theme, a port of
// Theme::gruvbox_light.
func GruvboxLight() *Theme {
	return gruvboxTheme("gruvbox-light", gruvboxFlavor{
		dark:       false,
		bg0:        hexColor("#f9f5d7"),
		bg1:        hexColor("#f5edca"),
		bg4:        hexColor("#ddc7a1"),
		selectedBg: hexColor("#ebdbb2"),
		fg0:        hexColor("#654735"),
		fg1:        hexColor("#4f3829"),
		grey0:      hexColor("#a89984"),
		grey1:      hexColor("#928374"),
		red:        hexColor("#9d0006"),
		orange:     hexColor("#af3a03"),
		yellow:     hexColor("#b57614"),
		green:      hexColor("#79740e"),
		aqua:       hexColor("#427b58"),
		blue:       hexColor("#076678"),
		purple:     hexColor("#8f3f71"),
		bgRed:      hexColor("#f0dede"),
		bgGreen:    hexColor("#e4ecd5"),
	})
}

// nordFlavor is the Nord palette subset the generator consumes
// (nordtheme.com nord0..nord14).
type nordFlavor struct {
	dark        bool
	bg0         color.Color
	bg1         color.Color
	bg2         color.Color
	bg3         color.Color
	fg0         color.Color
	fg1         color.Color
	frost0      color.Color
	frost1      color.Color
	frost2      color.Color
	red         color.Color
	orange      color.Color
	yellow      color.Color
	green       color.Color
	syntaxStyle string
}

// nordTheme maps a Nord flavor onto the Theme slots, a port of tuicr's
// nord_theme. The diff/syntax backgrounds are green/red accents blended
// into bg0 at 15%/10%.
func nordTheme(name string, flavor nordFlavor) *Theme {
	accentFg := flavor.fg1
	if flavor.dark {
		accentFg = flavor.bg0
	}

	return &Theme{
		Name: name,

		PanelBg:     flavor.bg0,
		BgHighlight: flavor.bg1,
		FgPrimary:   flavor.fg0,
		FgSecondary: flavor.fg1,
		FgDim:       flavor.bg3,

		DiffAdd:           flavor.green,
		DiffAddBg:         Blend(flavor.bg0, flavor.green, 15),
		DiffDel:           flavor.red,
		DiffDelBg:         Blend(flavor.bg0, flavor.red, 15),
		DiffContext:       flavor.fg0,
		DiffHunkHeader:    flavor.frost1,
		ExpandedContextFg: flavor.bg3,

		SyntaxAddBg: Blend(flavor.bg0, flavor.green, 10),
		SyntaxDelBg: Blend(flavor.bg0, flavor.red, 10),

		FileAdded:    flavor.green,
		FileModified: flavor.yellow,
		FileDeleted:  flavor.red,
		FileRenamed:  flavor.frost2,

		Reviewed: flavor.green,
		Pending:  flavor.yellow,

		CommentNote:       flavor.frost1,
		CommentSuggestion: flavor.frost0,
		CommentIssue:      flavor.red,
		CommentPraise:     flavor.green,

		BorderFocused:   flavor.frost1,
		BorderUnfocused: flavor.bg1,
		StatusBarBg:     flavor.bg2,
		CursorColor:     flavor.frost2,
		CursorLineBg:    flavor.bg2,
		BranchName:      flavor.frost0,
		HelpIndicator:   flavor.bg3,

		MessageInfoFg:    accentFg,
		MessageInfoBg:    flavor.frost1,
		MessageWarningFg: accentFg,
		MessageWarningBg: flavor.orange,
		MessageErrorFg:   accentFg,
		MessageErrorBg:   flavor.red,
		UpdateBadgeFg:    accentFg,
		UpdateBadgeBg:    flavor.orange,

		ModeFg: accentFg,
		ModeBg: flavor.frost1,

		SyntaxStyle: flavor.syntaxStyle,
	}
}

// nordAccents returns the accent colors shared by every Nord flavor
// (frost0..frost2 and the aurora red/orange/yellow/green).
func nordAccents() (frost0, frost1, frost2, red, orange, yellow, green color.Color) {
	return hexColor("#8fbcbb"), // nord7
		hexColor("#88c0d0"), // nord8
		hexColor("#81a1c1"), // nord9
		hexColor("#bf616a"), // nord11
		hexColor("#d08770"), // nord12
		hexColor("#ebcb8b"), // nord13
		hexColor("#a3be8c") // nord14
}

// NordDark returns the Nord dark theme, a port of Theme::nord_dark.
func NordDark() *Theme {
	frost0, frost1, frost2, red, orange, yellow, green := nordAccents()
	return nordTheme("nord-dark", nordFlavor{
		dark:   true,
		bg0:    hexColor("#2e3440"), // nord0
		bg1:    hexColor("#3b4252"), // nord1
		bg2:    hexColor("#434c5e"), // nord2
		bg3:    hexColor("#4c566a"), // nord3
		fg0:    hexColor("#d8dee9"), // nord4
		fg1:    hexColor("#e5e9f0"), // nord5
		frost0: frost0, frost1: frost1, frost2: frost2,
		red: red, orange: orange, yellow: yellow, green: green,
		// chroma's native nord style (tuicr uses syntect Nord).
		syntaxStyle: "nord",
	})
}

// NordLight returns the Nord light theme, a port of Theme::nord_light.
func NordLight() *Theme {
	frost0, frost1, frost2, red, orange, yellow, green := nordAccents()
	return nordTheme("nord-light", nordFlavor{
		dark:   false,
		bg0:    hexColor("#eceff4"), // nord6
		bg1:    hexColor("#e5e9f0"), // nord5
		bg2:    hexColor("#d8dee9"), // nord4
		bg3:    hexColor("#4c566a"), // nord3
		fg0:    hexColor("#2e3440"), // nord0
		fg1:    hexColor("#3b4252"), // nord1
		frost0: frost0, frost1: frost1, frost2: frost2,
		red: red, orange: orange, yellow: yellow, green: green,
		// tuicr uses syntect Base16OceanLight; chroma has no base16 light
		// port, so the light default is used (matching the landed light
		// theme's mapping).
		syntaxStyle: "github",
	})
}

// NordDarkHighContrast returns the Nord dark high-contrast theme (fg0
// boosted from nord4 to nord6), a port of Theme::nord_dark_high_contrast.
func NordDarkHighContrast() *Theme {
	frost0, frost1, frost2, red, orange, yellow, green := nordAccents()
	return nordTheme("nord-dark-high-contrast", nordFlavor{
		dark:   true,
		bg0:    hexColor("#2e3440"), // nord0
		bg1:    hexColor("#3b4252"), // nord1
		bg2:    hexColor("#434c5e"), // nord2
		bg3:    hexColor("#4c566a"), // nord3
		fg0:    hexColor("#eceff4"), // nord6 (boosted from nord4 for contrast)
		fg1:    hexColor("#e5e9f0"), // nord5
		frost0: frost0, frost1: frost1, frost2: frost2,
		red: red, orange: orange, yellow: yellow, green: green,
		syntaxStyle: "nord",
	})
}

// NordLightHighContrast returns the Nord light high-contrast theme (bg3
// deepened from nord3 to nord2), a port of Theme::nord_light_high_contrast.
func NordLightHighContrast() *Theme {
	frost0, frost1, frost2, red, orange, yellow, green := nordAccents()
	return nordTheme("nord-light-high-contrast", nordFlavor{
		dark:   false,
		bg0:    hexColor("#eceff4"), // nord6
		bg1:    hexColor("#e5e9f0"), // nord5
		bg2:    hexColor("#d8dee9"), // nord4
		bg3:    hexColor("#434c5e"), // nord2 (deeper than nord3 for contrast)
		fg0:    hexColor("#2e3440"), // nord0
		fg1:    hexColor("#3b4252"), // nord1
		frost0: frost0, frost1: frost1, frost2: frost2,
		red: red, orange: orange, yellow: yellow, green: green,
		syntaxStyle: "github",
	})
}

// everforestFlavor is the Everforest palette subset the generator consumes
// (sainnhe/everforest medium variants).
type everforestFlavor struct {
	dark        bool
	bg0         color.Color
	bg1         color.Color
	bg3         color.Color
	bg5         color.Color
	bgRed       color.Color
	bgGreen     color.Color
	fg          color.Color
	grey0       color.Color
	grey1       color.Color
	grey2       color.Color
	red         color.Color
	orange      color.Color
	yellow      color.Color
	green       color.Color
	aqua        color.Color
	blue        color.Color
	purple      color.Color
	syntaxStyle string
}

// everforestTheme maps an Everforest flavor onto the Theme slots, a port of
// tuicr's everforest_theme. The syntax diff backgrounds are green/red
// accents blended into bg0 at 12%.
func everforestTheme(name string, flavor everforestFlavor) *Theme {
	accentFg := flavor.fg
	if flavor.dark {
		accentFg = flavor.bg0
	}

	return &Theme{
		Name: name,

		PanelBg:     flavor.bg0,
		BgHighlight: flavor.bg3,
		FgPrimary:   flavor.fg,
		FgSecondary: flavor.grey2,
		FgDim:       flavor.grey0,

		DiffAdd:           flavor.green,
		DiffAddBg:         flavor.bgGreen,
		DiffDel:           flavor.red,
		DiffDelBg:         flavor.bgRed,
		DiffContext:       flavor.fg,
		DiffHunkHeader:    flavor.blue,
		ExpandedContextFg: flavor.grey1,

		SyntaxAddBg: Blend(flavor.bg0, flavor.green, 12),
		SyntaxDelBg: Blend(flavor.bg0, flavor.red, 12),

		FileAdded:    flavor.green,
		FileModified: flavor.yellow,
		FileDeleted:  flavor.red,
		FileRenamed:  flavor.purple,

		Reviewed: flavor.green,
		Pending:  flavor.yellow,

		CommentNote:       flavor.blue,
		CommentSuggestion: flavor.aqua,
		CommentIssue:      flavor.red,
		CommentPraise:     flavor.green,

		BorderFocused:   flavor.aqua,
		BorderUnfocused: flavor.bg5,
		StatusBarBg:     flavor.bg1,
		CursorColor:     flavor.orange,
		CursorLineBg:    flavor.bg1,
		BranchName:      flavor.aqua,
		HelpIndicator:   flavor.grey0,

		MessageInfoFg:    accentFg,
		MessageInfoBg:    flavor.blue,
		MessageWarningFg: accentFg,
		MessageWarningBg: flavor.yellow,
		MessageErrorFg:   accentFg,
		MessageErrorBg:   flavor.red,
		UpdateBadgeFg:    accentFg,
		UpdateBadgeBg:    flavor.orange,

		ModeFg: accentFg,
		ModeBg: flavor.green,

		SyntaxStyle: flavor.syntaxStyle,
	}
}

// EverforestDark returns the Everforest dark (medium) theme, a port of
// Theme::everforest_dark.
func EverforestDark() *Theme {
	return everforestTheme("everforest-dark", everforestFlavor{
		dark:    true,
		bg0:     hexColor("#2d353b"),
		bg1:     hexColor("#343f44"),
		bg3:     hexColor("#475258"),
		bg5:     hexColor("#56635f"),
		bgRed:   hexColor("#514045"),
		bgGreen: hexColor("#425047"),
		fg:      hexColor("#d3c6aa"),
		grey0:   hexColor("#7a8478"),
		grey1:   hexColor("#859289"),
		grey2:   hexColor("#9da9a0"),
		red:     hexColor("#e67e80"),
		orange:  hexColor("#e69875"),
		yellow:  hexColor("#dbbc7f"),
		green:   hexColor("#a7c080"),
		aqua:    hexColor("#83c092"),
		blue:    hexColor("#7fbbb3"),
		purple:  hexColor("#d699b6"),
		// chroma has no Everforest style; gruvbox is the closest warm,
		// low-saturation, earthy match (mirrors tuicr's GruvboxDark choice).
		syntaxStyle: "gruvbox",
	})
}

// EverforestLight returns the Everforest light (medium) theme, a port of
// Theme::everforest_light.
func EverforestLight() *Theme {
	return everforestTheme("everforest-light", everforestFlavor{
		dark:    false,
		bg0:     hexColor("#fdf6e3"),
		bg1:     hexColor("#f4f0d9"),
		bg3:     hexColor("#e6e2cc"),
		bg5:     hexColor("#bdc3af"),
		bgRed:   hexColor("#fde3da"),
		bgGreen: hexColor("#f0f1d2"),
		fg:      hexColor("#5c6a72"),
		grey0:   hexColor("#a6b0a0"),
		grey1:   hexColor("#939f91"),
		grey2:   hexColor("#829181"),
		red:     hexColor("#f85552"),
		orange:  hexColor("#f57d26"),
		yellow:  hexColor("#dfa000"),
		green:   hexColor("#8da101"),
		aqua:    hexColor("#35a77c"),
		blue:    hexColor("#3a94c5"),
		purple:  hexColor("#df69ba"),
		// chroma has no Everforest style; gruvbox-light is the closest match
		// (mirrors tuicr's GruvboxLight choice).
		syntaxStyle: "gruvbox-light",
	})
}
