package forgejof

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	forgejo "codeberg.org/mvdkleijn/forgejo-sdk/forgejo/v3"

	"github.com/infrashift/mrman/internal/forge"
)

// maxCommitPages bounds ListCommits pagination; 10 pages of 100 commits is
// far beyond any reviewable PR.
const maxCommitPages = 10

// GetPullRequest fetches the full PR record. BaseSHA is the merge base —
// the correct diff base — not the moving tip of the base branch.
func (d *Driver) GetPullRequest(ctx context.Context, target forge.Target) (*forge.PullRequestDetails, error) {
	const op = "get_pull_request"
	if target.Repository == nil {
		return nil, d.err(op, forge.ErrorValidation, 0, "",
			fmt.Errorf("merge request target %q does not include a repository", target.Original))
	}
	if target.Number == 0 {
		return nil, d.err(op, forge.ErrorValidation, 0, "",
			fmt.Errorf("merge request target %q does not include a valid number", target.Original))
	}
	api, err := d.api(ctx)
	if err != nil {
		return nil, d.wrap(op, nil, err)
	}
	pr, resp, err := api.GetPullRequest(target.Repository.Owner, target.Repository.Name,
		int64(target.Number)) //nolint:gosec // G115: pull request numbers are small forge-assigned integers
	if err != nil {
		return nil, d.wrap(op, resp, err)
	}
	headSHA := ""
	if pr.Head != nil {
		headSHA = pr.Head.Sha
	}
	baseSHA := pr.MergeBase
	if baseSHA == "" && pr.Base != nil {
		baseSHA = pr.Base.Sha
	}
	if headSHA == "" || baseSHA == "" {
		return nil, d.err(op, forge.ErrorValidation, 0, "",
			fmt.Errorf("forgejo response did not include head/merge-base SHA for MR #%d", target.Number))
	}
	details := &forge.PullRequestDetails{
		PullRequestSummary: prSummary(*target.Repository, pr),
		HeadSHA:            headSHA,
		BaseSHA:            baseSHA,
		Body:               pr.Body,
		Closed:             pr.State == forgejo.StateClosed,
		MergedAt:           pr.Merged,
	}
	details.Number = target.Number
	return details, nil
}

// GetDiff returns the cumulative merge-base..head unified diff of the PR
// via the .diff endpoint.
func (d *Driver) GetDiff(ctx context.Context, pr *forge.PullRequestDetails) (string, error) {
	const op = "get_diff"
	api, err := d.api(ctx)
	if err != nil {
		return "", d.wrap(op, nil, err)
	}
	diff, resp, err := api.GetPullRequestDiff(pr.Repository.Owner, pr.Repository.Name,
		int64(pr.Number), forgejo.PullRequestDiffOptions{Binary: false}) //nolint:gosec // G115: pull request numbers are small forge-assigned integers
	if err != nil {
		return "", d.wrap(op, resp, err)
	}
	return string(diff), nil
}

// GetCommitRangeDiff returns the diff between two commits of the PR. Only
// the local checkout can answer — Forgejo serves no commit-range diff API —
// so a miss surfaces ErrorUnsupported; the CommitRangeDiff=false capability
// keeps the app from offering the range selector in the first place.
func (d *Driver) GetCommitRangeDiff(_ context.Context, _ *forge.PullRequestDetails, startSHA, endSHA string) (string, error) {
	const op = "get_commit_range_diff"
	if diff, ok := d.localRangeDiff(startSHA, endSHA); ok {
		return diff, nil
	}
	return "", d.err(op, forge.ErrorUnsupported, 0, hintRangeDiff,
		fmt.Errorf("commit range %s..%s not available locally and Forgejo has no compare-diff API", startSHA, endSHA))
}

// localRangeDiff runs `git diff start..end` in the local checkout when both
// SHAs are present. ok=false means the range cannot be served.
func (d *Driver) localRangeDiff(startSHA, endSHA string) (string, bool) {
	if d.localCheckout == "" || d.runner == nil {
		return "", false
	}
	for _, sha := range []string{startSHA, endSHA} {
		if _, _, err := d.runner.Run(d.localCheckout, "git", "cat-file", "-e", sha); err != nil {
			return "", false
		}
	}
	stdout, _, err := d.runner.Run(d.localCheckout, "git", "diff", startSHA+".."+endSHA)
	if err != nil {
		return "", false
	}
	return string(stdout), true
}

// ListCommits returns the PR's commits oldest first. Forgejo serves them in
// git-log order (newest first), so the collected pages are reversed.
func (d *Driver) ListCommits(ctx context.Context, pr *forge.PullRequestDetails) ([]forge.Commit, error) {
	const op = "list_commits"
	api, err := d.api(ctx)
	if err != nil {
		return nil, d.wrap(op, nil, err)
	}
	var commits []forge.Commit
	page := 1
	for range maxCommitPages {
		rows, resp, err := api.ListPullRequestCommits(pr.Repository.Owner, pr.Repository.Name,
			int64(pr.Number), forgejo.ListPullRequestCommitsOptions{ //nolint:gosec // G115: pull request numbers are small forge-assigned integers
				ListOptions: forgejo.ListOptions{Page: page, PageSize: 100},
			})
		if err != nil {
			return nil, d.wrap(op, resp, err)
		}
		for _, row := range rows {
			commits = append(commits, convertCommit(row))
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		page = resp.NextPage
	}
	slices.Reverse(commits)
	return commits, nil
}

// convertCommit maps one SDK commit row to the neutral commit type.
func convertCommit(row *forgejo.Commit) forge.Commit {
	var sha string
	var created time.Time
	if row.CommitMeta != nil {
		sha = row.SHA
		created = row.Created
	}
	shortOID := sha
	if len(shortOID) > 7 {
		shortOID = shortOID[:7]
	}
	summary := ""
	author := "unknown"
	var timestamp *time.Time
	if rc := row.RepoCommit; rc != nil {
		summary, _, _ = strings.Cut(rc.Message, "\n")
		if a := rc.Author; a != nil {
			switch {
			case a.Name != "":
				author = a.Name
			case a.Email != "":
				author = a.Email
			}
			if parsed, err := time.Parse(time.RFC3339, a.Date); err == nil {
				timestamp = &parsed
			}
		}
	}
	if author == "unknown" && row.Author != nil && row.Author.UserName != "" {
		author = row.Author.UserName
	}
	if timestamp == nil && !created.IsZero() {
		timestamp = &created
	}
	return forge.Commit{
		OID:       sha,
		ShortOID:  shortOID,
		Summary:   summary,
		Author:    author,
		Timestamp: timestamp,
	}
}
