---
name: mrman
description: Use mrman's review CLI to read and add comments in active TUI review sessions, and launch mrman in tmux/zellij when a user needs an interactive review pane.
---

# mrman Review Workflow

`mrman review` is the agent interface. The TUI is where the human reviews
code; the CLI is how you discover active sessions, read their comments, and
— only when it is appropriate — add your own.

All `review` output is JSON.

## Decide which workflow this is, first

The two workflows have opposite rules about who may write comments. Getting
this wrong means either putting words in the user's review or sitting idle
while they wait for your findings.

**1. User-led review of changes you produced.**
The user wants to read the patch and write comments themselves. Open or find
the session, then read their comments with `mrman review comments` when they
say the review is ready — or poll for new comment IDs if you are explicitly
waiting. Do **not** add comments of your own, do not pre-review your own
patch, and never author a comment that could read as theirs.

**2. You reviewing a patch.**
The user wants you to critique or summarize a patch. Inspect it and propose
findings. If the workflow and the target session are both unambiguous, write
the findings with `mrman review add` and an explicit `--username` naming
you. Ask before writing when either is unclear.

If the request does not clearly say which, ask.

## Attach to a session

1. Work out the repository from the request, the working directory, or
   recent file operations. Ask when it is ambiguous.

2. List sessions:

   ```bash
   mrman review list --repo /path/to/repo   # checkout, plus its PR sessions
   mrman review list --repo owner/repo      # every session for a forge repo
   mrman review list --all                  # everything, across all repos
   ```

   `--repo` is a selector, not just a path: a checkout also surfaces the PR
   sessions belonging to that checkout's `origin`, and a forge coordinate
   (`owner/repo`, `host/owner/repo`, or a PR URL) matches by coordinate. Use
   `--all` when you do not know the repository.

   Each row carries `slug`, `kind` (`local` or `pr`), `path`, `updated_at`,
   `comment_count`, `reviewed_count`, `file_count`, `anchor` and `active`.

3. Pick the session:
   - Exactly one relevant row with `"active": true` — attach to it.
   - Several active, or none obviously right — ask which slug.
   - The user gave a slug or a JSON path — use it directly.
   - PR slugs (`gh:owner/repo/pr/N`) are self-contained and need no `--repo`.
   - No active session — start or wait for one, below.

The CLI does not need tmux or zellij. Do not demand a multiplexer merely to
attach to a session that already exists.

## Start a session

Only when the user needs an interactive pane and none is running:

| Environment | Action |
|---|---|
| `$TMUX` set | `<skill-directory>/mrman-wrapper.sh /path/to/repo` |
| `$ZELLIJ` set | `<skill-directory>/mrman-wrapper-zellij.sh /path/to/repo` |
| Neither | Say you are waiting for them to start `mrman` in the repo, then attach with `mrman review list` |

If both are set, prefer the innermost if that is clear; otherwise ask.

The wrappers block until the TUI exits, so give them a long timeout — ten
minutes or more. mrman prints `mrman-session: <slug>` to stderr as it
starts, which is the fastest way to learn the slug; `mrman review list
--repo /path/to/repo` works too. If your environment cannot run anything
else while the wrapper blocks, read the comments after the user exits.

## Read the user's comments

There is no push channel. Read on demand:

```bash
mrman review comments --session <slug>
```

Each comment carries `id`, `location`, `path`, `start_line`, `end_line`,
`side`, `comment_type`, `lifecycle_state`, `content` and `author`.

Treat the types as the user's intent:

- `issue` — a blocking problem; fix it first
- `suggestion` — implement it, or explain why not
- `note` — answer or acknowledge
- `praise` — nothing to do

Comment types are user-configurable, so a session may use different ids. A
type's `definition` in the user's config says what it means.

While waiting during a live review, poll roughly every 30 seconds and
compare `id`s against the previous result; read immediately when the user
says they are done. Stop once the review is finished or polling would block
more useful work. Because the user can keep reviewing while you work, re-run
`mrman review comments` before claiming you have addressed everything.

An empty result usually means the wrong session — ask whether they saved
into the one you are reading.

## Add your own comments

Only in workflow 2, and only after the user approves writing into the
session.

- Prefer a line comment when you know the file and line.
- Use a file comment for file-scoped feedback.
- Reserve review-level comments for whole-review summaries.
- `--type issue` for problems; `suggestion`, `note` or `praise` when they
  fit better.
- Always pass `--username` so your comments are visibly yours. mrman colors
  each author distinctly and marks anything that is not the configured
  `username` as someone else's.

```bash
# Line comment
mrman review add --session <slug> \
  --target-file internal/app/gaps.go \
  --line 42 --side new \
  --type issue --username "Claude" \
  "This returns before the deferred close runs."

# File-level comment
mrman review add --session <slug> \
  --target-file internal/app/gaps.go \
  --type suggestion --username "Claude" \
  "This file is doing two jobs; the cache could move out."

# Review-level: omit --target-file
mrman review add --session <slug> \
  --type note --username "Claude" \
  "The parser changes look right; I could not verify the Windows path."
```

`--side old` for removed lines, `--side new` for added or unchanged ones.
Add `--end-line` for a range.

For structured input use `--input` with literal JSON, `@path/to/file.json`,
or `-` for stdin. Target types: `review`, `file`, `line`, `line_range`.

```bash
mrman review add --session <slug> --username "Claude" --input - <<'JSON'
{"target": {"type": "line", "file": "main.go", "line": 12, "side": "new"},
 "type": "issue", "content": "Unchecked error."}
JSON
```

## Comments you must not touch

A comment whose `lifecycle_state` is `pushed_draft` or `submitted` has
already been written to the forge. mrman treats those as read-only, and so
should you: they cannot be edited or deleted, and asking the user to change
one means asking them to do it on the forge.

The forge's own existing review threads are not in this CLI at all — they
live on the remote and mrman only displays them.

## Multiplexer tips

tmux: `Ctrl-b` then arrows to switch panes, `Ctrl-b z` to zoom, `q` to close
mrman.

zellij: `Alt` plus arrows to switch panes, `Alt f` to float, `q` to close
mrman.
