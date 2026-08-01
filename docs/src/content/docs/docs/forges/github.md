---
title: GitHub
description: Review and submit GitHub merge requests from the terminal — authentication, target syntax, and the full capability set mrman has on github.com.
sidebar:
  order: 1
---

GitHub is mrman's most complete backend: every capability is available, so
nothing on this page is a workaround.

On-premise GitHub Enterprise Server is configured differently — see [GitHub
Enterprise Server](../github-enterprise/).

## Authenticate

mrman talks to `api.github.com` directly using
[go-github](https://github.com/google/go-github) for REST and
[githubv4](https://github.com/shurcooL/githubv4) for GraphQL. It does **not**
shell out to `gh` to make API calls — the only thing it may borrow from `gh` is
a token.

Pick whichever you already have:

```sh
# 1. Already using the GitHub CLI? Nothing to do.
gh auth status

# 2. Or an environment variable
export GITHUB_TOKEN=ghp_...     # GH_TOKEN also works
```

Or put it in config, which is the right answer when you juggle several hosts:

```toml
[[forge.hosts]]
host = "github.com"
forge = "github"
token_cmd = "pass show github-token"
```

Resolution order for `github.com`, first hit wins: `GITHUB_TOKEN` → `GH_TOKEN`
→ the host's `token` → the host's `token_cmd` → `gh auth token --hostname
github.com`. The `gh` fallback can be turned off with
`[forge] cli_token_fallback = false`.

A public repository needs no token at all. A **private** repository without one
404s — that is GitHub's standard answer for "you cannot see this", not an mrman
bug. See [Troubleshooting](../../project/troubleshooting/).

### Scopes

`repo` covers everything: reading private merge requests, posting review
comments, approving. A fine-grained token needs **Pull requests: read &
write** and **Contents: read**.

If you only ever want to read, a token without write scope is a genuinely
useful safety measure — mrman surfaces the forge's refusal rather than
pretending the submit worked.

## Open a merge request

```sh
mrman pr 125                                  # from inside the checkout
mrman pr owner/repo#125                        # addressed explicitly
mrman pr github.com/owner/repo#125             # host-qualified
mrman pr https://github.com/owner/repo/pull/125
mrman                                          # then Tab to Merge Requests
```

mrman detects the repository from your remotes, so a bare number is usually
all you need. `github.com` is claimed by the GitHub driver automatically —
there is no configuration required for github.com at all.

## What you get

Everything. GitHub is the reference implementation:

| | |
|---|---|
| Draft reviews | ✓ pending review, published from GitHub's UI or a later submit |
| Approve / request changes | ✓ |
| Review summaries | ✓ rendered inline alongside threads |
| Thread resolution | ✓ real, via GraphQL — resolved threads hide by default |
| Outdated threads | ✓ marked |
| Multi-line comments | ✓ true ranges, no downgrade |
| Per-commit range diff | ✓ `(` / `)` narrows to one commit |
| Atomic submit | ✓ one API call — a review either posts entirely or not at all |
| Review-requested filter | ✓ `r` in the MR list |
| Commit-scoped reviews | ✓ mrman knows which commits your last review covered |

**Atomic submit** is worth calling out because the other forges do not have it.
mrman sends the body and every inline comment as a single
`POST /pulls/{n}/reviews`. There is no partial-failure state to reason about:
either the whole review lands or nothing does.

## Submit

```
:submit                  # picker
:submit comment
:submit approve
:submit request-changes
:submit draft
```

`draft` creates a GitHub **pending review** — the comments are attached but
unpublished, visible only to you until you submit it. Useful for building a
review over several sittings.

Submitted comments get a `[TYPE]` prefix from their comment type
(`[forge] comment_type_prefix = false` turns that off), and the review body is
rendered from a [template](../../reference/templates/) you can replace.

## Reading the merge request

The forge's existing review threads and review summaries render inline,
read-only. Resolved threads are hidden until `:comments all`; `:comments hide`
removes them entirely. `:e` refetches, and if the merge request advanced to a
new head, mrman opens the review for that head rather than moving your anchors.

On a multi-commit merge request, mrman marks the commits your last review
already covered and preselects what landed since — so re-reviewing after a
force-push or a follow-up commit starts on the new work.

## Going deeper

[Testing Against a Real Forge](../../contributing/live-testing/) walks through
setting up a scratch merge request and exercising every driver method against
it, with real transcripts. It is written for contributors, but it is also the
most concrete description of what mrman does over the wire.
