---
title: CLI Reference
description: Every mrman flag and subcommand — the TUI entry points, the pr/mr targets, and the review JSON surface agents use.
---

```
mrman [flags]              # the TUI
mrman tui [flags]          # explicit, identical
mrman pr <target>          # review a pull request
mrman mr <target>          # alias of pr
mrman review <subcommand>  # non-interactive JSON surface
```

`mrman --version` prints the build; `mrman --help` and `-h` work at every level.

## TUI flags

These are persistent flags on the root command, so they apply to `mrman`,
`mrman tui` and `mrman pr`.

| Flag | Default | Meaning |
|---|---|---|
| `-r`, `--revisions <range>` | — | Commit range or revset to review (backend-specific syntax) |
| `-w`, `--working-tree` | false | Review working-tree changes, skipping the selector |
| `-p`, `--path <prefix>` | — | Filter the diff to a file or directory prefix |
| `--file <path>` | — | Review a file or directory with no VCS involved |
| `-A`, `--all-files` | false | Pristine mode: annotate every tracked file (git only) |
| `--theme <name>` | — | Bundled theme name, or a file in `themes/` |
| `--appearance <mode>` | — | `light`, `dark` or `system`, used when no explicit theme |
| `--stdout` | false | Export review markdown to stdout instead of the clipboard |
| `--repo-url <url>` | — | Override the forge repository for PR operations |
| `--forge <kind>` | — | Forge for ambiguous targets: `github`, `gitlab`, `azuredevops`, `forgejo` |
| `--json` | false | Open the pull request headlessly and print its session as JSON |
| `--auto[=events]` | — | Authorize agent submission for this session (see below) |

### Mutually exclusive combinations

mrman rejects these rather than picking a winner:

- `--file` with `--working-tree`, or with `--all-files`
- `--all-files` with `--path`, `--revisions` or `--working-tree`
- **`--json` with `--auto`** — load-bearing, so that a command an agent can run
  can never issue a grant

Passing any TUI flag to `mrman review` is also a hard error:

```
TUI options cannot be used with `mrman review` (--theme)
```

### `--auto`

Authorizes an agent to submit reviews for the session this TUI opens. It
requires an interactive terminal and cannot be combined with `--json`.

```sh
mrman pr 1 --auto                        # grants comment + draft
mrman pr 1 --auto=comment,draft,approve  # explicit set
```

Valid events: `comment`, `approve`, `request-changes`, `draft`. A bare `--auto`
grants `comment` and `draft` only — approval is what gets code merged, so it is
never in the default set. `--auto=` with an empty list is an error telling you
to omit the flag instead.

Full behavior: [the submit interlock](../../guides/agents/#the-submit-interlock).

## `mrman pr <target>` / `mrman mr <target>`

Exactly one target argument. `mr` is a pure alias, provided because "merge
request" is the right word on GitLab.

| Form | Example |
|---|---|
| Bare number | `125` (needs a checkout mrman can resolve) |
| Coordinate | `owner/repo#125`, `owner/repo!125` |
| Host-qualified | `github.com/owner/repo#125` |
| GitLab subgroups | `gitlab.com/group/sub/project!42` |
| Azure DevOps | `dev.azure.com/org/project/repo#7`, or `project/repo#7` in an ADO checkout |
| URL | Any supported forge's pull-request or merge-request web URL |

See [How PR Review Works](../../guides/pull-requests/#target-syntax) for the
resolution rules.

## `mrman review`

The agent integration surface. Every subcommand prints JSON. `--repo` is a
persistent flag: a checkout path (which also surfaces the PR sessions belonging
to that checkout's `origin`) or a forge coordinate.

### `review list`

```sh
mrman review list --repo .
mrman review list --repo owner/repo
mrman review list --all
```

| Flag | Meaning |
|---|---|
| `--repo <selector>` | Checkout path or forge coordinate |
| `--all` | Every session, ignoring `--repo` |

Each row carries `slug`, `kind` (`local` or `pr`), `path`, `updated_at`,
`comment_count`, `reviewed_count`, `file_count`, `anchor`, `active` and
`granted_events`.

### `review add [comment]`

```sh
mrman review add --session <slug> \
  --target-file src/x.go --line 42 --side new \
  --type issue --username "Claude" "This leaks the file handle."
```

| Flag | Default | Meaning |
|---|---|---|
| `--session <slug>` | — | **Required.** Session slug or path |
| `--input <json>` | — | JSON payload, `@file`, or `-` for stdin |
| `--type <id>` | `none` | Comment type id |
| `--target-file <path>` | — | File path for file and line comments |
| `--line <n>` | — | Line number for a line comment |
| `--end-line <n>` | — | End line for a range comment |
| `--side <old\|new>` | `new` | Diff side |
| `--username <name>` | — | Comment author |

A comment argument **or** `--input` is required. Scope follows from what you
pass: `--target-file` with `--line` is a line comment, `--target-file` alone is
a file comment, neither is a review-level comment.

Structured input target types: `review`, `file`, `line`, `line_range`.

```sh
mrman review add --session <slug> --username "Claude" --input - <<'JSON'
{"target": {"type": "line", "file": "main.go", "line": 12, "side": "new"},
 "type": "issue", "content": "Unchecked error."}
JSON
```

### `review comments`

Aliased as `review get`.

```sh
mrman review comments --session <slug>
```

`--session` is required. Each comment carries `id`, `location`, `path`,
`start_line`, `end_line`, `side`, `comment_type`, `lifecycle_state`, `content`
and `author`.

### `review watch`

Streams newline-delimited JSON until the session is submitted or closed.

```sh
mrman review watch --session <slug> --timeout 600
```

| Flag | Default | Meaning |
|---|---|---|
| `--session <slug>` | — | **Required** |
| `--timeout <sec>` | `0` | Stop after N seconds; `0` waits indefinitely |
| `--interval <ms>` | — | Poll interval; defaults to `review_watch_interval_ms` |
| `--since <id>` | — | Resume after this comment id instead of sending a snapshot |

Events: `snapshot`, `comment_added`, `comment_changed`, `comment_removed`,
`submitted`, `closed`.

### `review submit`

```sh
mrman review submit --session <slug> --event comment --username "Claude"
```

| Flag | Default | Meaning |
|---|---|---|
| `--session <slug>` | — | **Required** |
| `--event <event>` | `comment` | `comment`, `approve`, `request-changes`, `draft` |
| `--username <name>` | — | Author recorded on submit |

Requires the session's agent-submit grant. Without one, it prints a refusal as
JSON on stdout and exits `1`:

```json
{"error":"agent_submit_not_permitted","reason":"no_grant","requested_event":"comment",
 "granted_events":[],"message":"..."}
```

`reason` is `no_grant`, `grant_expired` or `event_not_granted`.

## Environment

| Variable | Read for |
|---|---|
| `GITHUB_TOKEN`, `GH_TOKEN` | `github.com` |
| `GH_ENTERPRISE_TOKEN` | Other GitHub hosts |
| `GITLAB_TOKEN` | `gitlab.com` |
| `AZURE_DEVOPS_EXT_PAT` | `dev.azure.com` |
| `FORGEJO_TOKEN`, `CODEBERG_TOKEN` | `codeberg.org` |
| `EDITOR` | `:edit` |

Token variables are host-scoped on purpose, so an environment token never leaks
to an on-premise instance.

## Paths

| Path | Holds |
|---|---|
| `~/.config/mrman/config.toml` | [Configuration](../configuration/) |
| `~/.config/mrman/themes/` | [Local themes](../themes/) |
| `~/.config/mrman/templates/` | [Templates](../templates/) (by convention) |
| `~/.local/share/mrman/reviews/` | Saved sessions (`sessions/<repo>@<what>-<hash>.json`) |
| `<repo>/.mrmanignore` | Per-repository ignore rules |

`$XDG_CONFIG_HOME` and `$XDG_DATA_HOME` are honored if set.

## Live test environment variables

Not part of the CLI, but useful to know they exist:

| Variable | Effect |
|---|---|
| `MRMAN_LIVE_PR=owner/repo#N` | Enables the opt-in tests against a real pull request |
| `MRMAN_LIVE_SUBMIT=1` | Additionally allows the tests that post a real review |

See [Testing Against a Real Forge](../../contributing/live-testing/).
