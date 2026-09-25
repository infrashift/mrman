# PLAN: verify the GitLab driver against a live GitLab

The GitLab driver (`internal/forge/gitlabf/`) is documented as **Experimental**:
"implemented and unit-tested, but has not yet been exercised against a live
instance" (`docs/src/content/docs/docs/forges/gitlab.md:8-13`). This plan
exercises its read, comment, draft, **request changes** (reject) and **approve**
paths end to end against a real self-hosted GitLab. Only after that passes are
the docs promoted.

The first target is **GitLab CE 19.3.2** on the infrashift `gcloud-dc` lab. It is
served over plain http, with the API on the Traefik `forge-api` door and no auth
gateway in front of `/api/v4`.

## Status ledger

| Part | What | Status | Evidence |
|---|---|---|---|
| A | Live tests forge-generic: `internal/livetest`, four live tests rewired, `countReviews` GitLab-aware | **DONE**, `make check` green | 85.7% coverage (gate 85%) |
| B | `TestLiveGitLabReview` L1-L7 | WRITTEN, not yet run live | `internal/forge/gitlabf/live_test.go` |
| C | Fixture runbook, `scripts/gitlab-live-fixture.sh` | WRITTEN, not yet run | shellcheck clean |
| D1 | Live run of L1-L7 plus the agent-submit and UI live tests | BLOCKED: the gcloud-dc tunnel timed out on 2026-09-25 | |
| D2 | Binary smoke: `mrman pr --json`, `review add`, submit without a grant is refused | PENDING | |
| D3 | AI-agent pass: TUI `--auto=approve,request-changes`, with Claude Code driving `skills/mrman` | PENDING (interactive) | |
| E | Docs: Experimental becomes verified, plus a GitLab transcript in `contributing/live-testing.md` | PENDING D | |

## Facts the design rests on

- **mrman makes no LLM calls.** The "AI-augmented" review is an outside agent
  driving the JSON CLI (`mrman review add|submit`). Submits are gated by a
  human's TTY grant: `mrman pr <t> --auto=…` writes it into `active_sessions.json`,
  tied to the TUI's pid. A test grants in-process with
  `store.MarkSessionActiveWithGrant`, as `internal/agentsubmit/live_test.go` does.
- **On GitLab, reject means `SubmitRequestChanges`,** which is the GraphQL
  mutation `mergeRequestRequestChanges`, sent with a `PRIVATE-TOKEN` header
  (`gitlabf/submit.go`, `requestChanges`). **Approve** is `POST …/approve`.
- **The driver's result state is made up locally** (`submitState`): APPROVED,
  CHANGES_REQUESTED and so on. GitLab never returns it. The live test therefore
  reads every outcome back from GitLab over a second channel: the SDK plus GraphQL
  with a **bearer** token. A pass then does not depend on the header the driver
  chose.
- **mrman has no unapprove, merge or close.** The test unapproves through the raw
  API so it can be run again.
- **Before Part A, every live test was hard-wired to github.com and `config.Default()`**
  (no `[[forge.hosts]]`, `api_base`, `token_cmd` or CA). `TestLiveAgentSubmit`
  also counted `ListReviewSummaries`, which is always empty on GitLab.
- **Plain http works.** `api_base = "http://HOST:PORT/api/v4"` carries the scheme
  and port, and an http MR URL parses (`registry.go`, `parseBuiltinTargetURL`).
  mrman only warns that the token travels in plaintext (`config/load.go`).

## Part A — forge-generic live tests (DONE)

- `internal/livetest`:
  - `Parse` / `Target` resolve `MRMAN_LIVE_PR` through `forge.ParseTarget`, so
    `owner/repo#N`, `host/group/repo!N` and full MR URLs (http or https, with a
    port, with subgroups) all work.
  - `Config` loads `$XDG_CONFIG_HOME/mrman/config.toml` exactly as the binary does.
  - `RequireKind` / `RequireSubmit` gate driver-specific tests and tests that write.
- `internal/ui/livepr_test.go`, `internal/ui/livesubmit_test.go`,
  `internal/agentsubmit/live_test.go` and `internal/forge/githubf/livepr_test.go`
  now use it. The GitHub driver test skips a non-GitHub target, so one
  `MRMAN_LIVE_PR` can drive `go test ./...`.
- `countReviews` (agentsubmit) counts review threads on a forge without
  `ReviewSummaries`. A GitLab review body lands as a general MR note, which is a
  path-less thread.

## Part B — `TestLiveGitLabReview`

The steps run in order, and each is read back from GitLab:

| # | Step | Read back |
|---|---|---|
| L1 | `GetPullRequest`, `GetDiff`, `ListCommits`, `FetchFileLines`, `ReviewMetadata`, `ListPullRequests(ScopeReviewRequested)` | Diff refs equal `GET …/merge_requests/:iid`. Per-file status equals `GET …/diffs`. Commit count and head match. Line 1 equals the raw file. The viewer is listed as a requested reviewer. |
| L2 | Comment submit with one inline comment per anchor kind: addition, deletion, context, a two-line addition range, a whole-hunk span, and a line in the renamed file | Each lands as a **DiffNote** whose `position` new_line/old_line/line_range match. The body lands as one general note. `ListReviewThreads` reads each back on the same line and side. |
| L3 | Draft submit | Two `draft_notes` exist (the inline one positioned) and nothing is published. Cleanup deletes them. |
| L4 | Request changes plus an inline comment | GraphQL `reviewers…reviewState == REQUESTED_CHANGES` for the viewer. |
| L5 | Granted agent session, then a `[skip ci]` commit pushed to the head branch, then `agentsubmit.Submit` | Refused with "advanced", and the thread count is unchanged. |
| L6 | Approve on the new head | `GET …/approvals` `approved_by` contains the viewer, GraphQL `approvedBy` contains the viewer, and `reviewState == APPROVED`. Cleanup unapproves. |
| L7 | Optional (`MRMAN_LIVE_READONLY_TOKEN`): a `read_api` token submits | A `forge.Error` with status 403 and the api-scope hint, and no write. |

Suspects the run is designed to expose. Each gets a fixture test in
`gitlabf/*_test.go` and a fix, and each is recorded here:

1. `mergeRequestRequestChanges` may not exist or work in CE 19.3 (Free), may need
   the user to be an assigned reviewer, or may reject a `PRIVATE-TOKEN` header on
   `/api/graphql`.
2. `rangeEndpoint` computes `line_code` as `sha_0_new` even when a range endpoint
   is a **context** line, whose real code is `sha_old_new`. `mapRange` also never
   sets `CounterpartLine`, so a hunk-span range ending on a context line sends
   `new_line` without `old_line`.
3. `ListReviewThreads` does not read `line_range` back, so a range thread
   round-trips as a single line. L2 asserts only the end line.
4. A comment submit whose only content is a review body returns an empty
   `review_id`, because the note ID from `CreateMergeRequestNote` is discarded.
   This is harmless, but it is why the agentsubmit test does not assert
   `review_id`.

## Part C — fixture runbook

`scripts/gitlab-live-fixture.sh` (generic). It never touches the default branch:

- A scratch base branch `mrman-e2e-base` holds the "before" files.
- A head branch `mrman-e2e-<stamp>` holds modify, delete, rename and add changes.
- The MR is head → base, with the reviewer set.
- Every commit creates its branch in the same call and says `[skip ci]`. On
  gcloud-dc, the devcontainer pipeline runs on every push and on MR events
  (`workflow:` rules).

On **gcloud-dc**:
- **chad** is the test user (decided 2026-09-25). He is always bootstrapped with
  his workspace projects, so the fixture lives in **`chad/python`**.
- root authors the MR with the seed token, so chad is never approving his own MR.
- chad reviews with an impersonation token.
- Addresses come from `datacenters/gcloud-dc.env`. Never write an IP here.

```sh
# 0. Reachability. A tunnel that is up but times out needs a bounce.
curl -s -o /dev/null -w '%{http_code}\n' "http://$SM_INGRESS_HOST:8093/users/sign_in"   # 200
curl -s -o /dev/null -w '%{http_code}\n' "http://$SM_INGRESS_HOST:8093/api/v4/user"     # 401

# 1. Admin token = the GitLab root seed token (hashistack repo, terraform/live/gitlab).
#    It stays in the environment and is never printed.
eval "$(make -s creds DC=gcloud-dc 2>/dev/null </dev/null)"
export ADMIN_TOKEN="$(vault kv get -field=seed_token secret/nomad/namespaces/gitlab/server)"
export GITLAB_URL="http://$SM_INGRESS_HOST:8093" PROJECT=chad/python REVIEWER=chad

# 2. Fixture and chad's tokens (1-day impersonation tokens, mode 0600).
S=$(mktemp -d)
scripts/gitlab-live-fixture.sh token api      "$S/chad-api"
scripts/gitlab-live-fixture.sh token read_api "$S/chad-ro"
MR_URL=$(scripts/gitlab-live-fixture.sh up)

# 3. An isolated mrman config, so the user's own sessions and config are untouched.
export XDG_CONFIG_HOME="$S/config" XDG_DATA_HOME="$S/data"
mkdir -p "$XDG_CONFIG_HOME/mrman"
cat >"$XDG_CONFIG_HOME/mrman/config.toml" <<EOF
[[forge.hosts]]
host = "$SM_INGRESS_HOST"
forge = "gitlab"
api_base = "http://$SM_INGRESS_HOST:8093/api/v4"
token_cmd = "cat $S/chad-api"
EOF

# 4. The live run (Part D1).
MRMAN_LIVE_PR="$MR_URL" MRMAN_LIVE_SUBMIT=1 MRMAN_LIVE_READONLY_TOKEN="$(cat "$S/chad-ro")" \
  go test -count=1 ./internal/forge/gitlabf/ ./internal/agentsubmit/ ./internal/ui/ -run Live -v

# 5. Teardown: close the MRs, delete the mrman-e2e-* branches, revoke chad's mrman-live tokens.
scripts/gitlab-live-fixture.sh down && rm -rf "$S"
```

## Part D — runs

- **D1** is the command in step 4 above. Every L-step passes, and so do
  `TestLiveAgentSubmit` (no grant is refused with nothing reaching GitLab; a
  grant lands a note) and `TestLivePullRequestReview/Submit`.
- **D2**, with the same environment:
  - `mrman pr "$MR_URL" --json` succeeds.
  - `mrman review add --username Claude …` adds a finding.
  - `mrman review submit --event request-changes` exits 1 with
    `agent_submit_not_permitted` / `no_grant`.
- **D3** is interactive:
  1. The user runs `mrman pr "$MR_URL" --auto=approve,request-changes` in a terminal.
  2. Claude Code follows `skills/mrman/SKILL.md`: it reviews, adds findings, and
     submits **request-changes**.
  3. root pushes a fix, and Claude re-opens and submits **approve**.
  4. In the GitLab UI and API: chad's state goes REQUESTED_CHANGES → APPROVED, the
     DiffNotes sit on the right lines, and `approved_by = chad`.

## Part E — docs (after D)

- `docs/…/forges/gitlab.md`, `reference/forge-capabilities.md` and `README.md`:
  replace Experimental with "verified against GitLab CE 19.3.2 (self-hosted,
  http), <date>", and keep every limitation found on the list.
- `contributing/live-testing.md`: add a GitLab transcript next to the GitHub one.
