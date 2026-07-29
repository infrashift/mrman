package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validThemeTOML builds a complete theme file body, then applies overrides:
// an empty override value removes the key, anything else replaces the line.
func validThemeTOML(overrides map[string]string) string {
	var b strings.Builder
	for i, key := range ThemeColorKeys {
		if v, ok := overrides[key]; ok {
			if v == "" {
				continue
			}
			b.WriteString(key + " = " + v + "\n")
			continue
		}
		// Mix hex and named values across keys.
		if i%2 == 0 {
			b.WriteString(key + " = \"#11aa33\"\n")
		} else {
			b.WriteString(key + " = \"light_blue\"\n")
		}
	}
	for key, v := range overrides {
		if !strings.HasPrefix(key, "+") {
			continue
		}
		b.WriteString(strings.TrimPrefix(key, "+") + " = " + v + "\n")
	}
	return b.String()
}

func writeTheme(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "my-theme.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write theme: %v", err)
	}
	return path
}

func TestValidateThemeTOMLAcceptsCompleteTheme(t *testing.T) {
	path := writeTheme(t, validThemeTOML(map[string]string{
		"+syntax_style": `"tokyonight-storm"`,
	}))
	colors, style, styleFile, warnings, err := ValidateThemeTOML(path)
	if err != nil {
		t.Fatalf("ValidateThemeTOML: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %q, want none", warnings)
	}
	if len(colors) != len(ThemeColorKeys) {
		t.Errorf("len(colors) = %d, want %d", len(colors), len(ThemeColorKeys))
	}
	if colors["panel_bg"] != "#11aa33" {
		t.Errorf("panel_bg = %q, want #11aa33", colors["panel_bg"])
	}
	if colors["bg_highlight"] != "light_blue" {
		t.Errorf("bg_highlight = %q, want light_blue", colors["bg_highlight"])
	}
	if style != "tokyonight-storm" {
		t.Errorf("syntax_style = %q, want tokyonight-storm", style)
	}
	if styleFile != "" {
		t.Errorf("syntax_style_file = %q, want empty", styleFile)
	}
}

func TestValidateThemeTOMLSyntaxStyleFile(t *testing.T) {
	path := writeTheme(t, validThemeTOML(map[string]string{
		"+syntax_style_file": `"my-style.xml"`,
	}))
	_, style, styleFile, _, err := ValidateThemeTOML(path)
	if err != nil {
		t.Fatalf("ValidateThemeTOML: %v", err)
	}
	if style != "" || styleFile != "my-style.xml" {
		t.Errorf("style = %q styleFile = %q, want empty / my-style.xml", style, styleFile)
	}
}

func TestValidateThemeTOMLMissingKey(t *testing.T) {
	path := writeTheme(t, validThemeTOML(map[string]string{"panel_bg": ""}))
	_, _, _, _, err := ValidateThemeTOML(path)
	if err == nil || !strings.Contains(err.Error(), "missing required key `panel_bg`") {
		t.Errorf("err = %v, want missing panel_bg", err)
	}
}

func TestValidateThemeTOMLBadHex(t *testing.T) {
	path := writeTheme(t, validThemeTOML(map[string]string{"diff_add": `"#12"`}))
	_, _, _, _, err := ValidateThemeTOML(path)
	if err == nil || !strings.Contains(err.Error(), "key `diff_add` must be #RRGGBB or a named terminal color") {
		t.Errorf("err = %v, want diff_add color error", err)
	}
}

func TestValidateThemeTOMLBadNamedColor(t *testing.T) {
	path := writeTheme(t, validThemeTOML(map[string]string{"mode_bg": `"chartreuse"`}))
	_, _, _, _, err := ValidateThemeTOML(path)
	if err == nil || !strings.Contains(err.Error(), "key `mode_bg` must be #RRGGBB or a named terminal color") {
		t.Errorf("err = %v, want mode_bg color error", err)
	}
}

func TestValidateThemeTOMLNonStringValue(t *testing.T) {
	path := writeTheme(t, validThemeTOML(map[string]string{"fg_dim": "12"}))
	_, _, _, _, err := ValidateThemeTOML(path)
	if err == nil || !strings.Contains(err.Error(), "`fg_dim`") {
		t.Errorf("err = %v, want fg_dim error", err)
	}
}

func TestValidateThemeTOMLBadSyntaxStyleType(t *testing.T) {
	path := writeTheme(t, validThemeTOML(map[string]string{"+syntax_style": "7"}))
	_, _, _, _, err := ValidateThemeTOML(path)
	if err == nil || !strings.Contains(err.Error(), "key `syntax_style` must be a string") {
		t.Errorf("err = %v, want syntax_style type error", err)
	}
}

func TestValidateThemeTOMLMultipleErrorsReported(t *testing.T) {
	path := writeTheme(t, validThemeTOML(map[string]string{
		"panel_bg": "",
		"diff_add": `"#12"`,
	}))
	_, _, _, _, err := ValidateThemeTOML(path)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "`panel_bg`") || !strings.Contains(err.Error(), "`diff_add`") {
		t.Errorf("err = %v, want both panel_bg and diff_add mentioned", err)
	}
}

func TestValidateThemeTOMLUnknownKeyWarns(t *testing.T) {
	path := writeTheme(t, validThemeTOML(map[string]string{
		"+syntax_theme": `"my-theme.tmTheme"`,
	}))
	colors, _, _, warnings, err := ValidateThemeTOML(path)
	if err != nil {
		t.Fatalf("ValidateThemeTOML: %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "unknown theme key `syntax_theme`") {
		t.Errorf("warnings = %q, want one unknown syntax_theme warning", warnings)
	}
	if len(colors) != len(ThemeColorKeys) {
		t.Errorf("len(colors) = %d, want %d", len(colors), len(ThemeColorKeys))
	}
}

func TestValidateThemeTOMLMissingFile(t *testing.T) {
	_, _, _, _, err := ValidateThemeTOML(filepath.Join(t.TempDir(), "nope.toml"))
	if err == nil || !strings.Contains(err.Error(), "read theme") {
		t.Errorf("err = %v, want read error", err)
	}
}

func TestValidateThemeTOMLParseError(t *testing.T) {
	path := writeTheme(t, "panel_bg =\n")
	_, _, _, _, err := ValidateThemeTOML(path)
	if err == nil || !strings.Contains(err.Error(), "parse theme") {
		t.Errorf("err = %v, want parse error", err)
	}
}

func TestThemeColorKeysCount(t *testing.T) {
	// tuicr's LOCAL_THEME_KEYS has 42 entries; one (syntax_theme) is not a
	// color, so mrman requires 41 palette keys.
	if len(ThemeColorKeys) != 41 {
		t.Errorf("len(ThemeColorKeys) = %d, want 41", len(ThemeColorKeys))
	}
	seen := map[string]struct{}{}
	for _, key := range ThemeColorKeys {
		if _, dup := seen[key]; dup {
			t.Errorf("duplicate theme key %q", key)
		}
		seen[key] = struct{}{}
	}
}
