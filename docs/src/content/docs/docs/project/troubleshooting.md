---
title: Troubleshooting
description: Auth failures, unknown hosts, config warnings, terminal quirks and build problems — with the checks that tell you which one you have.
---

## "Not found" on a repository I can see in the browser

You are unauthenticated for that host. A private repository returns **404**, not
403 — that is the forge's standard answer for "you cannot see this", and it looks
identical to a typo in the repository name.

Walk the resolution chain for your host. Tokens resolve **per host**, first hit
wins:

1. A SaaS-scoped environment variable
2. The host's `token` in config (`$VAR` expanded)
3. The host's `token_cmd`, run through `sh -c` and cached for the process
4. `gh auth token --hostname <host>` — GitHub hosts only, if
   `[forge] cli_token_fallback` is on

| Host | Environment variable |
|---|---|
| `github.com` | `GITHUB_TOKEN`, then `GH_TOKEN` |
| Other GitHub hosts | `GH_ENTERPRISE_TOKEN` |
| `gitlab.com` | `GITLAB_TOKEN` |
| `dev.azure.com` | `AZURE_DEVOPS_EXT_PAT` |
| `codeberg.org` | `FORGEJO_TOKEN`, then `CODEBERG_TOKEN` |

Variables are host-scoped **on purpose**: `GITHUB_TOKEN` is not read for a GitHub
Enterprise host, so a personal token never leaks to your employer's instance. If
you set the wrong one for your host, mrman behaves as though you set nothing.

### Proving which link fired

Hide `gh` and unset the variable. If a private repository starts 404ing, step 4
was doing the work:

```console
$ env PATH=/tmp MRMAN_LIVE_PR=OWNER/REPO#1 \
    go test -count=1 ./internal/forge/githubf/ -run TestLivePullRequest
ListPullRequests: github: list_pull_requests: not found (HTTP 404) on github.com
```

Supply the token directly and it works with `gh` still off `PATH` — which is also
the proof that mrman uses the API SDKs rather than the CLI:

```console
$ env PATH=/tmp GITHUB_TOKEN="$(gh auth token)" MRMAN_LIVE_PR=OWNER/REPO#1 \
    go test -count=1 ./internal/forge/githubf/ -run TestLivePullRequest
--- PASS
```

`-count=1` matters. Go caches test results, so without it a "pass" may be a
cached run from before you changed anything.

### `token_cmd` failing

A failing `token_cmd` is **fatal**, not skipped — you configured it on purpose,
so falling through to anonymous access would hide the problem. Run the command
yourself; it goes through `sh -c`, so quoting is on you.

A host entry may set `token` **or** `token_cmd`, never both. The schema rejects
that rather than picking one silently.

## "cannot determine the forge for host"

```
cannot determine the forge for host "git.mycorp.com" — add it to your config:

[[forge.hosts]]
host  = "git.mycorp.com"
forge = "github"  # one of: github, gitlab, azuredevops, forgejo
```

mrman claims `github.com`, `gitlab.com`, `dev.azure.com` and `codeberg.org`
automatically. Anything else needs an entry. Paste the snippet from the error and
set `forge` correctly.

If the host is a GitHub Enterprise Server, remember `api_base`:

```toml
api_base = "https://ghe.mycorp.com/api/v3"
```

GitLab and Forgejo usually need no `api_base` — mrman derives `/api/v4` and
`/api/v1` from the host.

## A bare PR number does not work

```
bare PR number "125" requires a repository checkout; run inside a checkout,
or pass owner/repo#125 or a full URL
```

You are outside a checkout, or mrman could not resolve a forge repository from
your remotes. Pass a coordinate or a URL. From inside a checkout with an
unrecognized remote host, add the `[[forge.hosts]]` entry above.

## TLS errors on an on-premise host

Add your organization's CA for that host:

```toml
ca_file = "/etc/ssl/corp-root.pem"
```

`insecure_skip_verify = true` also exists per host. It disables certificate
verification for that host entirely and should stay a last resort.

## My config option is being ignored

Configuration is validated against embedded CUE schemas, and a mistake **never
stops mrman starting**: the offending key is dropped, everything else is kept,
and you get a precise warning in the status bar. If an option seems inert, look
at the status bar on startup — the warning names the key.

Two specific gotchas:

- **`backend`** is accepted and deliberately ignored. It exists for tuicr config
  compatibility; mrman uses the git CLI by design.
- **`ignore_whitespace`** applies to local git and Jujutsu diffs only. A pull
  request's diff arrives from the forge already rendered, so there is no flag
  left to re-run it with.

## Keys do not do what the docs say

Almost always the terminal. `Shift-Enter` needs the kitty keyboard protocol;
`Alt-Enter` survives tmux; `Ctrl-j` works without either. See [Terminal
Setup](../../guides/terminals/) for the full set of aliases and which terminals
support what.

If the mouse has taken over your terminal's own text selection, hold Shift (or
Option on macOS), or set `mouse = false`.

## mrman hangs at startup under `script` or a pipe

```sh
script -qec "mrman pr 1" /dev/null   # hangs
```

Bubble Tea queries the terminal for capabilities at startup and a piped stdin
never answers. This is not fixable from mrman's side. For non-interactive use,
that is what `--json` and the [`mrman review`](../../guides/agents/) commands are
for.

## `--auto` is refused

```
error: --auto requires an interactive terminal
error: if any flags in the group [json auto] are set none of the others can be
```

Both are intentional. `--auto` authorizes an agent to submit reviews, so it must
come from a person at a terminal — see [the submit
interlock](../../guides/agents/#the-submit-interlock).

## Submit posted some comments and not others

On **GitLab** and **Azure DevOps** there is no atomic-submit endpoint, so mrman
posts a sequence and reports a partial submit if a step fails. Only the comments
that landed are marked submitted; the rest stay editable, so you can retry
without double-posting. Details: [Forge
Capabilities](../../reference/forge-capabilities/#atomic-submit).

## A comment went into "Unplaced comments"

mrman could not anchor it to a line the forge would accept, so it moved into the
review body rather than being dropped. The reason is stated:

| Reason | Fix |
|---|---|
| `line not in current diff` | The line is outside every hunk — comment on a changed line, or leave it review-level |
| `anchored line changed since the comment was written` | The code the comment describes moved out of reach or is no longer identifiable — see [outdated comments](#a-comment-is-marked-outdated) |
| `range spans both diff sides` | Keep an inline range on one side |
| `no valid anchor line` | A file-level comment found no new-side line |
| `binary file` | Nothing to anchor to |
| `file too large` | The file was not diffed |

If you are writing comments through `mrman review add`, note that setting a
cursor position by hand is not the same as navigating: comments file under the
*current file*, so an out-of-band cursor move can aim a comment at the wrong
file. mrman catches this and reports *"line not in current diff"* rather than
posting somewhere wrong.

## A comment is marked `(outdated)`

The diff moved under it. Every comment records the line it was written about, and
on reopen or `:e` mrman checks that record against the current diff. `(outdated)`
means the check failed one of two ways:

- **The line is gone.** The code it described is no longer in the diff.
- **The line is ambiguous.** Its content now appears in several places, so there
  is no telling which one was meant. A comment on a lone `}` is the usual case.

mrman deliberately does not guess. The old line number still resolves to *some*
line, and posting there would attach your criticism to code you never read — so
the comment keeps its text, wears the badge, and at submit time goes to the
resolver instead of inline. Edit it, re-place it by hand, or send it to the
review body.

`(moved)` is the benign sibling: the content was found at exactly one other
line and the comment followed it. Nothing is wrong; the badge exists so a
comment that silently changed position tells you it did.

## My review did not come back after a rebase or amend

A working-tree review is carried onto the new HEAD automatically, so this
usually means one of the deliberate exclusions applied:

| Situation | Why |
|---|---|
| **Detached HEAD** | The identity *is* the commit, so there is no stable anchor to carry — two detached checkouts are unrelated positions, not one review |
| **You switched branches** | A review is anchored to its branch |
| **A commit range** (`-r`) | A range names its own endpoints, so a different range is a different review |
| **Nothing was in it** | An untouched session is not carried, to avoid churning files on every commit |
| **Staged vs working tree** | Different diff sources are different reviews |

The previous session is not deleted in any of these cases. Find it with
`mrman review list --repo .` — the `anchor` column shows which HEAD each one
belongs to.

## `go install` fails, or `GOWORK=off go build` fails

Expected until the first release. mrman keeps three reusable layers in a nested
`charmkit` module; `go.work` is committed so every clone builds, but `go install`
ignores workspaces and `go.mod` cannot require a `charmkit` version that has
never been tagged.

Build from a checkout (`make build` or `make install`) in the meantime. After the
first release this must pass — it is treated as a release blocker, not a
local-setup problem. See [Contributing](../contributing/).

## Something else

Please [open an issue](https://github.com/infrashift/mrman/issues) with your
terminal, forge, and what you expected. If it is a forge-behavior question,
`mrman --version` and the exact error text are the two most useful things to
include.
