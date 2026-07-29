package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/adrg/xdg"
)

// writeConfig writes content to a temp config.toml and returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// loadString loads a config from an inline TOML string.
func loadString(t *testing.T, content string) (Config, []string) {
	t.Helper()
	return LoadFrom(writeConfig(t, content))
}

// hasWarning reports whether any warning contains substr.
func hasWarning(warnings []string, substr string) bool {
	for _, w := range warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}

func requireWarning(t *testing.T, warnings []string, substr string) {
	t.Helper()
	if !hasWarning(warnings, substr) {
		t.Errorf("expected a warning containing %q, got %q", substr, warnings)
	}
}

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.Theme != "" || cfg.ThemeDark != "" || cfg.ThemeLight != "" {
		t.Errorf("theme defaults should be empty, got %q %q %q", cfg.Theme, cfg.ThemeDark, cfg.ThemeLight)
	}
	if cfg.Appearance != "system" {
		t.Errorf("Appearance = %q, want system", cfg.Appearance)
	}
	if cfg.Backend != "" {
		t.Errorf("Backend = %q, want empty", cfg.Backend)
	}
	if cfg.CommentTypes != nil {
		t.Errorf("CommentTypes = %v, want nil", cfg.CommentTypes)
	}
	if !cfg.ShowFileList || !cfg.ShowCommits {
		t.Error("ShowFileList and ShowCommits should default to true")
	}
	if cfg.DiffView != "unified" {
		t.Errorf("DiffView = %q, want unified", cfg.DiffView)
	}
	if cfg.CommitOrder != "descending" {
		t.Errorf("CommitOrder = %q, want descending", cfg.CommitOrder)
	}
	if cfg.InitialCommitSelection != "all" {
		t.Errorf("InitialCommitSelection = %q, want all", cfg.InitialCommitSelection)
	}
	if cfg.IgnoreWhitespace || cfg.Wrap || cfg.CommentVim || cfg.SingleFileView {
		t.Error("IgnoreWhitespace, Wrap, CommentVim, SingleFileView should default to false")
	}
	if !cfg.ExportLegend || !cfg.CursorLine || !cfg.Mouse || !cfg.TransparentBackground {
		t.Error("ExportLegend, CursorLine, Mouse, TransparentBackground should default to true")
	}
	if cfg.CommentTabWidth != 4 {
		t.Errorf("CommentTabWidth = %d, want 4", cfg.CommentTabWidth)
	}
	if cfg.Leader != ";" {
		t.Errorf("Leader = %q, want ;", cfg.Leader)
	}
	if cfg.ScrollOffset != 0 {
		t.Errorf("ScrollOffset = %d, want 0", cfg.ScrollOffset)
	}
	if cfg.ReviewWatchIntervalMS != 1000 {
		t.Errorf("ReviewWatchIntervalMS = %d, want 1000", cfg.ReviewWatchIntervalMS)
	}
	if cfg.Username != "" {
		t.Errorf("Username = %q, want empty", cfg.Username)
	}
	if cfg.Templates != (TemplatesConfig{}) {
		t.Errorf("Templates = %+v, want zero", cfg.Templates)
	}
	if cfg.Forge.Default != "github" {
		t.Errorf("Forge.Default = %q, want github", cfg.Forge.Default)
	}
	if !cfg.Forge.CommentTypePrefix || !cfg.Forge.CLITokenFallback {
		t.Error("Forge.CommentTypePrefix and Forge.CLITokenFallback should default to true")
	}
	if cfg.Forge.Hosts != nil {
		t.Errorf("Forge.Hosts = %v, want nil", cfg.Forge.Hosts)
	}
}

func TestLoadFromMissingFile(t *testing.T) {
	cfg, warnings := LoadFrom(filepath.Join(t.TempDir(), "does-not-exist.toml"))
	if len(warnings) != 0 {
		t.Errorf("warnings = %q, want none", warnings)
	}
	if cfg.DiffView != "unified" || cfg.Leader != ";" {
		t.Errorf("missing file should yield defaults, got %+v", cfg)
	}
}

func TestLoadFromUnreadablePath(t *testing.T) {
	// A directory path fails to read but must still yield defaults + warning.
	cfg, warnings := LoadFrom(t.TempDir())
	if len(warnings) != 1 {
		t.Fatalf("warnings = %q, want exactly one", warnings)
	}
	if cfg.DiffView != "unified" {
		t.Errorf("unreadable path should yield defaults, got %+v", cfg)
	}
}

func TestLoadFromTOMLSyntaxError(t *testing.T) {
	cfg, warnings := loadString(t, "theme =\n")
	if len(warnings) != 1 || !hasWarning(warnings, "invalid TOML") {
		t.Errorf("warnings = %q, want one invalid-TOML warning", warnings)
	}
	if cfg.Theme != "" || cfg.DiffView != "unified" {
		t.Errorf("syntax error should yield defaults, got %+v", cfg)
	}
}

func TestLoadEmptyConfigIsDefaults(t *testing.T) {
	cfg, warnings := loadString(t, "")
	if len(warnings) != 0 {
		t.Errorf("warnings = %q, want none", warnings)
	}
	if cfg.DiffView != "unified" || cfg.CommentTabWidth != 4 {
		t.Errorf("empty config should be defaults, got %+v", cfg)
	}
}

func TestUnknownKeysWarnAndKnownValuesSurvive(t *testing.T) {
	cfg, warnings := loadString(t, "theme = \"light\"\nthemes = \"typo\"\n")
	if cfg.Theme != "light" {
		t.Errorf("Theme = %q, want light", cfg.Theme)
	}
	requireWarning(t, warnings, "unknown config key `themes` — ignored")
	if len(warnings) != 1 {
		t.Errorf("warnings = %q, want exactly one", warnings)
	}
}

func TestUnknownNestedKeys(t *testing.T) {
	cfg, warnings := loadString(t, `
[forge]
comment_type_prefix = false
foo = "bar"

[templates]
notes = "n.tmpl"
extra = 1

[[forge.hosts]]
host = "gh.corp"
fingerprint = "xx"
`)
	requireWarning(t, warnings, "unknown config key `forge.foo` — ignored")
	requireWarning(t, warnings, "unknown config key `templates.extra` — ignored")
	requireWarning(t, warnings, "unknown config key `forge.hosts[0].fingerprint` — ignored")
	if cfg.Forge.CommentTypePrefix {
		t.Error("comment_type_prefix = false should survive unknown siblings")
	}
	if cfg.Templates.Notes != "n.tmpl" {
		t.Errorf("Templates.Notes = %q, want n.tmpl", cfg.Templates.Notes)
	}
	if len(cfg.Forge.Hosts) != 1 || cfg.Forge.Hosts[0].Host != "gh.corp" {
		t.Errorf("Hosts = %+v, want the gh.corp entry", cfg.Forge.Hosts)
	}
}

func TestPerKeyRejectionsFallBackWhileOthersSurvive(t *testing.T) {
	// Every invalid key is paired with a valid username so the test also
	// proves other keys survive.
	cases := []struct {
		name     string
		toml     string
		wantWarn string
		check    func(t *testing.T, cfg Config)
	}{
		{
			name:     "diff_view enum",
			toml:     "diff_view = \"split\"",
			wantWarn: "config `diff_view`: must be \"unified\" or \"side-by-side\"",
			check: func(t *testing.T, cfg Config) {
				if cfg.DiffView != "unified" {
					t.Errorf("DiffView = %q, want unified", cfg.DiffView)
				}
			},
		},
		{
			name:     "commit_order enum",
			toml:     "commit_order = \"sideways\"",
			wantWarn: "config `commit_order`: must be \"descending\" or \"ascending\"",
			check: func(t *testing.T, cfg Config) {
				if cfg.CommitOrder != "descending" {
					t.Errorf("CommitOrder = %q, want descending", cfg.CommitOrder)
				}
			},
		},
		{
			name:     "initial_commit_selection enum",
			toml:     "initial_commit_selection = \"newest\"",
			wantWarn: "config `initial_commit_selection`: must be \"all\" or \"oldest\"",
			check: func(t *testing.T, cfg Config) {
				if cfg.InitialCommitSelection != "all" {
					t.Errorf("InitialCommitSelection = %q, want all", cfg.InitialCommitSelection)
				}
			},
		},
		{
			name:     "appearance enum",
			toml:     "appearance = \"auto\"",
			wantWarn: "config `appearance`: must be \"dark\", \"light\", or \"system\"",
			check: func(t *testing.T, cfg Config) {
				if cfg.Appearance != "system" {
					t.Errorf("Appearance = %q, want system", cfg.Appearance)
				}
			},
		},
		{
			name:     "leader multi-char",
			toml:     "leader = \",,\"",
			wantWarn: "config `leader`: must be a single character",
			check: func(t *testing.T, cfg Config) {
				if cfg.Leader != ";" {
					t.Errorf("Leader = %q, want ;", cfg.Leader)
				}
			},
		},
		{
			name:     "leader empty",
			toml:     "leader = \"\"",
			wantWarn: "config `leader`: must be a single character",
			check: func(t *testing.T, cfg Config) {
				if cfg.Leader != ";" {
					t.Errorf("Leader = %q, want ;", cfg.Leader)
				}
			},
		},
		{
			name:     "comment_tab_width below minimum",
			toml:     "comment_tab_width = 0",
			wantWarn: "config `comment_tab_width`: must be an integer >= 1",
			check: func(t *testing.T, cfg Config) {
				if cfg.CommentTabWidth != 4 {
					t.Errorf("CommentTabWidth = %d, want 4", cfg.CommentTabWidth)
				}
			},
		},
		{
			name:     "scroll_offset negative",
			toml:     "scroll_offset = -1",
			wantWarn: "config `scroll_offset`: must be a non-negative integer",
			check: func(t *testing.T, cfg Config) {
				if cfg.ScrollOffset != 0 {
					t.Errorf("ScrollOffset = %d, want 0", cfg.ScrollOffset)
				}
			},
		},
		{
			name:     "review_watch_interval_ms negative",
			toml:     "review_watch_interval_ms = -5",
			wantWarn: "config `review_watch_interval_ms`: must be a non-negative integer",
			check: func(t *testing.T, cfg Config) {
				if cfg.ReviewWatchIntervalMS != 1000 {
					t.Errorf("ReviewWatchIntervalMS = %d, want 1000", cfg.ReviewWatchIntervalMS)
				}
			},
		},
		{
			name:     "wrap wrong type",
			toml:     "wrap = \"yes\"",
			wantWarn: "config `wrap`: must be true or false",
			check: func(t *testing.T, cfg Config) {
				if cfg.Wrap {
					t.Error("Wrap should stay false")
				}
			},
		},
		{
			name:     "theme wrong type",
			toml:     "theme = 123",
			wantWarn: "config `theme`: must be a string",
			check: func(t *testing.T, cfg Config) {
				if cfg.Theme != "" {
					t.Errorf("Theme = %q, want empty", cfg.Theme)
				}
			},
		},
		{
			name:     "comment_types wrong type",
			toml:     "comment_types = \"nope\"",
			wantWarn: "config `comment_types`: must be an array of tables",
			check: func(t *testing.T, cfg Config) {
				if cfg.CommentTypes != nil {
					t.Errorf("CommentTypes = %v, want nil", cfg.CommentTypes)
				}
			},
		},
		{
			name:     "forge not a table",
			toml:     "forge = true",
			wantWarn: "config `forge`: must be a table",
			check: func(t *testing.T, cfg Config) {
				if cfg.Forge.Default != "github" || !cfg.Forge.CommentTypePrefix {
					t.Errorf("Forge = %+v, want defaults", cfg.Forge)
				}
			},
		},
		{
			name:     "forge default enum",
			toml:     "[forge]\ndefault = \"bitbucket\"",
			wantWarn: "config `forge.default`: must be \"github\", \"gitlab\", \"azuredevops\", or \"forgejo\"",
			check: func(t *testing.T, cfg Config) {
				if cfg.Forge.Default != "github" {
					t.Errorf("Forge.Default = %q, want github", cfg.Forge.Default)
				}
			},
		},
		{
			name:     "templates value wrong type",
			toml:     "[templates]\nnotes = 5",
			wantWarn: "config `templates.notes`: must be a string",
			check: func(t *testing.T, cfg Config) {
				if cfg.Templates.Notes != "" {
					t.Errorf("Templates.Notes = %q, want empty", cfg.Templates.Notes)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, warnings := loadString(t, "username = \"me\"\n"+tc.toml+"\n")
			requireWarning(t, warnings, tc.wantWarn)
			if cfg.Username != "me" {
				t.Errorf("Username = %q, want me — other keys must survive", cfg.Username)
			}
			tc.check(t, cfg)
		})
	}
}

func TestMultipleInvalidKeysAllWarn(t *testing.T) {
	cfg, warnings := loadString(t, `
diff_view = "split"
commit_order = "sideways"
leader = ",,"
username = "me"
`)
	requireWarning(t, warnings, "`diff_view`")
	requireWarning(t, warnings, "`commit_order`")
	requireWarning(t, warnings, "`leader`")
	if cfg.Username != "me" {
		t.Errorf("Username = %q, want me", cfg.Username)
	}
	if cfg.DiffView != "unified" || cfg.CommitOrder != "descending" || cfg.Leader != ";" {
		t.Errorf("invalid keys should fall back, got %+v", cfg)
	}
}

func TestValidScalarValues(t *testing.T) {
	cfg, warnings := loadString(t, `
leader = ","
comment_tab_width = 8
scroll_offset = 4
review_watch_interval_ms = 0
diff_view = "side-by-side"
commit_order = "ascending"
initial_commit_selection = "oldest"
appearance = "dark"
`)
	if len(warnings) != 0 {
		t.Errorf("warnings = %q, want none", warnings)
	}
	if cfg.Leader != "," {
		t.Errorf("Leader = %q, want ,", cfg.Leader)
	}
	if cfg.CommentTabWidth != 8 {
		t.Errorf("CommentTabWidth = %d, want 8", cfg.CommentTabWidth)
	}
	if cfg.ScrollOffset != 4 {
		t.Errorf("ScrollOffset = %d, want 4", cfg.ScrollOffset)
	}
	if cfg.ReviewWatchIntervalMS != 0 {
		t.Errorf("ReviewWatchIntervalMS = %d, want 0 (disabled)", cfg.ReviewWatchIntervalMS)
	}
	if cfg.DiffView != "side-by-side" || cfg.CommitOrder != "ascending" ||
		cfg.InitialCommitSelection != "oldest" || cfg.Appearance != "dark" {
		t.Errorf("enums not applied: %+v", cfg)
	}
}

func TestBackendAlwaysWarns(t *testing.T) {
	for _, backend := range []string{"libgit2", "cli", "gitoxide"} {
		cfg, warnings := loadString(t, "backend = \""+backend+"\"\n")
		if cfg.Backend != backend {
			t.Errorf("Backend = %q, want %q", cfg.Backend, backend)
		}
		requireWarning(t, warnings, "mrman uses the git CLI")
	}
}

func TestCommentTypesValid(t *testing.T) {
	cfg, warnings := loadString(t, `comment_types = [
  { id = "note", label = "question", definition = "ask for clarification", color = "yellow" },
  { id = "issue" },
  { id = "nit", color = "#d19a66" },
]`)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %q, want none", warnings)
	}
	want := []CommentTypeConfig{
		{ID: "note", Label: "question", Definition: "ask for clarification", Color: "yellow"},
		{ID: "issue", Label: "ISSUE"},
		{ID: "nit", Label: "NIT", Color: "#d19a66"},
	}
	if len(cfg.CommentTypes) != len(want) {
		t.Fatalf("CommentTypes = %+v, want %+v", cfg.CommentTypes, want)
	}
	for i := range want {
		if cfg.CommentTypes[i] != want[i] {
			t.Errorf("CommentTypes[%d] = %+v, want %+v", i, cfg.CommentTypes[i], want[i])
		}
	}
}

func TestCommentTypesInvalidEntries(t *testing.T) {
	cfg, warnings := loadString(t, `comment_types = [
  { id = "" },
  { id = "note" },
  { id = "NOTE" },
  42,
]`)
	if len(cfg.CommentTypes) != 1 || cfg.CommentTypes[0].ID != "note" {
		t.Fatalf("CommentTypes = %+v, want only the note entry", cfg.CommentTypes)
	}
	requireWarning(t, warnings, "config `comment_types[0].id`: cannot be empty")
	requireWarning(t, warnings, "config `comment_types`: duplicate id `note`")
	requireWarning(t, warnings, "config `comment_types[3]`: must be a table")
	if len(warnings) != 3 {
		t.Errorf("warnings = %q, want exactly three", warnings)
	}
}

func TestCommentTypesIDTrimmedAndLowercased(t *testing.T) {
	cfg, warnings := loadString(t, `comment_types = [{ id = "  NitPick " }]`)
	if len(warnings) != 0 {
		t.Errorf("warnings = %q, want none", warnings)
	}
	if len(cfg.CommentTypes) != 1 || cfg.CommentTypes[0].ID != "nitpick" || cfg.CommentTypes[0].Label != "NITPICK" {
		t.Errorf("CommentTypes = %+v, want trimmed lowercased id with uppercased label", cfg.CommentTypes)
	}
}

func TestCommentTypesInvalidColorDropsColorOnly(t *testing.T) {
	cfg, warnings := loadString(t, `comment_types = [{ id = "note", color = "not-a-color" }]`)
	if len(cfg.CommentTypes) != 1 || cfg.CommentTypes[0].ID != "note" || cfg.CommentTypes[0].Color != "" {
		t.Errorf("CommentTypes = %+v, want note entry without color", cfg.CommentTypes)
	}
	requireWarning(t, warnings, "config `comment_types[0].color`: must be a named terminal color or #RRGGBB")
}

func TestCommentTypesFieldTypeAndEmptyWarnings(t *testing.T) {
	cfg, warnings := loadString(t, `comment_types = [
  { id = "note", label = 5, definition = " " },
  { id = 7 },
]`)
	requireWarning(t, warnings, "config `comment_types[0].label`: must be a string")
	requireWarning(t, warnings, "config `comment_types[0].definition`: cannot be empty")
	requireWarning(t, warnings, "config `comment_types[1].id`: must be a string")
	if len(cfg.CommentTypes) != 1 || cfg.CommentTypes[0].Label != "NOTE" || cfg.CommentTypes[0].Definition != "" {
		t.Errorf("CommentTypes = %+v, want note with default label", cfg.CommentTypes)
	}
}

func TestCommentTypesAllInvalid(t *testing.T) {
	cfg, warnings := loadString(t, `comment_types = [{ id = "" }]`)
	if cfg.CommentTypes == nil || len(cfg.CommentTypes) != 0 {
		t.Errorf("CommentTypes = %v, want empty non-nil slice", cfg.CommentTypes)
	}
	requireWarning(t, warnings, "config `comment_types`: no valid entries")
}

func TestCommentTypesUnknownEntryKey(t *testing.T) {
	cfg, warnings := loadString(t, `comment_types = [{ id = "note", severity = 3 }]`)
	requireWarning(t, warnings, "unknown config key `comment_types[0].severity` — ignored")
	if len(cfg.CommentTypes) != 1 || cfg.CommentTypes[0].ID != "note" {
		t.Errorf("CommentTypes = %+v, want note entry", cfg.CommentTypes)
	}
}

func TestCommentTypeColors(t *testing.T) {
	valid := []string{"yellow", "light_red", "LIGHTBLUE", "dark_grey", "#AbCdEf", " cyan "}
	for _, c := range valid {
		if !isSupportedColor(c) {
			t.Errorf("isSupportedColor(%q) = false, want true", c)
		}
	}
	invalid := []string{"", "chartreuse", "#12345", "#1234567", "#12345g", "12ab34"}
	for _, c := range invalid {
		if isSupportedColor(c) {
			t.Errorf("isSupportedColor(%q) = true, want false", c)
		}
	}
}

func TestForgeSectionFull(t *testing.T) {
	cfg, warnings := loadString(t, `
[forge]
default = "gitlab"
comment_type_prefix = false
cli_token_fallback = false

[[forge.hosts]]
host = "gh.corp.example"
forge = "github"
api_base = "https://gh.corp.example/api/v3"
token = "$CORP_GH_TOKEN"
ca_file = "/etc/ssl/corp.pem"

[[forge.hosts]]
host = "git.internal"
forge = "forgejo"
token_cmd = "pass show git.internal"
insecure_skip_verify = true
`)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %q, want none", warnings)
	}
	f := cfg.Forge
	if f.Default != "gitlab" || f.CommentTypePrefix || f.CLITokenFallback {
		t.Errorf("Forge = %+v, want gitlab/false/false", f)
	}
	if len(f.Hosts) != 2 {
		t.Fatalf("Hosts = %+v, want two entries", f.Hosts)
	}
	want0 := ForgeHost{
		Host:    "gh.corp.example",
		Forge:   "github",
		APIBase: "https://gh.corp.example/api/v3",
		Token:   "$CORP_GH_TOKEN",
		CAFile:  "/etc/ssl/corp.pem",
	}
	if f.Hosts[0] != want0 {
		t.Errorf("Hosts[0] = %+v, want %+v", f.Hosts[0], want0)
	}
	want1 := ForgeHost{
		Host:               "git.internal",
		Forge:              "forgejo",
		TokenCmd:           "pass show git.internal",
		InsecureSkipVerify: true,
	}
	if f.Hosts[1] != want1 {
		t.Errorf("Hosts[1] = %+v, want %+v", f.Hosts[1], want1)
	}
}

func TestForgeHostTokenExclusivity(t *testing.T) {
	cfg, warnings := loadString(t, `
[[forge.hosts]]
host = "bad.example"
token = "a"
token_cmd = "b"

[[forge.hosts]]
host = "good.example"
token = "c"
`)
	requireWarning(t, warnings, "config `forge.hosts[0]`: at most one of `token` and `token_cmd` may be set — entry ignored")
	if len(cfg.Forge.Hosts) != 1 || cfg.Forge.Hosts[0].Host != "good.example" {
		t.Errorf("Hosts = %+v, want only good.example", cfg.Forge.Hosts)
	}
}

func TestForgeHostMissingHost(t *testing.T) {
	cfg, warnings := loadString(t, "[[forge.hosts]]\ntoken = \"a\"\n")
	requireWarning(t, warnings, "config `forge.hosts[0]`: `host` is required and must be a non-empty string")
	if len(cfg.Forge.Hosts) != 0 {
		t.Errorf("Hosts = %+v, want none", cfg.Forge.Hosts)
	}
}

func TestForgeHostEmptyHost(t *testing.T) {
	_, warnings := loadString(t, "[[forge.hosts]]\nhost = \"\"\n")
	requireWarning(t, warnings, "config `forge.hosts[0]`: `host` is required and must be a non-empty string")
}

func TestForgeHostBadForgeKind(t *testing.T) {
	cfg, warnings := loadString(t, `
[[forge.hosts]]
host = "bad.example"
forge = "bitbucket"

[[forge.hosts]]
host = "good.example"
forge = "azuredevops"
`)
	requireWarning(t, warnings, "config `forge.hosts[0]`: `forge` must be \"github\", \"gitlab\", \"azuredevops\", or \"forgejo\" — entry ignored")
	if len(cfg.Forge.Hosts) != 1 || cfg.Forge.Hosts[0].Forge != "azuredevops" {
		t.Errorf("Hosts = %+v, want only the azuredevops entry", cfg.Forge.Hosts)
	}
}

func TestForgeHostBadAPIBase(t *testing.T) {
	cfg, warnings := loadString(t, "[[forge.hosts]]\nhost = \"x\"\napi_base = \"not a url\"\n")
	requireWarning(t, warnings, "config `forge.hosts[0]`: `api_base` must be an http(s) URL — entry ignored")
	if len(cfg.Forge.Hosts) != 0 {
		t.Errorf("Hosts = %+v, want none", cfg.Forge.Hosts)
	}
}

func TestForgeHostNotATable(t *testing.T) {
	cfg, warnings := loadString(t, "[forge]\nhosts = [true]\n")
	requireWarning(t, warnings, "config `forge.hosts[0]`: must be a table — entry ignored")
	if len(cfg.Forge.Hosts) != 0 {
		t.Errorf("Hosts = %+v, want none", cfg.Forge.Hosts)
	}
}

func TestForgeHostsWrongType(t *testing.T) {
	cfg, warnings := loadString(t, "[forge]\nhosts = \"nope\"\n")
	requireWarning(t, warnings, "config `forge.hosts`: must be an array of tables")
	if len(cfg.Forge.Hosts) != 0 {
		t.Errorf("Hosts = %+v, want none", cfg.Forge.Hosts)
	}
}

func TestForgeFieldWrongTypeKeepsRestOfSection(t *testing.T) {
	cfg, warnings := loadString(t, "[forge]\ncomment_type_prefix = \"yes\"\ndefault = \"forgejo\"\n")
	requireWarning(t, warnings, "config `forge.comment_type_prefix`: must be true or false")
	if !cfg.Forge.CommentTypePrefix {
		t.Error("CommentTypePrefix should fall back to true")
	}
	if cfg.Forge.Default != "forgejo" {
		t.Errorf("Forge.Default = %q, want forgejo — siblings must survive", cfg.Forge.Default)
	}
}

func TestTemplatesSection(t *testing.T) {
	cfg, warnings := loadString(t, `
[templates]
notes = "~/.config/mrman/templates/notes.md.tmpl"
review_body = "/abs/review.md.tmpl"
`)
	if len(warnings) != 0 {
		t.Errorf("warnings = %q, want none", warnings)
	}
	if cfg.Templates.Notes != "~/.config/mrman/templates/notes.md.tmpl" {
		t.Errorf("Templates.Notes = %q", cfg.Templates.Notes)
	}
	if cfg.Templates.ReviewBody != "/abs/review.md.tmpl" {
		t.Errorf("Templates.ReviewBody = %q", cfg.Templates.ReviewBody)
	}
}

func TestFullyPopulatedConfigRoundTrips(t *testing.T) {
	cfg, warnings := loadString(t, `
theme = "tokyo-night-storm"
theme_dark = "gruvbox-dark"
theme_light = "gruvbox-light"
appearance = "light"
backend = "cli"
show_file_list = false
show_commits = false
diff_view = "side-by-side"
commit_order = "ascending"
initial_commit_selection = "oldest"
ignore_whitespace = true
wrap = true
export_legend = false
cursor_line = false
mouse = false
comment_vim = true
comment_tab_width = 2
leader = ","
transparent_background = false
scroll_offset = 5
review_watch_interval_ms = 250
single_file_view = true
username = "reviewer"

comment_types = [
  { id = "note", label = "question", definition = "ask", color = "yellow" },
]

[templates]
notes = "notes.tmpl"
review_body = "body.tmpl"

[forge]
default = "forgejo"
comment_type_prefix = false
cli_token_fallback = false

[[forge.hosts]]
host = "codeberg.org"
forge = "forgejo"
token = "$CODEBERG_TOKEN"
`)
	if len(warnings) != 1 || !hasWarning(warnings, "git CLI") {
		t.Fatalf("warnings = %q, want only the backend warning", warnings)
	}
	want := Config{
		Theme:                  "tokyo-night-storm",
		ThemeDark:              "gruvbox-dark",
		ThemeLight:             "gruvbox-light",
		Appearance:             "light",
		Backend:                "cli",
		CommentTypes:           []CommentTypeConfig{{ID: "note", Label: "question", Definition: "ask", Color: "yellow"}},
		ShowFileList:           false,
		ShowCommits:            false,
		DiffView:               "side-by-side",
		CommitOrder:            "ascending",
		InitialCommitSelection: "oldest",
		IgnoreWhitespace:       true,
		Wrap:                   true,
		ExportLegend:           false,
		CursorLine:             false,
		Mouse:                  false,
		CommentVim:             true,
		CommentTabWidth:        2,
		Leader:                 ",",
		TransparentBackground:  false,
		ScrollOffset:           5,
		ReviewWatchIntervalMS:  250,
		SingleFileView:         true,
		Username:               "reviewer",
		Templates:              TemplatesConfig{Notes: "notes.tmpl", ReviewBody: "body.tmpl"},
		Forge: ForgeConfig{
			Default:           "forgejo",
			CommentTypePrefix: false,
			CLITokenFallback:  false,
			Hosts: []ForgeHost{{
				Host:  "codeberg.org",
				Forge: "forgejo",
				Token: "$CODEBERG_TOKEN",
			}},
		},
	}
	if len(cfg.CommentTypes) != 1 || cfg.CommentTypes[0] != want.CommentTypes[0] {
		t.Errorf("CommentTypes = %+v, want %+v", cfg.CommentTypes, want.CommentTypes)
	}
	if len(cfg.Forge.Hosts) != 1 || cfg.Forge.Hosts[0] != want.Forge.Hosts[0] {
		t.Errorf("Forge.Hosts = %+v, want %+v", cfg.Forge.Hosts, want.Forge.Hosts)
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("cfg = %+v\nwant %+v", cfg, want)
	}
}

func TestDirAndLoad(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	xdg.Reload()
	t.Cleanup(xdg.Reload)

	if got, want := Dir(), filepath.Join(base, "mrman"); got != want {
		t.Fatalf("Dir() = %q, want %q", got, want)
	}
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(Dir(), "config.toml"), []byte("username = \"xdg-user\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, warnings := Load()
	if len(warnings) != 0 {
		t.Errorf("warnings = %q, want none", warnings)
	}
	if cfg.Username != "xdg-user" {
		t.Errorf("Username = %q, want xdg-user", cfg.Username)
	}
}
