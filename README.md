# mrman

**mrman** is a terminal code-review tool: browse a GitHub-style continuous
diff with vim keybindings, leave typed comments at line / range / file /
review scope, track what you've reviewed across sessions, collaborate live
with AI agents through a JSON CLI, and submit the finished review to
**GitHub, GitLab, Azure DevOps, or Forgejo** — SaaS or on-premise. The
Forgejo/Gitea driver (Codeberg included) is **experimental**: implemented and
unit-tested, but not yet exercised against a live instance the way the GitHub
and GitLab ones have been.

mrman is a Go reimplementation of [tuicr](https://github.com/agavra/tuicr)
(Rust, MIT), built on [Bubble Tea](https://github.com/charmbracelet/bubbletea),
with feature parity plus a four-forge integration layer, CUE-validated
configuration, and user-templatable markdown output. tuicr is the project this
one is a port of, and much of what mrman is comes from there — that and
everything else mrman stands on is credited in
[Acknowledgements](https://infrashift.github.io/mrman/docs/project/acknowledgements/).

**Documentation: <https://infrashift.github.io/mrman/>**

## Install

```sh
go install github.com/infrashift/mrman@latest
# or from a checkout:
make install
```

mrman has no release tag yet, so `@latest` installs the newest commit on
`main`.

Requires `git` on PATH (and `jj` for Jujutsu repos). Best experienced in a
terminal with full kitty-keyboard support such as **Ghostty**.

## Usage

```sh
mrman                      # open the review-target selector
mrman -w                   # review working-tree changes
mrman -r main..HEAD        # review a commit range / revset
mrman -p src/              # limit the diff to a path prefix
mrman --file notes.md      # review any file, no VCS needed
mrman -A                   # pristine mode: annotate every tracked file
mrman --patch series.mbox  # review a .patch, .diff or mbox file, no repo needed
mrman diff old new         # review the difference between two paths, no repo needed
mrman pr 125               # review a pull request (repo from your checkout)
mrman pr owner/repo#125    # ... or addressed explicitly
mrman pr <PR/MR URL>       # ... or by URL (any supported forge)
```

With no arguments mrman opens the target selector: pick a commit range or
staged/unstaged changes on the **Local** tab, or `Tab` over to **Pull
Requests** to browse what is open on the forge — `/` filters, `r` switches
between everything open and what is waiting on your review.

### Keys (excerpt — press `?` in the app for everything)

| Key | Action |
|---|---|
| `j/k`, `Ctrl-d/u`, `g/G`, `zz/zt/zb` | vim navigation |
| `}` `{` / `]` `[` | next/previous file / hunk |
| `c` / `C` / `v` | comment on line / file / visual range |
| `r` / `R` | toggle file / hunk reviewed |
| `m` / `M`, `dd`, `i` | next/prev comment, delete, edit |
| `Enter`/`Space`, `o`/`O` | expand context gap, expand/collapse dirs |
| `(` / `)` | walk commit by commit through a multi-commit review |
| `Tab`, `;e`, `;s`, `;f` | focus panes, toggle file list / commit selector / single-file view |
| mouse | wheel scrolls, click jumps, drag selects (`mouse = false` to disable) |
| `y` / `:clip` | export review markdown to the clipboard |
| `:submit` | submit to the forge (picker: comment/approve/request-changes/draft) |
| `:e` / `:edit` | reload the diff / open the focused file in `$EDITOR` |
| `:comments unresolved\|all\|hide` | show the forge's existing comments |
| `:q` `:w` `:wq` `ZZ` | quit / save session |

Full list: [Keybindings](https://infrashift.github.io/mrman/docs/reference/keybindings/),
or press `?`.

### Reviewing a pull request

The forge's existing review threads and summaries render inline in the diff,
read-only — mrman never replies, resolves or rewrites them. Hidden context
expands on demand from the forge, `:e` refetches (a pull request that
advanced to a new head opens the review for that head rather than
re-anchoring your comments), and on a multi-commit pull request mrman marks
the commits your last review already covered and preselects what landed
since.

### Reviewing patches

`--patch` reviews a `.patch`, `.diff` or mbox file with no repository behind
it — for airgapped work, and for mailing-list projects where the unit of
review is a posted series. A series presents as a commit strip, so `(` / `)`
walk it patch by patch and each patch's changelog is itself reviewable.
`:patch` replies with the diff quoted and your comments interleaved, tabs
intact, threaded when the artifact carried a `Message-Id`. See
[Reviewing Patches](https://infrashift.github.io/mrman/docs/guides/patches/)
and [Patch Workflows](https://infrashift.github.io/mrman/docs/guides/patch-workflows/).

### Sharing a review

A pull request review goes to the forge with `:submit`. A local review —
working tree or commit range — has no forge to post to, so `y` / `:clip`
exports it as markdown addressed to a reader, numbered and located
(`src/cache.go:75`), and copies it to your clipboard; over SSH, tmux or
Zellij that falls back to OSC 52 so it lands in *your* clipboard, not the
remote host's. See
[Sharing a Review](https://infrashift.github.io/mrman/docs/guides/sharing/).

### Sessions and agent collaboration

Reviews persist automatically (`~/.local/share/mrman/reviews`). A working-tree
review survives an amend or rebase: it is carried onto the new HEAD, and every
comment's anchor is re-checked against the new diff — one whose line moved
follows it, one whose code is gone is flagged `(outdated)` and refused an
inline position at submit rather than posted against whatever now occupies its
line number.

On start mrman prints `mrman-session: <slug>` to stderr; agents can then read
and write the same review while you have it open — changes merge live:

```sh
mrman review list --repo .
mrman review add --session <slug> --target-file src/x.go --line 42 \
    --type issue --username "Claude" "This branch leaks the file handle."
mrman review comments --session <slug>
```

```sh
mrman pr 1 --json                       # open a PR session headlessly, no TUI
mrman review watch --session <slug>     # stream changes until submitted/closed
mrman review submit --session <slug> --event comment   # requires a grant
```

Submitting is the one agent command that writes to a forge, and it is gated:
it works only while a human has the review open with `mrman pr <target>
--auto`, which cannot be passed alongside `--json` and requires a terminal —
so a command an agent runs can never authorize one. The grant is held
against the TUI's process and dies with it. This is a deliberate-action
interlock, not a security boundary; see
[Agent Collaboration](https://infrashift.github.io/mrman/docs/guides/agents/).

All `review` output is JSON. [`skills/mrman/`](skills/mrman/) packages this
as an agent skill, with tmux and zellij wrappers that open a review pane and
hand the session slug back — it draws the line between *the user reviews
your patch* (never write comments yourself) and *you review a patch* (write
findings under an explicit `--username`).

## Configuration

`~/.config/mrman/config.toml` — friendly TOML validated by embedded CUE
schemas (mistakes degrade to warnings with precise messages, never crashes).
Every option is documented in
[Configuration](https://infrashift.github.io/mrman/docs/reference/configuration/):

```toml
theme = "tokyo-night-storm"        # or theme_dark / theme_light + appearance
leader = ";"
comment_vim = true                 # vim mode in the comment editor
diff_view = "unified"              # or "side-by-side"
username = "ryan"

[[comment_types]]
id = "issue"
label = "ISSUE"
definition = "must fix before merge"
color = "red"

[templates]
notes = "~/.config/mrman/templates/notes.md.tmpl"        # optional overrides
review_body = "~/.config/mrman/templates/review_body.md.tmpl"

[forge]
default = "github"

[[forge.hosts]]                     # on-premise example
host = "gitlab.mycorp.com"
forge = "gitlab"
token_cmd = "pass show gitlab-token"

[[forge.hosts]]
host = "ghe.mycorp.com"
forge = "github"
api_base = "https://ghe.mycorp.com/api/v3"
token = "$GHE_TOKEN"
```

Auth resolution per host: environment (`GITHUB_TOKEN`/`GH_TOKEN` for
github.com, `GITLAB_TOKEN` for gitlab.com, `AZURE_DEVOPS_EXT_PAT` for
dev.azure.com and `*.visualstudio.com`, `FORGEJO_TOKEN`/`CODEBERG_TOKEN` for
codeberg.org, `GH_ENTERPRISE_TOKEN` for GitHub Enterprise hosts listed in
`[[forge.hosts]]`) → config `token` → `token_cmd` → `gh auth token` (GitHub).
Credentials only ever go to SaaS hosts and hosts you list. Custom CAs via
`ca_file` per host.

### Themes

Bundled: `tokyo-night-storm`, `tokyo-night-day`, `dark`, `light`,
catppuccin ×4, gruvbox ×2, nord ×4, solarized ×2, everforest ×2, ayu ×2,
`onedark`, `github-light`, `github-dark`. Custom themes live in
`~/.config/mrman/themes/<name>.toml` (41 color slots, CUE-validated, plus
`syntax_style` naming any chroma style or `syntax_style_file` for chroma
XML).

### Templates

The exported notes markdown, the review body posted with `:submit`, and the
`:patch` reply are rendered through Go `text/template`s with embedded
defaults. Override them via `[templates]` (`notes`, `review_body`,
`patch_reply`); parse errors fall back to the defaults with a warning.

## Development

```sh
make check      # fmt + vet + lint + tests + 85% coverage gate
make build      # ./bin/mrman
make package    # cross-platform release artifacts in ./dist
make docs-dev   # the documentation site at localhost:4321/mrman/
```

The codebase mirrors tuicr's layout under `internal/`; tuicr's own test
suite is ported throughout as the behavioral parity spec.

Opt-in live tests exercise a real forge instead of fakes; they skip unless
`MRMAN_LIVE_PR` is set, so `make check` is unaffected. `MRMAN_LIVE_PR` takes
any target `mrman pr` does, on any forge, and each test skips the forges it
does not cover. `scripts/live-fixture.sh URL up` builds a scratch merge
request to point them at on GitHub, GitLab, Azure DevOps or Codeberg. See
[Testing Against a Real Forge](https://infrashift.github.io/mrman/docs/contributing/live-testing/).

```sh
MRMAN_LIVE_PR=owner/repo#1 go test -count=1 -run Live ./...        # reads only
MRMAN_LIVE_PR=owner/repo#1 MRMAN_LIVE_SUBMIT=1 \
    go test -count=1 -run Live ./...                               # also posts reviews
```

| Test | Forge | Covers |
|---|---|---|
| `TestLivePullRequest` | GitHub | every driver method |
| `TestLivePullRequestReview` | any | the whole stack through the renderer |
| `TestLivePullRequestSubmit` | any | a real review posted from the TUI (`MRMAN_LIVE_SUBMIT`) |
| `TestLiveAgentSubmit` | any | the agent-submit interlock, refused and granted (`MRMAN_LIVE_SUBMIT`) |
| `TestLiveGitLabReview` | GitLab | comment, draft, request changes, moved head, approve; `MRMAN_LIVE_READONLY_TOKEN` adds a read-only-token step |

### charmkit

Three layers that other Bubble Tea projects can reuse live in the nested
[`charmkit`](charmkit/) module — modal text editing (`vimtext`), vim chord
and count resolution (`keychord`), and a terminal-cell text layer with
horizontal scrolling and wrap-safe background overlays (`cellrender`). It is
a separate module so consumers do not inherit mrman's dependency tree.
`go.work` is committed, so every clone builds against the local charmkit;
`go.mod` requires the tagged `charmkit/v0.1.0`, so `go install` and
`GOWORK=off go build ./...` resolve it as a module (CI checks the latter).

### Deliberate differences from tuicr

- **No self-updater.** Use your package manager or `go install`; there is no
  `:update` and no startup version check.
- **No Mercurial backend.** git, Jujutsu, `--file` and `-A` are supported.
- **`Space` also expands context gaps**, alongside tuicr's `Enter`.
- **Four comment types ship by default** (NOTE, ISSUE, SUGGESTION, PRAISE);
  tuicr ships only the untyped one, so the feature is invisible until
  configured.
- **Four forges instead of two**, through their APIs rather than by shelling
  out to `gh` and `glab` — Forgejo experimental.
- **CUE-validated configuration** and **user-templatable markdown output**.

## Documentation

The full site lives at <https://infrashift.github.io/mrman/> and its source is
in [`docs/`](docs/) — an Astro + Starlight project built with bun. `make
docs-dev` serves it locally with live reload; `make docs-build` produces a
production build.

## License

Apache 2.0 — see [LICENSE](LICENSE).
