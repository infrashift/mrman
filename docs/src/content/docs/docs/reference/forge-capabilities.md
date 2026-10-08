---
title: Forge Capabilities
description: What each forge can do, what it cannot, and exactly what mrman does instead — the canonical table, straight from the drivers.
---

## Support levels

Capability and maturity are different questions, and this page answers both.
The table below says what a driver *implements*; this section says how hard it
has been leaned on.

| Level | Forges | What it means |
|---|---|---|
| Supported | GitHub, GitHub Enterprise Server, Azure DevOps, GitLab (self-managed), Codeberg | Exercised end to end against a live instance, not only against recorded API shapes. GitLab: CE 19.3.2, 2026-09-25; gitlab.com not yet. Codeberg: Forgejo 16, 2026-10-08. |
| **Experimental** | Self-hosted Forgejo & Gitea | The Forgejo driver passed against Codeberg; other Forgejo versions, and Gitea, have not been exercised. |

Experimental does **not** mean unfinished — read the table below for what each
driver actually does, which in Forgejo's case is most of it. It
means the API shapes come from documentation and recorded fixtures rather than
from a real server answering back, so a mismatch between what a forge documents
and what it returns would reach you before it reaches us.

A rejected submit is reported as a failure and your comments stay unlocked in
the local session, so nothing is lost. On **GitLab**, whatever its level, the
caveat below about [atomic submit](#atomic-submit) applies with more force than
usual: there is no single-call endpoint, so a failure partway leaves some
comments already posted. mrman names which ones — read that report before
re-submitting. The live GitLab run hit exactly this: four of six comments landed
before a fifth was refused, and the report named the four.

If you run mrman against one of these, [a bug report](https://github.com/infrashift/mrman/issues)
is the thing that moves it off this list.

## The table

Capabilities are checked **before** mrman acts, so nothing fails halfway
through a submit: an unsupported operation is refused with a reason rather than
attempted. Note the check happens when you choose an action, not when the
picker is built — the submit picker always lists all four events, and picking
one the forge cannot do is refused before any request goes out.

This table is the canonical one. Each row corresponds to a field the driver
declares, so it does not drift from the code.

| | GitHub | GitLab | Forgejo / Gitea ⚗️ | Azure DevOps |
|---|:--:|:--:|:--:|:--:|
| Draft reviews | ✓ | ✓ | ✓ | **—** |
| Approve | ✓ | ✓ | ✓ | ✓ (vote) |
| Request changes | ✓ | ✓ (15.11+) | ✓ | ✓ (vote) |
| Review summaries | ✓ | **—** | ✓ | **—** |
| Review threads | ✓ | ✓ | ✓ (synthesized) | ✓ |
| Thread resolution | ✓ | ✓ | ~ approximated | ✓ |
| Outdated threads | ✓ | **—** | ~ approximated | **—** |
| Multi-line comments | ✓ | ✓ | **—** downgraded | ✓ |
| Per-commit range diff | ✓ | ✓ | local checkout only | **—** |
| Atomic submit | ✓ | **—** | ✓ | **—** |
| Review-requested filter | ✓ | ✓ | ✓ | ✓ |
| Commit-scoped reviews | ✓ | ✓ | ✓ | **—** |

GitHub Enterprise Server matches the GitHub column; Codeberg matches the
Forgejo one. ⚗️ marks an experimental driver, as defined above.

## What each gap actually does

### Draft reviews

A draft is an unpublished review — comments attached to the merge request but
visible only to you until you submit.

- **GitHub**: a pending review.
- **GitLab**: draft notes. mrman creates them and stops; you publish from
  GitLab's "Submit review" button.
- **Forgejo**: a `PENDING` review.
- **Azure DevOps**: no such concept. The picker still lists `Draft (pending
  review)`, but choosing it — or running `:submit draft` — is refused with
  *"This forge does not support draft reviews"* before any request is made.

### Approve and request changes

On GitHub, GitLab and Forgejo these are review states. On **Azure DevOps** they
are numeric **votes** on the viewer's own reviewer record: `+10` for approve,
`-10` for request changes. Same effect as clicking the button in the web UI,
including any branch policies keyed off it.

On **GitLab**, request-changes needs the `mergeRequestRequestChanges` GraphQL
mutation, which requires **GitLab 15.11 or newer**. Older instances should use
`:submit comment`.

### Review summaries

The forge's own review-level bodies, rendered in the overview above the diff.

GitLab and Azure DevOps have no review object to carry one. mrman shows their
general, file-less discussions there instead, so review bodies posted by
anyone, mrman included, appear in the same place: a general MR note on GitLab,
a context-less thread on Azure DevOps. They carry no review state.

### Thread resolution and outdated state

**GitHub** reports both properly (resolution via GraphQL). **GitLab** and
**Azure DevOps** report resolution but not outdated state.

**Azure DevOps** models resolution as a seven-value disposition rather than a
boolean — `active`, `pending`, `fixed`, `wontFix`, `closed`, `byDesign`,
`unknown`. mrman reads all seven. The four terminal ones count as resolved for
filtering, while `active`, `pending` and a missing status stay open — and the
thread badge names the actual disposition, so `(won't fix)` is distinguishable
from `(resolved)`. See [Thread
dispositions](../../forges/azure-devops/#thread-dispositions), which records
what was verified against a live merge request.

The other three forges have nothing richer than resolved/unresolved to report,
so their threads keep the plain `(resolved)` badge.

**Forgejo** reports neither, because its API returns comments rather than
threads. mrman groups them into threads itself and infers:

- *resolved* — any comment in the group carries a resolver
- *outdated* — both line positions are zero, or the commit does not match

These are good heuristics, not ground truth. `:comments unresolved` / `all` /
`hide` all work; if a resolved state looks wrong, believe the web UI.

### Multi-line comments

**Forgejo** accepts a single `old_position` or `new_position`, not a span. A
range comment **downgrades**: it posts on its end line with the span preserved
in prose.

```
Lines 40–47: This loop re-reads the file on every iteration.
```

Nothing is lost and nothing fails. You write the range comment normally.

### Per-commit range diff

This is what `(` and `)` use to narrow a multi-commit review to one commit.

- **GitHub, GitLab**: served by the forge.
- **Forgejo**: no compare-diff API. mrman falls back to `git diff start..end`
  in your **local checkout** when both SHAs are present there. Reviewing purely
  remotely, commit scoping is unavailable.
- **Azure DevOps**: unavailable. The commit strip is still shown, and `(` / `)`
  are still accepted, but the diff does not narrow — mrman warns *"This forge
  cannot diff a commit range — showing the whole merge request"* and keeps
  showing everything.

Where it is missing, mrman **widens back** to the whole merge request rather than
failing.

### Atomic submit

Whether a review posts in one API call.

**GitHub and Forgejo**: yes. One call, so a review either lands entirely or not
at all — there is no partial state to reason about.

**GitLab and Azure DevOps**: no such endpoint exists, so mrman posts a sequence.
GitLab: the body as a general note, each comment as a positioned discussion,
then the event. Azure DevOps: the body as a context-less thread, each comment as
its own file-anchored thread, then the vote.

If a step fails partway, mrman reports a **partial submit** naming exactly which
comments made it, and marks only those as submitted locally. The rest stay
editable, so retrying does not double-post what already landed. This is the
practical reason to prefer smaller reviews on these two forges.

One nuance on Azure DevOps: a *pure vote* submit — no body, no comments — that
fails, fails outright, because nothing landed worth keeping.

### Commit-scoped reviews

Whether the forge records which commit a review covered, which is what lets
mrman mark the commits your last review already saw and preselect what landed
since. **Azure DevOps** does not, so a re-review there starts on the whole pull
request.

Azure DevOps compensates elsewhere: mrman attaches iteration context and the
file's change-tracking id to each comment, which is what keeps anchors attached
to the right line across new iterations.

### Review-requested filter

Every forge supports it. `r` in the merge-request list toggles between everything
open and what is waiting on your review.

## Where this comes from

Each driver declares its own capabilities:

| Forge | Source |
|---|---|
| GitHub | `internal/forge/githubf/githubf.go` |
| GitLab | `internal/forge/gitlabf/gitlabf.go` |
| Forgejo / Gitea | `internal/forge/forgejof/forgejof.go` |
| Azure DevOps | `internal/forge/azdof/azdof.go` |

If this page and a driver disagree, the driver is right — please
[open an issue](https://github.com/infrashift/mrman/issues).
