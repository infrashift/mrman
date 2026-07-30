---
title: Reviewing Locally
description: Review working-tree changes, commit ranges, Jujutsu revsets, plain files with no VCS at all, or every tracked file in the repository.
sidebar:
  order: 2
---

Most of mrman is not about pull requests. It reviews whatever diff you point
it at — including diffs that exist only on your machine, and files that are
not in a diff at all.

## The target selector

Run `mrman` with no arguments and you land in the target selector.

The **Local** tab lists your commits, with synthetic rows for staged and
unstaged changes at the top. `j` / `k` moves, `Space` toggles a commit into
the selection, `Enter` confirms and loads the diff. Selecting several commits
reviews them as one continuous diff, with an inline commit strip you can walk
with `(` and `)`.

`Tab` switches to the **Pull Requests** tab — see [How PR Review
Works](../pull-requests/). `Esc` goes back to Local, or leaves the selector
entirely.

## Starting directly

| Command | What you get |
|---|---|
| `mrman -w` | Working-tree changes, skipping the selector |
| `mrman -r main..HEAD` | A commit range, with the commit strip |
| `mrman -p src/` | Any of the above, filtered to a path prefix |
| `mrman --file notes.md` | One file or directory, no VCS involved |
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

## Staging what you reviewed

```
:stage
```

Stages every file you marked reviewed. This only works on an **unstaged git
review** — mrman says *"Staging is only available for unstaged reviews in
git"* rather than silently doing nothing, because there is no coherent meaning
for it on a commit range or a pull request.

## Whitespace

```toml
ignore_whitespace = true
```

Ignores all whitespace when generating **local** diffs, git and Jujutsu alike.
It has no effect on pull requests: a PR diff arrives from the forge already
rendered, so there is no flag left to re-run it with.

## Ignoring files

`.gitignore` is honored automatically. A `.mrmanignore` at the repository root
layers on top of it with the same syntax — `!` negation included — and
excludes matching files from every review diff. Generated code, lockfiles and
vendored trees belong here.

## Exporting

With nothing selected, `y` copies the whole review to your clipboard as
markdown. `:clip` and `:export` do the same by name, and `--stdout` prints it
instead of copying, which is what you want in a pipeline.

The markdown is rendered through a Go template with an embedded default, and
you can replace it — see [Templates](../../reference/templates/).
`export_legend = false` drops the comment-type legend if you find it noisy.

## Sessions

Every review persists to `~/.local/share/mrman/reviews` automatically. Reopen
the same target and your comments and reviewed marks come back.

`:w` saves explicitly, `:q` refuses to quit while comments are unsaved, `:q!`
discards, and `ZZ` / `:wq` save and quit. `:clear` wipes comments and reviewed
marks; `:clearc` wipes only the comments.

Sessions are also the interface an agent reads and writes — see [Agent
Collaboration](../agents/).
