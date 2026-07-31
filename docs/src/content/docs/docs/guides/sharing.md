---
title: Sharing a Review
description: Getting a finished review to the person who has to act on it — by forge, by markdown export, or by handing it to an agent.
---

A review nobody else reads is just notes. mrman has three ways to hand one
over, and which you use is decided by what you reviewed rather than by
preference.

| You reviewed | Send it with | They get |
|---|---|---|
| A pull request (`mrman pr …`) | **Submit to the forge** | Inline comments on the PR, in their normal review flow |
| Local commits or the working tree | **Markdown export** | A document they can read anywhere — chat, email, an issue |
| A posted patch (`mrman --patch …`) | **A quoted-diff reply** | The diff back with your comments inline, ready to send |
| Anything, for an AI agent on this machine | **The session slug** | Live JSON access to your comments |

There is no fourth option where you send someone a session file and they open
it — see [Session files are not a transport](#session-files-are-not-a-transport).

## Pull requests: submit to the forge

This is the path with no seams in it. Review with `mrman pr 125`, run
`:submit`, pick an action, and your comments land as a native review on GitHub,
GitLab, Azure DevOps or Forgejo. The author sees them where they already look,
threaded against the right lines, and can reply. `:submit comment`,
`:submit approve`, `:submit request-changes` and `:submit draft` skip the
picker.

Everything about it is covered in [How PR Review
Works](../pull-requests/#submitting). The one thing to carry over here: a
comment whose anchor no longer checks out is deliberately kept *out* of the
inline set and put in the review body instead, so a stale note never lands on
code it was not written about.

## Local reviews: export markdown

A review of your working tree or a commit range has no forge to post to. Export
it instead:

```
y            # copy the whole review to the clipboard as markdown
:export      # the same thing, by name
:clip        # ditto
```

`y` with a selection active yanks the selection instead; it only exports when
nothing is selected. Starting mrman with `--stdout` sends the export to stdout
rather than the clipboard, printed once mrman exits.

Note that this is still an interactive flow — you export from inside the TUI,
and mrman renders to your terminal, so `--stdout` is not a headless dump. When
you want a review out of mrman without opening it, use the JSON CLI:

```sh
mrman review list --repo .                # find the session
mrman review comments --session <slug>    # its comments, as JSON
```

The output is a markdown document addressed to a reader — the default template
opens *"I reviewed your code and have the following comments. Please address
them."* — followed by the session slug, an optional comment-type legend, and
every comment as a numbered entry with its file and line:

```markdown
## Session: infrashift/mrman@main/worktree/abc1234

I reviewed your code and have the following comments. Please address them.

Comment types: NOTE (…), ISSUE (…), SUGGESTION (…), PRAISE (…)

## Local mrman Comments

1. **[ISSUE]** `src/cache.go:75` - This drops the error on a full disk.
2. **[NOTE]** `src/config.py:8` - Worth a comment saying why 30s.
```

Numbering is continuous across the whole review, so "point 3" is unambiguous
when they reply. Deleted-side lines are marked `path:~42`, and ranges render as
`path:10-18`.

Both the template and the legend are yours to change — see
[Templates](../../reference/templates/) for the fields available, and
`export_legend = false` if the legend is noise for your team.

### Exporting from a remote machine

The clipboard copy works over SSH, tmux and Zellij: mrman falls back to an OSC
52 escape sequence, which asks *your* terminal to take the text rather than the
remote machine's clipboard daemon. Reviewing on a server and pasting into a
local chat window works, provided your terminal honours OSC 52 — most do, some
need it enabled. See [Terminal Setup](../terminals/).

If it does not, `--stdout` prints the export to the terminal when you quit so
you can copy it by hand, and `mrman review comments --session <slug>` gets you
the same review as JSON without opening the TUI at all.

### Writing for a reader

Two things make an exported review land better:

- **A review-level comment** (`<leader>c`, from the comments panel) is not
  attached to any file, so it renders first, before the per-line notes. Use it
  for the verdict — *"the caching change looks right, but I have two concerns
  about eviction"* — so the reader gets your conclusion before your list.
- **Comment types** (`Tab` while composing) mark each note as NOTE, ISSUE,
  SUGGESTION or PRAISE. The recipient can tell "this blocks" from "here is a
  thought" without inferring it from your tone, and the legend explains the
  vocabulary to someone who has never used mrman.

## Patches: reply with the diff quoted

A review of a posted patch has its own artifact: `:patch` renders the diff back
with your comments interleaved beneath the lines they refer to, threaded
against the original when it carried a `Message-Id`. That is what a mailing
list expects, and markdown notes are not a substitute for it.

[Reviewing Patches](../patches/) covers the mode;
[Patch Workflows](../patch-workflows/) walks the exchange end to end.

## Agents: hand over the slug

On the same machine, an agent reads and writes your session directly. Print the
slug on startup, then let it work:

```sh
mrman review comments --session <slug>    # read the review as JSON
mrman review watch --session <slug>       # stream changes as they happen
mrman review add --session <slug> …       # write a comment back
```

This is a collaboration channel, not a delivery one — the agent is a
participant in the review rather than its audience. [Agent
Collaboration](../agents/) covers it properly, including the submit grant that
lets an agent post to a forge on your behalf.

## Session files are not a transport

Sessions live in `~/.local/share/mrman/reviews/sessions/` as JSON, which makes
copying one to a colleague look tempting. It does not work the way you would
hope:

- A session is keyed by the **canonical path of your checkout**. On their
  machine the path differs, so it will not resolve to their clone and will not
  appear in their `mrman review list`.
- It records **your** HEAD. Their checkout is somewhere else in history.
- It stores comments against line numbers plus a snapshot of each anchored
  line, which is enough for mrman to re-check anchors as *your* diff moves —
  not to transplant them onto a different working tree.

The one thing that does work is reading a copied file directly, since
`--session` accepts a path as well as a slug:

```sh
mrman review comments --session ./their-review.json
```

That prints the comments as JSON. It is a fine way to inspect a session someone
sent you for debugging, and a poor way to run a review. For actual delivery,
export markdown or submit to the forge.
