package theme

import (
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2/styles"
)

// TestExtraBuiltinPalettes spot-checks every theme added beyond the initial
// four against hex values hand-derived from tuicr's src/theme/mod.rs, and
// verifies each SyntaxStyle resolves in chroma's registry.
func TestExtraBuiltinPalettes(t *testing.T) {
	tests := []struct {
		name        string
		ctor        func() *Theme
		syntaxStyle string
		spots       map[string]string
	}{
		{
			name: "solarized-light", ctor: SolarizedLight, syntaxStyle: "solarized-light",
			spots: map[string]string{
				"PanelBg": "#fdf6e3", "FgPrimary": "#657b83", "DiffAddBg": "#def0cd",
				"BorderFocused": "#268bd2", "CursorColor": "#cb4b16",
				"CursorLineBg": "#e1dec8", "StatusBarBg": "#eee8d5",
			},
		},
		{
			name: "solarized-dark", ctor: SolarizedDark, syntaxStyle: "solarized-dark",
			spots: map[string]string{
				"PanelBg": "#002b36", "FgPrimary": "#839496", "DiffAddBg": "#003c14",
				"BorderFocused": "#268bd2", "CursorLineBg": "#0f3c4b",
				"FileRenamed": "#6c71c4", "DiffDel": "#f05a5a",
			},
		},
		{
			name: "ayu-light", ctor: AyuLight, syntaxStyle: "github",
			spots: map[string]string{
				"PanelBg": "#fafafa", "FgPrimary": "#5c6773", "DiffAddBg": "#eef7d0",
				"BorderFocused": "#36a3d9", "CursorColor": "#ff6a00",
				"FileModified": "#e7c547", "SyntaxAddBg": "#f4fbe4",
			},
		},
		{
			name: "ayu-mirage", ctor: AyuMirage, syntaxStyle: "base16-snazzy",
			spots: map[string]string{
				"PanelBg": "#1f2430", "FgPrimary": "#cccac2", "DiffAddBg": "#233529",
				"BorderFocused": "#ffa659", "BranchName": "#5ccfe6",
				"ModeBg": "#ffa659", "BorderUnfocused": "#3f4654",
			},
		},
		{
			name: "onedark", ctor: OneDark, syntaxStyle: "onedark",
			spots: map[string]string{
				"PanelBg": "#282c34", "FgPrimary": "#abb2bf", "DiffAddBg": "#2c382b",
				"BorderFocused": "#61afef", "StatusBarBg": "#21252b",
				"ModeFg": "#282c34", "FileRenamed": "#c678dd",
			},
		},
		{
			name: "github-light", ctor: GitHubLight, syntaxStyle: "github",
			spots: map[string]string{
				"PanelBg": "#ffffff", "FgPrimary": "#1f2328", "DiffAddBg": "#e6ffec",
				"BorderFocused": "#0969da", "FileRenamed": "#8250df",
				"StatusBarBg": "#f6f8fa", "CommentSuggestion": "#148282",
			},
		},
		{
			name: "github-dark", ctor: GitHubDark, syntaxStyle: "github-dark",
			spots: map[string]string{
				"PanelBg": "#0d1117", "FgPrimary": "#e6edf3", "DiffAddBg": "#10231c",
				"BorderFocused": "#58a6ff", "FileModified": "#d29922",
				"CursorLineBg": "#161b22", "MessageInfoFg": "#0d1117",
			},
		},
		{
			name: "catppuccin-latte", ctor: CatppuccinLatte, syntaxStyle: "catppuccin-latte",
			spots: map[string]string{
				"PanelBg": "#eff1f5", "FgPrimary": "#4c4f69", "DiffAddBg": "#cce0cc",
				"BorderFocused": "#1e66f5", "ModeBg": "#7287fd",
				"CursorColor": "#fe640b", "ModeFg": "#dce0e8",
			},
		},
		{
			name: "catppuccin-frappe", ctor: CatppuccinFrappe, syntaxStyle: "catppuccin-frappe",
			spots: map[string]string{
				"PanelBg": "#303446", "FgPrimary": "#c6d0f5", "DiffAddBg": "#475353",
				"BorderFocused": "#8caaee", "BranchName": "#81c8be",
				"StatusBarBg": "#292c3c", "ModeFg": "#303446",
			},
		},
		{
			name: "catppuccin-macchiato", ctor: CatppuccinMacchiato, syntaxStyle: "catppuccin-macchiato",
			spots: map[string]string{
				"PanelBg": "#24273a", "FgPrimary": "#cad3f5", "DiffAddBg": "#3e4a4c",
				"BorderFocused": "#8aadf4", "FileRenamed": "#f5bde6",
				"CursorColor": "#f5a97f", "BgHighlight": "#494d64",
			},
		},
		{
			name: "catppuccin-mocha", ctor: CatppuccinMocha, syntaxStyle: "catppuccin-mocha",
			spots: map[string]string{
				"PanelBg": "#1e1e2e", "FgPrimary": "#cdd6f4", "DiffAddBg": "#394545",
				"BorderFocused": "#89b4fa", "ModeBg": "#b4befe",
				"StatusBarBg": "#181825", "FgDim": "#6c7086",
			},
		},
		{
			name: "gruvbox-dark", ctor: GruvboxDark, syntaxStyle: "gruvbox",
			spots: map[string]string{
				"PanelBg": "#1d2021", "FgPrimary": "#d4be98", "DiffAddBg": "#34381b",
				"BorderFocused": "#8ec07c", "CursorColor": "#fe8019",
				"ModeBg": "#b8bb26", "DiffDelBg": "#402120",
			},
		},
		{
			name: "gruvbox-light", ctor: GruvboxLight, syntaxStyle: "gruvbox-light",
			spots: map[string]string{
				"PanelBg": "#f9f5d7", "FgPrimary": "#654735", "DiffAddBg": "#e4ecd5",
				"BorderFocused": "#427b58", "BgHighlight": "#ebdbb2",
				"ModeFg": "#4f3829", "DiffHunkHeader": "#076678",
			},
		},
		{
			name: "nord-dark", ctor: NordDark, syntaxStyle: "nord",
			spots: map[string]string{
				"PanelBg": "#2e3440", "FgPrimary": "#d8dee9", "DiffAddBg": "#3f484b",
				"BorderFocused": "#88c0d0", "CursorColor": "#81a1c1",
				"StatusBarBg": "#434c5e", "ModeFg": "#2e3440",
			},
		},
		{
			name: "nord-light", ctor: NordLight, syntaxStyle: "github",
			spots: map[string]string{
				"PanelBg": "#eceff4", "FgPrimary": "#2e3440", "DiffAddBg": "#e1e7e4",
				"BorderFocused": "#88c0d0", "ModeFg": "#3b4252",
				"FgDim": "#4c566a", "StatusBarBg": "#d8dee9",
			},
		},
		{
			name: "nord-dark-high-contrast", ctor: NordDarkHighContrast, syntaxStyle: "nord",
			spots: map[string]string{
				"PanelBg": "#2e3440", "FgPrimary": "#eceff4", "DiffAddBg": "#3f484b",
				"BorderFocused": "#88c0d0", "ModeFg": "#2e3440",
				"HelpIndicator": "#4c566a", "FgSecondary": "#e5e9f0",
			},
		},
		{
			name: "nord-light-high-contrast", ctor: NordLightHighContrast, syntaxStyle: "github",
			spots: map[string]string{
				"PanelBg": "#eceff4", "FgPrimary": "#2e3440", "DiffAddBg": "#e1e7e4",
				"BorderFocused": "#88c0d0", "FgDim": "#434c5e",
				"HelpIndicator": "#434c5e", "FileRenamed": "#81a1c1",
			},
		},
		{
			name: "everforest-dark", ctor: EverforestDark, syntaxStyle: "gruvbox",
			spots: map[string]string{
				"PanelBg": "#2d353b", "FgPrimary": "#d3c6aa", "DiffAddBg": "#425047",
				"BorderFocused": "#83c092", "SyntaxAddBg": "#3b4543",
				"CursorColor": "#e69875", "DiffDelBg": "#514045",
			},
		},
		{
			name: "everforest-light", ctor: EverforestLight, syntaxStyle: "gruvbox-light",
			spots: map[string]string{
				"PanelBg": "#fdf6e3", "FgPrimary": "#5c6a72", "DiffAddBg": "#f0f1d2",
				"BorderFocused": "#35a77c", "BgHighlight": "#e6e2cc",
				"ModeBg": "#8da101", "FileRenamed": "#df69ba",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			th := tc.ctor()
			if th.Name != tc.name {
				t.Errorf("Name = %q, want %q", th.Name, tc.name)
			}
			if th.SyntaxStyle != tc.syntaxStyle {
				t.Errorf("SyntaxStyle = %q, want %q", th.SyntaxStyle, tc.syntaxStyle)
			}
			slots := colorSlots(t, th)
			for slot, want := range tc.spots {
				if got := colorToHex(slots[slot]); got != want {
					t.Errorf("%s.%s = %s, want %s", tc.name, slot, got, want)
				}
			}
		})
	}
}

// TestBuiltinSyntaxStylesResolveInChroma verifies every bundled theme names
// a chroma style that actually exists in the registry (no silent fallback).
func TestBuiltinSyntaxStylesResolveInChroma(t *testing.T) {
	for _, name := range BuiltinNames() {
		th, ok := Lookup(name)
		if !ok {
			t.Fatalf("Lookup(%q) failed", name)
		}
		if th.SyntaxStyle == "" {
			t.Errorf("%s has empty SyntaxStyle", name)
			continue
		}
		if _, ok := styles.Registry[strings.ToLower(th.SyntaxStyle)]; !ok {
			t.Errorf("%s SyntaxStyle %q not in chroma registry", name, th.SyntaxStyle)
		}
	}
}

// TestCatppuccinGeneratorBlends checks the latte generator's blended diff
// backgrounds against hand-computed Blend values:
//
//	base #eff1f5, green #40a02b, red #d20f39
//	add 20%: (239*80+64*20)/100=204, (241*80+160*20)/100=224, (245*80+43*20)/100=204
//	del 20%: 233, 195, 207 — add 16%: 211, 228, 212 — del 16%: 234, 204, 214
func TestCatppuccinGeneratorBlends(t *testing.T) {
	th := CatppuccinLatte()
	want := map[string]string{
		"DiffAddBg":   "#cce0cc",
		"DiffDelBg":   "#e9c3cf",
		"SyntaxAddBg": "#d3e4d4",
		"SyntaxDelBg": "#eaccd6",
	}
	slots := colorSlots(t, th)
	for slot, hex := range want {
		if got := colorToHex(slots[slot]); got != hex {
			t.Errorf("catppuccin-latte.%s = %s, want %s", slot, got, hex)
		}
	}
}

// TestNordGeneratorBlends checks the nord-dark generator's blended diff
// backgrounds against hand-computed Blend values:
//
//	bg0 #2e3440, green #a3be8c, red #bf616a
//	add 15%: (46*85+163*15)/100=63, (52*85+190*15)/100=72, (64*85+140*15)/100=75
//	del 15%: 67, 58, 70 — add 10%: 57, 65, 71 — del 10%: 60, 56, 68
func TestNordGeneratorBlends(t *testing.T) {
	th := NordDark()
	want := map[string]string{
		"DiffAddBg":   "#3f484b",
		"DiffDelBg":   "#433a46",
		"SyntaxAddBg": "#394147",
		"SyntaxDelBg": "#3c3844",
	}
	slots := colorSlots(t, th)
	for slot, hex := range want {
		if got := colorToHex(slots[slot]); got != hex {
			t.Errorf("nord-dark.%s = %s, want %s", slot, got, hex)
		}
	}
}

// TestEverforestGeneratorBlends checks the everforest-dark generator's
// blended syntax backgrounds against hand-computed Blend values (the diff
// backgrounds are palette colors, not blends):
//
//	bg0 #2d353b, green #a7c080, red #e67e80
//	add 12%: (45*88+167*12)/100=59, (53*88+192*12)/100=69, (59*88+128*12)/100=67
//	del 12%: 67, 61, 67
func TestEverforestGeneratorBlends(t *testing.T) {
	th := EverforestDark()
	want := map[string]string{
		"SyntaxAddBg": "#3b4543",
		"SyntaxDelBg": "#433d43",
		"DiffAddBg":   "#425047", // palette bg_green, unblended
		"DiffDelBg":   "#514045", // palette bg_red, unblended
	}
	slots := colorSlots(t, th)
	for slot, hex := range want {
		if got := colorToHex(slots[slot]); got != hex {
			t.Errorf("everforest-dark.%s = %s, want %s", slot, got, hex)
		}
	}
}
