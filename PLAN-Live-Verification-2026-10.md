# Live verification, October 2026: GitHub, gitlab.com, Azure DevOps, Codeberg

The October 2026 review found bugs by reading the code. This ledger records
each one reproduced on a real forge with the released code, and the same
step passing on the fixed build.

- **Baseline** is `main` at `cdcb08f`.
- **Fixed** is `review/integration`, which merges the `fix/ws*` and
  `test/ws6-live-tooling` branches.

Fixtures come from `scripts/live-fixture.sh URL up` and are torn down with
`down`.

## Targets

| Forge | Repository | Fixture | Credentials |
|---|---|---|---|
| GitHub | github.com/infrashift/scratch | #2 (fixture, 11 files), #3 (310 files) | `gh auth token` |
| gitlab.com | gitlab.com/infrashift-group/scratch | pending | `GITLAB_TOKEN` |
| Azure DevOps | dev.azure.com/ryanscraig/scratch | PR 2 (fixture), PR 3 (repository `scratch space`) | `AZURE_DEVOPS_EXT_PAT` |
| Codeberg (Forgejo 16.0.0-dev) | codeberg.org/ryancraig/scratch | #1 (fixture) | `CODEBERG_TOKEN` (no `read:user`) |

## Ledger

| # | Finding | Forge | Baseline | Fixed |
|---|---|---|---|---|
| 1.1 | Diff rows that look like file headers are dropped | GitHub #2 | **Reproduced.** `config.yaml`: the deleted `---` row is missing, and `size: 2` is numbered old line 6 (it is 7). `counter.c`: hunk `+1,7` shows 6 rows, with no `++i;`. | **Pass.** `---` at old line 5, `size: 2` at old line 7, `++i;` at new line 4, 7 rows. |
| 1.2 | A comment moved into the review body is posted again on every submit | GitHub #2 | **Reproduced.** `review add` on `long.go:10` (outside the diff), then `review submit` twice: `locked_comments: 0` both times, and **2** reviews carry the marker. | **Pass.** First submit: `moved_to_body: 1`, `locked_comments: 1`. Second: "Nothing to submit". **1** review carries the marker. |
| 1.3 | Quoted non-ASCII paths | GitHub #2 | **Reproduced.** The file tree shows a `"b/` folder holding `caf\303\251.md"`. The header reads `"b/mrman-e2e/edge/caf\303\251.md" [M] · renamed from "a/…"`. | **Pass.** `mrman-e2e/edge/café.md [M]` in the right folder, not marked as renamed. |
| new | A freshly opened TUI session is saved with no diff files, so `review add` refuses every file | GitHub #2 | **Reproduced** (found during this run). With `mrman pr … --auto=comment`, the session file lists 0 files, and `review add --target-file …` fails with "is not part of this review session". | **Pass.** The session lists 11 files, and `review add` succeeds immediately. |
| 2.14a | A GitHub range comment spanning two hunks is accepted? (suspected) | GitHub #1 | **Not a bug.** A review comment with `start_line: 80`, `line: 115` (hunks 72–81 and 113–118 of `src/cache.go`) was accepted (review 5458358994). | No change needed. |
| 2.14b | A PR with more than 300 files cannot be opened | GitHub #3 | **Reproduced.** `mrman pr infrashift/scratch#3 --json` fails with "406 Sorry, the diff exceeded the maximum number of files (300)". | **Pass.** `file_count: 310`, built from the files listing. |
| WS3 | GitHub thread comments stopped at 100 | GitHub #2 | Not run on the baseline. By construction, `comments(first: 100)` returned at most 100 comments. | **Pass.** A 105-comment thread (root plus 104 replies) reads back with all 105 comments; the last is "reply 104". |
| 2.1 | Azure DevOps diffs against the target tip, not the merge base | ADO PR 2 | **Reproduced**, once a new head commit made Azure DevOps recompute the merge. `lastMergeTarget` became `4b4db20` while the merge base stayed `a6e16fe`. `shared.txt` gained a second hunk reversing the base branch's own line-5 change. | **Pass.** base `a6e16fe`, and only the line-30 hunk. |
| new | Deleted files missing from Azure DevOps reviews | ADO PR 2 | **Reproduced** (found during this run). The delete change entry has `item.path: null` and the path in `originalPath`. `obsolete.txt` was absent from the diff on both builds. | **Pass.** `obsolete.txt` shows as deleted. |
| 2.2 | Azure DevOps names with spaces | ADO PR 3 | **Reproduced.** `mrman pr 3 --json` in a clone of `.../_git/scratch%20space` fails with 404. | **Pass.** The PR opens; the slug `.../scratch space/pr/3` works with `review add` and `review list`. |
| 2.10 | GitLab and Azure DevOps general discussions (review bodies) never shown | ADO PR 2 | **Reproduced.** `TestLiveAgentSubmit` could not see its own Azure DevOps review: summaries=0. | **Pass.** threads=10 summaries=2, and the TUI navigator lists the review bodies. |
| 2.11 | Old-side comment on a renamed file posted with the old path, untracked | ADO PR 2 | **Confirmed by probe.** Azure DevOps accepts both paths, but presents an old-path thread under the new path with `origFilePath`, and only when it is given a tracking id. mrman's old-path lookup found none. | **Pass.** `review add --side old` on `rename-dst.go:11`, then submit: thread 43 on `/mrman-e2e/edge/rename-dst.go`, left line 11, changeTrackingId 7. |
| votes | Azure DevOps approve and request changes | ADO PR 2 | n/a | **Pass.** `:submit approve` gives vote 10; `:submit request-changes` gives -10; reset to 0 afterwards. |
| 1.1 | Header-lookalike rows | Codeberg #1 | **Reproduced.** `counter.c` has no `++i;` row. | **Pass.** |
| Forgejo | Live end-to-end on Codeberg | Codeberg #1 | n/a | **Pass.** All generic live tests pass. threads=9 summaries=3 render; a draft review lands as `PENDING`. Own-PR approve and request-changes are refused with Forgejo's 422 ("approve/reject your own pull is not allowed"), shown verbatim, and the comment stays local. Without `read:user`, metadata degrades silently (no "commits since your last review"). |
| live | `TestLiveAgentSubmit` fails when run alongside the other live tests | GitHub #2 | **Reproduced.** In `go test ./...` the UI package's live submit posted between its counts. | **Pass.** It counts only reviews carrying its own marker. All GitHub live tests pass in one parallel run. |

## Runs

GitHub, fixed build, every live test in one parallel run:

```console
$ GITHUB_TOKEN="$(gh auth token)" MRMAN_LIVE_PR=infrashift/scratch#2 MRMAN_LIVE_SUBMIT=1 \
    go test -count=1 -run Live ./internal/agentsubmit/ ./internal/ui/ ./internal/forge/githubf/
ok   github.com/infrashift/mrman/internal/agentsubmit   5.369s
ok   github.com/infrashift/mrman/internal/ui            11.175s
ok   github.com/infrashift/mrman/internal/forge/githubf  4.841s
```

GitHub, complete integration build (`86f0f34`), every live test package at
once:

```console
$ GITHUB_TOKEN="$(gh auth token)" MRMAN_LIVE_PR=infrashift/scratch#2 MRMAN_LIVE_SUBMIT=1 \
    go test -count=1 -run Live ./...
ok   github.com/infrashift/mrman/internal/agentsubmit     7.608s
ok   github.com/infrashift/mrman/internal/forge/githubf   4.860s
ok   github.com/infrashift/mrman/internal/ui              9.731s
```

Azure DevOps and Codeberg, complete integration build, generic live tests:

```console
$ MRMAN_LIVE_PR=dev.azure.com/ryanscraig/scratch/scratch#2 MRMAN_LIVE_SUBMIT=1 go test -count=1 -run Live ./internal/ui/ ./internal/agentsubmit/
ok   github.com/infrashift/mrman/internal/ui            15.243s
ok   github.com/infrashift/mrman/internal/agentsubmit    6.906s
$ MRMAN_LIVE_PR=https://codeberg.org/ryancraig/scratch/pulls/1 MRMAN_LIVE_SUBMIT=1 go test -count=1 -run Live ./internal/ui/ ./internal/agentsubmit/
ok   github.com/infrashift/mrman/internal/ui             7.042s
ok   github.com/infrashift/mrman/internal/agentsubmit    6.633s
```

Codeberg was promoted to Supported. Self-hosted Forgejo and Gitea stay
Experimental (no other version exercised).

## Teardown (2026-10-08)

- **GitHub:** #2 and #3 closed; `mrman-e2e-*` branches deleted.
- **Azure DevOps:** PR 2 abandoned and its vote reset to 0; branches deleted;
  the `scratch space` repository (and with it PR 3) deleted.
- **Codeberg:** #1 closed; branches deleted.
- **Left in place:** GitHub #1 and Azure DevOps PR 1, which predate this run.
  The cross-hunk probe review on GitHub #1 (5458358994) remains.

## Still to run

- **gitlab.com:** blocked on the token. The supplied `GITLAB_TOKEN` is a
  fine-grained token without `Project: Read` or `User: Read`, so the API
  answers 403 `insufficient_granular_scope`. Use a classic token with the
  `api` scope.
