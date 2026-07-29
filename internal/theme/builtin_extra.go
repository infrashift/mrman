package theme

// Direct hex-for-hex ports of tuicr's individually-defined built-in themes:
// Solarized, Ayu, One Dark, and GitHub. The flavor-generated families
// (Catppuccin, Gruvbox, Nord, Everforest) live in builtin_generated.go.

// SolarizedLight returns the Solarized Light theme (Ethan Schoonover's
// palette), a port of Theme::solarized_light.
func SolarizedLight() *Theme {
	base03 := hexColor("#002b36")
	base01 := hexColor("#586e75")
	base00 := hexColor("#657b83")
	base1 := hexColor("#93a1a1")
	base2 := hexColor("#eee8d5")
	base3 := hexColor("#fdf6e3")
	yellow := hexColor("#b58900")
	orange := hexColor("#cb4b16")
	red := hexColor("#dc322f")
	violet := hexColor("#6c71c4")
	blue := hexColor("#268bd2")
	cyan := hexColor("#2aa198")
	green := hexColor("#859900")

	return &Theme{
		Name: "solarized-light",

		PanelBg:     base3,
		BgHighlight: base2,
		FgPrimary:   base00,
		FgSecondary: base01,
		FgDim:       base1,

		DiffAdd:           hexColor("#005000"), // Rgb(0, 80, 0)
		DiffAddBg:         hexColor("#def0cd"), // Rgb(222, 240, 205)
		DiffDel:           hexColor("#8c0000"), // Rgb(140, 0, 0)
		DiffDelBg:         hexColor("#fce1e0"), // Rgb(252, 225, 224)
		DiffContext:       base00,
		DiffHunkHeader:    blue,
		ExpandedContextFg: base1,

		SyntaxAddBg: hexColor("#def0cd"),
		SyntaxDelBg: hexColor("#fce1e0"),

		FileAdded:    green,
		FileModified: yellow,
		FileDeleted:  red,
		FileRenamed:  violet,

		Reviewed: green,
		Pending:  yellow,

		CommentNote:       blue,
		CommentSuggestion: cyan,
		CommentIssue:      red,
		CommentPraise:     green,

		BorderFocused:   blue,
		BorderUnfocused: base1,
		StatusBarBg:     base2,
		CursorColor:     orange,
		CursorLineBg:    hexColor("#e1dec8"), // Rgb(225, 222, 200)
		BranchName:      cyan,
		HelpIndicator:   base01,

		MessageInfoFg:    base3,
		MessageInfoBg:    blue,
		MessageWarningFg: base03,
		MessageWarningBg: yellow,
		MessageErrorFg:   base3,
		MessageErrorBg:   red,
		UpdateBadgeFg:    base03,
		UpdateBadgeBg:    yellow,

		ModeFg: base3,
		ModeBg: blue,

		SyntaxStyle: "solarized-light",
	}
}

// SolarizedDark returns the Solarized Dark theme, a port of
// Theme::solarized_dark.
func SolarizedDark() *Theme {
	base03 := hexColor("#002b36")
	base02 := hexColor("#073642")
	base01 := hexColor("#586e75")
	base00 := hexColor("#657b83")
	base0 := hexColor("#839496")
	base3 := hexColor("#fdf6e3")
	yellow := hexColor("#b58900")
	orange := hexColor("#cb4b16")
	red := hexColor("#dc322f")
	violet := hexColor("#6c71c4")
	blue := hexColor("#268bd2")
	cyan := hexColor("#2aa198")
	green := hexColor("#859900")

	return &Theme{
		Name: "solarized-dark",

		PanelBg:     base03,
		BgHighlight: base02,
		FgPrimary:   base0,
		FgSecondary: base00,
		FgDim:       base01,

		DiffAdd:           hexColor("#50dc78"), // Rgb(80, 220, 120)
		DiffAddBg:         hexColor("#003c14"), // Rgb(0, 60, 20)
		DiffDel:           hexColor("#f05a5a"), // Rgb(240, 90, 90)
		DiffDelBg:         hexColor("#460000"), // Rgb(70, 0, 0)
		DiffContext:       base0,
		DiffHunkHeader:    blue,
		ExpandedContextFg: base01,

		SyntaxAddBg: hexColor("#003c14"),
		SyntaxDelBg: hexColor("#460000"),

		FileAdded:    green,
		FileModified: yellow,
		FileDeleted:  red,
		FileRenamed:  violet,

		Reviewed: green,
		Pending:  yellow,

		CommentNote:       blue,
		CommentSuggestion: cyan,
		CommentIssue:      red,
		CommentPraise:     green,

		BorderFocused:   blue,
		BorderUnfocused: base01,
		StatusBarBg:     base02,
		CursorColor:     orange,
		CursorLineBg:    hexColor("#0f3c4b"), // Rgb(15, 60, 75)
		BranchName:      cyan,
		HelpIndicator:   base00,

		MessageInfoFg:    base03,
		MessageInfoBg:    blue,
		MessageWarningFg: base03,
		MessageWarningBg: yellow,
		MessageErrorFg:   base3,
		MessageErrorBg:   red,
		UpdateBadgeFg:    base03,
		UpdateBadgeBg:    yellow,

		ModeFg: base3,
		ModeBg: blue,

		SyntaxStyle: "solarized-dark",
	}
}

// AyuLight returns the Ayu Light theme (ayutheme.com), a port of
// Theme::ayu_light.
func AyuLight() *Theme {
	return &Theme{
		Name: "ayu-light",

		PanelBg:     hexColor("#fafafa"), // Rgb(250, 250, 250)
		BgHighlight: hexColor("#f0eee4"), // Rgb(240, 238, 228)
		FgPrimary:   hexColor("#5c6773"), // Rgb(92, 103, 115)
		FgSecondary: hexColor("#6b7682"), // Rgb(107, 118, 130)
		FgDim:       hexColor("#abb0b6"), // Rgb(171, 176, 182)

		DiffAdd:           hexColor("#86b300"), // Rgb(134, 179, 0)
		DiffAddBg:         hexColor("#eef7d0"), // Rgb(238, 247, 208)
		DiffDel:           hexColor("#f07178"), // Rgb(240, 113, 120)
		DiffDelBg:         hexColor("#fdebec"), // Rgb(253, 235, 236)
		DiffContext:       hexColor("#5c6773"),
		DiffHunkHeader:    hexColor("#36a3d9"), // Rgb(54, 163, 217)
		ExpandedContextFg: hexColor("#828c99"), // Rgb(130, 140, 153)

		SyntaxAddBg: hexColor("#f4fbe4"), // Rgb(244, 251, 228)
		SyntaxDelBg: hexColor("#fff1f2"), // Rgb(255, 241, 242)

		FileAdded:    hexColor("#86b300"),
		FileModified: hexColor("#e7c547"), // Rgb(231, 197, 71)
		FileDeleted:  hexColor("#f07178"),
		FileRenamed:  hexColor("#a37acc"), // Rgb(163, 122, 204)

		Reviewed: hexColor("#86b300"),
		Pending:  hexColor("#e7c547"),

		CommentNote:       hexColor("#36a3d9"),
		CommentSuggestion: hexColor("#4cbf99"), // Rgb(76, 191, 153)
		CommentIssue:      hexColor("#f07178"),
		CommentPraise:     hexColor("#86b300"),

		BorderFocused:   hexColor("#36a3d9"),
		BorderUnfocused: hexColor("#d9d8d7"), // Rgb(217, 216, 215)
		StatusBarBg:     hexColor("#ffffff"),
		CursorColor:     hexColor("#ff6a00"), // Rgb(255, 106, 0)
		CursorLineBg:    hexColor("#ebedf0"), // Rgb(235, 237, 240)
		BranchName:      hexColor("#36a3d9"),
		HelpIndicator:   hexColor("#abb0b6"),

		MessageInfoFg:    ansiBlack,
		MessageInfoBg:    hexColor("#8cdcff"), // Rgb(140, 220, 255)
		MessageWarningFg: ansiBlack,
		MessageWarningBg: hexColor("#f6d98c"), // Rgb(246, 217, 140)
		MessageErrorFg:   ansiWhite,
		MessageErrorBg:   hexColor("#d95757"), // Rgb(217, 87, 87)
		UpdateBadgeFg:    ansiBlack,
		UpdateBadgeBg:    hexColor("#f6d98c"),

		ModeFg: ansiWhite,
		ModeBg: hexColor("#ff6a00"),

		// tuicr uses syntect OneHalfLight; chroma has no onehalf port, so the
		// light default is used (panel-bg lightness fallback).
		SyntaxStyle: "github",
	}
}

// AyuMirage returns the Ayu Mirage theme, the dark variant of the Ayu
// palette; a port of Theme::ayu_mirage (resolved outputs of the official
// ayu-colors generator, matching upstream vscode-ayu).
func AyuMirage() *Theme {
	bg := hexColor("#1f2430")        // surface.base / ui.bg
	bgDark := hexColor("#1a1f29")    // editor.line
	bgPanel := hexColor("#282e3b")   // ui.panel.bg
	selection := hexColor("#293040") // ui.selection.active on bg
	fg := hexColor("#cccac2")        // editor.fg
	fgSecondary := hexColor("#9aa2af")
	comment := hexColor("#6e7c8f") // syntax.comment
	dim := hexColor("#707a8c")     // ui.fg

	yellow := hexColor("#ffcd66") // syntax.func
	orange := hexColor("#ffa659") // syntax.keyword
	green := hexColor("#87d96c")  // vcs.added
	red := hexColor("#f27983")    // vcs.removed
	cyan := hexColor("#5ccfe6")   // syntax.tag
	blue := hexColor("#73d0ff")   // syntax.entity
	purple := hexColor("#dfbfff") // syntax.constant
	mint := hexColor("#95e6cb")   // syntax.regexp

	return &Theme{
		Name: "ayu-mirage",

		PanelBg:     bg,
		BgHighlight: selection,
		FgPrimary:   fg,
		FgSecondary: fgSecondary,
		FgDim:       comment,

		DiffAdd:           green,
		DiffAddBg:         hexColor("#233529"), // Rgb(35, 53, 41)
		DiffDel:           red,
		DiffDelBg:         hexColor("#3a2429"), // Rgb(58, 36, 41)
		DiffContext:       fg,
		DiffHunkHeader:    blue,
		ExpandedContextFg: dim,

		SyntaxAddBg: hexColor("#1e2f24"), // Rgb(30, 47, 36)
		SyntaxDelBg: hexColor("#321e23"), // Rgb(50, 30, 35)

		FileAdded:    green,
		FileModified: yellow,
		FileDeleted:  red,
		FileRenamed:  purple,

		Reviewed: green,
		Pending:  yellow,

		CommentNote:       blue,
		CommentSuggestion: mint,
		CommentIssue:      red,
		CommentPraise:     green,

		BorderFocused:   orange,
		BorderUnfocused: hexColor("#3f4654"), // Rgb(63, 70, 84)
		StatusBarBg:     bgDark,
		CursorColor:     orange,
		CursorLineBg:    bgPanel,
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
		ModeBg: orange,

		// tuicr uses syntect Base16EightiesDark; chroma has no base16
		// eighties port, so the dark default is used (panel-bg lightness
		// fallback, matching the landed dark theme's mapping).
		SyntaxStyle: "base16-snazzy",
	}
}

// OneDark returns the One Dark theme (Atom's One Dark palette), a port of
// Theme::onedark.
func OneDark() *Theme {
	return &Theme{
		Name: "onedark",

		PanelBg:     hexColor("#282c34"), // Rgb(40, 44, 52)
		BgHighlight: hexColor("#3e4452"), // Rgb(62, 68, 82)
		FgPrimary:   hexColor("#abb2bf"), // Rgb(171, 178, 191)
		FgSecondary: hexColor("#c0c6d0"), // Rgb(192, 198, 208)
		FgDim:       hexColor("#5c6370"), // Rgb(92, 99, 112)

		DiffAdd:           hexColor("#98c379"), // Rgb(152, 195, 121)
		DiffAddBg:         hexColor("#2c382b"), // Rgb(44, 56, 43)
		DiffDel:           hexColor("#e06c75"), // Rgb(224, 108, 117)
		DiffDelBg:         hexColor("#3a2d2f"), // Rgb(58, 45, 47)
		DiffContext:       hexColor("#abb2bf"),
		DiffHunkHeader:    hexColor("#56b6c2"), // Rgb(86, 182, 194)
		ExpandedContextFg: hexColor("#5c6370"),

		SyntaxAddBg: hexColor("#253126"), // Rgb(37, 49, 38)
		SyntaxDelBg: hexColor("#3b2528"), // Rgb(59, 37, 40)

		FileAdded:    hexColor("#98c379"),
		FileModified: hexColor("#e5c07b"), // Rgb(229, 192, 123)
		FileDeleted:  hexColor("#e06c75"),
		FileRenamed:  hexColor("#c678dd"), // Rgb(198, 120, 221)

		Reviewed: hexColor("#98c379"),
		Pending:  hexColor("#e5c07b"),

		CommentNote:       hexColor("#61afef"), // Rgb(97, 175, 239)
		CommentSuggestion: hexColor("#56b6c2"),
		CommentIssue:      hexColor("#e06c75"),
		CommentPraise:     hexColor("#98c379"),

		BorderFocused:   hexColor("#61afef"),
		BorderUnfocused: hexColor("#3e4452"),
		StatusBarBg:     hexColor("#21252b"), // Rgb(33, 37, 43)
		CursorColor:     hexColor("#e5c07b"),
		CursorLineBg:    hexColor("#2c313a"), // Rgb(44, 49, 58)
		BranchName:      hexColor("#56b6c2"),
		HelpIndicator:   hexColor("#5c6370"),

		MessageInfoFg:    ansiBlack,
		MessageInfoBg:    hexColor("#56b6c2"),
		MessageWarningFg: ansiBlack,
		MessageWarningBg: hexColor("#e5c07b"),
		MessageErrorFg:   ansiWhite,
		MessageErrorBg:   hexColor("#e06c75"),
		UpdateBadgeFg:    ansiBlack,
		UpdateBadgeBg:    hexColor("#e5c07b"),

		ModeFg: hexColor("#282c34"),
		ModeBg: hexColor("#61afef"),

		// chroma's native onedark style — better fidelity than tuicr's
		// OneHalfDark syntect approximation.
		SyntaxStyle: "onedark",
	}
}

// GitHubLight returns the GitHub light theme (github.com Primer light
// tokens), a port of Theme::github_light.
func GitHubLight() *Theme {
	return &Theme{
		Name: "github-light",

		PanelBg:     hexColor("#ffffff"),
		BgHighlight: hexColor("#ddf4ff"), // Rgb(221, 244, 255)
		FgPrimary:   hexColor("#1f2328"), // Rgb(31, 35, 40)
		FgSecondary: hexColor("#59636e"), // Rgb(89, 99, 110)
		FgDim:       hexColor("#6e7781"), // Rgb(110, 119, 129)

		DiffAdd:           hexColor("#1a7f37"), // Rgb(26, 127, 55)
		DiffAddBg:         hexColor("#e6ffec"), // Rgb(230, 255, 236)
		DiffDel:           hexColor("#cf222e"), // Rgb(207, 34, 46)
		DiffDelBg:         hexColor("#ffebe9"), // Rgb(255, 235, 233)
		DiffContext:       hexColor("#1f2328"),
		DiffHunkHeader:    hexColor("#0969da"), // Rgb(9, 105, 218)
		ExpandedContextFg: hexColor("#6e7781"),

		SyntaxAddBg: hexColor("#e6ffec"),
		SyntaxDelBg: hexColor("#ffebe9"),

		FileAdded:    hexColor("#1a7f37"),
		FileModified: hexColor("#9a6700"), // Rgb(154, 103, 0)
		FileDeleted:  hexColor("#cf222e"),
		FileRenamed:  hexColor("#8250df"), // Rgb(130, 80, 223)

		Reviewed: hexColor("#1a7f37"),
		Pending:  hexColor("#9a6700"),

		CommentNote:       hexColor("#0969da"),
		CommentSuggestion: hexColor("#148282"), // Rgb(20, 130, 130)
		CommentIssue:      hexColor("#cf222e"),
		CommentPraise:     hexColor("#1a7f37"),

		BorderFocused:   hexColor("#0969da"),
		BorderUnfocused: hexColor("#d0d7de"), // Rgb(208, 215, 222)
		StatusBarBg:     hexColor("#f6f8fa"), // Rgb(246, 248, 250)
		CursorColor:     hexColor("#9a6700"),
		CursorLineBg:    hexColor("#ddf4ff"),
		BranchName:      hexColor("#0969da"),
		HelpIndicator:   hexColor("#6e7781"),

		MessageInfoFg:    ansiWhite,
		MessageInfoBg:    hexColor("#0969da"),
		MessageWarningFg: ansiWhite,
		MessageWarningBg: hexColor("#9a6700"),
		MessageErrorFg:   ansiWhite,
		MessageErrorBg:   hexColor("#cf222e"),
		UpdateBadgeFg:    ansiWhite,
		UpdateBadgeBg:    hexColor("#9a6700"),

		ModeFg: ansiWhite,
		ModeBg: hexColor("#0969da"),

		// chroma's native github style (tuicr uses syntect InspiredGithub).
		SyntaxStyle: "github",
	}
}

// GitHubDark returns the GitHub dark theme (github.com Primer dark tokens),
// a port of Theme::github_dark.
func GitHubDark() *Theme {
	return &Theme{
		Name: "github-dark",

		PanelBg:     hexColor("#0d1117"), // Rgb(13, 17, 23)
		BgHighlight: hexColor("#21262d"), // Rgb(33, 38, 45)
		FgPrimary:   hexColor("#e6edf3"), // Rgb(230, 237, 243)
		FgSecondary: hexColor("#c9d1d9"), // Rgb(201, 209, 217)
		FgDim:       hexColor("#8b949e"), // Rgb(139, 148, 158)

		DiffAdd:           hexColor("#3fb950"), // Rgb(63, 185, 80)
		DiffAddBg:         hexColor("#10231c"), // Rgb(16, 35, 28)
		DiffDel:           hexColor("#f85149"), // Rgb(248, 81, 73)
		DiffDelBg:         hexColor("#301b1f"), // Rgb(48, 27, 31)
		DiffContext:       hexColor("#e6edf3"),
		DiffHunkHeader:    hexColor("#58a6ff"), // Rgb(88, 166, 255)
		ExpandedContextFg: hexColor("#8b949e"),

		SyntaxAddBg: hexColor("#10231c"),
		SyntaxDelBg: hexColor("#301b1f"),

		FileAdded:    hexColor("#3fb950"),
		FileModified: hexColor("#d29922"), // Rgb(210, 153, 34)
		FileDeleted:  hexColor("#f85149"),
		FileRenamed:  hexColor("#a371f7"), // Rgb(163, 113, 247)

		Reviewed: hexColor("#3fb950"),
		Pending:  hexColor("#d29922"),

		CommentNote:       hexColor("#58a6ff"),
		CommentSuggestion: hexColor("#56d4dd"), // Rgb(86, 212, 221)
		CommentIssue:      hexColor("#f85149"),
		CommentPraise:     hexColor("#3fb950"),

		BorderFocused:   hexColor("#58a6ff"),
		BorderUnfocused: hexColor("#30363d"), // Rgb(48, 54, 61)
		StatusBarBg:     hexColor("#161b22"), // Rgb(22, 27, 34)
		CursorColor:     hexColor("#d29922"),
		CursorLineBg:    hexColor("#161b22"),
		BranchName:      hexColor("#58a6ff"),
		HelpIndicator:   hexColor("#8b949e"),

		MessageInfoFg:    hexColor("#0d1117"),
		MessageInfoBg:    hexColor("#58a6ff"),
		MessageWarningFg: hexColor("#0d1117"),
		MessageWarningBg: hexColor("#d29922"),
		MessageErrorFg:   ansiWhite,
		MessageErrorBg:   hexColor("#f85149"),
		UpdateBadgeFg:    hexColor("#0d1117"),
		UpdateBadgeBg:    hexColor("#d29922"),

		ModeFg: ansiWhite,
		ModeBg: hexColor("#58a6ff"),

		// chroma's native github-dark style — better fidelity than tuicr's
		// OneHalfDark syntect approximation.
		SyntaxStyle: "github-dark",
	}
}
