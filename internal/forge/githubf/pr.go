package githubf

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/go-github/v76/github"

	"github.com/infrashift/mrman/internal/forge"
)

// maxCommitPages bounds ListCommits pagination; GitHub caps PRs at 250
// commits via the API, so 10 pages of 100 is well past the ceiling.
const maxCommitPages = 10

// GetPullRequest fetches the full PR record.
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
	pr, _, err := d.rest.PullRequests.Get(ctx, target.Repository.Owner, target.Repository.Name,
		int(target.Number)) //nolint:gosec // G115: pull request numbers are small forge-assigned integers
	if err != nil {
		return nil, d.wrap(op, err)
	}
	headSHA := pr.GetHead().GetSHA()
	baseSHA := pr.GetBase().GetSHA()
	if headSHA == "" || baseSHA == "" {
		return nil, d.err(op, forge.ErrorValidation, 0, "",
			fmt.Errorf("GitHub response did not include head/base SHA for MR #%d", target.Number))
	}
	state := pr.GetState()
	return &forge.PullRequestDetails{
		PullRequestSummary: forge.PullRequestSummary{
			Repository:  *target.Repository,
			Number:      uint64(pr.GetNumber()), //nolint:gosec // G115: the forge never returns a negative id
			Title:       pr.GetTitle(),
			Author:      pr.GetUser().GetLogin(),
			HeadRefName: pr.GetHead().GetRef(),
			BaseRefName: pr.GetBase().GetRef(),
			UpdatedAt:   timePtr(pr.UpdatedAt),
			URL:         pr.GetHTMLURL(),
			State:       state,
			IsDraft:     pr.GetDraft(),
		},
		HeadSHA:  headSHA,
		BaseSHA:  baseSHA,
		Body:     pr.GetBody(),
		Closed:   state == "closed",
		MergedAt: timePtr(pr.MergedAt),
	}, nil
}

// GetDiff returns the cumulative base..head unified diff of the PR.
func (d *Driver) GetDiff(ctx context.Context, pr *forge.PullRequestDetails) (string, error) {
	const op = "get_diff"
	diff, _, err := d.rest.PullRequests.GetRaw(ctx, pr.Repository.Owner, pr.Repository.Name,
		int(pr.Number), github.RawOptions{Type: github.Diff}) //nolint:gosec // G115: pull request numbers are small forge-assigned integers
	if err != nil {
		return "", d.wrap(op, err)
	}
	return diff, nil
}

// GetCommitRangeDiff returns the cumulative diff between two commits of the
// PR. When both SHAs are present in the local checkout, `git diff` answers
// without a network round-trip; otherwise the compare API serves the raw
// diff.
func (d *Driver) GetCommitRangeDiff(ctx context.Context, pr *forge.PullRequestDetails, startSHA, endSHA string) (string, error) {
	const op = "get_commit_range_diff"
	if diff, ok := d.localRangeDiff(startSHA, endSHA); ok {
		return diff, nil
	}
	diff, _, err := d.rest.Repositories.CompareCommitsRaw(ctx, pr.Repository.Owner, pr.Repository.Name,
		startSHA, endSHA, github.RawOptions{Type: github.Diff})
	if err != nil {
		return "", d.wrap(op, err)
	}
	return diff, nil
}

// localRangeDiff runs `git diff start..end` in the local checkout when both
// SHAs are present. ok=false means the caller must fall back to the API.
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

// ListCommits returns the PR's commits oldest-first, as GitHub returns them.
func (d *Driver) ListCommits(ctx context.Context, pr *forge.PullRequestDetails) ([]forge.Commit, error) {
	const op = "list_commits"
	var commits []forge.Commit
	page := 1
	for range maxCommitPages {
		rows, resp, err := d.rest.PullRequests.ListCommits(ctx, pr.Repository.Owner, pr.Repository.Name,
			int(pr.Number), &github.ListOptions{Page: page, PerPage: 100}) //nolint:gosec // G115: pull request numbers are small forge-assigned integers
		if err != nil {
			return nil, d.wrap(op, err)
		}
		for _, row := range rows {
			commits = append(commits, convertCommit(row))
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		page = resp.NextPage
	}
	return commits, nil
}

// convertCommit maps one REST commit row to the neutral commit type.
func convertCommit(row *github.RepositoryCommit) forge.Commit {
	sha := row.GetSHA()
	shortOID := sha
	if len(shortOID) > 7 {
		shortOID = shortOID[:7]
	}
	summary, _, _ := strings.Cut(row.GetCommit().GetMessage(), "\n")
	author := "unknown"
	commitAuthor := row.GetCommit().GetAuthor()
	switch {
	case commitAuthor.GetName() != "":
		author = commitAuthor.GetName()
	case commitAuthor.GetEmail() != "":
		author = commitAuthor.GetEmail()
	}
	var timestamp *time.Time
	if commitAuthor != nil {
		timestamp = timePtr(commitAuthor.Date)
	}
	return forge.Commit{
		OID:       sha,
		ShortOID:  shortOID,
		Summary:   summary,
		Author:    author,
		Timestamp: timestamp,
	}
}
