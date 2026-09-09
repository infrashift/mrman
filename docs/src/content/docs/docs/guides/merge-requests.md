---
title: How MR Review Works
description: Opening a merge request, reading the forge's existing threads inline, narrowing to one commit, and submitting a review back — the parts that are the same on every forge.
sidebar:
  order: 0
---

This page is the forge-agnostic half of merge-request review. Everything here
works the same way on all four forges; the per-forge pages cover
authentication, URL shapes and the specific things a forge cannot do.

Jump to yours: [GitHub](../../forges/github/) · [GitHub Enterprise
Server](../../forges/github-enterprise/) · [GitLab](../../forges/gitlab/) ⚗️ ·
[Azure DevOps](../../forges/azure-devops/) · [Forgejo &
Gitea](../../forges/forgejo/) ⚗️ · [Codeberg](../../forges/codeberg/) ⚗️

⚗️ marks a driver that is [experimental](../../reference/forge-capabilities/#support-levels)
— implemented and unit-tested, but not yet run against a live instance.

## Opening one

```sh
mrman pr 125                            # bare number, repo from your checkout
mrman pr owner/repo#125                 # addressed explicitly
mrman pr owner/repo!125                 # ! reads better for merge requests
mrman pr https://github.com/o/r/pull/1  # or by URL, any forge
mrman                                   # then Tab to the Merge Requests tab
```

### Target syntax

| Form | Notes |
|---|---|
| `125` | Requires a checkout mrman can resolve a forge repository from |
| `owner/repo#125` | Host and forge inherited from your checkout, or from `[forge] default` outside one |
| `owner/repo!125` | `!` and `#` are interchangeable everywhere |
| `host/owner/repo#125` | Host-qualified; the host decides the forge |
| `host/group/sub/repo#125` | GitLab nested subgroups — any depth |
| `host/org/project/repo#125` | Azure DevOps, which identifies repos by org **and** project |
| `project/repo#125` | Azure DevOps, from inside an ADO checkout: the org comes from the checkout |
| A full web URL | GitHub `/pull/N`, Forgejo `/pulls/N`, GitLab `/-/merge_requests/N`, Azure DevOps `/_git/{repo}/pullrequest/N` |

Configuration wins over URL shape. If a `[[forge.hosts]]` entry claims a host,
that entry decides which driver runs even when the path looks like some other
forge's — which is what makes a self-hosted GitLab at `git.example.com` work.

An unrecognized host produces an actionable error containing the
`[[forge.hosts]]` snippet you need, rather than a guess.

### Browsing instead

The **Merge Requests** tab of the target selector lists what is open on the
forge. `/` filters the loaded list, `r` toggles between everything open and
what is waiting on your review, `Enter` opens one or loads more. `<leader>p`
(`;p`) gets you back to the list from inside a review.

## Reading the forge's side

The merge request's existing review threads and review summaries render
**inline in the diff, read-only**. mrman never replies to them, resolves them,
or rewrites them — `dd` on one answers *"Existing forge comments are
read-only"*.

Everything the forge sends — titles, branch names, author logins, comment
bodies, file contents — is scrubbed of terminal escape sequences and other
control characters before it is drawn, so a comment cannot restyle the
review, hide text under a hyperlink, or ring the bell. The same scrub runs
on patch files and on comments an agent writes with `mrman review add`.

Resolved threads are hidden by default:

| Command | Shows |
|---|---|
| `:comments unresolved` | Unresolved threads only (the default) |
| `:comments all` | Resolved threads too |
| `:comments hide` | No forge comments at all |

`m` / `M` navigate forge comments alongside your own.

## Context and refetching

Merge request diffs arrive with the same hidden context as local ones. `Enter`
or `Space` on an expander fetches the surrounding lines **from the forge** on
demand, so you are not downloading whole files you will not read.

`:e` (or `:reload`) refetches. If the merge request advanced to a new head
while you were reading, mrman opens the review **for that head** rather than
re-anchoring your existing comments onto lines that may have moved — the
anchors you wrote were about the code you saw.

## Narrowing to one commit

On a multi-commit merge request, `(` and `)` walk commit by commit, narrowing
the diff to one commit's changes and back out again. mrman marks the commits
your last review already covered and preselects what landed since, so a
re-review starts on the new work.

This uses the forge's range-diff endpoint. On a forge without one, the review
widens back to the whole merge request instead of failing — see [Forge
Capabilities](../../reference/forge-capabilities/).

## Submitting

```
:submit                     # opens the picker
:submit comment
:submit approve
:submit request-changes
:submit draft
```

The picker always lists all four events. Capabilities are checked when you
**choose** one, before anything is sent — so picking an event the forge cannot
do is refused outright rather than failing halfway through. On Azure DevOps,
choosing `Draft (pending review)` answers *"This forge does not support draft
reviews"* and leaves the picker open for another choice.

### Preflight

Before anything is posted, mrman resolves each of your comments to a position
the forge will accept and shows you the result:

```
preflight: 2 inline, 0 unmappable, 1 review-level
  inline: src/cache.go:75 side=new
  inline: src/config.py:8 side=new
```

A comment that cannot become an inline comment is not dropped. It moves into
an **Unplaced comments** section of the review body, with the reason:

| Reason | Meaning |
|---|---|
| `line not in current diff` | The anchor line is outside every hunk |
| `anchored line changed since the comment was written` | The comment's anchor failed validation against the current diff |
| `range spans both diff sides` | An inline range comment must stay on one side |
| `no valid anchor line` | A file-level comment found no line on the new side |
| `binary file` | No anchor can be derived |
| `file too large` | The file exceeded the threshold and was not diffed |

The second one is worth understanding, because it is the only reason where the
line number *would* still have mapped. If a comment is marked `(outdated)` —
the code it was written about has gone, or now appears in several places —
mrman refuses it an inline position on purpose. An inline comment on the wrong
line is worse than one in the review body, and the tool does not get to guess
where your criticism lands. This holds on the `--auto` agent path too: the
resolver runs before the skip-confirm branch, so nothing posts inline on an
anchor that failed. See [outdated
comments](../../project/troubleshooting/#a-comment-is-marked-outdated).

On a forge without multi-line comment support, range comments **downgrade**
rather than fail: the comment posts on its end line with a `Lines X–Y:` prefix
so the span is still visible to the next reviewer.

### After submitting

Submitted comments are locked. Editing or deleting one is refused — it exists
on the forge now, and mrman will not pretend otherwise.

The review body is rendered from a Go template you can override, and comment
types are prefixed onto submitted comments as `[TYPE]` unless you set
`comment_type_prefix = false`. See [Templates](../../reference/templates/) and
[Configuration](../../reference/configuration/#forges).

## Authentication, briefly

mrman talks to forge APIs directly — it does not shell out to `gh` or `glab`.
Tokens resolve **per host**, first hit wins:

1. A SaaS-scoped environment variable, scoped so an environment token never
   leaks to an on-premise host.
2. The host's `token` in config, with `$VAR` expansion.
3. The host's `token_cmd`, run through `sh -c` and cached for the process.
4. `gh auth token --hostname <host>`, for GitHub hosts only, when
   `[forge] cli_token_fallback` allows it (it does by default).

An empty result is not an error — it means unauthenticated access, which is
fine for a public repository. Your forge's page has the specific variable
names; [Troubleshooting](../../project/troubleshooting/) has the debugging
checklist.

## Agents

An agent can follow a merge-request review as you write it, or contribute
findings of its own, through `mrman review`. Submitting to a forge is gated
behind a grant only a person at a terminal can issue. See [Agent
Collaboration](../agents/).
