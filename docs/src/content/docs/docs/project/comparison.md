---
title: Comparisons
description: How mrman differs from tuicr, from reviewing in your forge's web UI, and from gh pr review.
---

## mrman vs the web UI

The forge's review UI is good at what it is for: linking to a discussion,
@-mentioning, and being the record of what was decided. mrman is better at the
part where you actually read the code.

| | mrman | Web UI |
|---|---|---|
| Diff | One continuous diff across all files | Per-file, collapsed by default |
| Navigation | vim keys with count prefixes, hunk and file jumps, search | Scroll and click |
| Hidden context | `Enter` expands, fetched on demand | Click, per gap |
| Progress | Reviewed marks per file **and** hunk, persisted | "Viewed" checkbox per file |
| Interruption | Session resumes exactly where you left it | Browser tab and hope |
| Where comments go | Drafted locally, submitted in one deliberate act | Live, in a text box |
| Four forges | One interface | Four different interfaces |

The last row is the one people underrate. If your work spans GitHub and an
internal GitLab, or GitHub and Azure DevOps, mrman is the same keys and the same
review model on both.

Where the web UI still wins: threaded back-and-forth discussion, notifications,
and being the durable public record. mrman renders existing threads read-only
and does not try to replace that — it never replies, resolves or rewrites them.

## mrman vs `gh pr review`

`gh pr review` is a submission mechanism, not a reading environment. You still
read the diff somewhere else, and inline comments mean composing JSON:

```sh
# gh: the inline-comment path
gh api repos/O/R/pulls/1/reviews -X POST --input review.json
```

versus putting the cursor on the line and pressing `c`.

`gh` also only speaks GitHub. mrman talks to four forges through their APIs and
does not shell out to `gh` or `glab` at all — the one thing it may borrow from
`gh` is a token, and only for GitHub hosts.

If you already use `gh` for everything else, nothing here conflicts: mrman reads
`gh`'s token by default, so there is no separate auth step.

## mrman vs tuicr

mrman is a Go reimplementation of [tuicr](https://github.com/agavra/tuicr)
(Rust), built on [Bubble Tea](https://github.com/charmbracelet/bubbletea). It
targets feature parity — tuicr's own test suite is ported throughout as the
behavioral spec — and adds a few things.

### What mrman adds

| | |
|---|---|
| **Four forges** | GitHub, GitLab, Azure DevOps and Forgejo, through their APIs rather than by shelling out to `gh` and `glab`. GitLab and Forgejo are [experimental](../../reference/forge-capabilities/#support-levels) |
| **CUE-validated configuration** | Mistakes degrade to precise warnings instead of crashing or being silently ignored |
| **Templatable markdown** | Both the exported notes and the submitted review body are Go templates you can override |
| **Agent collaboration** | A JSON CLI for reading and writing a live review session, with a human-held submit interlock |
| **`Space` expands context gaps** | Alongside tuicr's `Enter` |
| **Built-in comment types** | NOTE, ISSUE, SUGGESTION, PRAISE out of the box; tuicr ships only the untyped type, so classification is invisible until configured |

### Deliberate differences

- **No self-updater.** Use your package manager or `go install`; there is no
  `:update` and no startup version check.
- **No Mercurial backend.** git, Jujutsu, `--file` and `-A` are supported.

Both are choices, not gaps. A TUI that phones home on startup and rewrites its
own binary is a TUI with opinions about your package manager.

### If you are coming from tuicr

Your muscle memory transfers. The keys are the same, the review model is the
same, and the exported markdown is byte-for-byte tuicr's (with the tool word
renamed in the heading). `backend` in the config file is accepted and ignored —
mrman uses the git CLI by design — so an old config will not error at you.

## When mrman is the wrong tool

Worth being honest about:

- **You want threaded discussion.** mrman writes a review; it does not carry a
  conversation. Reply in the web UI.
- **You review mostly rendered artifacts** — screenshots, notebooks, design
  files. A terminal diff is not the right lens.
- **You are not comfortable in vim keys.** Everything has an arrow-key or mouse
  equivalent, but the tool is shaped around modal navigation and you will feel
  it.
