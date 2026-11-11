---
title: Vizzini Findings
description: Show an AI review from Vizzini on the diff, read each finding next to its code, and turn the ones you agree with into your own review comments.
---

[Vizzini](https://github.com/infrashift/vizzini-pr-reviewer) is an AI code
reviewer. mrman never runs it, and never calls a model. You or your CI run
`pr-review --format json`, and mrman shows that report on the diff it reviewed:

```sh
pr-review --mode review --patch-file change.patch --format json > review.json
mrman pr https://dev.azure.com/org/proj/_git/repo/pullrequest/42 --vizzini-report review.json
```

`--vizzini-report` works with `mrman pr` / `mr`, `--patch`, and local reviews.

## What you see

- **Header:** `vizzini 72 (diff 41·model 72) · 9 findings`. This is the report's risk score: the deterministic diff score and the model's own estimate. When the report suggests auto-approval, the header says so, but that is only a suggestion.
- **Gutter:** `◆` marks the first line of a finding and `┊` the rest of its range. The color shows the most severe finding on the line: Critical, Major, Minor or Trivial. Findings anchor to the new side, so deleted lines are never marked.
- **Keys:**
  - `F` jumps to the next finding.
  - `f` opens a panel with the findings on the cursor line: severity, category, title, body, the suggested fix, and any knowledge-base documents it cites.

## Making a finding your comment

In the findings panel, `p` copies a finding into the comment box. The comment is anchored to the finding's lines and uses the `issue` type if you have one. You then edit it like any comment and save it with Enter. It becomes an ordinary local draft, and is submitted with the rest of your review. Nothing reaches the forge until you submit. A finding you promote is marked in the panel, so you don't copy it twice.

## Staleness and trust

mrman scores the diff on screen with the same policy as the
[risk score](../../reference/configuration/#risk-score-optional). If that score
differs from the report's `base_risk_score`, the report was made for a
different diff. The header then says **STALE** and a warning explains why.
Findings in files that are not in the diff are counted in a warning, not shown.

Finding text is model output. mrman strips control characters and escape
sequences from it on load.

For agents, `mrman pr <target> --json --vizzini-report review.json` prints the
same findings in the session JSON ([reference](../../reference/cli/#--json)).
