// Package config loads mrman's user configuration from TOML with CUE-backed
// validation. The pipeline is deliberately forgiving (tuicr parity): invalid
// or unknown settings never abort startup — every problem is reported as a
// warning and the offending key falls back to its built-in default.
package config

import (
	"path/filepath"

	"github.com/adrg/xdg"
)

// DefaultLeader is the leader key used when the config does not override it.
const DefaultLeader = ";"

// CommentTypeConfig describes one user-defined comment classification.
type CommentTypeConfig struct {
	// ID is the stable lowercase identifier persisted in sessions.
	ID string
	// Label is the visible tag shown in the UI and exports; it defaults to
	// the uppercased ID when the config omits it.
	Label string
	// Definition is optional guidance text included in export legends.
	Definition string
	// Color is a named terminal color or "#RRGGBB" value; empty means the
	// theme default.
	Color string
}

// TemplatesConfig points at user-supplied markdown template files that
// override the embedded defaults.
type TemplatesConfig struct {
	// Notes is the path to the notes-export template file.
	Notes string
	// ReviewBody is the path to the review/PR-body template file.
	ReviewBody string
	// PatchReply is the path to the :patch reply template file.
	PatchReply string
}

// ForgeHost configures forge routing, authentication, and TLS for one host,
// mirroring a [[forge.hosts]] TOML entry.
type ForgeHost struct {
	// Host is the hostname the entry applies to (required).
	Host string
	// Forge names the driver for the host: "github", "gitlab",
	// "azuredevops", or "forgejo".
	Forge string
	// APIBase overrides the API base URL for on-premise installations.
	APIBase string
	// Token is a personal access token, or "$VAR" to read an env var.
	Token string
	// TokenCmd is a shell command whose stdout supplies the token.
	// Mutually exclusive with Token.
	TokenCmd string
	// CAFile is a path to an extra PEM CA bundle for this host.
	CAFile string
	// InsecureSkipVerify disables TLS certificate verification.
	InsecureSkipVerify bool
}

// ForgeConfig holds the [forge] section.
type ForgeConfig struct {
	// Default is the forge assumed for hosts that cannot be identified:
	// "github", "gitlab", "azuredevops", or "forgejo".
	Default string
	// CommentTypePrefix prepends "[TYPE] " to comment bodies on submit.
	CommentTypePrefix bool
	// CLITokenFallback allows borrowing `gh auth token` for GitHub hosts
	// when no other credential source matches.
	CLITokenFallback bool
	// Hosts lists per-host [[forge.hosts]] overrides.
	Hosts []ForgeHost
}

// Config is mrman's fully-resolved user configuration. Every field carries a
// usable value after Load; warnings describe anything that fell back to a
// default.
type Config struct {
	// Theme is an explicit theme name (bundled or local).
	Theme string
	// ThemeDark is the theme used for dark appearance.
	ThemeDark string
	// ThemeLight is the theme used for light appearance.
	ThemeLight string
	// Appearance is "dark", "light", or "system".
	Appearance string
	// Backend is parsed for tuicr compatibility only; any value produces a
	// warning because mrman always uses the git CLI.
	Backend string
	// CommentTypes fully replaces the configured comment classifications.
	CommentTypes []CommentTypeConfig
	// ShowFileList controls file list panel visibility on startup.
	ShowFileList bool
	// ShowCommits controls the inline commit selector visibility on startup.
	ShowCommits bool
	// DiffView is "unified" or "side-by-side".
	DiffView string
	// CommitOrder is "descending" (newest first) or "ascending".
	CommitOrder string
	// InitialCommitSelection is "all" or "oldest".
	InitialCommitSelection string
	// IgnoreWhitespace ignores all whitespace in local diffs.
	IgnoreWhitespace bool
	// Wrap enables line wrap in the diff view.
	Wrap bool
	// ExportLegend includes the comment-type legend in markdown exports.
	ExportLegend bool
	// CursorLine highlights the current cursor line.
	CursorLine bool
	// Mouse enables wheel scrolling, clicks, and drag-to-select.
	Mouse bool
	// CommentVim enables vim-style modal editing in the comment box.
	CommentVim bool
	// CommentTabWidth is the number of spaces Tab inserts in the vim
	// comment box.
	CommentTabWidth int
	// Leader is the single-character shortcut prefix.
	Leader string
	// TransparentBackground lets the terminal background show through.
	TransparentBackground bool
	// ScrollOffset is the minimum number of lines kept visible above and
	// below the cursor while scrolling.
	ScrollOffset int
	// ReviewWatchIntervalMS is the poll interval for persisted
	// review-session changes; 0 disables watching.
	ReviewWatchIntervalMS int
	// SingleFileView renders single-file and pristine views full-width by
	// default.
	SingleFileView bool
	// Username is the display name stamped on locally authored comments.
	Username string
	// Templates holds paths to user markdown template overrides.
	Templates TemplatesConfig
	// Forge holds the [forge] section.
	Forge ForgeConfig
}

// Default returns the built-in configuration used when the config file is
// missing, and the base every loaded config is decoded onto.
func Default() Config {
	return Config{
		Appearance:             "system",
		ShowFileList:           true,
		ShowCommits:            true,
		DiffView:               "unified",
		CommitOrder:            "descending",
		InitialCommitSelection: "all",
		ExportLegend:           true,
		CursorLine:             true,
		Mouse:                  true,
		CommentTabWidth:        4,
		Leader:                 DefaultLeader,
		TransparentBackground:  true,
		ReviewWatchIntervalMS:  1000,
		Forge: ForgeConfig{
			Default:           "github",
			CommentTypePrefix: true,
			CLITokenFallback:  true,
		},
	}
}

// Dir returns the mrman configuration directory
// ($XDG_CONFIG_HOME/mrman, typically ~/.config/mrman).
func Dir() string {
	return filepath.Join(xdg.ConfigHome, "mrman")
}

// Load reads the configuration from Dir()/config.toml. A missing file yields
// Default() with no warnings; any other problem yields warnings, never an
// error.
func Load() (Config, []string) {
	return LoadFrom(filepath.Join(Dir(), "config.toml"))
}
