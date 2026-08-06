---
title: Agent Collaboration
description: How an AI agent reads a review as you write it, contributes findings of its own, and why submitting to a forge needs a grant only a human at a terminal can issue.
sidebar:
  order: 0
---

Code review is the one place where a human and an agent are looking at exactly
the same thing and need to say things to each other about specific lines of it.
mrman makes the review session a shared object: you write in the TUI, an agent
reads and writes through a JSON CLI, and changes merge live.

Everything an agent needs is under `mrman review`. All of its output is JSON.
[`skills/mrman/`](https://github.com/infrashift/mrman/tree/main/skills/mrman)
packages this as a ready-made agent skill, including tmux and zellij wrappers.

## The session slug

On start mrman prints its session slug to **stderr**:

```
mrman-session: gh:github.com/infrashift/scratch/pr/1
```

That slug is the handle for everything below. An agent that missed it can find
sessions instead:

```sh
mrman review list --repo .                # this checkout, plus its MR sessions
mrman review list --repo owner/repo       # every session for a forge repo
mrman review list --all                   # everything
```

`--repo` is a selector, not just a path: a checkout also surfaces the pull
request sessions belonging to that checkout's `origin`, and a forge coordinate
matches by coordinate. Each row carries `slug`, `kind` (`local` or `pr`),
`path`, `updated_at`, `comment_count`, `reviewed_count`, `file_count`, `anchor`,
`active` and `granted_events`.

A local slug embeds the HEAD it was taken at, so **do not cache one across a
commit**. If the user amends or rebases, mrman carries the review onto the new
HEAD under a new slug and the old one stops resolving. Re-run
`mrman review list` to get the current handle rather than treating a slug as
permanent.

## Two workflows, opposite rules

This is the distinction that matters, and the packaged skill leads with it
because getting it wrong is worse than doing nothing.

### 1. The user reviews your patch

You wrote the code; they want to read it and write comments themselves. The
agent's job is to **wait and listen** — never to add comments of its own.

```sh
mrman review watch --session <slug>
```

`watch` blocks and emits one JSON object per line as the session changes:

```
{"event":"snapshot","comments":[...]}
{"event":"comment_added","comment":{...}}
{"event":"comment_changed","comment":{...}}
{"event":"comment_removed","id":"..."}
{"event":"submitted","lifecycle_state":"submitted"}
{"event":"closed","reason":"tui_exited"}
```

The stream ends on `submitted` or `closed` — which are the two answers to "is
the human finished", so the agent never has to ask. `--since <comment-id>`
resumes without re-reading, and `--timeout` bounds the wait.

A one-shot read works too:

```sh
mrman review comments --session <slug>
```

Each comment carries `id`, `location`, `path`, `start_line`, `end_line`,
`side`, `comment_type`, `lifecycle_state`, `content` and `author`. Comment
types are your intent made machine-readable, and the four built-ins mean what
they say: `issue` blocks, `suggestion` should be implemented or argued with,
`note` wants an answer, `praise` wants nothing. Since types are replaceable, an
agent should read a type's `definition` rather than assume — a session may use
`blocker` and `nit` instead.

### 2. The agent reviews a patch

Here the agent writes findings, under a `--username` that names it:

```sh
mrman review add --session <slug> \
  --target-file internal/app/gaps.go --line 42 --side new \
  --type issue --username "Claude" \
  "This returns before the deferred close runs."
```

Omit `--line` for a file comment, omit `--target-file` entirely for a
review-level comment, and add `--end-line` for a range. `--side old` for
removed lines, `--side new` for added or unchanged ones. Structured input
comes in via `--input` with literal JSON, `@file.json` or `-` for stdin:

```sh
mrman review add --session <slug> --username "Claude" --input - <<'JSON'
{"target": {"type": "line", "file": "main.go", "line": 12, "side": "new"},
 "type": "issue", "content": "Unchecked error."}
JSON
```

Target types are `review`, `file`, `line` and `line_range`.

mrman colors each author distinctly and marks anything that is not your
configured `username` as someone else's, so agent findings are never
mistakable for yours in the TUI. Your own comments are deliberately left
unbadged — every `@name` you see is someone who is not you. Set
`show_own_author = true` if you would rather have every comment attributed.

**Always pass `--username` from an agent.** Without it the comment falls back
to the `username` in the config file it happens to be running under — yours —
and the finding is attributed to you rather than to the agent that wrote it.

## Opening a session without a terminal

An agent that does not need a human watching can open a merge request
headlessly:

```sh
mrman pr 1 --json
```

It prints the session and exits — no TUI, no multiplexer:

```json
{"slug":"gh:github.com/owner/repo/pr/1","kind":"pr","path":"...",
 "repo":"owner/repo","number":1,"title":"...","head_sha":"...","base_sha":"...",
 "file_count":2,"read_only":false,"granted_events":[]}
```

`granted_events` is always empty here and **cannot** be otherwise — see below.

If the user does need an interactive pane, the packaged skill's wrappers open
one and hand the slug back: `mrman-wrapper.sh` under tmux,
`mrman-wrapper-zellij.sh` under zellij. Neither is required to attach to a
session that already exists.

## The submit interlock

Submitting a review is the one `review` command that writes to a forge, and it
is gated. It works only while a human has the review open with a grant:

```sh
mrman pr 1 --auto                        # comment and draft
mrman pr 1 --auto=comment,draft,approve  # explicitly wider
```

While a grant is live the header carries a persistent `AGENT SUBMIT:
comment,draft` chip in warning colors. `:agent` reports it, `:agent off`
revokes it immediately, and quitting the TUI ends it — the grant is held
against that process, so there is no teardown step to skip or crash through.

Then, and only then:

```sh
mrman review submit --session <slug> --event comment --username "Claude"
```

A refusal is JSON on stdout with exit status 1:

```json
{"error":"agent_submit_not_permitted","reason":"no_grant","requested_event":"approve",
 "granted_events":["comment","draft"],
 "message":"... Ask the user to reopen it with: mrman pr <target> --auto=approve"}
```

`reason` is `no_grant`, `grant_expired` or `event_not_granted`. Note that
`comment` and `approve` are not interchangeable: an approval is what gets code
merged, so a bare `--auto` deliberately excludes it.

### Why an agent cannot grant itself one

Both routes are closed, by design:

```console
$ mrman pr 1 --json --auto
error: if any flags in the group [json auto] are set none of the others can be

$ mrman pr 1 --auto < /dev/null
error: --auto requires an interactive terminal: it authorizes an agent to submit
reviews, so it must be given by a person running mrman, not by a program invoking it
```

The terminal check is a real `isatty`, not a `ModeCharDevice` test — `/dev/null`
is a character device and would otherwise pass. There is a test pinning that.

There is also **no config key** for this, deliberately. A persistent switch
would be too easy to enable once and forget, and it would apply to every
repository and every merge request until someone noticed.

### What the interlock is, and is not

It is a **deliberate-action interlock**: agent submission cannot happen by
accident, by an agent misreading its instructions, or by an agent deciding
unilaterally that it would be helpful — and when it is possible, a human is
looking at a screen that says so.

It is **not a security boundary**. An agent with a shell can call the forge API
directly and mrman cannot stop it. If that is your threat model, the control
belongs at the credential: give agent sessions a token without write scope, or
no token at all.

## Comments an agent must not touch

A comment whose `lifecycle_state` is `pushed_draft` or `submitted` is already
on the forge. mrman treats those as read-only for everyone, and changing one
means changing it on the forge.

The forge's own existing review threads are not in this CLI at all — they live
on the remote, and mrman only displays them.

## Verifying the whole path

The interlock is covered by tests, including against a real merge request:

```sh
MRMAN_LIVE_PR=owner/repo#1 MRMAN_LIVE_SUBMIT=1 \
  go test ./internal/agentsubmit/ -run TestLiveAgentSubmit -v
```

See [Testing Against a Real Forge](../../contributing/live-testing/) for a full
transcript, including proof that a refused submit leaves the forge untouched.
