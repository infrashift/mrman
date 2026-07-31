---
title: Patch Workflows
description: End-to-end walkthroughs for receiving, sending, applying and exchanging patches — and exactly which part of each is mrman's job.
---

Four walkthroughs, organised by the role you are playing, because that is what
decides what you actually do. [Reviewing Patches](../patches/) is the reference
for the mode itself; this page is the surrounding work.

| You are | Walkthrough |
|---|---|
| A reviewer, someone sent you a series | [Reviewing a series you received](#1-reviewing-a-series-you-received) |
| A contributor, about to post your work | [Sending a series](#2-sending-a-series) |
| A maintainer, deciding what lands | [Applying a series you reviewed](#3-applying-a-series-you-reviewed) |
| Either side of an airgap | [Exchanging a review across an airgap](#4-exchanging-a-review-across-an-airgap) |

:::note[What mrman does and does not do]
mrman **reviews** patches. It does not create them and it does not apply them —
`git format-patch` and `git am` do, and they do it well. Two of the
walkthroughs below are therefore mostly git, with mrman as the step in the
middle where a human reads the change. That division is deliberate: a review
tool that also rewrites your tree is a review tool you have to trust with your
tree.
:::

---

## 1. Reviewing a series you received

The core case. A series arrives by mail; you read it, comment on it, and reply.

### Get the series

If you use [`b4`](https://b4.docs.kernel.org/), which is the usual tool for
this on kernel lists:

```sh
b4 mbox -o ~/inbox 20260101120000.12345-1-dev@example.org
```

Or save the thread from your mail client into `~/inbox` as `.mbox`. Anything
that produces a file works — mrman does not care how it got there.

### Open it

```sh
mrman --patch ~/inbox/v3-net-fix.mbox
```

Or browse: `mrman` anywhere, then `:patches`, point it at your inbox, and pick
a row.

### Read it patch by patch

The series appears as a commit strip. Walk it with `(` and `)`.

Press `Space` on a strip row to narrow to that patch alone. Do this — it is the
difference between reviewing a series and reviewing a pile of diffs. With one
patch selected you get its changelog as a reviewable file (`Commit Message
(2/5)`), and every comment you write is scoped to that patch.

### Comment

The four scopes behave as they do everywhere: `c` on a line, `v` then `c` for a
range, `C` for the file, `<leader>c` for the review as a whole.

Two habits worth forming for a mailing-list review:

- **Use the review-level comment for your verdict.** It renders first in the
  reply, so the author reads your conclusion before your list of nits.
- **Use comment types.** `Tab` while composing cycles NOTE, ISSUE, SUGGESTION
  and PRAISE. The author can tell "this blocks" from "here is a thought"
  without inferring it from your tone.

Mark patches reviewed with `r` as you go; the state persists, so a long series
can be read across several sittings.

### Reply

```
:patch
```

The reply lands on your clipboard: the diff quoted with `> `, your comments
interleaved, tabs intact, threaded against the original when the series carried
a `Message-Id`. Paste it into your mail client and send.

If you would rather have it as a file, start mrman with `--stdout`: `:patch`
then prints the reply to your terminal when you quit, instead of copying it.
Note that this is not a shell pipeline — mrman draws its interface on stdout
too, so redirecting the whole command captures the interface along with the
reply.

### When v2 arrives

Save it over the old file and reopen. Because a patch session is identified by
the artifact's bytes, a changed file is a new review — deliberately, since v2 is
a different change. To carry your v1 notes forward, keep both files and read
them side by side; mrman does not (yet) match a v2 to its v1.

---

## 2. Sending a series

You wrote the code. mrman's job here is to make you the first reviewer of it —
which is the cheapest review anyone will ever do.

### Build the series

```sh
git format-patch --thread --cover-letter -3 -o /tmp/outgoing
```

`--thread` matters if you want to be able to reply to yourself later: plain
`format-patch` writes no `Message-Id`, so nothing can thread against it.

### Review your own work

```sh
git format-patch --thread --stdout -3 > /tmp/outgoing.mbox
mrman --patch /tmp/outgoing.mbox
```

Reading the series as one mbox is the better way round: you see it as the list
will, cover letter and all. `--patch` takes a single file, so review individual
`.patch` files one at a time, or point `:patches` at `/tmp/outgoing` and pick
rows from the list.

Read it the way a reviewer will: narrow to each patch with `Space`, read the
changelog as its own file, walk the diff. You are looking for the things that
are invisible while writing and obvious while reading — a debug print left in,
a commit message that describes the old approach, a patch that does two things.

This is the step people skip, and it is why "please ignore patch 3, I'll
resend" exists.

### Fix and rebuild

Amend or rebase as needed, then regenerate:

```sh
git rebase -i HEAD~3
git format-patch --thread --cover-letter -3 -o /tmp/outgoing
```

Reopen the new file in mrman. It is a new session, which is correct: you
changed the series.

### Send

```sh
git send-email /tmp/outgoing/*.patch
```

mrman has no part in this step and should not.

---

## 3. Applying a series you reviewed

You maintain the tree. The review is the gate; `git am` is the gate opening.

### Review first

```sh
mrman --patch ~/inbox/v3-net-fix.mbox
```

Work through it as in walkthrough 1. What you are deciding is narrower than a
contributor's review: does this belong in the tree, in this shape, now.

Reviewed marks are useful here as a checklist — `r` per file, `R` per hunk —
because a maintainer's read is often "which parts have I actually looked at".

### Decide

If it needs work, `:patch` and reply. Nothing gets applied.

If it is good, note that mrman has told you nothing about whether it *applies*
— only whether it should. Those are different questions, and the second is
git's:

```sh
git checkout -b net-fix-v3
git am --3way ~/inbox/v3-net-fix.mbox
```

`--3way` falls back to a three-way merge when the context has drifted, which is
common for a series that sat on a list for a while.

### If `git am` fails

That is a rebase problem, not a review problem. Resolve it the usual way
(`git am --show-current-patch`, fix, `git am --continue`), or ask the
contributor to rebase and resend.

Your mrman session is still there and still valid — it was a review of the
posted patch, and the posted patch has not changed.

### After it lands

The session stays on disk under the artifact's identity. Delete the file, or
prune the review:

```sh
rm ~/.local/share/mrman/reviews/sessions/inbox@v3-net-fix-patch-*
```

---

## 4. Exchanging a review across an airgap

No mail, no forge, no network. Code crosses as a file and feedback crosses
back as one. The mechanics differ enough from the mailing-list case to be
worth their own walkthrough.

### Outside → inside: producing the patch

On the network side:

```sh
git format-patch --cover-letter -o /tmp/transfer main..feature
```

Or, for a whole branch as one artifact:

```sh
git format-patch --stdout main..feature > /tmp/transfer/feature.mbox
```

Carry it across on whatever your organisation permits.

### Reviewing it

On the airgapped side, with no clone of the repository at all:

```sh
mrman --patch /media/transfer/feature.mbox
```

This is the case `--patch` exists for. mrman needs nothing but the file — no
remote, no forge credentials, no checkout. The review persists locally like any
other.

If you *do* have a checkout of the tree on this side, you can still review the
patch as a patch; mrman will not try to reach the network either way.

### Sending feedback back

Two artifacts, and the choice matters:

```
:patch      →  a quoted-diff reply, best when the recipient will read it
               alongside the patch they sent
:export     →  markdown notes, better when it is going into a ticket, a
               document, or anything that is not a mail client
```

Both go to the clipboard by default. On a machine with no clipboard — which an
airgapped box often is — start mrman with `--stdout` and the artifact prints to
the terminal when you quit, ready to select and save.

`--stdout` is not a headless mode, and it is worth being clear about why:
mrman draws its interface on stdout, so `mrman … --stdout > review.txt` would
capture the interface too. For a genuinely scripted read of a finished review,
use the JSON CLI, which never opens a terminal interface at all:

```sh
mrman review list --repo /media/transfer
mrman review comments --session <slug> > /media/transfer/review.json
```

That JSON carries every comment with its file, line and type — enough to
render whatever your process needs on the other side.

### A note on what crosses

The review file contains your comments and the quoted lines they refer to. It
does not contain anything mrman inferred from your system — no paths outside
the artifact, no repository state, no credentials. If your process requires
review of what crosses the boundary, the file is plain text and reads exactly
as it will be received.

---

## Choosing between the reply and the notes

Both `:patch` and `:export` produce a review artifact; they are for different
readers.

| | `:patch` (reply) | `:export` (notes) |
|---|---|---|
| Shape | Quoted diff, comments inline | Numbered list of comments |
| Best for | Mail, mailing lists, anyone holding the patch | Tickets, documents, chat |
| Includes the code | Yes, verbatim | No, locations only |
| Threads | Yes, when the artifact carried a `Message-Id` | n/a |

When in doubt on a mailing list, reply. When in doubt anywhere else, export.
