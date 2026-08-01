---
title: Forgejo & Gitea
description: Review merge requests on self-hosted Forgejo or Gitea — tokens, api_base, and the places where mrman approximates what the API does not report.
sidebar:
  order: 5
---

:::caution[Experimental]
The Forgejo/Gitea driver is implemented and unit-tested, but has not yet been
exercised against a live instance the way the GitHub one has. Everything on
this page describes what it does; what it has not had is a real server
disagreeing with it. See [support levels](../../reference/forge-capabilities/#support-levels).
:::

Forgejo and Gitea share an API, so mrman serves both with one driver. Set
`forge = "forgejo"` or `forge = "gitea"` — the names are interchangeable.

For [Codeberg](../codeberg/), the public Forgejo instance, there is nothing to
configure at all.

## Configure the host

A self-hosted instance needs a config entry so mrman knows what it is:

```toml
# ~/.config/mrman/config.toml
[[forge.hosts]]
host = "git.mycorp.com"
forge = "forgejo"                 # or "gitea"
token_cmd = "pass show forgejo-token"
# api_base = "https://git.mycorp.com/api/v1"   # only if non-standard
# ca_file = "/etc/ssl/corp-root.pem"
```

`api_base` is usually unnecessary — Forgejo and Gitea serve their API under
`/api/v1` at the host root, which mrman derives. Set it for an instance behind
a subpath.

### Tokens

Generate one under *Settings → Applications → Access Tokens*, with
**repository** read and write permission.

For self-hosted instances, put it in `token` or `token_cmd`. The
`FORGEJO_TOKEN` / `CODEBERG_TOKEN` environment variables are read for
`codeberg.org` only, deliberately, so a token for public work never travels to
your own instance.

A host entry may set `token` **or** `token_cmd`, never both — the schema rejects
that rather than picking one silently.

## Open a merge request

```sh
mrman pr 12                                   # from inside the checkout
mrman pr owner/repo#12
mrman pr git.mycorp.com/owner/repo#12          # host-qualified
mrman pr https://git.mycorp.com/owner/repo/pulls/12
mrman                                         # then Tab to Merge Requests
```

Note the URL path is `/pulls/N` — plural — which is how mrman tells a Forgejo
URL from a GitHub `/pull/N` one by shape alone. A `[[forge.hosts]]` entry
overrides the shape guess anyway, so a host you configured is never
misidentified.

## What works, and where mrman approximates

| | |
|---|---|
| Draft reviews | ✓ Forgejo `PENDING` review state |
| Approve / request changes | ✓ |
| Review summaries | ✓ review bodies render inline |
| Thread resolution | ~ **approximated** — see below |
| Outdated threads | ~ **approximated** — see below |
| Multi-line comments | **—** downgraded, see below |
| Per-commit range diff | **—** local checkout only |
| Atomic submit | ✓ one-shot `CreatePullReview` |
| Review-requested filter | ✓ `r` in the MR list |

### Threads are synthesized

Forgejo's API returns review comments, not threads. mrman groups comments into
threads itself, which works well — but two pieces of thread state are inferred
rather than reported:

- **Resolved** — a thread counts as resolved when any of its comments carries a
  resolver. That matches the common case, and it can differ from what the web UI
  shows in unusual ones.
- **Outdated** — a thread counts as outdated when both of its line positions are
  zero, or the commit does not match. Again, a heuristic.

Practically: `:comments unresolved` / `all` / `hide` all work, and if a thread's
resolved state looks wrong, believe the web UI. This is why the [capability
table](../../reference/forge-capabilities/) marks these as approximations
rather than checkmarks — mrman would rather tell you the difference than let you
assume.

### Multi-line comments downgrade

Forgejo accepts a single `old_position` or `new_position`, not a span. A range
comment therefore posts on its **end line**, with the span preserved in the
body:

```
Lines 40–47: This loop re-reads the file on every iteration.
```

Nothing is lost and nothing fails — the next reviewer still sees what the
comment covered. mrman does this automatically; you write the range comment
normally.

### Commit scoping needs a local checkout

Forgejo has no compare-diff API. mrman falls back to running `git diff
start..end` in your **local checkout** when both SHAs are present there — so
if you are reviewing a merge request from a clone that has the commits, `(` and
`)` work. Reviewing purely remotely, they do not, and mrman says so instead of
producing a wrong diff.

Because `CommitRangeDiff` is reported as unavailable, the app does not offer
commit scoping in the first place; the local fast path is a bonus when it
applies.

## Submitting

```
:submit                  # picker
:submit comment
:submit approve
:submit request-changes
:submit draft
```

Forgejo has **atomic submit**: mrman sends the body and every inline comment in
one `CreatePullReview` call, so a review either lands entirely or not at all.
There is no partial-failure state to reconcile — the same guarantee GitHub
gives, which GitLab and Azure DevOps cannot.

`:submit draft` creates a `PENDING` review, visible only to you until you
publish it.

## Reading the merge request

Existing review comments and review bodies render inline, read-only. Resolved
threads hide until `:comments all`. `:e` refetches.

`r` in the merge-request list toggles between everything open and what is
waiting on your review, using Forgejo's issue search with
`review_requested=true`.
