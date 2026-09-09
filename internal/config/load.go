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
	"export_diff":              {},
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
	"show_own_author":          {},
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
	"patch_reply": {},
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

// stripUnknown warns about and removes the keys of one table that its
// schema does not know. prefix names the table in the warning
// ("forge.hosts[0].").
func stripUnknown(table map[string]any, known map[string]struct{}, prefix string, warnings *[]string) {
	for _, key := range sortedKeys(table) {
		if _, ok := known[key]; !ok {
			*warnings = append(*warnings, fmt.Sprintf("unknown config key `%s%s` — ignored", prefix, key))
			delete(table, key)
		}
	}
}

// stripUnknownList applies stripUnknown to each table of an array of
// tables, naming entries by index. Non-table entries are left for the
// schema pass to reject.
func stripUnknownList(items []any, known map[string]struct{}, prefix string, warnings *[]string) {
	for i, item := range items {
		if entry, ok := item.(map[string]any); ok {
			stripUnknown(entry, known, fmt.Sprintf("%s[%d].", prefix, i), warnings)
		}
	}
}

// stripUnknownKeys warns about and removes keys the schema does not know, at
// the top level and inside the nested [forge], [[forge.hosts]], [templates],
// and comment_types tables.
func stripUnknownKeys(raw map[string]any, warnings *[]string) {
	stripUnknown(raw, knownTopLevelKeys, "", warnings)
	if forge, ok := raw["forge"].(map[string]any); ok {
		stripUnknown(forge, knownForgeKeys, "forge.", warnings)
		if hosts, ok := tableList(forge["hosts"]); ok {
			stripUnknownList(hosts, knownHostKeys, "forge.hosts", warnings)
			forge["hosts"] = hosts
		}
	}
	if templates, ok := raw["templates"].(map[string]any); ok {
		stripUnknown(templates, knownTemplateKeys, "templates.", warnings)
	}
	if items, ok := tableList(raw["comment_types"]); ok {
		stripUnknownList(items, knownCommentTypeKeys, "comment_types", warnings)
		raw["comment_types"] = items
	}
}

// apply decodes the CUE-cleaned map onto cfg. Types are already vetted, so
// failed assertions simply keep the default. The scalar keys are tables of
// destination fields; the structured ones are handled by hand.
func apply(raw map[string]any, cfg *Config, warnings *[]string) {
	stringKeys := map[string]*string{
		"theme": &cfg.Theme, "theme_dark": &cfg.ThemeDark, "theme_light": &cfg.ThemeLight,
		"appearance": &cfg.Appearance, "backend": &cfg.Backend, "diff_view": &cfg.DiffView,
		"commit_order": &cfg.CommitOrder, "initial_commit_selection": &cfg.InitialCommitSelection,
		"leader": &cfg.Leader, "username": &cfg.Username,
	}
	boolKeys := map[string]*bool{
		"show_file_list": &cfg.ShowFileList, "show_commits": &cfg.ShowCommits,
		"ignore_whitespace": &cfg.IgnoreWhitespace, "wrap": &cfg.Wrap,
		"export_legend": &cfg.ExportLegend, "export_diff": &cfg.ExportDiff,
		"show_own_author": &cfg.ShowOwnAuthor, "cursor_line": &cfg.CursorLine,
		"mouse": &cfg.Mouse, "comment_vim": &cfg.CommentVim,
		"transparent_background": &cfg.TransparentBackground, "single_file_view": &cfg.SingleFileView,
	}
	intKeys := map[string]*int{
		"comment_tab_width": &cfg.CommentTabWidth, "scroll_offset": &cfg.ScrollOffset,
		"review_watch_interval_ms": &cfg.ReviewWatchIntervalMS,
	}
	for key, dst := range stringKeys {
		if v, ok := stringAt(raw, key); ok {
			*dst = v
		}
	}
	for key, dst := range boolKeys {
		if v, ok := boolAt(raw, key); ok {
			*dst = v
		}
	}
	for key, dst := range intKeys {
		if v, ok := intAt(raw, key); ok {
			*dst = v
		}
	}
	if _, ok := stringAt(raw, "backend"); ok {
		*warnings = append(*warnings, "config `backend` is ignored: mrman uses the git CLI by design, not for want of an alternative")
	}
	if v, ok := raw["comment_types"]; ok {
		cfg.CommentTypes = parseCommentTypes(v, warnings)
	}
	if t, ok := raw["templates"].(map[string]any); ok {
		templateKeys := map[string]*string{
			"notes": &cfg.Templates.Notes, "review_body": &cfg.Templates.ReviewBody, "patch_reply": &cfg.Templates.PatchReply,
		}
		for key, dst := range templateKeys {
			if v, ok := stringAt(t, key); ok {
				*dst = v
			}
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
