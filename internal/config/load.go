package config

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// knownTopLevelKeys lists every recognized top-level config key; anything
// else warns and is ignored.
var knownTopLevelKeys = map[string]struct{}{
	"theme":                    {},
	"theme_dark":               {},
	"theme_light":              {},
	"appearance":               {},
	"backend":                  {},
	"comment_types":            {},
	"show_file_list":           {},
	"show_commits":             {},
	"diff_view":                {},
	"commit_order":             {},
	"initial_commit_selection": {},
	"ignore_whitespace":        {},
	"wrap":                     {},
	"export_legend":            {},
	"cursor_line":              {},
	"mouse":                    {},
	"comment_vim":              {},
	"comment_tab_width":        {},
	"leader":                   {},
	"transparent_background":   {},
	"scroll_offset":            {},
	"review_watch_interval_ms": {},
	"single_file_view":         {},
	"username":                 {},
	"templates":                {},
	"forge":                    {},
}

var knownForgeKeys = map[string]struct{}{
	"default":             {},
	"comment_type_prefix": {},
	"cli_token_fallback":  {},
	"hosts":               {},
}

var knownHostKeys = map[string]struct{}{
	"host":                 {},
	"forge":                {},
	"api_base":             {},
	"token":                {},
	"token_cmd":            {},
	"ca_file":              {},
	"insecure_skip_verify": {},
}

var knownTemplateKeys = map[string]struct{}{
	"notes":       {},
	"review_body": {},
}

var knownCommentTypeKeys = map[string]struct{}{
	"id":         {},
	"label":      {},
	"definition": {},
	"color":      {},
}

// namedColors mirrors tuicr's supported named terminal colors.
var namedColors = map[string]struct{}{
	"black": {}, "red": {}, "green": {}, "yellow": {}, "blue": {},
	"magenta": {}, "cyan": {}, "gray": {}, "grey": {},
	"darkgray": {}, "dark_gray": {}, "darkgrey": {}, "dark_grey": {},
	"lightred": {}, "light_red": {}, "lightgreen": {}, "light_green": {},
	"lightyellow": {}, "light_yellow": {}, "lightblue": {}, "light_blue": {},
	"lightmagenta": {}, "light_magenta": {}, "lightcyan": {}, "light_cyan": {},
	"white": {},
}

// LoadFrom reads and validates the config file at path. Warnings are never
// fatal: every invalid piece is reported and replaced by its default.
func LoadFrom(path string) (Config, []string) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil
		}
		return cfg, []string{fmt.Sprintf("config %s: %v — using defaults", path, err)}
	}
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return cfg, []string{fmt.Sprintf("config %s: invalid TOML (%v) — using defaults", path, err)}
	}
	var warnings []string
	stripUnknownKeys(raw, &warnings)
	vetConfig(raw, &warnings)
	apply(raw, &cfg, &warnings)
	return cfg, warnings
}

// sortedKeys returns the map's keys in deterministic order for stable
// warning output.
func sortedKeys(m map[string]any) []string {
	return slices.Sorted(maps.Keys(m))
}

// tableList normalizes a decoded TOML array into []any. BurntSushi/toml
// decodes [[array-of-tables]] syntax as []map[string]any and inline arrays
// as []any; callers see a single shape.
func tableList(v any) ([]any, bool) {
	switch t := v.(type) {
	case []any:
		return t, true
	case []map[string]any:
		out := make([]any, len(t))
		for i, m := range t {
			out[i] = m
		}
		return out, true
	}
	return nil, false
}

// stripUnknownKeys warns about and removes keys the schema does not know, at
// the top level and inside the nested [forge], [[forge.hosts]], [templates],
// and comment_types tables.
func stripUnknownKeys(raw map[string]any, warnings *[]string) {
	for _, key := range sortedKeys(raw) {
		if _, ok := knownTopLevelKeys[key]; !ok {
			*warnings = append(*warnings, fmt.Sprintf("unknown config key `%s` — ignored", key))
			delete(raw, key)
		}
	}
	if forge, ok := raw["forge"].(map[string]any); ok {
		for _, key := range sortedKeys(forge) {
			if _, ok := knownForgeKeys[key]; !ok {
				*warnings = append(*warnings, fmt.Sprintf("unknown config key `forge.%s` — ignored", key))
				delete(forge, key)
			}
		}
		if hosts, ok := tableList(forge["hosts"]); ok {
			for i, item := range hosts {
				entry, ok := item.(map[string]any)
				if !ok {
					continue // CUE rejects the entry later
				}
				for _, key := range sortedKeys(entry) {
					if _, ok := knownHostKeys[key]; !ok {
						*warnings = append(*warnings,
							fmt.Sprintf("unknown config key `forge.hosts[%d].%s` — ignored", i, key))
						delete(entry, key)
					}
				}
			}
			forge["hosts"] = hosts
		}
	}
	if templates, ok := raw["templates"].(map[string]any); ok {
		for _, key := range sortedKeys(templates) {
			if _, ok := knownTemplateKeys[key]; !ok {
				*warnings = append(*warnings, fmt.Sprintf("unknown config key `templates.%s` — ignored", key))
				delete(templates, key)
			}
		}
	}
	if items, ok := tableList(raw["comment_types"]); ok {
		for i, item := range items {
			entry, ok := item.(map[string]any)
			if !ok {
				continue // reported when comment_types are parsed
			}
			for _, key := range sortedKeys(entry) {
				if _, ok := knownCommentTypeKeys[key]; !ok {
					*warnings = append(*warnings,
						fmt.Sprintf("unknown config key `comment_types[%d].%s` — ignored", i, key))
					delete(entry, key)
				}
			}
		}
		raw["comment_types"] = items
	}
}

// apply decodes the CUE-cleaned map onto cfg. Types are already vetted, so
// failed assertions simply keep the default.
func apply(raw map[string]any, cfg *Config, warnings *[]string) {
	if v, ok := stringAt(raw, "theme"); ok {
		cfg.Theme = v
	}
	if v, ok := stringAt(raw, "theme_dark"); ok {
		cfg.ThemeDark = v
	}
	if v, ok := stringAt(raw, "theme_light"); ok {
		cfg.ThemeLight = v
	}
	if v, ok := stringAt(raw, "appearance"); ok {
		cfg.Appearance = v
	}
	if v, ok := stringAt(raw, "backend"); ok {
		cfg.Backend = v
		*warnings = append(*warnings, "config `backend` is ignored: mrman uses the git CLI by design, not for want of an alternative")
	}
	if v, ok := raw["comment_types"]; ok {
		cfg.CommentTypes = parseCommentTypes(v, warnings)
	}
	if v, ok := boolAt(raw, "show_file_list"); ok {
		cfg.ShowFileList = v
	}
	if v, ok := boolAt(raw, "show_commits"); ok {
		cfg.ShowCommits = v
	}
	if v, ok := stringAt(raw, "diff_view"); ok {
		cfg.DiffView = v
	}
	if v, ok := stringAt(raw, "commit_order"); ok {
		cfg.CommitOrder = v
	}
	if v, ok := stringAt(raw, "initial_commit_selection"); ok {
		cfg.InitialCommitSelection = v
	}
	if v, ok := boolAt(raw, "ignore_whitespace"); ok {
		cfg.IgnoreWhitespace = v
	}
	if v, ok := boolAt(raw, "wrap"); ok {
		cfg.Wrap = v
	}
	if v, ok := boolAt(raw, "export_legend"); ok {
		cfg.ExportLegend = v
	}
	if v, ok := boolAt(raw, "cursor_line"); ok {
		cfg.CursorLine = v
	}
	if v, ok := boolAt(raw, "mouse"); ok {
		cfg.Mouse = v
	}
	if v, ok := boolAt(raw, "comment_vim"); ok {
		cfg.CommentVim = v
	}
	if v, ok := intAt(raw, "comment_tab_width"); ok {
		cfg.CommentTabWidth = v
	}
	if v, ok := stringAt(raw, "leader"); ok {
		cfg.Leader = v
	}
	if v, ok := boolAt(raw, "transparent_background"); ok {
		cfg.TransparentBackground = v
	}
	if v, ok := intAt(raw, "scroll_offset"); ok {
		cfg.ScrollOffset = v
	}
	if v, ok := intAt(raw, "review_watch_interval_ms"); ok {
		cfg.ReviewWatchIntervalMS = v
	}
	if v, ok := boolAt(raw, "single_file_view"); ok {
		cfg.SingleFileView = v
	}
	if v, ok := stringAt(raw, "username"); ok {
		cfg.Username = v
	}
	if t, ok := raw["templates"].(map[string]any); ok {
		if v, ok := stringAt(t, "notes"); ok {
			cfg.Templates.Notes = v
		}
		if v, ok := stringAt(t, "review_body"); ok {
			cfg.Templates.ReviewBody = v
		}
	}
	if f, ok := raw["forge"].(map[string]any); ok {
		applyForge(f, &cfg.Forge, warnings)
	}
}

// applyForge decodes the vetted [forge] table onto the defaults. A host
// that disables certificate verification is reported every start: it is a
// setting people add to get past a broken proxy and then forget, and a
// forgotten one silently strips TLS from every token that host sees.
func applyForge(f map[string]any, forge *ForgeConfig, warnings *[]string) {
	if v, ok := stringAt(f, "default"); ok {
		forge.Default = v
	}
	if v, ok := boolAt(f, "comment_type_prefix"); ok {
		forge.CommentTypePrefix = v
	}
	if v, ok := boolAt(f, "cli_token_fallback"); ok {
		forge.CLITokenFallback = v
	}
	hosts, ok := tableList(f["hosts"])
	if !ok {
		return
	}
	for _, item := range hosts {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		h := ForgeHost{}
		h.Host, _ = stringAt(entry, "host")
		h.Forge, _ = stringAt(entry, "forge")
		h.APIBase, _ = stringAt(entry, "api_base")
		h.Token, _ = stringAt(entry, "token")
		h.TokenCmd, _ = stringAt(entry, "token_cmd")
		h.CAFile, _ = stringAt(entry, "ca_file")
		h.InsecureSkipVerify, _ = boolAt(entry, "insecure_skip_verify")
		if strings.HasPrefix(strings.ToLower(h.APIBase), "http://") {
			*warnings = append(*warnings, fmt.Sprintf(
				"forge host %s: api_base uses plaintext http — tokens will be sent unencrypted", h.Host))
		}
		if h.InsecureSkipVerify {
			*warnings = append(*warnings, fmt.Sprintf(
				"forge host %s: insecure_skip_verify is on — TLS certificates are not verified", h.Host))
		}
		forge.Hosts = append(forge.Hosts, h)
	}
}

// parseCommentTypes validates comment_types entries with full-replacement
// semantics: invalid entries warn and drop; when nothing survives the result
// is empty and comments stay untyped.
func parseCommentTypes(v any, warnings *[]string) []CommentTypeConfig {
	items, ok := tableList(v)
	if !ok {
		return nil // non-arrays were already rejected by CUE
	}
	out := []CommentTypeConfig{}
	seen := map[string]struct{}{}
	for i, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			*warnings = append(*warnings,
				fmt.Sprintf("config `comment_types[%d]`: must be a table — entry ignored", i))
			continue
		}
		idRaw, ok := entry["id"].(string)
		if !ok {
			*warnings = append(*warnings,
				fmt.Sprintf("config `comment_types[%d].id`: must be a string — entry ignored", i))
			continue
		}
		id := strings.ToLower(strings.TrimSpace(idRaw))
		if id == "" {
			*warnings = append(*warnings,
				fmt.Sprintf("config `comment_types[%d].id`: cannot be empty — entry ignored", i))
			continue
		}
		if _, dup := seen[id]; dup {
			*warnings = append(*warnings,
				fmt.Sprintf("config `comment_types`: duplicate id `%s` — entry ignored", id))
			continue
		}
		ct := CommentTypeConfig{ID: id}
		ct.Label = optionalEntryString(entry, "label", i, warnings)
		if ct.Label == "" {
			ct.Label = strings.ToUpper(id)
		}
		ct.Definition = optionalEntryString(entry, "definition", i, warnings)
		if colorRaw, present := entry["color"]; present {
			s, isStr := colorRaw.(string)
			trimmed := strings.TrimSpace(s)
			if !isStr || !isSupportedColor(trimmed) {
				*warnings = append(*warnings,
					fmt.Sprintf("config `comment_types[%d].color`: must be a named terminal color or #RRGGBB — ignored", i))
			} else {
				ct.Color = trimmed
			}
		}
		seen[id] = struct{}{}
		out = append(out, ct)
	}
	if len(out) == 0 {
		*warnings = append(*warnings, "config `comment_types`: no valid entries — comments will be untyped")
	}
	return out
}

// optionalEntryString reads an optional non-empty string field from a
// comment_types entry, warning on wrong types or empty values.
func optionalEntryString(entry map[string]any, field string, idx int, warnings *[]string) string {
	raw, present := entry[field]
	if !present {
		return ""
	}
	s, isStr := raw.(string)
	if !isStr {
		*warnings = append(*warnings,
			fmt.Sprintf("config `comment_types[%d].%s`: must be a string — ignored", idx, field))
		return ""
	}
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		*warnings = append(*warnings,
			fmt.Sprintf("config `comment_types[%d].%s`: cannot be empty — ignored", idx, field))
		return ""
	}
	return trimmed
}

// isSupportedColor reports whether value is a named terminal color or a
// "#RRGGBB" hex value (tuicr's color grammar).
func isSupportedColor(value string) bool {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" {
		return false
	}
	if hex, ok := strings.CutPrefix(v, "#"); ok {
		if len(hex) != 6 {
			return false
		}
		for _, r := range hex {
			if !isHexDigit(r) {
				return false
			}
		}
		return true
	}
	_, ok := namedColors[v]
	return ok
}

func isHexDigit(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

func stringAt(m map[string]any, key string) (string, bool) {
	v, ok := m[key].(string)
	return v, ok
}

func boolAt(m map[string]any, key string) (bool, bool) {
	v, ok := m[key].(bool)
	return v, ok
}

func intAt(m map[string]any, key string) (int, bool) {
	switch v := m[key].(type) {
	case int64:
		return int(v), true
	case int:
		return v, true
	}
	return 0, false
}
