---
title: Reviewing Patches
description: Review a .patch, .diff or mbox file with no repository behind it, and reply with an annotated quoted diff.
---

mrman reviews a patch file the same way it reviews a working tree or a pull
request: `mrman --patch series.mbox`. There is no repository behind it, nothing
to clone, and nothing to fetch — which is the point. It exists for two
audiences the other modes exclude:

- **Mailing-list projects**, the Linux kernel above all, where the unit of
  review *is* a posted series and the review artifact is a mail reply.
- **Airgapped organisations**, where code crosses the boundary as a file and
  the feedback has to cross back as one.

If you want the end-to-end walkthroughs — receiving, sending, applying,
exchanging across an airgap — see [Patch Workflows](../patch-workflows/). This
page is what the mode does.

## Opening a patch

```sh
mrman --patch 0001-guard-the-probe.patch    # one patch
mrman --patch v3-series.mbox                # a whole series
```

`--patch` takes one file, and it has to be a real file on disk. mrman re-reads
the artifact whenever you reload with `:e`, so a pipe or a process
substitution — `--patch <(git format-patch --stdout -3)` — is refused rather
than half-read. Redirect first:

```sh
git format-patch --stdout -3 > /tmp/series.mbox
mrman --patch /tmp/series.mbox
```

Four shapes are understood, and mrman works out which it has:

| Shape | Example |
|---|---|
| `git format-patch` output | one message, mail headers, a trailing `-- \n2.43.0` |
| An mbox of a series | `format-patch --stdout`, a `b4 am` result, a lore export |
| A single saved mail | headers and a body, no `From ` separator |
| A bare diff | `git diff` output, quilt, `diff -u` — no mail at all |

Quoted-printable and base64 bodies are decoded, RFC 2047 encoded-words in
subjects and author names are decoded, and CRLF is normalised. A file that
turns out not to be a patch says so rather than reporting an empty diff.

`--patch` is mutually exclusive with `--file`, `--all-files`, `--path`,
`-r` and `-w`: a patch artifact is its own review target, and there is no
repository to filter or compare against.

### Path depth

Posted patches declare paths like `a/drivers/foo.c`, and mrman strips one
leading component by default — the `patch -p1` convention `git am` also
follows. When a patch was generated differently, say the depth:

```sh
mrman --patch weird.diff --patch-strip 0    # paths are already correct
mrman --patch deep.diff --patch-strip 2
```

## A series is a commit range

Each patch in a series is its own review target, and mrman presents them
through the commit strip you already know from `mrman -r`:

```
2/2  net: use the counter          Dev Eloper   just now
1/2  net: rename the counter       Dev Eloper   just now
0/2  net: tidy the foo path        Dev Eloper   just now
```

- `(` and `)` walk patch by patch.
- `Space` on a strip row narrows the diff to that patch alone.
- With exactly one patch selected, **its changelog appears as a reviewable
  file** — `Commit Message (1/2)` — and comments you write are scoped to that
  patch, so they follow it rather than smearing across the series.

That last point matters more than it sounds. Mailing-list reviewers comment on
the commit message as often as on the code, and this is where you do it.

The `0/N` cover letter is a row like any other, but it carries prose rather
than a diff. Selecting it alone reports *"that message is prose only — a cover
letter carries no diff"*; select it together with a patch, or just read it in
the strip.

Two patches touching the same file do not collide: each patch's diff is parsed
separately and scoped to its own identity.

## What is different from a repository review

A patch carries only the context inside its own hunks, and there is no tree to
read the rest from. So:

- **There are no context expanders.** mrman does not draw the "N lines hidden"
  rows between hunks, because pressing one could never do anything. Nothing is
  hidden from you that mrman could show.
- **`e` cannot open the file** in `$EDITOR` — it is not on disk.
- **There is nothing to stage**, and the target selector offers no
  staged/unstaged rows.

Everything else is the same: vim navigation, the four comment scopes, comment
types, reviewed marks, the comment navigator, search, and sessions.

## Sessions

A patch review persists like any other. Its identity is the **artifact's
bytes**, so reopening the same file resumes the review, and a re-sent or
edited patch is a new one:

```
sessions/inbox@v3-series-patch-9f3c1a2-3c9d0e11ab22ff40.json
```

Comments still record the line they were written about, so if a v2 lands in
the same file, reopening re-anchors what it can and flags what it cannot —
see [outdated comments](../../project/troubleshooting/#a-comment-is-marked-outdated).

Because a patch review has no checkout, `mrman review list` finds it from the
directory the artifact lives in:

```sh
mrman review list --repo ~/inbox
```

## The Patches tab

`:patches` opens the target selector on the Patches tab — an inbox view,
listing the `.patch`, `.diff`, `.mbox` and `.eml` files in a directory, one
level deep, newest first:

```
▸ v3-net-fix.mbox      net: tidy the foo path      3 patches   Dev Eloper   2h ago
  0001-guard.patch     drivers/foo: guard probe                Dev Eloper   1d ago
  notes.diff           (no subject)                                         3d ago
```

`j`/`k` move, `Enter` opens, `/` filters across every visible column, and `r`
rescans. It starts in the current review's directory, so a patch review's
siblings are already listed.

A file it cannot read as a patch is still listed, with the reason. A patch you
expected to see and cannot open is worth knowing about — silently omitting it
looks exactly like it not being there.

## Replying

`:patch` (or `:reply`) renders the review as a mail reply: the diff quoted with
`> ` and your comments interleaved beneath the lines they refer to.

```
Subject: Re: [PATCH v3 2/5] drivers/foo: guard the probe
In-Reply-To: <20260101120000.12345-3-dev@example.org>

On Wed, 01 Jan 2026, Dev Eloper wrote:

> @@ -100,4 +100,6 @@ static int foo_probe(struct platform_device *pdev)
>  	int ret;
>
> +	if (!bar)

[ISSUE] Wrong errno — the caller distinguishes -ENODEV here.

> +		return -EINVAL;
```

Tabs are preserved exactly as the author wrote them, which is why this is
usable for kernel C at all. The reply goes to your clipboard — over SSH, tmux
or Zellij it uses OSC 52, so it lands in *your* clipboard, not the remote
host's. Starting mrman with `--stdout` prints it on exit instead, which is the
answer when there is no clipboard to copy to.

Details worth knowing:

- **Only commented hunks are quoted.** The author already has the patch;
  re-quoting a whole series is unreadable.
- **Review-level comments go at the top**, where a reply's prose belongs.
- **Reply headers are derived, never invented.** They appear only when the
  artifact carried a `Message-Id` — which means mail from a list, or
  `git format-patch --thread`. Plain `format-patch` writes none, and mrman
  will not fabricate one: a reply threaded to a made-up id lands in the wrong
  conversation, which is worse than one that does not thread.
- **Outdated comments are never quoted inline.** Their line numbers still
  resolve, so quoting them would attach your criticism to code you never read.
  They go to a trailing section with the reason and a snapshot of what the
  comment *was* about.

`:export` still produces the ordinary markdown notes, which is the better
artifact when the recipient is not reading mail — see
[Sharing a Review](../sharing/).
