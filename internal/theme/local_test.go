package theme

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	lipgloss "charm.land/lipgloss/v2"

	"github.com/infrashift/mrman/internal/config"
)

// localThemeBody renders a complete local theme TOML: every palette key
// gets a distinct grey hex unless overridden, keys in omit are dropped, and
// extra is appended verbatim (for syntax_style lines).
func localThemeBody(overrides map[string]string, omit []string, extra string) string {
	omitted := make(map[string]bool, len(omit))
	for _, key := range omit {
		omitted[key] = true
	}
	var b strings.Builder
	for i, key := range config.ThemeColorKeys {
		if omitted[key] {
			continue
		}
		value, ok := overrides[key]
		if !ok {
			value = fmt.Sprintf("#%02x%02x%02x", i+10, i+10, i+10)
		}
		fmt.Fprintf(&b, "%s = %q\n", key, value)
	}
	b.WriteString(extra)
	return b.String()
}

func writeLocalTheme(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name+".toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadLocalHappyPath(t *testing.T) {
	dir := t.TempDir()
	writeLocalTheme(t, dir, "forest", localThemeBody(map[string]string{
		"panel_bg":       "#101418",
		"fg_primary":     "#e6ffe6",
		"diff_add_bg":    "#003c14",
		"border_focused": "#5ac8ff",
	}, nil, "syntax_style = \"nord\"\n"))

	th, warnings, err := LoadLocal("Forest", dir) // mixed case resolves lowercased file
	if err != nil {
		t.Fatalf("LoadLocal: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if th.Name != "forest" {
		t.Errorf("Name = %q, want forest", th.Name)
	}
	if th.SyntaxStyle != "nord" {
		t.Errorf("SyntaxStyle = %q, want nord", th.SyntaxStyle)
	}
	spots := map[string]string{
		"PanelBg": "#101418", "FgPrimary": "#e6ffe6",
		"DiffAddBg": "#003c14", "BorderFocused": "#5ac8ff",
	}
	slots := colorSlots(t, th)
	for slot, want := range spots {
		if got := colorToHex(slots[slot]); got != want {
			t.Errorf("%s = %s, want %s", slot, got, want)
		}
	}
	// Non-overridden keys map positionally onto their slots.
	if got := colorToHex(th.ModeBg); got != fmt.Sprintf("#%02x%02x%02x", 50, 50, 50) {
		t.Errorf("ModeBg = %s (mode_bg is key index 40)", got)
	}
	if h := th.Highlighter(); h.StyleName() != "nord" {
		t.Errorf("Highlighter style = %q, want nord", h.StyleName())
	}
}

func TestLoadLocalNamedColors(t *testing.T) {
	dir := t.TempDir()
	writeLocalTheme(t, dir, "ansi", localThemeBody(map[string]string{
		"panel_bg":     "black",
		"fg_primary":   "white",
		"fg_dim":       "gray",
		"bg_highlight": "dark_gray",
		"mode_bg":      "light_blue",
		"diff_add":     "green",
		"diff_del":     "lightred",
	}, nil, ""))

	th, _, err := LoadLocal("ansi", dir)
	if err != nil {
		t.Fatalf("LoadLocal: %v", err)
	}
	if th.PanelBg != lipgloss.Black {
		t.Errorf("PanelBg = %v, want ANSI black", th.PanelBg)
	}
	if th.FgPrimary != lipgloss.BrightWhite {
		t.Errorf("FgPrimary = %v, want ANSI 15 (ratatui White)", th.FgPrimary)
	}
	if th.FgDim != lipgloss.White {
		t.Errorf("FgDim = %v, want ANSI 7 (ratatui Gray)", th.FgDim)
	}
	if th.BgHighlight != lipgloss.BrightBlack {
		t.Errorf("BgHighlight = %v, want ANSI 8 (ratatui DarkGray)", th.BgHighlight)
	}
	if th.ModeBg != lipgloss.BrightBlue {
		t.Errorf("ModeBg = %v, want ANSI 12 (ratatui LightBlue)", th.ModeBg)
	}
	if th.DiffAdd != lipgloss.Green {
		t.Errorf("DiffAdd = %v, want ANSI green", th.DiffAdd)
	}
	if th.DiffDel != lipgloss.BrightRed {
		t.Errorf("DiffDel = %v, want ANSI 9 (ratatui LightRed)", th.DiffDel)
	}
	// ANSI panel bg counts as dark for the syntax fallback.
	if th.SyntaxStyle != "base16-snazzy" {
		t.Errorf("SyntaxStyle = %q, want dark fallback base16-snazzy", th.SyntaxStyle)
	}
}

func TestLoadLocalSyntaxStyleValidation(t *testing.T) {
	dir := t.TempDir()

	// Unknown style on a dark panel warns and falls back to the dark default.
	writeLocalTheme(t, dir, "darkish", localThemeBody(
		map[string]string{"panel_bg": "#101010"}, nil, "syntax_style = \"no-such-style\"\n"))
	th, warnings, err := LoadLocal("darkish", dir)
	if err != nil {
		t.Fatalf("LoadLocal: %v", err)
	}
	if th.SyntaxStyle != "base16-snazzy" {
		t.Errorf("SyntaxStyle = %q, want base16-snazzy", th.SyntaxStyle)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "unknown syntax_style") {
		t.Errorf("warnings = %v, want unknown syntax_style warning", warnings)
	}

	// Unknown style on a light panel falls back to the light default.
	writeLocalTheme(t, dir, "lightish", localThemeBody(
		map[string]string{"panel_bg": "#fafafa"}, nil, "syntax_style = \"no-such-style\"\n"))
	th, _, err = LoadLocal("lightish", dir)
	if err != nil {
		t.Fatalf("LoadLocal: %v", err)
	}
	if th.SyntaxStyle != "github" {
		t.Errorf("SyntaxStyle = %q, want github", th.SyntaxStyle)
	}

	// No syntax keys at all: silent panel-bg fallback.
	writeLocalTheme(t, dir, "plain", localThemeBody(
		map[string]string{"panel_bg": "#fafafa"}, nil, ""))
	th, warnings, err = LoadLocal("plain", dir)
	if err != nil {
		t.Fatalf("LoadLocal: %v", err)
	}
	if th.SyntaxStyle != "github" || len(warnings) != 0 {
		t.Errorf("SyntaxStyle = %q warnings = %v, want silent github fallback", th.SyntaxStyle, warnings)
	}

	// Registered style names are accepted case-insensitively.
	writeLocalTheme(t, dir, "cased", localThemeBody(nil, nil, "syntax_style = \"Nord\"\n"))
	th, warnings, err = LoadLocal("cased", dir)
	if err != nil {
		t.Fatalf("LoadLocal: %v", err)
	}
	if th.SyntaxStyle != "nord" || len(warnings) != 0 {
		t.Errorf("SyntaxStyle = %q warnings = %v, want nord with no warnings", th.SyntaxStyle, warnings)
	}
}

const customStyleXML = `<style name="my-custom">
  <entry type="Keyword" style="bold #ff0000"/>
  <entry type="Comment" style="italic #00ff00"/>
</style>
`

func TestLoadLocalSyntaxStyleFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "custom.xml"), []byte(customStyleXML), 0o644); err != nil {
		t.Fatal(err)
	}
	writeLocalTheme(t, dir, "custom", localThemeBody(nil, nil, "syntax_style_file = \"custom.xml\"\n"))

	th, warnings, err := LoadLocal("custom", dir)
	if err != nil {
		t.Fatalf("LoadLocal: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if th.SyntaxStyle != "my-custom" {
		t.Errorf("SyntaxStyle = %q, want my-custom", th.SyntaxStyle)
	}
	if got := th.Highlighter().StyleName(); got != "my-custom" {
		t.Errorf("Highlighter style = %q, want my-custom", got)
	}

	// syntax_style_file wins over syntax_style, with a warning.
	writeLocalTheme(t, dir, "both", localThemeBody(nil, nil,
		"syntax_style = \"nord\"\nsyntax_style_file = \"custom.xml\"\n"))
	th, warnings, err = LoadLocal("both", dir)
	if err != nil {
		t.Fatalf("LoadLocal: %v", err)
	}
	if th.SyntaxStyle != "my-custom" {
		t.Errorf("SyntaxStyle = %q, want my-custom", th.SyntaxStyle)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "using syntax_style_file") {
		t.Errorf("warnings = %v, want both-set warning", warnings)
	}

	// A missing style file is a hard error.
	writeLocalTheme(t, dir, "broken", localThemeBody(nil, nil, "syntax_style_file = \"nope.xml\"\n"))
	if _, _, err := LoadLocal("broken", dir); err == nil {
		t.Error("missing syntax_style_file must error")
	}

	// An unparseable style file is a hard error.
	if err := os.WriteFile(filepath.Join(dir, "bad.xml"), []byte("not xml"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeLocalTheme(t, dir, "badxml", localThemeBody(nil, nil, "syntax_style_file = \"bad.xml\"\n"))
	if _, _, err := LoadLocal("badxml", dir); err == nil {
		t.Error("unparseable syntax_style_file must error")
	}
}

func TestLoadLocalMissingKey(t *testing.T) {
	dir := t.TempDir()
	writeLocalTheme(t, dir, "partial", localThemeBody(nil, []string{"panel_bg"}, ""))
	_, _, err := LoadLocal("partial", dir)
	if err == nil || !strings.Contains(err.Error(), "panel_bg") {
		t.Fatalf("err = %v, want missing panel_bg", err)
	}
}

func TestLoadLocalNameNormalization(t *testing.T) {
	dir := t.TempDir()
	for _, bad := range []string{"", "  ", "a/b", `a\b`, "../evil", ".", ".."} {
		if _, _, err := LoadLocal(bad, dir); err == nil ||
			errors.Is(err, ErrLocalThemeNotFound) {
			t.Errorf("LoadLocal(%q) = %v, want name validation error", bad, err)
		}
	}
	if _, _, err := LoadLocal("ghost", dir); !errors.Is(err, ErrLocalThemeNotFound) {
		t.Errorf("LoadLocal(ghost) = %v, want ErrLocalThemeNotFound", err)
	}
	if _, _, err := LoadLocal("ghost", ""); !errors.Is(err, ErrLocalThemeNotFound) {
		t.Errorf("LoadLocal with empty themesDir = %v, want ErrLocalThemeNotFound", err)
	}
}

func TestResolveNamedBundledPrecedence(t *testing.T) {
	dir := t.TempDir()
	// A local dark.toml must NOT shadow the bundled dark theme.
	writeLocalTheme(t, dir, "dark", localThemeBody(map[string]string{"panel_bg": "#123456"}, nil, ""))
	th, warnings, err := ResolveNamed("dark", dir)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("ResolveNamed(dark): warnings=%v err=%v", warnings, err)
	}
	if got := colorToHex(th.PanelBg); got != "#18181c" {
		t.Errorf("PanelBg = %s, want bundled #18181c", got)
	}

	// A non-bundled name resolves to the local file.
	writeLocalTheme(t, dir, "mine", localThemeBody(map[string]string{"panel_bg": "#123456"}, nil, ""))
	th, _, err = ResolveNamed("mine", dir)
	if err != nil {
		t.Fatalf("ResolveNamed(mine): %v", err)
	}
	if got := colorToHex(th.PanelBg); got != "#123456" {
		t.Errorf("PanelBg = %s, want local #123456", got)
	}
}

func TestResolveWithThemesDir(t *testing.T) {
	dir := t.TempDir()
	writeLocalTheme(t, dir, "mine", localThemeBody(map[string]string{"panel_bg": "#123456"}, nil, ""))

	// Config theme resolves through the themes directory.
	th, warnings, err := Resolve("", "mine", "", "", AppearanceUnset, AppearanceUnset, nil, dir)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("config local theme: warnings=%v err=%v", warnings, err)
	}
	if th.Name != "mine" {
		t.Errorf("Name = %q, want mine", th.Name)
	}

	// Flag theme resolves local names too.
	th, _, err = Resolve("mine", "", "", "", AppearanceUnset, AppearanceUnset, nil, dir)
	if err != nil || th.Name != "mine" {
		t.Fatalf("flag local theme: %v %v", th, err)
	}

	// theme_dark/theme_light slots accept local names.
	th, _, err = Resolve("", "", "mine", "", AppearanceDark, AppearanceUnset, nil, dir)
	if err != nil || th.Name != "mine" {
		t.Fatalf("theme_dark local: %v %v", th, err)
	}

	// Unknown flag theme errors and mentions the themes directory.
	_, _, err = Resolve("nope", "", "", "", AppearanceUnset, AppearanceUnset, nil, dir)
	if err == nil || !strings.Contains(err.Error(), dir) {
		t.Errorf("unknown flag theme error = %v, want mention of %s", err, dir)
	}

	// A broken local file under a config key demotes to a warning and falls
	// through to the appearance default.
	writeLocalTheme(t, dir, "broken", "panel_bg = 42\n")
	th, warnings, err = Resolve("", "broken", "", "", AppearanceLight, AppearanceUnset, nil, dir)
	if err != nil {
		t.Fatalf("broken config theme: %v", err)
	}
	if th.Name != "light" {
		t.Errorf("Name = %q, want light fallback", th.Name)
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "Failed to load theme 'broken'") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %v, want failed-to-load warning", warnings)
	}

	// A broken local file behind the --theme flag is a hard error.
	if _, _, err := Resolve("broken", "", "", "", AppearanceUnset, AppearanceUnset, nil, dir); err == nil {
		t.Error("broken flag theme must be a hard error")
	}
}
