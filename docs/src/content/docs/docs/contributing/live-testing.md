---
title: Testing Against a Real Forge
description: Setting up a throwaway GitHub pull request and exercising mrman's whole stack against it — a transcript of an actual run.
---

Every unit test in mrman runs against fakes. Fakes cannot catch a driver
that builds a request GitHub then rejects, or a pane that renders correctly
from invented data and wrongly from real data. This walks through setting up
a throwaway pull request and exercising the whole stack against it.

It is written as a transcript of an actual run, so every command here has
been executed and every output is real.

## What you need

- `gh` authenticated with the `repo` scope (`gh auth status` to check)
- A repository you are willing to have branches and review comments created
  in — a private scratch repo is ideal
- mrman built from a checkout: `make build`, or `go build -o /tmp/mrman .`

## How mrman actually talks to GitHub

Worth being precise about, because two different things are going on in
this document.

**mrman never shells out to `gh` to make API calls.** It uses
[`go-github`](https://github.com/google/go-github) for REST and
[`githubv4`](https://github.com/shurcooL/githubv4) for GraphQL, over an
`oauth2` HTTP client, straight to `api.github.com`. The `gh` commands in
this tutorial are *me* setting up and verifying fixtures, not mrman working.

The one thing mrman may use `gh` for is borrowing a **token**.
`forge.TokenForHost` resolves credentials per host, first hit wins:

1. A SaaS-scoped environment variable — `GITHUB_TOKEN` then `GH_TOKEN` for
   github.com, `GH_ENTERPRISE_TOKEN` for GHE hosts. Scoped by host so an
   environment token never leaks to an on-prem instance.
2. The host's `token` in config, with `$VAR` expansion.
3. The host's `token_cmd`, run through `sh -c` and cached for the process.
   A failure here is fatal rather than skipped: you configured it on
   purpose.
4. `gh auth token --hostname <host>`, for GitHub hosts, when
   `[forge] cli_token_fallback` allows it (it does by default).

An empty result is not an error — it means unauthenticated access, which is
fine for a public repository.

In the runs below there was no `GITHUB_TOKEN` and no `~/.config/mrman/`, so
step 4 fired: mrman ran `gh auth token` once, then made every call itself
with that bearer token.

You can prove which link fired. Hide `gh` and unset the variable, and a
private repository 404s — GitHub's standard answer for "you cannot see
this":

```console
$ env PATH=/tmp MRMAN_LIVE_PR=OWNER/REPO#1 go test -count=1 ./internal/forge/githubf/ -run TestLivePullRequest
ListPullRequests: github: list_pull_requests: not found (HTTP 404) on github.com:
  GET https://api.github.com/repos/OWNER/REPO/pulls?... : 404 Not Found
  — Pull request or repository not found — check the target and that the
  token can see this repository.
```

Supply the token directly and it works with `gh` still off `PATH`, which is
what proves the SDK — not the CLI — is doing the work:

```console
$ env PATH=/tmp GITHUB_TOKEN="$(gh auth token)" MRMAN_LIVE_PR=OWNER/REPO#1 \
    go test -count=1 -v ./internal/forge/githubf/ -run TestLivePullRequest
    listing row: #1 "Expire cache entries after their TTL" by ryancraig [feat/ttl-expiry]
--- PASS
```

Note `-count=1`. Go caches test results, and the first attempt at this
"passed" only because `gh` was still at `/usr/bin/gh` and the result was
cached; without forcing a re-run the control proves nothing.

## 1. Give the repo something worth reviewing

A pull request only exercises the interesting paths if it has some shape to
it. Aim for:

- **Two or more files**, so the file tree and navigation matter.
- **Two or more commits**, so the inline commit selector appears and
  `(` / `)` cycling has somewhere to go.
- **Files long enough that a mid-file change leaves hidden context above and
  below it.** This is what makes context expansion testable — a hunk at the
  top of a 10-line file has no gap to expand.

In the scratch repo used here, `main` got a ~115-line `src/cache.go` with
filler functions between the interesting parts, plus a small `src/config.py`:

```sh
git checkout main
# ... write src/cache.go (long) and src/config.py (short) ...
git add -A && git commit -m "Add a cache and a config parser"
git push origin main
```

## 2. Open a pull request with several commits

```sh
git checkout -b feat/ttl-expiry

# Commit 1: a change in the middle of the long file
git commit -am "Expire entries on read"

# Commit 2: a second hunk in the same file
git commit -am "Stamp expiry on write"

# Commit 3: a different file
git commit -am "Strip quotes and add merge()"

git push -u origin feat/ttl-expiry
gh pr create --base main --head feat/ttl-expiry \
  --title "Expire cache entries after their TTL" \
  --body "..."
```

Confirm the shape:

```sh
gh pr view 1 --json number,headRefOid,changedFiles,commits \
  --jq '{number, head: .headRefOid, files: .changedFiles,
         commits: [.commits[].messageHeadline]}'
```

```json
{"commits":["Expire entries on read","Stamp expiry on write","Strip quotes and add merge()"],
 "files":2,"head":"f13746222c7f07a93828a41f34cea40f2e33f159","number":1}
```

## 3. Put existing review comments on it

mrman renders the forge's own threads and review summaries read-only, and
filters resolved ones. To test that, the pull request needs some.

Comments can only anchor to lines that appear in the diff, so read the patch
first:

```sh
gh api repos/OWNER/REPO/pulls/1/files --jq '.[] | {path: .filename, patch: .patch}'
```

Then post a review with a body and inline comments. Use `--input` with a
JSON file — `gh api -f 'comments[][path]=...'` mangles the nested objects
and fails with `Field is not defined on DraftPullRequestReviewComment`:

```sh
cat > /tmp/review.json <<'JSON'
{
  "event": "COMMENT",
  "body": "Looks close. Two things inline before this goes in.",
  "comments": [
    {"path": "src/cache.go", "line": 76, "side": "RIGHT",
     "body": "Deleting inside Get means the read path mutates the map."},
    {"path": "src/config.py", "line": 8, "side": "RIGHT",
     "body": "This only strips double quotes; single-quoted values keep theirs."}
  ]
}
JSON
gh api repos/OWNER/REPO/pulls/1/reviews -X POST --input /tmp/review.json
```

If you get `422 Line could not be resolved`, the line is not in the diff.

Resolve one thread so the unresolved-vs-all filter has something to filter.
This needs GraphQL; REST cannot resolve threads:

```sh
TID=$(gh api graphql -f query='
{ repository(owner:"OWNER", name:"REPO") { pullRequest(number:1) {
    reviewThreads(first:10){nodes{id path}} } } }' \
  --jq '.data.repository.pullRequest.reviewThreads.nodes[]
        | select(.path=="src/config.py") | .id')

gh api graphql -f query="mutation {
  resolveReviewThread(input:{threadId:\"$TID\"}) { thread { isResolved } } }"
```

## 4. Review it interactively

```sh
mrman pr 1                      # from inside the checkout
mrman pr OWNER/REPO#1           # or addressed explicitly
mrman pr https://github.com/... # or by URL
```

mrman prints its session slug to stderr as it starts:

```
mrman-session: gh:github.com/infrashift/scratch/pr/1
```

Things worth trying, and what should happen:

| Do this | Expect |
|---|---|
| Look at the header | `OWNER/REPO#1 · <title> · OPEN`, and a thread count |
| Scroll to a commented line | The forge's thread inline, read-only, with its author |
| `dd` on one of those | *"Existing forge comments are read-only"* |
| `:comments all` | The resolved thread appears too |
| `:comments hide` | Both disappear |
| `Enter` or `Space` on an expander | Context loads from the forge |
| `(` / `)` | The diff narrows to one commit and back |
| `c`, type, `Enter` | A local draft comment |
| `:submit` | Picker → confirm → posted |

Without a multiplexer, run mrman in one terminal and use another for `gh`.
`script -qec "mrman pr 1" /dev/null` does **not** work: Bubble Tea v2 queries
the terminal for capabilities and a piped stdin never answers, so it hangs.

## 5. Verify the whole stack non-interactively

Two opt-in tests drive the real forge without a terminal. They skip unless
their environment variables are set, so `make check` is unaffected.

### Read-only: every driver method

```sh
MRMAN_LIVE_PR=OWNER/REPO#1 go test ./internal/forge/githubf/ -run TestLivePullRequest -v
```

Exercises `ListPullRequests` (both scopes), `GetPullRequest`, `GetDiff`,
`ListCommits`, `FileLineCount`, `FetchFileLines`, `ListReviewThreads`,
`ListReviewSummaries`, `ReviewMetadata` and `GetCommitRangeDiff`, and logs
what each returned. From the real run:

```
capabilities: {DraftReviews:true Approve:true RequestChanges:true ReviewSummaries:true
  ReviewThreads:true ThreadResolution:true ThreadOutdated:true MultiLineComments:true
  CommitRangeDiff:true ReviewRequestedFilter:true AtomicSubmit:true CommitScopedReviews:true}
listing row: #1 "Expire cache entries after their TTL" by ryancraig [feat/ttl-expiry]
pr #1 head=f137462 base=95643b6 readonly=false
commits (oldest first): 3
context: src/cache.go has 118 lines
threads: 2
  src/cache.go:76 resolved=false outdated=false — ryancraig: Deleting inside Get…
  src/config.py:8 resolved=true  outdated=false — ryancraig: This only strips…
review summaries: 1
viewer="ryancraig" reviews=1
range diff ab0d7db..f137462: 487 bytes vs 1084 for the whole PR
```

### Read-only: the whole stack, through the renderer

```sh
MRMAN_LIVE_PR=OWNER/REPO#1 go test ./internal/ui/ -run TestLivePullRequestReview -v
```

Builds the real model against the real pull request and asserts on the
rendered frame: the header, the commit strip, threads rendering, resolved
ones hidden until `:comments all`, gap expansion actually fetching, and
`( )` narrowing through `GetCommitRangeDiff`.

```
header:  mrman   infrashift/scratch#1 · Expire cache entries after their TTL · OPEN
files: 2
  src/cache.go (2 hunks)
  src/config.py (1 hunks)
threads=2 summaries=1
visibility: 5 thread rows unresolved-only, 9 with all
gap expansion: 52 rows -> 72
narrowed to the newest commit: 2 files (whole PR had 2)
```

### Writing: submit

This posts a real review, so it is gated twice:

```sh
MRMAN_LIVE_PR=OWNER/REPO#1 MRMAN_LIVE_SUBMIT=1 \
  go test ./internal/ui/ -run TestLivePullRequestSubmit -v
```

It writes a line comment on each file plus a review-level comment, submits,
and then checks that everything sent is locked and that deleting a submitted
comment is refused.

```
anchored a comment on src/cache.go (row 8)
anchored a comment on src/config.py (row 37)
preflight: 2 inline, 0 unmappable, 1 review-level
  inline: src/cache.go:75 side=new
  inline: src/config.py:8 side=new
result: Review submitted (Comment)
locked 3/3 comments after submit
```

Confirm from GitHub's side that the anchors are where mrman said:

```sh
gh api repos/OWNER/REPO/pulls/1/comments --jq '.[] | {path, line, side, body}'
gh api repos/OWNER/REPO/pulls/1/reviews  --jq '.[] | {id, state, body}'
```

```json
{"body":"[NOTE] Live submit check from mrman's integration test.","line":75,"path":"src/cache.go","side":"RIGHT"}
{"body":"[NOTE] Live submit check from mrman's integration test.","line":8,"path":"src/config.py","side":"RIGHT"}
```

The `[NOTE]` prefix is `[forge] comment_type_prefix`, and the review body is
the review-level comment.

**Do not truncate bodies when checking them.** `--jq '.body|.[0:80]'` cut a
review body at exactly the `## Unplaced comments` heading and made it look
like mrman was emitting an empty section. It was not — the item was on the
next line. Print the whole body, ideally through `cat -A` so the blank lines
are visible.

## 6. Exercise the agent-submit interlock

An agent can read a pull request freely, but submitting a review is gated
behind a grant only a human can issue. Walk it:

**Nothing granted — the refusal must reach the forge not at all.**

```console
$ mrman pr 1 --json > /dev/null          # open the session headlessly
$ mrman review add --session gh:github.com/OWNER/REPO/pr/1 \
    --target-file src/cache.go --line 75 --side new \
    --type note --username "Claude" "Draft comment."
$ mrman review submit --session gh:github.com/OWNER/REPO/pr/1 --event comment
{"error":"agent_submit_not_permitted","reason":"no_grant","requested_event":"comment",
 "granted_events":[],"message":"This session has no agent-submit grant. Ask the user
 to open the review interactively with: mrman pr <target> --auto=comment"}
$ echo $?
1
```

Confirm it truly did nothing:

```sh
gh api repos/OWNER/REPO/pulls/1/reviews --jq 'length'
```

**Try to grant it yourself — both routes are closed.**

```console
$ mrman pr 1 --json --auto
error: if any flags in the group [json auto] are set none of the others can be

$ mrman pr 1 --auto < /dev/null
error: --auto requires an interactive terminal: it authorizes an agent to submit
reviews, so it must be given by a person running mrman, not by a program invoking it
```

The second matters more than it looks. `/dev/null` is a character device, so
a `ModeCharDevice` test would call it a terminal — the check has to be a real
`isatty`, and there is a test pinning that.

**Granted — from a terminal, by a person.**

```sh
mrman pr 1 --auto            # comment + draft
mrman pr 1 --auto=comment,draft,approve
```

The header now carries a persistent `AGENT SUBMIT: comment,draft` chip in
warning colors. `:agent` reports it; `:agent off` revokes it immediately.
`mrman review list` shows `granted_events` so an agent can check before
attempting.

With the TUI open, an agent's `review submit --event comment` succeeds and
`--event approve` still refuses — approving is what gets code merged, so a
default grant excludes it. Quit the TUI and the grant is gone: it is held
against that process, so there is no teardown step to skip or crash through.

The whole path is covered by tests, including against a real pull request:

```sh
MRMAN_LIVE_PR=OWNER/REPO#1 MRMAN_LIVE_SUBMIT=1 \
  go test ./internal/agentsubmit/ -run TestLiveAgentSubmit -v
```

```
refused: This session has no agent-submit grant. Ask the user to open the review …
forge unchanged after refusal: 4 reviews
submitted: review=4811395236 state=COMMENTED inline=0 locked=1
```

### What the interlock is, and is not

It is a **deliberate-action interlock**: agent submission cannot happen by
accident, by an agent misreading its instructions, or by an agent deciding
unilaterally that it would be helpful — and when it is possible, a human is
looking at a screen that says so.

It is **not a security boundary**. An agent with a shell can call the GitHub
API directly and mrman cannot stop it. If that is your threat model, the
control belongs at the credential: give agent sessions a token without
`repo` scope, or no token at all.

## What live testing actually caught

The read paths all worked first time. The one failure was in the test rather
than the product, and it is worth knowing about because it is an invariant
you can trip over in real code too:

> Setting `DiffState.CursorLine` directly does **not** update
> `DiffState.CurrentFileIdx`, and a new comment files under `CurrentFileIdx`.
> The test set the cursor by hand, so a comment aimed at `src/config.py` was
> filed under `src/cache.go` at a line that file does not have — and mrman
> correctly reported it as *"line not in current diff"* and moved it to the
> review summary rather than posting it somewhere wrong.

Every real path — `j`/`k`, a mouse click, `{N}G`, the comment navigator —
goes through `MoveCursorToAnnotation`, which syncs both. Use it.

The other near-miss was the truncated `jq` above: a reporting artifact that
looked like a formatting bug for several minutes. Verify against full
output before concluding anything.

## Cleaning up

The pull request is more useful kept around than deleted — it is a stable
fixture for the live tests. To reset it between runs:

```sh
# Remove review comments mrman posted
gh api repos/OWNER/REPO/pulls/1/comments --jq '.[].id' \
  | xargs -I{} gh api repos/OWNER/REPO/pulls/comments/{} -X DELETE

# Or start over entirely
gh pr close 1 --delete-branch
```

Reviews themselves cannot be deleted through the API; only dismissed. That
is a GitHub constraint, not an mrman one, and it is the main reason to point
these tests at a scratch repository rather than anything real.
