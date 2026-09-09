package theme

import (
	"hash/fnv"
	"image/color"
	"reflect"
	"testing"

	lipgloss "charm.land/lipgloss/v2"
)

func rgba(r, g, b uint8) color.RGBA {
	return color.RGBA{R: r, G: g, B: b, A: 0xff}
}

func TestShiftLightness(t *testing.T) {
	tests := []struct {
		name   string
		in     color.Color
		amount int
		want   color.Color
	}{
		{"dark color lightens", rgba(24, 24, 28), 18, rgba(42, 42, 46)},
		{"light color darkens", rgba(225, 226, 231), 18, rgba(207, 208, 213)},
		{"boundary avg 127 lightens", rgba(127, 127, 127), 10, rgba(137, 137, 137)},
		{"boundary avg 128 darkens", rgba(128, 128, 128), 10, rgba(118, 118, 118)},
		{"clamps at 255", rgba(10, 20, 250), 18, rgba(28, 38, 255)},
		{"clamps at 0", rgba(200, 200, 5), 18, rgba(182, 182, 0)},
		{"nil passes through", nil, 18, nil},
		{"ansi passes through", lipgloss.Cyan, 18, lipgloss.Cyan},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ShiftLightness(tc.in, tc.amount)
			if got != tc.want {
				t.Errorf("ShiftLightness(%v, %d) = %v, want %v", tc.in, tc.amount, got, tc.want)
			}
		})
	}
}

func TestBlend(t *testing.T) {
	tests := []struct {
		name    string
		base    color.Color
		accent  color.Color
		percent int
		want    color.Color
	}{
		{"even mix", rgba(0, 0, 0), rgba(100, 100, 100), 50, rgba(50, 50, 50)},
		{"zero percent is base", rgba(10, 20, 30), rgba(200, 200, 200), 0, rgba(10, 20, 30)},
		{"full percent is accent", rgba(10, 20, 30), rgba(200, 200, 200), 100, rgba(200, 200, 200)},
		{"integer truncation", rgba(0, 0, 0), rgba(0, 0, 255), 20, rgba(0, 0, 51)},
		{"catppuccin-style 16%", rgba(239, 241, 245), rgba(64, 160, 43), 16, rgba(211, 228, 212)},
		{"nil base returns accent", nil, rgba(1, 2, 3), 50, rgba(1, 2, 3)},
		{"ansi base returns accent", lipgloss.Cyan, rgba(1, 2, 3), 50, rgba(1, 2, 3)},
		{"nil accent returns accent", rgba(1, 2, 3), nil, 50, nil},
		{"percent clamped high", rgba(0, 0, 0), rgba(100, 100, 100), 150, rgba(100, 100, 100)},
		{"percent clamped low", rgba(9, 9, 9), rgba(100, 100, 100), -5, rgba(9, 9, 9)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Blend(tc.base, tc.accent, tc.percent)
			if got != tc.want {
				t.Errorf("Blend(%v, %v, %d) = %v, want %v", tc.base, tc.accent, tc.percent, got, tc.want)
			}
		})
	}
}

func TestSectionHighlightBg(t *testing.T) {
	storm := TokyoNightStorm()
	// #24283b avg 45 < 128 -> +18 -> #363a4d
	if got := colorToHex(storm.SectionHighlightBg()); got != "#363a4d" {
		t.Errorf("storm SectionHighlightBg = %s, want #363a4d", got)
	}
	day := TokyoNightDay()
	// #e1e2e7 avg 227 >= 128 -> -18 -> #cfd0d5
	if got := colorToHex(day.SectionHighlightBg()); got != "#cfd0d5" {
		t.Errorf("day SectionHighlightBg = %s, want #cfd0d5", got)
	}
	transparent := Dark()
	transparent.ApplyTransparentBackground()
	if got := transparent.SectionHighlightBg(); got != nil {
		t.Errorf("transparent SectionHighlightBg = %v, want nil", got)
	}
}

func TestHexColor(t *testing.T) {
	tests := []struct {
		in   string
		want color.Color
	}{
		{"#24283b", rgba(0x24, 0x28, 0x3b)},
		{"#FFD25A", rgba(0xff, 0xd2, 0x5a)},
		{"#abc", rgba(0xaa, 0xbb, 0xcc)},
		{"", nil},
		{"24283b", nil},
		{"#24283", nil},
		{"#zzzzzz", nil},
	}
	for _, tc := range tests {
		if got := hexColor(tc.in); got != tc.want {
			t.Errorf("hexColor(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestColorToHex(t *testing.T) {
	if got := colorToHex(nil); got != "" {
		t.Errorf("colorToHex(nil) = %q, want empty", got)
	}
	if got := colorToHex(rgba(0x1c, 0x2a, 0x34)); got != "#1c2a34" {
		t.Errorf("colorToHex = %q, want #1c2a34", got)
	}
	// Round trip.
	if got := colorToHex(hexColor("#f5d5d9")); got != "#f5d5d9" {
		t.Errorf("round trip = %q, want #f5d5d9", got)
	}
}

// colorSlots collects every exported color.Color field of a Theme by name.
func colorSlots(t *testing.T, th *Theme) map[string]color.Color {
	t.Helper()
	slots := map[string]color.Color{}
	v := reflect.ValueOf(th).Elem()
	colorType := reflect.TypeFor[color.Color]()
	for i := 0; i < v.NumField(); i++ {
		field := v.Type().Field(i)
		if !field.IsExported() || field.Type != colorType {
			continue
		}
		c, _ := reflect.TypeAssert[color.Color](v.Field(i))
		slots[field.Name] = c
	}
	return slots
}

func checkSlots(t *testing.T, th *Theme, want map[string]string) {
	t.Helper()
	slots := colorSlots(t, th)
	const slotCount = 41
	if len(slots) != slotCount {
		t.Fatalf("theme has %d color slots, want %d", len(slots), slotCount)
	}
	if len(want) != slotCount {
		t.Fatalf("expectation table has %d entries, want %d", len(want), slotCount)
	}
	for name, wantHex := range want {
		c, ok := slots[name]
		if !ok {
			t.Errorf("missing slot %s", name)
			continue
		}
		if got := colorToHex(c); got != wantHex {
			t.Errorf("%s.%s = %s, want %s", th.Name, name, got, wantHex)
		}
	}
}

func TestTokyoNightStormPalette(t *testing.T) {
	th := TokyoNightStorm()
	if th.Name != "tokyo-night-storm" {
		t.Errorf("Name = %q", th.Name)
	}
	if th.SyntaxStyle != "tokyonight-storm" {
		t.Errorf("SyntaxStyle = %q, want tokyonight-storm", th.SyntaxStyle)
	}
	checkSlots(t, th, map[string]string{
		"PanelBg": "#24283b", "BgHighlight": "#292e42", "FgPrimary": "#c0caf5",
		"FgSecondary": "#a9b1d6", "FgDim": "#545c7e",
		"DiffAdd": "#9ece6a", "DiffAddBg": "#20303b", "DiffDel": "#f7768e",
		"DiffDelBg": "#37222c", "DiffContext": "#a9b1d6", "DiffHunkHeader": "#7aa2f7",
		"ExpandedContextFg": "#545c7e",
		"SyntaxAddBg":       "#1c2a34", "SyntaxDelBg": "#2f1e26",
		"FileAdded": "#9ece6a", "FileModified": "#e0af68", "FileDeleted": "#f7768e",
		"FileRenamed": "#bb9af7",
		"Reviewed":    "#9ece6a", "Pending": "#e0af68",
		"CommentNote": "#7aa2f7", "CommentSuggestion": "#7dcfff",
		"CommentIssue": "#f7768e", "CommentPraise": "#9ece6a",
		"BorderFocused": "#7aa2f7", "BorderUnfocused": "#414868",
		"StatusBarBg": "#1f2335", "CursorColor": "#ff9e64", "CursorLineBg": "#292e42",
		"BranchName": "#7dcfff", "HelpIndicator": "#565f89",
		"MessageInfoFg": "#1f2335", "MessageInfoBg": "#7aa2f7",
		"MessageWarningFg": "#1f2335", "MessageWarningBg": "#e0af68",
		"MessageErrorFg": "#c0caf5", "MessageErrorBg": "#f7768e",
		"UpdateBadgeFg": "#1f2335", "UpdateBadgeBg": "#e0af68",
		"ModeFg": "#1f2335", "ModeBg": "#7aa2f7",
	})
}

func TestTokyoNightDayPalette(t *testing.T) {
	th := TokyoNightDay()
	if th.Name != "tokyo-night-day" {
		t.Errorf("Name = %q", th.Name)
	}
	if th.SyntaxStyle != "tokyonight-day" {
		t.Errorf("SyntaxStyle = %q, want tokyonight-day", th.SyntaxStyle)
	}
	checkSlots(t, th, map[string]string{
		"PanelBg": "#e1e2e7", "BgHighlight": "#c4c8da", "FgPrimary": "#3760bf",
		"FgSecondary": "#6172b0", "FgDim": "#848cb5",
		"DiffAdd": "#587539", "DiffAddBg": "#c5dde6", "DiffDel": "#f52a65",
		"DiffDelBg": "#f3c5cb", "DiffContext": "#3760bf", "DiffHunkHeader": "#2e7de9",
		"ExpandedContextFg": "#848cb5",
		"SyntaxAddBg":       "#d8e6ec", "SyntaxDelBg": "#f5d5d9",
		"FileAdded": "#587539", "FileModified": "#8c6c3e", "FileDeleted": "#f52a65",
		"FileRenamed": "#7847bd",
		"Reviewed":    "#587539", "Pending": "#8c6c3e",
		"CommentNote": "#2e7de9", "CommentSuggestion": "#007197",
		"CommentIssue": "#f52a65", "CommentPraise": "#587539",
		"BorderFocused": "#2e7de9", "BorderUnfocused": "#6c6e75",
		"StatusBarBg": "#d0d5e3", "CursorColor": "#b15c00", "CursorLineBg": "#c4c8da",
		"BranchName": "#007197", "HelpIndicator": "#848cb5",
		"MessageInfoFg": "#e1e2e7", "MessageInfoBg": "#2e7de9",
		"MessageWarningFg": "#e1e2e7", "MessageWarningBg": "#8c6c3e",
		"MessageErrorFg": "#e1e2e7", "MessageErrorBg": "#f52a65",
		"UpdateBadgeFg": "#e1e2e7", "UpdateBadgeBg": "#8c6c3e",
		"ModeFg": "#e1e2e7", "ModeBg": "#2e7de9",
	})
}

func TestDarkTheme(t *testing.T) {
	th := Dark()
	if th.Name != "dark" {
		t.Errorf("Name = %q", th.Name)
	}
	spot := map[string]string{
		"PanelBg": "#18181c", "BgHighlight": "#464646", "FgSecondary": "#d2d2d2",
		"FgDim": "#a0a0a0", "DiffAdd": "#50dc78", "DiffAddBg": "#003c14",
		"DiffDel": "#f05a5a", "DiffDelBg": "#460000", "SyntaxAddBg": "#00230c",
		"SyntaxDelBg": "#2d0000", "BorderFocused": "#5ac8ff", "StatusBarBg": "#1e1e1e",
		"CursorColor": "#ffd25a", "CursorLineBg": "#28282d", "BranchName": "#5adcf0",
		"FileRenamed": "#ff8cdc", "CommentNote": "#5aaaff", "ModeBg": "#5ac8ff",
	}
	slots := colorSlots(t, th)
	for name, want := range spot {
		if got := colorToHex(slots[name]); got != want {
			t.Errorf("dark.%s = %s, want %s", name, got, want)
		}
	}
	// ANSI slots keep ratatui's named-color indices.
	if th.FgPrimary != lipgloss.BrightWhite {
		t.Errorf("dark FgPrimary = %v, want ANSI 15", th.FgPrimary)
	}
	if th.ModeFg != lipgloss.Black || th.MessageInfoFg != lipgloss.Black {
		t.Error("dark ModeFg/MessageInfoFg should be ANSI black")
	}
	if th.MessageInfoBg != lipgloss.Cyan {
		t.Errorf("dark MessageInfoBg = %v, want ANSI cyan", th.MessageInfoBg)
	}
}

func TestLightTheme(t *testing.T) {
	th := Light()
	if th.Name != "light" {
		t.Errorf("Name = %q", th.Name)
	}
	spot := map[string]string{
		"PanelBg": "#f5f3e8", "BgHighlight": "#c8c8dc", "FgPrimary": "#000000",
		"FgSecondary": "#1e1e1e", "DiffAdd": "#005000", "DiffAddBg": "#dcffdc",
		"DiffDel": "#780000", "DiffDelBg": "#fff0f0", "SyntaxAddBg": "#dcffdc",
		"SyntaxDelBg": "#ffe6e6", "BorderFocused": "#003c8c", "StatusBarBg": "#d2d2dc",
		"CursorColor": "#8c5000", "ModeBg": "#0050a0", "FileRenamed": "#640064",
	}
	slots := colorSlots(t, th)
	for name, want := range spot {
		if got := colorToHex(slots[name]); got != want {
			t.Errorf("light.%s = %s, want %s", name, got, want)
		}
	}
	if th.ModeFg != lipgloss.BrightWhite {
		t.Errorf("light ModeFg = %v, want ANSI 15", th.ModeFg)
	}
}

func TestBuiltinNames(t *testing.T) {
	want := []string{
		"dark", "light",
		"ayu-light", "ayu-mirage",
		"onedark",
		"github-light", "github-dark",
		"catppuccin-latte", "catppuccin-frappe", "catppuccin-macchiato", "catppuccin-mocha",
		"gruvbox-dark", "gruvbox-light",
		"nord-dark", "nord-light", "nord-dark-high-contrast", "nord-light-high-contrast",
		"solarized-light", "solarized-dark",
		"tokyo-night-storm", "tokyo-night-day",
		"everforest-dark", "everforest-light",
	}
	if got := BuiltinNames(); !reflect.DeepEqual(got, want) {
		t.Errorf("BuiltinNames() = %v, want %v", got, want)
	}
}

func TestLookup(t *testing.T) {
	for _, name := range BuiltinNames() {
		th, ok := Lookup(name)
		if !ok || th == nil {
			t.Errorf("Lookup(%q) failed", name)
			continue
		}
		if th.Name != name {
			t.Errorf("Lookup(%q).Name = %q", name, th.Name)
		}
	}
	if th, ok := Lookup("  Tokyo-Night-Storm "); !ok || th.Name != "tokyo-night-storm" {
		t.Error("Lookup should be case-insensitive and trim whitespace")
	}
	if _, ok := Lookup("no-such-theme"); ok {
		t.Error("Lookup of unknown theme should fail")
	}
	// Fresh instance every call.
	a, _ := Lookup("dark")
	b, _ := Lookup("dark")
	if a == b {
		t.Error("Lookup should return fresh instances")
	}
}

func TestHighlighterCaching(t *testing.T) {
	th := TokyoNightStorm()
	h1 := th.Highlighter()
	h2 := th.Highlighter()
	if h1 == nil {
		t.Fatal("Highlighter() returned nil")
	}
	if h1 != h2 {
		t.Error("Highlighter() should return the cached instance")
	}
	if got := h1.StyleName(); got != "tokyonight-storm" {
		t.Errorf("StyleName() = %q, want tokyonight-storm", got)
	}
	// A distinct theme instance gets its own highlighter.
	if other := TokyoNightStorm().Highlighter(); other == h1 {
		t.Error("distinct themes should not share highlighters")
	}
}

func TestApplyTransparentBackground(t *testing.T) {
	th := TokyoNightDay()
	th.ApplyTransparentBackground()
	if th.PanelBg != nil {
		t.Errorf("PanelBg = %v after ApplyTransparentBackground, want nil", th.PanelBg)
	}
	// Other slots untouched.
	if colorToHex(th.StatusBarBg) != "#d0d5e3" {
		t.Error("ApplyTransparentBackground must only clear PanelBg")
	}
}

// TestAuthorPaletteHashParity independently recomputes the FNV-1a mapping
// with hash/fnv to prove the hand-rolled loop matches the Rust port.
func TestAuthorPaletteHashParity(t *testing.T) {
	for _, author := range []string{"alice", "bob", "claude", "reviewer-9", ""} {
		h := fnv.New64a()
		if _, err := h.Write([]byte(author)); err != nil {
			t.Fatal(err)
		}
		want := authorPalette[h.Sum64()%uint64(len(authorPalette))]
		if got := AuthorColorFor(author); got != want {
			t.Errorf("AuthorColorFor(%q) = %v, want %v", author, got, want)
		}
	}
}
