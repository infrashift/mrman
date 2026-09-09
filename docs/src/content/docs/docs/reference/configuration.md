---
title: Configuration
description: Every option in ~/.config/mrman/config.toml — display, comment types, forge hosts, authentication, themes and templates.
---

mrman reads `$XDG_CONFIG_HOME/mrman/config.toml`, which is
`~/.config/mrman/config.toml` unless you have moved it. Themes live beside
it in `themes/`, and sessions in `~/.local/share/mrman/reviews/`.

Configuration is validated against embedded CUE schemas. A mistake never
stops mrman starting: the offending key is dropped, the rest is kept, and
you get a precise warning in the status bar.

## Options

| Key | Default | Meaning |
|---|---|---|
| `theme` | — | A bundled theme name, or a file in `themes/` |
| `theme_dark` / `theme_light` | — | Paired variants selected by appearance |
| `appearance` | `"system"` | `dark`, `light` or `system` |
| `diff_view` | `"unified"` | `unified` or `side-by-side` |
| `commit_order` | `"descending"` | Commit selector order; `ascending` puts oldest first |
| `initial_commit_selection` | `"all"` | Or `oldest`, to walk forward with `(` / `)` |
| `ignore_whitespace` | `false` | Ignore all whitespace in **local** diffs |
| `show_file_list` | `true` | File tree visible at startup |
| `show_commits` | `true` | Commit selector visible at startup |
| `single_file_view` | `false` | Render one file at a time |
| `wrap` | `false` | Wrap long diff lines |
| `cursor_line` | `true` | Highlight the cursor line and visual selection |
| `transparent_background` | `true` | Let the terminal background show through |
| `scroll_offset` | `0` | Vim's `scrolloff`: lines kept above and below the cursor |
| `mouse` | `true` | Wheel, click and drag-select |
| `leader` | `";"` | Leader key; must be a single character |
| `comment_vim` | `false` | Modal editing in the comment box |
| `comment_tab_width` | `4` | Spaces `Tab` inserts in vim Insert mode |
| `export_legend` | `true` | Include the comment-type legend in exported markdown |
| `username` | — | Author stamped on your comments; also distinguishes yours from an agent's |
| `review_watch_interval_ms` | `1000` | Poll interval for external session changes; `0` disables |
| `backend` | — | Accepted for tuicr compatibility and ignored: mrman uses the git CLI by design |

`ignore_whitespace` applies to local git and Jujutsu diffs only. A pull
request's diff arrives from the forge already rendered, so there is no flag
to re-run it with.

## Comment types

Types drive the badge in the diff, the `[TYPE]` tag in exported markdown and
on submitted comments, and the `Tab` cycle order.

```toml
[[comment_types]]
id = "issue"                          # stable; stored in sessions
label = "ISSUE"                       # shown in the UI; defaults to id uppercased
definition = "must fix before merge"  # guidance text, included in the legend
color = "red"                         # a terminal color name or #RRGGBB
```

### The built-in types

With nothing declared you get four, in this cycle order:

| Type | Means |
|---|---|
| `note` | worth knowing; answer or acknowledge |
| `issue` | must fix before merge |
| `suggestion` | optional improvement; implement it or say why not |
| `praise` | positive feedback; nothing to do |
| *(untyped)* | no badge, no tag, no legend entry |

`note` leads, so it is the type a new comment starts on — an unclassified
remark should read as a remark, not as a blocker. `Tab` walks the list and ends
on the untyped entry, so a comment can still be left unclassified.

These four ids are also the ones themes color, through the `comment_note`,
`comment_issue`, `comment_suggestion` and `comment_praise`
[slots](../themes/#the-41-slots), so they follow your theme rather than a
pinned color.

### Declaring your own

Declaring any types **replaces the built-ins entirely** — you get exactly what
you list, not your types added to theirs. The first entry becomes the default,
and the comment box shows the current type in its title (`Add L60 comment
[ISSUE]`). An untyped option is still appended at the end of the cycle, unless
you declare a `none` entry yourself.

`definition` is what appears in the exported legend, which lists only the types
a review actually used.

## Forges

```toml
[forge]
default = "github"          # for ambiguous targets
comment_type_prefix = true  # prepend [TYPE] on submitted comments
cli_token_fallback = true   # allow `gh auth token`

[[forge.hosts]]
host = "gitlab.mycorp.com"
forge = "gitlab"
token_cmd = "pass show gitlab-token"

[[forge.hosts]]
host = "ghe.mycorp.com"
forge = "github"
api_base = "https://ghe.mycorp.com/api/v3"
token = "$GHE_TOKEN"        # $VAR is expanded
ca_file = "/etc/ssl/corp.pem"
```

A host entry may set `token` or `token_cmd`, not both — the schema rejects
it rather than leaving you guessing which one won.

Tokens resolve per host in this order: a SaaS-scoped environment variable
(`GITHUB_TOKEN`, `GITLAB_TOKEN`, `AZURE_DEVOPS_EXT_PAT`,
`FORGEJO_TOKEN` / `CODEBERG_TOKEN`; `GH_ENTERPRISE_TOKEN` for other GitHub
hosts), then `token`, then `token_cmd`, then `gh auth token` for GitHub.

Only a **trusted host** receives any of them: the four SaaS hosts, or a host
with its own `[[forge.hosts]]` entry. Every other host is connected to
without credentials and a warning names the entry to add — mrman can guess
which forge a host runs from a URL, but a guess is not grounds to send it a
token. `insecure_skip_verify` exists per host and should stay a last resort;
mrman warns at every start while it is on.

Requests to a host stay on that host: a redirect to another origin — or from
https down to http — is refused rather than followed with the token attached.
`api_base` may be plain `http://` for a fixture server, and mrman warns at
start that tokens for that host travel unencrypted. Every request carries a
90-second timeout so a stalled forge cannot hang the review.

### What each forge can do

Capabilities are checked before mrman offers an action, so nothing fails
halfway through a submit:

| | GitHub | GitLab | Forgejo | Azure DevOps |
|---|:--:|:--:|:--:|:--:|
| Draft reviews | ✓ | ✓ | ✓ | — |
| Approve / request changes | ✓ | ✓ | ✓ | ✓ (vote) |
| Review summaries | ✓ | — | ✓ | — |
| Thread resolution shown | ✓ | ✓ | approx. | ✓ |
| Multi-line comments | ✓ | ✓ | downgraded | ✓ |
| Per-commit range diff | ✓ | ✓ | local only | — |
| Atomic submit | ✓ | — | ✓ | — |

Where a forge cannot do something, mrman degrades and says so: multi-line
comments collapse to a single line with the span in the body, and a forge
without range diffs widens back to the whole merge request instead of
failing.

## Themes

Bundled: `dark`, `light`, `tokyo-night-storm`, `tokyo-night-day`,
`catppuccin-latte` / `-frappe` / `-macchiato` / `-mocha`, `gruvbox-dark` /
`-light`, `nord-dark` / `-light` / `-dark-high-contrast` /
`-light-high-contrast`, `solarized-dark` / `-light`, `everforest-dark` /
`-light`, `ayu-light`, `ayu-mirage`, `onedark`, `github-light`,
`github-dark`.

A local theme is a flat TOML file at `themes/<name>.toml` with all 41 color
slots, each `#RRGGBB` or a named terminal color. Optionally set
`syntax_style` to any chroma style name, or `syntax_style_file` to a chroma
XML file resolved relative to the theme. Bundled names win over local files
of the same name.

Resolution order: `--theme` → `theme` → `theme_dark`/`theme_light` chosen by
appearance → whichever of the two is set → `--appearance` → `appearance` →
the bundled default. An invalid `--theme` exits; an invalid configured theme
warns and falls through.

## Templates

```toml
[templates]
notes = "~/.config/mrman/templates/notes.md.tmpl"
review_body = "~/.config/mrman/templates/review_body.md.tmpl"
patch_reply = "~/.config/mrman/templates/patch_reply.txt.tmpl"
```

`notes` renders `y` / `:clip` output; `review_body` renders the body posted
with `:submit`. Both are Go `text/template` with embedded defaults, and a
template that fails to parse falls back to its default with a warning rather
than losing your review.

## Agent submission

There is no config key for this, deliberately. Authorizing an agent to
submit a review is done per-session, at launch:

```sh
mrman pr 1 --auto                        # comment and draft
mrman pr 1 --auto=comment,draft,approve  # explicitly wider
```

A persistent switch in this file was considered and rejected: it would be
too easy to enable once and forget, and it would apply to every repository
and every merge request until noticed. The grant is instead held against the
running TUI's process, so it lasts exactly as long as you have the review
open. `:agent` shows it, `:agent off` revokes it.

`--auto` is refused alongside `--json`, and refused without a terminal, so
that a command an agent runs can never create one.

## Ignoring files

`.gitignore` is honored automatically. A `.mrmanignore` at the repository
root layers on top of it with the same syntax, `!` negation included, and
excludes matching files from every review diff.
