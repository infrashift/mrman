package theme

import (
	"errors"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/styles"

	"github.com/infrashift/mrman/internal/config"
)

// ErrLocalThemeNotFound reports that no <name>.toml exists in the themes
// directory. Resolve treats it as "unknown theme" (warning, or hard error
// for the --theme flag) rather than as a load failure.
var ErrLocalThemeNotFound = errors.New("local theme not found")

// namedTermColors maps the named terminal colors accepted in theme files to
// their lipgloss ANSI equivalents, following ratatui's palette indices
// (Gray→7, DarkGray→8, Light*→bright 9..14, White→15).
var namedTermColors = map[string]color.Color{
	"black":    lipgloss.Black,
	"red":      lipgloss.Red,
	"green":    lipgloss.Green,
	"yellow":   lipgloss.Yellow,
	"blue":     lipgloss.Blue,
	"magenta":  lipgloss.Magenta,
	"cyan":     lipgloss.Cyan,
	"gray":     lipgloss.White, // ratatui Gray = ANSI 7
	"grey":     lipgloss.White,
	"darkgray": lipgloss.BrightBlack, "dark_gray": lipgloss.BrightBlack,
	"darkgrey": lipgloss.BrightBlack, "dark_grey": lipgloss.BrightBlack,
	"lightred": lipgloss.BrightRed, "light_red": lipgloss.BrightRed,
	"lightgreen": lipgloss.BrightGreen, "light_green": lipgloss.BrightGreen,
	"lightyellow": lipgloss.BrightYellow, "light_yellow": lipgloss.BrightYellow,
	"lightblue": lipgloss.BrightBlue, "light_blue": lipgloss.BrightBlue,
	"lightmagenta": lipgloss.BrightMagenta, "light_magenta": lipgloss.BrightMagenta,
	"lightcyan": lipgloss.BrightCyan, "light_cyan": lipgloss.BrightCyan,
	"white": lipgloss.BrightWhite, // ratatui White = ANSI 15
}

// parseThemeColor turns a validated theme value ("#RRGGBB" or a named
// terminal color, case-insensitive) into a color. Unknown values yield nil
// (terminal default); config.ValidateThemeTOML rejects them before this
// point.
func parseThemeColor(value string) color.Color {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if strings.HasPrefix(normalized, "#") {
		return hexColor(normalized)
	}
	return namedTermColors[normalized]
}

// isDarkColor reports whether a color reads as dark (channel average below
// 128); nil and ANSI palette colors count as dark, a port of tuicr's
// is_dark_color.
func isDarkColor(c color.Color) bool {
	r, g, b, ok := rgbComponents(c)
	if !ok {
		return true
	}
	return (int(r)+int(g)+int(b))/3 < 128
}

// fallbackSyntaxStyle picks the default chroma style for a theme that does
// not name one, by panel-background lightness — the same styles the bundled
// dark/light themes use (port of fallback_embedded_theme_for_panel_bg).
func fallbackSyntaxStyle(panelBg color.Color) string {
	if isDarkColor(panelBg) {
		return "base16-snazzy"
	}
	return "github"
}

// normalizeLocalThemeName lowercases a local theme name and rejects
// anything that is not a single plain path element, a port of tuicr's
// normalize_local_theme_name.
func normalizeLocalThemeName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", errors.New("theme name cannot be empty")
	}
	if strings.ContainsAny(trimmed, `/\`) || trimmed == "." || trimmed == ".." {
		return "", errors.New("theme name must be a plain name, not a path")
	}
	return strings.ToLower(trimmed), nil
}

// LoadLocal loads themesDir/<name>.toml as a Theme. The file is validated
// by config.ValidateThemeTOML; named terminal colors map to lipgloss ANSI
// colors and #RRGGBB values to RGB. syntax_style selects a registered
// chroma style (unknown names warn and fall back by panel-bg lightness);
// syntax_style_file loads a chroma XML style file resolved relative to the
// themes directory. A missing file yields ErrLocalThemeNotFound.
func LoadLocal(name, themesDir string) (*Theme, []string, error) {
	normalized, err := normalizeLocalThemeName(name)
	if err != nil {
		return nil, nil, err
	}
	if themesDir == "" {
		return nil, nil, fmt.Errorf("%w: %s (no themes directory)", ErrLocalThemeNotFound, normalized)
	}
	path := filepath.Join(themesDir, normalized+".toml")
	if _, statErr := os.Stat(path); statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return nil, nil, fmt.Errorf("%w: %s", ErrLocalThemeNotFound, path)
		}
		return nil, nil, statErr
	}

	colors, style, styleFile, warnings, err := config.ValidateThemeTOML(path)
	if err != nil {
		return nil, warnings, err
	}

	c := func(key string) color.Color { return parseThemeColor(colors[key]) }
	t := &Theme{
		Name: normalized,

		PanelBg:     c("panel_bg"),
		BgHighlight: c("bg_highlight"),
		FgPrimary:   c("fg_primary"),
		FgSecondary: c("fg_secondary"),
		FgDim:       c("fg_dim"),

		DiffAdd:           c("diff_add"),
		DiffAddBg:         c("diff_add_bg"),
		DiffDel:           c("diff_del"),
		DiffDelBg:         c("diff_del_bg"),
		DiffContext:       c("diff_context"),
		DiffHunkHeader:    c("diff_hunk_header"),
		ExpandedContextFg: c("expanded_context_fg"),

		SyntaxAddBg: c("syntax_add_bg"),
		SyntaxDelBg: c("syntax_del_bg"),

		FileAdded:    c("file_added"),
		FileModified: c("file_modified"),
		FileDeleted:  c("file_deleted"),
		FileRenamed:  c("file_renamed"),

		Reviewed: c("reviewed"),
		Pending:  c("pending"),

		CommentNote:       c("comment_note"),
		CommentSuggestion: c("comment_suggestion"),
		CommentIssue:      c("comment_issue"),
		CommentPraise:     c("comment_praise"),

		BorderFocused:   c("border_focused"),
		BorderUnfocused: c("border_unfocused"),
		StatusBarBg:     c("status_bar_bg"),
		CursorColor:     c("cursor_color"),
		CursorLineBg:    c("cursor_line_bg"),
		BranchName:      c("branch_name"),
		HelpIndicator:   c("help_indicator"),

		MessageInfoFg:    c("message_info_fg"),
		MessageInfoBg:    c("message_info_bg"),
		MessageWarningFg: c("message_warning_fg"),
		MessageWarningBg: c("message_warning_bg"),
		MessageErrorFg:   c("message_error_fg"),
		MessageErrorBg:   c("message_error_bg"),
		UpdateBadgeFg:    c("update_badge_fg"),
		UpdateBadgeBg:    c("update_badge_bg"),

		ModeFg: c("mode_fg"),
		ModeBg: c("mode_bg"),
	}

	syntaxWarnings, err := applySyntaxStyle(t, path, style, styleFile)
	warnings = append(warnings, syntaxWarnings...)
	if err != nil {
		return nil, warnings, err
	}
	return t, warnings, nil
}

// applySyntaxStyle resolves a local theme's syntax configuration onto t:
// syntax_style_file (chroma XML, wins when both are set) > syntax_style
// (registered chroma name, warn+fallback on unknown) > panel-bg fallback.
func applySyntaxStyle(t *Theme, themePath, style, styleFile string) ([]string, error) {
	var warnings []string
	if styleFile != "" {
		if style != "" {
			warnings = append(warnings, fmt.Sprintf(
				"theme %s sets both syntax_style and syntax_style_file; using syntax_style_file", themePath))
		}
		loaded, err := loadSyntaxStyleFile(themePath, styleFile)
		if err != nil {
			return warnings, err
		}
		t.customSyntax = loaded
		t.SyntaxStyle = loaded.Name
		return warnings, nil
	}
	if style != "" {
		if _, ok := styles.Registry[strings.ToLower(strings.TrimSpace(style))]; !ok {
			fallback := fallbackSyntaxStyle(t.PanelBg)
			warnings = append(warnings, fmt.Sprintf(
				"theme %s: unknown syntax_style %q, falling back to %q", themePath, style, fallback))
			t.SyntaxStyle = fallback
			return warnings, nil
		}
		t.SyntaxStyle = strings.ToLower(strings.TrimSpace(style))
		return warnings, nil
	}
	t.SyntaxStyle = fallbackSyntaxStyle(t.PanelBg)
	return warnings, nil
}

// loadSyntaxStyleFile loads a chroma XML style file; relative paths resolve
// against the theme file's directory (tuicr's load_custom_syntect_theme
// path handling).
func loadSyntaxStyleFile(themePath, styleFile string) (*chroma.Style, error) {
	resolved := styleFile
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(filepath.Dir(themePath), styleFile)
	}
	f, err := os.Open(resolved)
	if err != nil {
		return nil, fmt.Errorf("load syntax style file %s: %w", resolved, err)
	}
	defer f.Close() //nolint:errcheck // read-only file
	loaded, err := chroma.NewXMLStyle(f)
	if err != nil {
		return nil, fmt.Errorf("parse syntax style file %s: %w", resolved, err)
	}
	return loaded, nil
}

// ResolveNamed resolves a theme name to a bundled theme first (bundled
// names always win) and otherwise to themesDir/<name>.toml via LoadLocal.
func ResolveNamed(name, themesDir string) (*Theme, []string, error) {
	if t, ok := Lookup(name); ok {
		return t, nil, nil
	}
	return LoadLocal(name, themesDir)
}
