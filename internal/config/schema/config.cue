// Schema for mrman's config.toml. The Go loader unifies the decoded TOML
// data with #Config and converts every violation into a startup warning; it
// never fails hard.
package schema

// #ForgeKind enumerates the supported forge drivers.
#ForgeKind: "github" | "gitlab" | "azuredevops" | "forgejo"

// #ForgeHostBase holds the fields shared by both #ForgeHost variants.
#ForgeHostBase: {
	// host is the hostname the entry applies to (required, non-empty).
	host!:                 string & !=""
	forge?:                #ForgeKind
	api_base?:             string & =~"^https?://[^\\s]+$"
	ca_file?:              string
	insecure_skip_verify?: bool
}

// #ForgeHost allows at most one of token / token_cmd: each closed variant
// admits only one of the two fields, so an entry carrying both fails both
// branches. The first branch is the default so that entries with neither
// field still resolve to a concrete value.
#ForgeHost: *{#ForgeHostBase, token?: string} | {#ForgeHostBase, token_cmd?: string}

// #Forge is the [forge] section.
#Forge: {
	default?:             #ForgeKind
	comment_type_prefix?: bool
	cli_token_fallback?:  bool
	hosts?: [...#ForgeHost]
}

// #Templates is the [templates] section: paths to markdown template files.
#Templates: {
	notes?:       string
	review_body?: string
}

// #Config is the top-level config.toml schema. comment_types entries are
// validated in Go (per-entry drop semantics); here only the array shape is
// enforced.
#Config: {
	theme?:       string
	theme_dark?:  string
	theme_light?: string
	appearance?:  "dark" | "light" | "system"
	backend?:     string
	comment_types?: [..._]
	show_file_list?:           bool
	show_commits?:             bool
	diff_view?:                "unified" | "side-by-side"
	commit_order?:             "descending" | "ascending"
	initial_commit_selection?: "all" | "oldest"
	ignore_whitespace?:        bool
	wrap?:                     bool
	export_legend?:            bool
	export_diff?:              bool
	cursor_line?:              bool
	mouse?:                    bool
	comment_vim?:              bool
	comment_tab_width?:        int & >=1
	leader?:                   string & =~"^.$"
	transparent_background?:   bool
	scroll_offset?:            int & >=0
	review_watch_interval_ms?: int & >=0
	single_file_view?:         bool
	username?:                 string
	show_own_author?:          bool
	templates?:                #Templates
	forge?:                    #Forge
}
