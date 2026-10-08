---
title: Azure DevOps
description: Review Azure DevOps merge requests from the terminal — PATs, the org/project/repo identity, votes instead of reviews, and the three things Azure DevOps cannot do.
sidebar:
  order: 4
---

Azure DevOps works, and it is the forge whose model differs most from GitHub's.
Nothing here is broken — it is Azure DevOps having no server-side "review"
object, and mrman being explicit about what follows from that.

Both **Azure DevOps Services** (`dev.azure.com`) and on-premise **Azure DevOps
Server** collections are supported.

## Authenticate

Azure DevOps uses Personal Access Tokens.

```sh
export AZURE_DEVOPS_EXT_PAT=...
```

The variable name matches the Azure CLI's `az devops` extension, so if you
already have it set for `az`, mrman picks it up. It is read for
`dev.azure.com` and legacy `*.visualstudio.com` hosts only, so it never leaks
to an on-premise collection.

**Scope: Code (Read & Write).** Read alone is enough to browse and review;
write is needed to post comments or cast a vote. Create one under *User
settings → Personal access tokens*.

If the PAT is wrong or missing, mrman says so in those terms rather than
showing you a bare 401:

```
Azure DevOps authentication failed. Set AZURE_DEVOPS_EXT_PAT (or configure a
token for this host) to a PAT with the Code (Read & Write) scope.
```

### On-premise Azure DevOps Server

Point `api_base` at the collection URL:

```toml
[[forge.hosts]]
host = "tfs.mycorp.com"
forge = "azuredevops"
api_base = "https://tfs.mycorp.com/DefaultCollection"
token_cmd = "pass show tfs-pat"
ca_file = "/etc/ssl/corp-root.pem"
```

`forge = "azuredevops"` is the canonical name; `azure_devops` and `ado` are
accepted too, for `forge` here and for `[forge] default`.

## Open a merge request

Azure DevOps identifies a repository by **organization, project and
repository** — three parts, not two. That shows up in every target form:

```sh
mrman pr 7                                     # from inside the checkout
mrman pr project/repo#7                         # org comes from the checkout
mrman pr dev.azure.com/org/project/repo#7       # fully qualified
mrman pr https://dev.azure.com/org/project/_git/repo/pullrequest/7
mrman                                           # then Tab to Merge Requests
```

Legacy `*.visualstudio.com` URLs work too, including the
`DefaultCollection` segment:

```sh
mrman pr https://org.visualstudio.com/DefaultCollection/project/_git/repo/pullrequest/7
```

SSH remotes in the `v3` form (`git@ssh.dev.azure.com:v3/org/project/repo`) are
recognized, and `ssh.dev.azure.com` is mapped back to `dev.azure.com` for API
calls — the transport hostname is right for git and wrong for the API. The
legacy `git@vs-ssh.visualstudio.com:v3/org/project/repo` form maps to
`org.visualstudio.com` the same way.

Projects and repositories whose names contain spaces work: clone URLs encode
a space as `%20`, and mrman decodes it before calling the API.

## How the diff is built

Azure DevOps serves no text diff, so mrman builds one. The merge request's
latest iteration lists the changed files; mrman fetches each file at the
**merge base** and at the head, eight files at a time, and diffs them
itself. The merge base is the iteration's common commit, which is what Azure
DevOps' own Files tab compares against. Diffing against the target branch's
tip instead would show every change made on the target since you branched,
reversed.

## What works, and what Azure DevOps cannot do

| | |
|---|---|
| Draft reviews | **—** Azure DevOps has no pending-review state |
| Approve / request changes | ✓ as **votes** |
| Review summaries | **—** no review object to carry one |
| Thread resolution | ✓ real; resolved threads hide by default |
| Outdated threads | — not reported |
| Multi-line comments | ✓ true ranges |
| Per-commit range diff | **—** `(` / `)` unavailable |
| Atomic submit | **—** see below |
| Review-requested filter | ✓ `r` in the MR list |

### No draft reviews

Azure DevOps has no pending-review state. The picker still lists all four
events, but choosing `Draft (pending review)` — or running `:submit draft`
directly — is refused before any request is made:

```
╭── Submit review to dev.azure.com? ──────────────────────
│    Comment
│    Approve
│    Request changes
│  > Draft (pending review)
│  Enter: submit   Esc: cancel
╰─────────────────────────────────────────────────────────
 SUBMIT              This forge does not support draft reviews
```

The picker stays open, so you can pick something that works without starting
over.

### Approve and request changes are votes

Azure DevOps records a reviewer's position as a numeric vote on their own
reviewer entry, not as a review with a state. mrman maps:

| mrman event | Azure DevOps vote | Verified |
|---|---|---|
| `:submit approve` | `+10` — Approved | ✓ read back as `vote=10` |
| `:submit request-changes` | `-10` — Rejected | ✓ read back as `vote=-10` |

Your comments post as threads either way. A vote is cast on the **viewer's own**
reviewer record, so it behaves exactly like clicking Approve in the web UI,
including any branch policies that key off it. Reading it back from the API
after an `:submit approve`:

```json
{"pullRequestId": 1, "status": "active",
 "reviewers": [{"displayName": "ryan craig", "vote": 10}]}
```

### No commit-range diff

Azure DevOps serves no range-diff endpoint, so commit scoping is unavailable.
The commit strip still appears and `(` / `)` are still accepted — but the diff
does not narrow, and mrman says so rather than showing you a wrong diff:

```
 NORMAL   This forge cannot diff a commit range — showing the whole merge request
```

A review therefore always covers the whole merge request.

### No atomic submit

There is no endpoint that takes a whole review, so mrman posts a sequence: the
review body becomes a context-less thread, and each inline comment becomes its
own file-anchored thread. A vote, if any, comes last.

If a step fails partway, mrman reports a **partial submit** naming exactly which
comments landed, and marks only those as submitted locally — the rest stay
editable so a retry does not double-post. One nuance: if you submit a *pure
vote* with no body and no comments and the vote fails, that fails outright,
because nothing landed to keep.

Because there is no server-side review object, mrman has no review id to report
back; the submit result carries the merge request URL instead.

### Threads track across iterations

When mrman anchors an inline comment it attaches Azure DevOps' iteration
context and the file's change-tracking id. That is what lets Azure DevOps keep
your comment attached to the right line as the merge request gets new
iterations, instead of stranding it. You do not have to do anything for this —
it is why the anchors survive a force-push better than a naive line number
would.

## Submitting

```
:submit                  # picker
:submit comment
:submit approve
:submit request-changes
```

One asymmetry that is not Azure DevOps' fault: **`:submit approve` works with
no comments at all, but `:submit request-changes` does not.** A bare approve is
a meaningful act on its own ("looks good"), so it is exempt from the
nothing-to-submit check; every other event answers *"Nothing to submit — no
local-draft comments"* when you have nothing pending. Leave at least a
review-level comment (`<leader>c`) with a request-changes, which you probably
want anyway.

## Reading the merge request

Existing threads render inline, read-only, with real resolution state — Azure
DevOps tracks that properly, so `:comments unresolved` / `all` / `hide` all
behave. `:e` refetches.

The header counts unresolved threads, and resolved ones are hidden until
`:comments all`, at which point they render with a `(resolved)` marker:

```
 mrman   ryanscraig/scratch/scratch#1 · Expire cache entries after their TTL · OPEN · 1 thread

                  ╒══ @ryan craig ═══════════════════════════════════════════
                  ║  @ryan craig · 1m
                  ║  Deleting inside Get means the read path mutates the map.
                  ╘══════════════════════════════════════════════════════════
```

```
 ... after :comments all ...

                  ╒══ @ryan craig (resolved) ════════════════════════════════
                  ║  @ryan craig · 2m
                  ║  Does this also handle escaped quotes inside the value?
                  ╘══════════════════════════════════════════════════════════
```

One asymmetry worth knowing: a thread with **no file context** — the kind the
web UI shows as a general MR discussion — is not rendered, because Azure DevOps
has no review-summary concept for mrman to slot it into. Only file-anchored
threads appear in the diff.

### Thread dispositions

Azure DevOps is unusual in giving a thread seven possible states rather than a
resolved boolean. mrman reads all of them and collapses them onto its
open/resolved distinction:

mrman shows the disposition on the thread's badge, so "won't fix" does not read
as "fixed":

```
╒══ @ryan craig (won't fix) ══════════════════════════════════════
║  @ryan craig · 56m
║  Deleting inside Get means the read path mutates the map.
╘═════════════════════════════════════════════════════════════════
```

Each row below was verified by setting that disposition on a real merge request
and reading the badge mrman drew:

| Azure DevOps status | Web UI label | Badge | Visibility |
|---|---|---|---|
| `active` | Active | *(none)* | **Open** — shown by default |
| `pending` | Pending | `(pending)` | **Open** — shown by default |
| `fixed` | Resolved | `(resolved)` | Resolved — hidden until `:comments all` |
| `wontFix` | Won't fix | `(won't fix)` | Resolved — hidden until `:comments all` |
| `closed` | Closed | `(closed)` | Resolved — hidden until `:comments all` |
| `byDesign` | By design | `(by design)` | Resolved — hidden until `:comments all` |
| *(status absent)* | Unknown | *(none)* | **Open** — shown by default |

`active` gets no badge because it is the default state and a badge on every
thread would be noise. `fixed` deliberately reads as `(resolved)`, so the
common case matches how every other forge labels a resolved thread.

A thread that is both resolved and outdated composes the two:
`(won't fix · outdated)`.

`pending` is the one that surprises people. It reads like a resolution, but in
Azure DevOps it means *waiting on the author*, so mrman keeps it visible.

`unknown` is not a value you will ever read back. Setting a thread to `unknown`
makes Azure DevOps **omit the status field entirely** rather than store the
string, so what mrman actually receives is a thread with no status — which it
treats as open. Absent state should never hide a comment from you.

The header counts accordingly, so you can tell at a glance whether anything is
hidden:

```
 ... · OPEN · 2 threads                    ← unresolved-only (the default)
 ... · OPEN · 3 threads (2 unresolved)     ← after :comments all
```

Two things to know:

- **Filtering still keys off resolved/unresolved, not the badge.** The
  disposition is display only, so the four terminal states hide together and
  `pending` stays visible. A forge adding a new status can never accidentally
  hide a thread from you.
- **mrman never sets a disposition.** Existing threads are read-only — it does
  not resolve, reply to, or reclassify them. Threads mrman creates on submit are
  always opened as `active`, which is what a new review comment should be.

Forges that only distinguish resolved from unresolved — GitHub, GitLab,
Forgejo — carry no disposition and keep the plain `(resolved)` badge.

`r` in the merge-request list toggles between everything open and what is
waiting on your review.

## What was verified against a real merge request

Everything on this page was exercised against a live Azure DevOps Services pull
request — three commits, two files, three pre-existing threads (one resolved).

All three target forms resolved to the same repository:

```console
$ mrman pr 1 --json          # from inside the checkout
{"slug":"ado:dev.azure.com/ryanscraig/scratch/scratch/pr/1","kind":"pr",
 "repo":"ryanscraig/scratch/scratch","number":1,
 "title":"Expire cache entries after their TTL","file_count":2,
 "read_only":false,"granted_events":[]}

$ mrman pr scratch/scratch#1 --json                          # project/repo
$ mrman pr dev.azure.com/ryanscraig/scratch/scratch#1 --json # fully qualified
$ mrman pr https://dev.azure.com/ryanscraig/scratch/_git/scratch/pullrequest/1 --json
```

A `:submit comment` carrying one inline and one review-level comment produced
exactly the N+1 shape described above — the review body as a context-less
thread, the inline comment file-anchored with iteration context and a
change-tracking id:

```
#4 active   (pr-level)      [no iteration ctx]
     Verified against a real Azure DevOps PR from mrman.
#5 active   /src/cache.go:65 [iter 1..1 changeTrackingId=1]
     The lock upgrade from RLock to Lock is the right call here.
```

Also confirmed end to end:

- **Multi-line comments are true spans, not downgrades.** A `v`-selected range
  posted with `rightFileStart {line: 76}` and `rightFileEnd {line: 77}`.
- **`[TYPE]` prefixes reach the forge.** A `NOTE`-typed comment arrived as
  `[NOTE] Read path mutates the map under a write lock.`; an untyped one arrived
  with no prefix.
- **Context expansion pulls from Azure DevOps on demand** — `Enter` on an
  expander took a file from 54 hidden lines to 34.
- **Read-only means read-only.** `dd` on a forge thread answers *"Existing forge
  comments are read-only"*; on a comment you already submitted, *"Comment already
  pushed — read only"*.
- **The merge-request list works**, including `/` filtering and `r` toggling
  between `scope:all` and `scope:requested`.
- **The agent interlock holds.** Without a grant, `mrman review submit` exits `1`
  with `agent_submit_not_permitted` and the thread count on the forge is
  unchanged; with `mrman pr 1 --auto` open, the same command succeeds and
  `--event approve` is still refused as `event_not_granted`.

All seven thread dispositions were set on a live thread in turn and the
rendering checked each time — `active` and `pending` stayed visible, the four
terminal ones were hidden until `:comments all` and then carried `(resolved)`,
and `unknown` came back as an absent status and stayed visible.

Live testing also caught a real bug that the unit-test fakes could not: the raw
`_apis/connectionData` call — which resolves your identity so a vote can be cast
against it — was sending `api-version=7.1`, and Azure DevOps serves that
resource under **preview versioning only**. Every approve and request-changes
failed with `VssInvalidPreviewVersionException` (HTTP 400). The fakes matched on
URL path and ignored the api-version, so nothing local noticed. It is fixed, and
a test now pins the `-preview` suffix.

If you are on a version of mrman where `:submit approve` fails on Azure DevOps
with a 400 mentioning a preview version, that is this bug — upgrade.

A second Azure-DevOps-only bug turned up the same way: `mrman review list --repo
<checkout>` returned nothing. Coordinate derivation took the last two segments
of the clone URL, and Azure DevOps' URLs are
`{org}/{project}/_git/{repo}` — so the literal `_git` marker became the owner
and never matched the `{project}/{repo}` coordinate derived from the repo's MR
slugs. Agents following the documented `--repo /path/to/repo` path would
conclude no session existed. Also fixed, with regression tests.
