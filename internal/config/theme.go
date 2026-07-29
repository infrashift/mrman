package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	cueerrors "cuelang.org/go/cue/errors"
)

// ThemeColorKeys lists the palette keys a local theme file must define, in
// schema order. The theme package consumes these when building a Theme.
var ThemeColorKeys = []string{
	"panel_bg", "bg_highlight", "fg_primary", "fg_secondary", "fg_dim",
	"diff_add", "diff_add_bg", "diff_del", "diff_del_bg", "diff_context",
	"diff_hunk_header", "expanded_context_fg",
	"syntax_add_bg", "syntax_del_bg",
	"file_added", "file_modified", "file_deleted", "file_renamed",
	"reviewed", "pending",
	"comment_note", "comment_suggestion", "comment_issue", "comment_praise",
	"border_focused", "border_unfocused", "status_bar_bg", "cursor_color",
	"cursor_line_bg", "branch_name", "help_indicator",
	"message_info_fg", "message_info_bg",
	"message_warning_fg", "message_warning_bg",
	"message_error_fg", "message_error_bg",
	"update_badge_fg", "update_badge_bg",
	"mode_fg", "mode_bg",
}

// ValidateThemeTOML reads and validates a local theme file against
// schema/theme.cue. It returns the palette (key → color value), the optional
// syntax_style and syntax_style_file values, warnings for unknown keys, and
// an error when the file is unreadable or the palette is invalid.
func ValidateThemeTOML(path string) (map[string]string, string, string, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", "", nil, fmt.Errorf("read theme %s: %w", path, err)
	}
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, "", "", nil, fmt.Errorf("parse theme %s: %w", path, err)
	}

	known := make(map[string]struct{}, len(ThemeColorKeys)+2)
	for _, key := range ThemeColorKeys {
		known[key] = struct{}{}
	}
	known["syntax_style"] = struct{}{}
	known["syntax_style_file"] = struct{}{}

	var warnings []string
	for _, key := range sortedKeys(raw) {
		if _, ok := known[key]; !ok {
			warnings = append(warnings, fmt.Sprintf("unknown theme key `%s` in %s — ignored", key, path))
			delete(raw, key)
		}
	}

	// CUE suppresses required-field diagnostics when value errors are also
	// present, so missing keys are detected here for complete reporting.
	var msgs []string
	for _, key := range ThemeColorKeys {
		if _, present := raw[key]; !present {
			msgs = append(msgs, fmt.Sprintf("missing required key `%s`", key))
		}
	}
	schema, err := compileSchema(themeSchemaSrc, "#Theme")
	if err != nil {
		return nil, "", "", warnings, fmt.Errorf("theme schema error: %w", err)
	}
	encoded := cuecontext.New().Encode(raw)
	if err := encoded.Err(); err != nil {
		return nil, "", "", warnings, fmt.Errorf("theme %s: %w", path, err)
	}
	if verr := schema.Unify(encoded).Validate(cue.All(), cue.Concrete(true)); verr != nil {
		msgs = append(msgs, themeErrorMessages(raw, verr)...)
	}
	if len(msgs) > 0 {
		return nil, "", "", warnings, fmt.Errorf("theme %s: %s", path, strings.Join(msgs, "; "))
	}

	colors := make(map[string]string, len(ThemeColorKeys))
	for _, key := range ThemeColorKeys {
		s, _ := raw[key].(string)
		colors[key] = strings.TrimSpace(s)
	}
	style, _ := raw["syntax_style"].(string)
	styleFile, _ := raw["syntax_style_file"].(string)
	return colors, style, styleFile, warnings, nil
}

// themeErrorMessages converts CUE validation errors into readable per-key
// messages: missing required keys vs invalid values.
func themeErrorMessages(raw map[string]any, verr error) []string {
	var msgs []string
	seen := map[string]struct{}{}
	for _, e := range cueerrors.Errors(verr) {
		path := trimDefinitionPrefix(e.Path())
		if len(path) == 0 {
			msgs = append(msgs, e.Error())
			continue
		}
		key := path[0]
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		switch {
		case rawValueMissing(raw, key):
			continue // already reported as a missing required key
		case key == "syntax_style" || key == "syntax_style_file":
			msgs = append(msgs, fmt.Sprintf("key `%s` must be a string", key))
		default:
			msgs = append(msgs, fmt.Sprintf("key `%s` must be #RRGGBB or a named terminal color", key))
		}
	}
	if len(msgs) == 0 {
		msgs = append(msgs, verr.Error())
	}
	return msgs
}

func rawValueMissing(raw map[string]any, key string) bool {
	_, present := raw[key]
	return !present
}
