---
title: GitLab
description: Review GitLab merge requests from the terminal — gitlab.com and self-managed, nested subgroups, draft notes, approvals, and what GitLab's API cannot do.
sidebar:
  order: 3
---

:::caution[Experimental]
The GitLab driver is implemented and unit-tested, but has not yet been
exercised against a live instance the way the GitHub one has. Everything on
this page describes what it does; what it has not had is a real server
disagreeing with it. See [support levels](../../reference/forge-capabilities/#support-levels).
:::

mrman reviews GitLab **merge requests** with the same interface it uses for
pull requests everywhere else. It talks to the GitLab REST API directly, with
one GraphQL call for "request changes" — it does not shell out to `glab`.

## Authenticate

### gitlab.com

```sh
export GITLAB_TOKEN=glpat-...
```

`GITLAB_TOKEN` is read **only** for `gitlab.com`, so it never leaks to a
self-managed instance.

### Self-managed

Self-managed hosts need a config entry, because mrman cannot guess that
`git.mycorp.com` is GitLab:

```toml
[[forge.hosts]]
host = "gitlab.mycorp.com"
forge = "gitlab"
token_cmd = "pass show gitlab-token"
# api_base = "https://gitlab.mycorp.com/api/v4"   # only if non-standard
# ca_file = "/etc/ssl/corp-root.pem"
```

`api_base` is usually unnecessary — GitLab's API lives under `/api/v4` at the
host root and mrman derives it. Set it only for an instance served from a
subpath or a separate API hostname.

Once the entry exists, configuration wins over URL shape: that host is GitLab
regardless of what the path looks like.

### Token scopes

`api` scope. A read-only `read_api` token is enough to browse and review; you
need `api` to post comments or approve.

## Open a merge request

```sh
mrman pr 42                                        # from inside the checkout
mrman pr group/project!42                          # ! reads naturally here
mrman pr group/project#42                          # # works too
mrman pr gitlab.mycorp.com/group/sub/project!42    # host-qualified
mrman pr https://gitlab.com/group/sub/project/-/merge_requests/42
mrman                                              # then Tab to Pull Requests
```

**Nested subgroups work at any depth.** `group/sub/subsub/project!42` and the
matching `/-/merge_requests/N` URL both parse — the last path segment is the
project and everything before it is the namespace.

## What works, and what GitLab cannot do

| | |
|---|---|
| Draft reviews | ✓ GitLab draft notes |
| Approve / request changes | ✓ |
| Review summaries | **—** GitLab has no review-summary object |
| Thread resolution | ✓ real; resolved discussions hide by default |
| Outdated threads | — not reported |
| Multi-line comments | ✓ true ranges |
| Per-commit range diff | ✓ `(` / `)` narrows to one commit |
| Atomic submit | **—** see below |
| Review-requested filter | ✓ `r` in the MR list |

### No review summaries

GitLab does not model "a review" as an object with a body the way GitHub does.
Existing threads render inline as you would expect, but there is no separate
summary block above them, because there is nothing on the GitLab side to read.

Your own review body still posts — as a general merge-request note.

### No atomic submit

This is the one behavioral difference worth understanding before you submit.
GitLab has no single endpoint that takes a review, so mrman posts a sequence:

1. The review body, as a general MR note.
2. Each inline comment, as a positioned discussion.
3. The event — approve, or the request-changes mutation.

If a step fails partway, mrman **does not** lie about it. It reports a partial
submit naming exactly which comments made it to GitLab, and marks only those as
submitted locally. The rest stay editable so you can retry them without
double-posting the ones that landed.

### Drafts

`:submit draft` creates GitLab **draft notes** — the pending-review primitive —
and deliberately stops short of publishing. You finish from GitLab's own
"Submit review" button. mrman creates the pending state; GitLab owns publishing
it.

### Request changes

GitLab has no REST endpoint for "request changes", so mrman uses the
`mergeRequestRequestChanges` GraphQL mutation. **This requires GitLab 15.11 or
newer.** On an older instance, use `:submit comment` and say so in the body.

## Submitting

```
:submit                  # picker
:submit comment
:submit approve
:submit request-changes
:submit draft
```

Approve calls the merge-request approvals endpoint, so it respects your
project's approval rules — including "approvals required" counts and
eligibility. If your project forbids self-approval, GitLab refuses and mrman
shows you the refusal.

## Reading the merge request

Existing discussions render inline, read-only, resolved ones hidden until
`:comments all`. `:e` refetches. `(` / `)` narrows to a single commit, which
GitLab supports properly.

`r` in the merge-request list toggles between everything open and what is
waiting on your review.
