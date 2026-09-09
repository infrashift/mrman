package gitlabf

import (
	"context"
	"encoding/json"
	"fmt"

	gitlab "gitlab.com/gitlab-org/api/client-go"

	"github.com/infrashift/mrman/internal/forge"
)

// Pagination bounds so a buggy server cannot hang the client.
const (
	// maxCommitPages bounds ListCommits pagination: 10 pages of 100 is far
	// beyond any reviewable MR.
	maxCommitPages = 10
	// maxDiffPages bounds GetDiff pagination over per-file diff entries.
	maxDiffPages = 20
)

// GetPullRequest fetches the full MR record. The diff_refs start SHA is
// stashed into ForgePayload because discussion positions need it at submit
// time.
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
	mr, _, err := d.client.MergeRequests.GetMergeRequest(projectID(*target.Repository),
		int64(target.Number), nil, gitlab.WithContext(ctx)) //nolint:gosec // G115: pull request numbers are small forge-assigned integers
	if err != nil {
		return nil, d.wrap(op, err)
	}
	headSHA := mr.DiffRefs.HeadSha
	if headSHA == "" {
		headSHA = mr.SHA
	}
	if headSHA == "" {
		return nil, d.err(op, forge.ErrorValidation, 0, "",
			fmt.Errorf("GitLab response missing diff_refs and sha for MR !%d", target.Number))
	}
	var payload json.RawMessage
	if mr.DiffRefs.StartSha != "" {
		raw, err := json.Marshal(forgePayload{StartSHA: mr.DiffRefs.StartSha})
		if err != nil {
			return nil, d.err(op, forge.ErrorValidation, 0, "", err)
		}
		payload = raw
	}
	author := ""
	if mr.Author != nil {
		author = mr.Author.Username
	}
	return &forge.PullRequestDetails{
		PullRequestSummary: forge.PullRequestSummary{
			Repository:  *target.Repository,
			Number:      uint64(mr.IID), //nolint:gosec // G115: the forge never returns a negative id
			Title:       mr.Title,
			Author:      author,
			HeadRefName: mr.SourceBranch,
			BaseRefName: mr.TargetBranch,
			UpdatedAt:   mr.UpdatedAt,
			URL:         mr.WebURL,
			State:       normalizeState(mr.State),
			IsDraft:     mr.Draft,
		},
		HeadSHA:      headSHA,
		BaseSHA:      mr.DiffRefs.BaseSha,
		Body:         mr.Description,
		Closed:       mr.State == "closed",
		MergedAt:     mr.MergedAt,
		ForgePayload: payload,
	}, nil
}

// GetDiff returns the MR's cumulative unified diff. GitLab serves per-file
// hunk bodies without git headers, so the driver synthesizes a git-style
// patch deterministically (works on every GitLab version, unlike the
// raw-diff endpoint).
func (d *Driver) GetDiff(ctx context.Context, pr *forge.PullRequestDetails) (string, error) {
	const op = "get_diff"
	var files []fileDiff
	page := int64(1)
	for range maxDiffPages {
		rows, resp, err := d.client.MergeRequests.ListMergeRequestDiffs(
			projectID(pr.Repository), int64(pr.Number), //nolint:gosec // G115: pull request numbers are small forge-assigned integers
			&gitlab.ListMergeRequestDiffsOptions{
				ListOptions: gitlab.ListOptions{Page: page, PerPage: 100},
			}, gitlab.WithContext(ctx))
		if err != nil {
			return "", d.wrap(op, err)
		}
		for _, row := range rows {
			files = append(files, fileDiff{
				OldPath: row.OldPath,
				NewPath: row.NewPath,
				AMode:   row.AMode,
				BMode:   row.BMode,
				Body:    row.Diff,
				New:     row.NewFile,
				Renamed: row.RenamedFile,
				Deleted: row.DeletedFile,
			})
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		page = resp.NextPage
	}
	return synthesizeUnifiedDiff(files), nil
}

// GetCommitRangeDiff returns the cumulative diff between two commits of
// the MR. When both SHAs are present in the local checkout, `git diff`
// answers without a network round-trip; otherwise the repository compare
// API serves per-file diffs and the same synthesis applies.
func (d *Driver) GetCommitRangeDiff(ctx context.Context, pr *forge.PullRequestDetails, startSHA, endSHA string) (string, error) {
	const op = "get_commit_range_diff"
	if diff, ok := d.localRangeDiff(startSHA, endSHA); ok {
		return diff, nil
	}
	compare, _, err := d.client.Repositories.Compare(projectID(pr.Repository),
		&gitlab.CompareOptions{From: new(startSHA), To: new(endSHA)},
		gitlab.WithContext(ctx))
	if err != nil {
		return "", d.wrap(op, err)
	}
	files := make([]fileDiff, 0, len(compare.Diffs))
	for _, row := range compare.Diffs {
		files = append(files, fileDiff{
			OldPath: row.OldPath,
			NewPath: row.NewPath,
			AMode:   row.AMode,
			BMode:   row.BMode,
			Body:    row.Diff,
			New:     row.NewFile,
			Renamed: row.RenamedFile,
			Deleted: row.DeletedFile,
		})
	}
	return synthesizeUnifiedDiff(files), nil
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

// ListCommits returns the MR's commits oldest-first. GitLab serves them
// newest-first, so the collected pages are reversed to match the interface
// contract (and githubf's ordering).
func (d *Driver) ListCommits(ctx context.Context, pr *forge.PullRequestDetails) ([]forge.Commit, error) {
	const op = "list_commits"
	var commits []forge.Commit
	page := int64(1)
	for range maxCommitPages {
		rows, resp, err := d.client.MergeRequests.GetMergeRequestCommits(
			projectID(pr.Repository), int64(pr.Number), //nolint:gosec // G115: pull request numbers are small forge-assigned integers
			&gitlab.GetMergeRequestCommitsOptions{
				ListOptions: gitlab.ListOptions{Page: page, PerPage: 100},
			}, gitlab.WithContext(ctx))
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
	for i, j := 0, len(commits)-1; i < j; i, j = i+1, j-1 {
		commits[i], commits[j] = commits[j], commits[i]
	}
	return commits, nil
}

// convertCommit maps one REST commit row to the neutral commit type.
func convertCommit(row *gitlab.Commit) forge.Commit {
	shortOID := row.ShortID
	if shortOID == "" {
		shortOID = row.ID
		if len(shortOID) > 7 {
			shortOID = shortOID[:7]
		}
	}
	return forge.Commit{
		OID:       row.ID,
		ShortOID:  shortOID,
		Summary:   row.Title,
		Author:    row.AuthorName,
		Timestamp: row.CommittedDate,
	}
}
