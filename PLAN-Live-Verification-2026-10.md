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
| Azure DevOps | dev.azure.com/ryanscraig/scratch | pending | `AZURE_DEVOPS_EXT_PAT` |
| Codeberg (Forgejo) | codeberg.org/ryancraig/scratch | pending | `CODEBERG_TOKEN` |

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

## Still to run

- **gitlab.com**: seed the empty repository, build the fixture, then run
  `TestLiveGitLabReview` and the generic tests. Waiting on `GITLAB_TOKEN`.
- **Azure DevOps**: build the fixture, then cover 2.1 (merge base), 2.2 (a
  repository whose name has a space), 2.10 (general threads) and 2.11
  (old-side comments on renamed files). Waiting on `AZURE_DEVOPS_EXT_PAT`.
- **Codeberg**: build the fixture, run the generic live tests and a TUI pass.
  If they pass, drop Codeberg's (and Forgejo's) Experimental label. Waiting on
  `CODEBERG_TOKEN`.
- **Teardown**: `scripts/live-fixture.sh URL down` on every forge, and close
  GitHub #3.
