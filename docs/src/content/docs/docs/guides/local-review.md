---
title: Reviewing Locally
description: Review working-tree changes, commit ranges, Jujutsu revsets, plain files with no VCS at all, or every tracked file in the repository.
sidebar:
  order: 2
---

Most of mrman is not about merge requests. It reviews whatever diff you point
it at — including diffs that exist only on your machine, and files that are
not in a diff at all.

## The target selector

Run `mrman` with no arguments and you land in the target selector.

The **Local** tab lists your commits, with synthetic rows for staged and
unstaged changes at the top. `j` / `k` moves, `Space` toggles a commit into
the selection, `Enter` confirms and loads the diff. Selecting several commits
reviews them as one continuous diff, with an inline commit strip you can walk
with `(` and `)`.

`Tab` switches to the **Merge Requests** tab — see [How MR Review
Works](../merge-requests/). `Esc` goes back to Local, or leaves the selector
entirely.

## Starting directly

| Command | What you get |
|---|---|
| `mrman -w` | Working-tree changes, skipping the selector |
| `mrman -r main..HEAD` | A commit range, with the commit strip |
| `mrman -p src/` | Any of the above, filtered to a path prefix |
| `mrman --file notes.md` | One file or directory, no VCS involved |
| `mrman diff old new` | The difference between two paths, no repository |
| `mrman -A` | Pristine mode: every tracked file, annotatable |

`-r` takes whatever range syntax the detected backend understands, so it is
`main..HEAD` in a git repository and a revset in a Jujutsu one. mrman detects
which you are in; there is no flag to choose.

`-p` filters the diff to a file or directory prefix. It applies after
`.mrmanignore`, so it narrows what survived the ignore rules rather than
overriding them.

## Reviewing files that are not a diff

`--file` takes a path — a single file or a whole directory — and opens it with
no version control involved at all. There is no repository requirement, no
remote, nothing to detect. It is the right tool for reviewing a design
document, a generated report, or a directory someone sent you.

`--patch` takes a `.patch`, `.diff` or mbox file and reviews it with no
repository at all — see [Reviewing Patches](../patches/).

## Reviewing two versions of the same thing

Sometimes both things you want to compare are just sitting on disk. A vendored
dependency before and after an upgrade. A generated file from two runs of the
same tool. The same config in staging and in production. A file someone
emailed you, against your own copy.

```sh
mrman diff vendor-1.2.0/ vendor-1.3.0/
mrman diff staging/config.yaml prod/config.yaml
```

`mrman diff` takes two paths — either two files or two directories — and
reviews the difference between them. Neither needs to be in version control,
and they need not be related to each other. Unlike `--file`, which can only
show you a file as one long addition, this produces a real two-sided diff, so
side-by-side view and the old-side gutter both work.

A few things worth knowing:

- **Directories are paired by relative path.** A file at the same path under
  both roots is one comparison; anything else is an addition or a deletion.
  The walk honours each root's own `.gitignore` exactly as `--file <dir>`
  does, so a `node_modules` or `target` directory stays out of your review.
  `.mrmanignore` is read from the **new** side and applies to the whole
  comparison, so a path ignored there is absent even if it exists only under
  the old root.
- **Moved files are detected as renames**, the way `git diff` does it: an
  exact content match first, then a similarity match for files that moved
  *and* changed. A rename shows once, under its new path, with the old one
  named in the file header — rather than twice, as a whole-file delete plus a
  whole-file add. Detection falls back to exact matches only on very large
  reorganisations, mirroring git's `diff.renameLimit`.
- **The session is keyed on the two paths, not their contents.** Edit either
  file, reopen the same command or press `:e`, and your comments are still
  there — mrman revalidates the anchors and tells you which ones moved. This
  is the opposite of `--patch`, where editing the artifact deliberately starts
  a new review.
- **`ignore_whitespace` applies**, so a pure reindentation can be made to
  disappear from the comparison.
- Because it is its own review target, `mrman diff` cannot be combined with
  `--file`, `--patch`, `-A`, `-r`, `-w` or `-p`.

`-A` (**pristine mode**) is the repository-wide version: it collects every
tracked file and presents all of them as reviewable, so you can annotate code
that nobody changed. Useful for onboarding notes, audits and architecture
reviews. Two constraints follow from what it is:

- It is **git only** — it needs a tracked-file list.
- It forces **unified rendering and single-file view**. Side-by-side has
  nothing to show when there is no "before", and mrman says so rather than
  rendering an empty column.

Pristine sessions get a stable identity derived from the head SHA and a hash
of the path set, so they survive a `git pull` that does not change which files
exist.

## Sessions across an amend or rebase

A review of your working tree is tied to the commit it sits on top of, so
amending or rebasing moves the ground under it. mrman carries the review
forward rather than dropping it: reopen after a `git commit --amend` and your
comments and reviewed marks are still there, on the same branch, now recorded
against the new HEAD.

Carrying them over is only safe because each comment remembers the line it was
written about, so on reopen mrman checks every one against the new diff:

- The line is unchanged — nothing to say.
- The line moved — the comment moves with it, and the status bar says how many
  were re-anchored.
- The line is gone, or its content now appears in several places so there is no
  telling which one was meant — the comment is marked `(outdated)` on its box
  and mrman refuses to post it inline, offering it for the review summary
  instead. It never guesses a line for you.

Two cases deliberately do not carry forward. A **detached HEAD** has no stable
anchor — its identity *is* the commit — so two detached checkouts are unrelated
positions rather than one review that moved. And a **commit range** names its
own endpoints, which makes a different range a different review by
construction.

## Working through a diff

Reviewed marks are the point of the tool. `r` toggles the current file
reviewed, `R` the current hunk. The file tree shows the state, so you always
know what is left.

`Enter` or `Space` on an expander pulls in hidden context around a hunk — a
hunk in the middle of a long file has context above and below it that the diff
omitted. `o` / `O` expand and collapse every directory in the tree.

Comments come in four scopes:

| Key | Scope |
|---|---|
| `c` | The line at the cursor |
| `v` then `c` | A visual range |
| `C` | The whole file |
| `<leader>c` (`;c`) | The review as a whole |

`Tab` in the comment box cycles the comment type, and the box title shows the
current one (`Add L60 comment [ISSUE]`). Four ship by default — **NOTE**,
**ISSUE**, **SUGGESTION**, **PRAISE** — starting on `NOTE`, with an untyped
entry at the end for comments that need no classifying. Declaring your own in
[comment types](../../reference/configuration/#comment-types) replaces those
four entirely.

`m` / `M` jump between comments, `i` or `A` edits the one at the cursor, `dd`
deletes it.

The **comment navigator** in the bottom-left lists every comment in the review.
Its marker shows scope (`★` review, `▣` file, `●` line, `◇` a forge thread) and
the marker's colour shows the comment type. `Tab` to it and `Enter` jumps to a
comment in the diff.

Comments stay listed when you mark their file reviewed, so folding a finished
file does not lose your notes on it. Because a folded file has no diff row to
jump to, `Enter` on one of those rows opens a read-only peek panel — the
commented line with a little context, and the comment — leaving the file
folded.

## Jujutsu repositories

A `jj` checkout is detected automatically and `-r` takes a revset instead of
a git range. jj has no staging area, so the parts of mrman built on one are
unavailable there: the *staged* / *unstaged* targets in the selector, `:stage`,
and `-A` (pristine mode, which is git-only). Working-tree and revset reviews,
comments, sessions and exports all work the same.

## Staging what you reviewed

```
:stage
```

Stages every file you marked reviewed. This only works on an **unstaged git
review** — mrman says *"Staging is only available for unstaged reviews in
git"* rather than silently doing nothing, because there is no coherent meaning
for it on a commit range or a merge request.

## Whitespace

```toml
ignore_whitespace = true
```

Ignores all whitespace when generating **local** diffs, git and Jujutsu alike.
It has no effect on merge requests: an MR diff arrives from the forge already
rendered, so there is no flag left to re-run it with.

## Ignoring files

`.gitignore` is honored automatically. A `.mrmanignore` at the repository root
layers on top of it with the same syntax — `!` negation included — and
excludes matching files from every review diff. Generated code, lockfiles and
vendored trees belong here. Only the two files at the repository root are
consulted; nested `.gitignore` files are not. A merge request's diff is
filtered only when you open it from inside a checkout.

## Exporting

With nothing selected, `y` copies the whole review to your clipboard as
markdown. `:clip` and `:export` do the same by name, and `--stdout` prints it
instead of copying, which is what you want in a pipeline.

The markdown is rendered through a Go template with an embedded default, and
you can replace it — see [Templates](../../reference/templates/).
`export_legend = false` drops the comment-type legend if you find it noisy.

Export is how a local review reaches another person, since there is no forge to
post it to — [Sharing a Review](../sharing/) covers that end to end, including
copying from a remote machine over SSH.

## Sessions

Every review persists to `~/.local/share/mrman/reviews` automatically. Reopen
the same target and your comments and reviewed marks come back.

Session files live in `reviews/sessions/` and are named for what they hold:

```
infrashift-mrman@main-worktree-abc1234-3f2a1b0c9d8e7f60.json
infrashift-mrman@github.com-pr-12-a1b2c3d4e5f60718.json
```

The trailing hash is the session's identity; everything before it is there so
the directory is legible. The repo comes first and is spelled the same way for
a repo's local and merge-request sessions, so one pattern reaches all of them:

```sh
ls   ~/.local/share/mrman/reviews/sessions/infrashift-mrman@*
rm   ~/.local/share/mrman/reviews/sessions/infrashift-mrman@*   # drop a repo's reviews
```

The repo name comes from your `origin` remote. A checkout without one falls
back to the directory's own name, so `mrman@main-worktree-…` rather than
`infrashift-mrman@main-worktree-…`. Don't parse these names — they follow the
branch and remote, and change when those do. `mrman review list --repo .` is
the supported way to ask what sessions exist.

`:w` saves explicitly, `:q` refuses to quit while comments are unsaved, `:q!`
discards, and `ZZ` / `:wq` save and quit. `:clear` wipes comments and reviewed
marks; `:clearc` wipes only the comments.

Sessions are also the interface an agent reads and writes — see [Agent
Collaboration](../agents/).
