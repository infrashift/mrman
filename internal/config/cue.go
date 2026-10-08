package config

import (
	_ "embed"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	cueerrors "cuelang.org/go/cue/errors"
)

//go:embed schema/config.cue
var configSchemaSrc string

//go:embed schema/theme.cue
var themeSchemaSrc string

// maxVetPasses bounds the delete-and-revalidate loop; one offending key is
// removed per pass, so the config key count is a natural ceiling.
const maxVetPasses = 64

// urlishPattern matches the api_base constraint in schema/config.cue.
var urlishPattern = regexp.MustCompile(`^https?://\S+$`)

var validForgeKinds = map[string]struct{}{
	"github": {}, "gitlab": {}, "azuredevops": {}, "forgejo": {},
	"azure_devops": {}, "ado": {}, "gitea": {}, // documented aliases
}

const forgeKindMessage = `must be "github", "gitlab", "azuredevops", or "forgejo"` +
	` (aliases: "azure_devops", "ado", "gitea")`

// keyMessages maps config key paths to human-readable constraint messages
// used when CUE validation rejects the key.
var keyMessages = map[string]string{ //nolint:gosec // G101: key names and their constraint text, not credentials
	"theme":                     "must be a string",
	"theme_dark":                "must be a string",
	"theme_light":               "must be a string",
	"appearance":                `must be "dark", "light", or "system"`,
	"backend":                   "must be a string",
	"comment_types":             "must be an array of tables",
	"show_file_list":            "must be true or false",
	"show_commits":              "must be true or false",
	"diff_view":                 `must be "unified" or "side-by-side"`,
	"commit_order":              `must be "descending" or "ascending"`,
	"initial_commit_selection":  `must be "all" or "oldest"`,
	"ignore_whitespace":         "must be true or false",
	"wrap":                      "must be true or false",
	"export_legend":             "must be true or false",
	"export_diff":               "must be true or false",
	"cursor_line":               "must be true or false",
	"mouse":                     "must be true or false",
	"comment_vim":               "must be true or false",
	"comment_tab_width":         "must be an integer >= 1",
	"leader":                    "must be a single character",
	"transparent_background":    "must be true or false",
	"scroll_offset":             "must be a non-negative integer",
	"review_watch_interval_ms":  "must be a non-negative integer",
	"single_file_view":          "must be true or false",
	"username":                  "must be a string",
	"show_own_author":           "must be true or false",
	"templates":                 "must be a table",
	"templates.notes":           "must be a string",
	"templates.review_body":     "must be a string",
	"templates.patch_reply":     "must be a string",
	"forge":                     "must be a table",
	"forge.default":             forgeKindMessage,
	"forge.comment_type_prefix": "must be true or false",
	"forge.cli_token_fallback":  "must be true or false",
	"forge.hosts":               "must be an array of tables",
}

// compileSchema compiles an embedded CUE source and looks up the named
// definition.
func compileSchema(src, defName string) (cue.Value, error) {
	ctx := cuecontext.New()
	v := ctx.CompileString(src, cue.Filename(strings.TrimPrefix(defName, "#")+".cue"))
	if err := v.Err(); err != nil {
		return cue.Value{}, err
	}
	s := v.LookupPath(cue.ParsePath(defName))
	if err := s.Err(); err != nil {
		return cue.Value{}, err
	}
	return s, nil
}

// vetConfig validates the decoded config map against schema/config.cue. Each
// violation is turned into a warning and the offending piece is deleted from
// the map before revalidating, so one bad key never poisons the rest.
func vetConfig(raw map[string]any, warnings *[]string) {
	schema, err := compileSchema(configSchemaSrc, "#Config")
	if err != nil {
		*warnings = append(*warnings, fmt.Sprintf("config schema error: %v", err))
		return
	}
	ctx := cuecontext.New()
	for range maxVetPasses {
		data := ctx.Encode(raw)
		if err := data.Err(); err != nil {
			clearMap(raw)
			*warnings = append(*warnings, fmt.Sprintf("config: could not validate (%v) — using defaults", err))
			return
		}
		verr := schema.Unify(data).Validate(cue.Final(), cue.Concrete(true))
		if verr == nil {
			return
		}
		if !dropFirstOffender(raw, verr, warnings) {
			clearMap(raw)
			*warnings = append(*warnings, "config: could not validate remaining settings — using defaults")
			return
		}
	}
}

func clearMap(m map[string]any) {
	for k := range m {
		delete(m, k)
	}
}

// dropFirstOffender inspects the CUE validation errors, removes the first
// attributable offending key (or [[forge.hosts]] entry) from the map, and
// records a warning. It reports whether anything was removed.
func dropFirstOffender(raw map[string]any, verr error, warnings *[]string) bool {
	for _, e := range cueerrors.Errors(verr) {
		path := trimDefinitionPrefix(e.Path())
		if len(path) == 0 {
			continue
		}
		switch path[0] {
		case "forge":
			if dropForgeOffender(raw, path, warnings) {
				return true
			}
		case "templates":
			if t, ok := raw["templates"].(map[string]any); ok && len(path) >= 2 {
				if _, present := t[path[1]]; present {
					delete(t, path[1])
					warnKey(warnings, "templates."+path[1])
					return true
				}
				continue
			}
			delete(raw, "templates")
			warnKey(warnings, "templates")
			return true
		default:
			if _, present := raw[path[0]]; present {
				delete(raw, path[0])
				warnKey(warnings, path[0])
				return true
			}
		}
	}
	return false
}

// dropForgeOffender removes the offending piece under the [forge] section:
// a whole host entry for forge.hosts[i] errors, a single forge field
// otherwise, or the entire section when it is not a table.
func dropForgeOffender(raw map[string]any, path []string, warnings *[]string) bool {
	forge, ok := raw["forge"].(map[string]any)
	if !ok {
		delete(raw, "forge")
		warnKey(warnings, "forge")
		return true
	}
	if len(path) >= 3 && path[1] == "hosts" {
		if idx, err := strconv.Atoi(path[2]); err == nil {
			if hosts, ok := tableList(forge["hosts"]); ok && idx >= 0 && idx < len(hosts) {
				*warnings = append(*warnings, hostWarning(hosts[idx], idx))
				forge["hosts"] = append(hosts[:idx:idx], hosts[idx+1:]...)
				return true
			}
		}
	}
	if len(path) >= 2 {
		if _, present := forge[path[1]]; present {
			delete(forge, path[1])
			warnKey(warnings, "forge."+path[1])
			return true
		}
		return false
	}
	delete(raw, "forge")
	warnKey(warnings, "forge")
	return true
}

// hostWarning builds a precise message for an invalid [[forge.hosts]] entry
// by inspecting the raw data (the CUE error text itself is too opaque).
func hostWarning(item any, idx int) string {
	prefix := fmt.Sprintf("config `forge.hosts[%d]`", idx)
	entry, ok := item.(map[string]any)
	if !ok {
		return prefix + ": must be a table — entry ignored"
	}
	if host, _ := entry["host"].(string); strings.TrimSpace(host) == "" {
		return prefix + ": `host` is required and must be a non-empty string — entry ignored"
	}
	_, hasToken := entry["token"]
	_, hasTokenCmd := entry["token_cmd"]
	if hasToken && hasTokenCmd {
		return prefix + ": at most one of `token` and `token_cmd` may be set — entry ignored"
	}
	if kind, present := entry["forge"]; present {
		s, isStr := kind.(string)
		if _, valid := validForgeKinds[s]; !isStr || !valid {
			return prefix + ": `forge` " + forgeKindMessage + " — entry ignored"
		}
	}
	if base, present := entry["api_base"]; present {
		if s, isStr := base.(string); !isStr || !urlishPattern.MatchString(s) {
			return prefix + ": `api_base` must be an http(s) URL — entry ignored"
		}
	}
	return prefix + ": invalid entry — ignored"
}

// trimDefinitionPrefix removes the leading schema-definition selector (for
// example "#Config") that CUE prepends to error paths when validating data
// unified with a definition.
func trimDefinitionPrefix(path []string) []string {
	if len(path) > 0 && strings.HasPrefix(path[0], "#") {
		return path[1:]
	}
	return path
}

// warnKey appends the standard "config `key`: <constraint> — ignored"
// warning for a rejected key.
func warnKey(warnings *[]string, key string) {
	msg, ok := keyMessages[key]
	if !ok {
		msg = "invalid value"
	}
	*warnings = append(*warnings, fmt.Sprintf("config `%s`: %s — ignored", key, msg))
}
