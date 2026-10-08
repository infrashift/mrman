---
title: GitLab
description: Review GitLab merge requests from the terminal — gitlab.com and self-managed, nested subgroups, draft notes, approvals, and what GitLab's API cannot do.
sidebar:
  order: 3
---

:::note[Verified against a live instance]
The GitLab driver was exercised end to end against a self-managed **GitLab CE
19.3.2** (Free tier) on 2026-09-25. The run covered browsing, inline comments of
every anchor kind, draft notes, request changes, approve, and an AI agent
submitting through the grant. Every outcome was read back from GitLab itself.
The run found two defects, both fixed before this note was written: a range
comment ending on an unchanged line was refused, and the TUI lost its connection
to the forge after a moved-head reload.

**gitlab.com** passed the same run on 2026-10-08, with every step read back:
comments of every anchor kind, a draft, request changes, the moved-head
guard, approve, and the agent interlock. It also covered a file over
gitlab.com's diff limits and a refused (repeat) approval. A self-managed
instance behind TLS with `ca_file` has not been exercised yet. See [support
levels](../../reference/forge-capabilities/#support-levels) and the
[transcript](../../contributing/live-testing/#gitlab-a-self-managed-instance).
:::

mrman reviews GitLab **merge requests** with the same interface it uses on
every other forge. It talks to the GitLab REST API directly, with
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

GitLab's newer **fine-grained** personal access tokens grant permissions one
by one instead. One without `Project: Read` and `User: Read` cannot open a
merge request at all: GitLab answers `403 insufficient_granular_scope`. A
classic token with `api` is the simplest choice.

## Open a merge request

```sh
mrman pr 42                                        # from inside the checkout
mrman pr group/project!42                          # ! reads naturally here
mrman pr group/project#42                          # # works too
mrman pr gitlab.mycorp.com/group/sub/project!42    # host-qualified
mrman pr https://gitlab.com/group/sub/project/-/merge_requests/42
mrman                                              # then Tab to Merge Requests
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
Existing threads render inline as you would expect. General merge-request
notes, the ones with no diff position, appear in the overview above the diff,
where GitHub's review summaries go. Your own review body posts as one of those
notes, so it shows up there too.

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

mrman never resends a write on its own. A `5xx` from a proxy in front of
GitLab can arrive after GitLab already created the note, so only reads are
retried on server errors. A rate-limit `429`, which GitLab answers without
acting, is retried for any request.

### Drafts

`:submit draft` creates GitLab **draft notes** — the pending-review primitive —
and deliberately stops short of publishing. You finish from GitLab's own
"Submit review" button. mrman creates the pending state; GitLab owns publishing
it.

### Request changes

GitLab has no REST endpoint for "request changes", so mrman uses the
`mergeRequestRequestChanges` GraphQL mutation. **This requires GitLab 15.11 or
newer.** On an older instance, use `:submit comment` and say so in the body.
Verified on CE 19.3.2, where GitLab then reports the reviewer's state as
`REQUESTED_CHANGES`. Only an assigned reviewer has been exercised.

### After the author pushes

mrman refuses to submit on a head that moved since you opened the review, and
asks you to reload: `r` on the submit confirmation, or `:e`. The check is only
as current as GitLab's own view of the merge request, and GitLab updates it
**asynchronously** after a push. That took about 1-6 s on the verified
instance. A submit inside that window can still land on the previous head.

A review is a session per head commit. Reloading onto a new head retires the
old head's session, with its comments, and opens one for the new head; an
agent-submit grant from `--auto` carries over to it. An agent's
`--session <slug>` keeps working, because a PR slug names the merge request,
not the head, and resolves to the newest session.

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
eligibility. GitLab answers a refused approval (you already approved, or the
project forbids authors approving their own merge requests) with `401`, so
mrman reports it as a refused approval rather than as a bad token.

## Reading the merge request

Existing discussions render inline, read-only, resolved ones hidden until
`:comments all`, and a multi-line discussion keeps its whole range. `:e`
refetches. `(` / `)` narrows to a single commit, which GitLab supports
properly. The header writes the merge request as `#N`, like every other
forge, rather than GitLab's `!N`.

A file over the instance's diff limits arrives from GitLab without its diff;
mrman shows it as *(file too large to display)* rather than as unchanged.

"Commits since your last review" works from your approvals and comments, each
placed on the merge request version that was current when you made it.
GitLab keeps an approval across later pushes unless the project resets
approvals, so an approval given before a push does not count as having
reviewed what came after.

`r` in the merge-request list toggles between everything open and what is
waiting on your review.
