package azdof

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/webapi"

	"github.com/infrashift/mrman/internal/diffgen"
	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

// maxCommitPages bounds ListCommits continuation-token pagination.
const maxCommitPages = 10

// maxChangePages bounds iteration-change pagination; at 2000 changes per
// page this is far beyond any reviewable PR.
const maxChangePages = 20

// changesPageSize is the iteration-changes page size (the API maximum).
const changesPageSize = 2000

// forgePayload is the driver-private anchoring data round-tripped through
// sessions inside PullRequestDetails.ForgePayload.
type forgePayload struct {
	// IterationID is the latest iteration at fetch time; thread creation
	// anchors comments to it.
	IterationID int `json:"iteration_id"`
	// ChangeTracking maps ADO item paths (leading slash) to the
	// changeTrackingId used to track comments across iterations.
	ChangeTracking map[string]int `json:"change_tracking"`
}

// decodePayload parses the details' ForgePayload; a missing or malformed
// payload yields the zero value so callers can fall back to refetching.
func decodePayload(pr *forge.PullRequestDetails) forgePayload {
	var payload forgePayload
	if len(pr.ForgePayload) > 0 {
		_ = json.Unmarshal(pr.ForgePayload, &payload)
	}
	return payload
}

// GetPullRequest fetches the full PR record plus the iteration and
// change-tracking data thread creation needs, cached into ForgePayload.
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
	project, repoName := d.coords(*target.Repository)
	prID := int(target.Number) //nolint:gosec // G115: pull request numbers are small forge-assigned integers
	pr, err := d.gitClient.GetPullRequestById(ctx, git.GetPullRequestByIdArgs{
		PullRequestId: &prID,
		Project:       &project,
	})
	if err != nil {
		return nil, d.wrap(op, err)
	}
	headSHA, baseSHA := "", ""
	if pr.LastMergeSourceCommit != nil && pr.LastMergeSourceCommit.CommitId != nil {
		headSHA = *pr.LastMergeSourceCommit.CommitId
	}
	if pr.LastMergeTargetCommit != nil && pr.LastMergeTargetCommit.CommitId != nil {
		baseSHA = *pr.LastMergeTargetCommit.CommitId
	}
	if headSHA == "" || baseSHA == "" {
		return nil, d.err(op, forge.ErrorValidation, 0, "",
			fmt.Errorf("response did not include merge source/target commits for MR #%d", target.Number))
	}

	details := &forge.PullRequestDetails{
		PullRequestSummary: d.summary(*target.Repository, project, repoName, pr),
		HeadSHA:            headSHA,
		BaseSHA:            baseSHA,
		Body:               deref(pr.Description),
		Closed:             prStatus(pr) != git.PullRequestStatusValues.Active,
		MergedAt:           mergedAt(pr),
	}

	payload, mergeBase, err := d.iterationPayload(ctx, op, project, repoName, prID)
	if err != nil {
		return nil, err
	}
	if mergeBase != "" {
		// lastMergeTargetCommit is the target branch's tip: diffing against
		// it would show the target's own later changes, reversed.
		details.BaseSHA = mergeBase
	}
	if payload.IterationID > 0 {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, d.err(op, forge.ErrorValidation, 0, "", err)
		}
		details.ForgePayload = raw
	}
	return details, nil
}

// iterationPayload fetches the latest iteration id and its per-path change
// tracking ids, plus that iteration's merge base (its commonRefCommit, ""
// when absent). A PR without iterations yields the zero payload so callers
// degrade gracefully instead of failing the whole fetch.
func (d *Driver) iterationPayload(ctx context.Context, op, project, repoName string, prID int) (forgePayload, string, error) {
	iterations, err := d.gitClient.GetPullRequestIterations(ctx, git.GetPullRequestIterationsArgs{
		RepositoryId:  &repoName,
		Project:       &project,
		PullRequestId: &prID,
	})
	if err != nil {
		return forgePayload{}, "", d.wrap(op, err)
	}
	latest, mergeBase := 0, ""
	if iterations != nil {
		for _, it := range *iterations {
			if it.Id != nil && *it.Id > latest {
				latest = *it.Id
				mergeBase = ""
				if it.CommonRefCommit != nil {
					mergeBase = deref(it.CommonRefCommit.CommitId)
				}
			}
		}
	}
	if latest == 0 {
		return forgePayload{}, "", nil
	}
	changes, err := d.iterationChanges(ctx, op, project, repoName, prID, latest)
	if err != nil {
		return forgePayload{}, "", err
	}
	tracking := make(map[string]int, len(changes))
	for _, change := range changes {
		if change.path != "" && change.changeTrackingID != 0 {
			tracking[change.path] = change.changeTrackingID
		}
	}
	return forgePayload{IterationID: latest, ChangeTracking: tracking}, mergeBase, nil
}

// iterationChange is the digested form of one iteration change entry.
type iterationChange struct {
	// path is the current-side ADO item path (leading slash).
	path string
	// originalPath is the base-side path for renames, "" otherwise.
	originalPath string
	// changeType is the raw ADO change type ("edit", "add", "delete",
	// "rename", "rename, edit", ...).
	changeType string
	// changeTrackingID tracks the file across iterations, 0 when absent.
	changeTrackingID int
	// isFolder marks tree entries, which carry no diffable content.
	isFolder bool
}

// iterationChanges pages through the iteration's change entries.
func (d *Driver) iterationChanges(ctx context.Context, op, project, repoName string, prID, iterationID int) ([]iterationChange, error) {
	var out []iterationChange
	skip := 0
	for range maxChangePages {
		top := changesPageSize
		skipArg := skip
		page, err := d.gitClient.GetPullRequestIterationChanges(ctx, git.GetPullRequestIterationChangesArgs{
			RepositoryId:  &repoName,
			Project:       &project,
			PullRequestId: &prID,
			IterationId:   &iterationID,
			Top:           &top,
			Skip:          &skipArg,
		})
		if err != nil {
			return nil, d.wrap(op, err)
		}
		if page.ChangeEntries != nil {
			for i := range *page.ChangeEntries {
				out = append(out, digestChange(&(*page.ChangeEntries)[i]))
			}
		}
		if page.NextSkip == nil || *page.NextSkip == 0 {
			break
		}
		skip = *page.NextSkip
	}
	return out, nil
}

// digestChange extracts the fields the driver needs from one change entry.
// The SDK types Item as interface{}, so path and folder-ness come from the
// raw JSON map.
func digestChange(change *git.GitPullRequestChange) iterationChange {
	out := iterationChange{
		originalPath: deref(change.OriginalPath),
	}
	if change.ChangeType != nil {
		out.changeType = string(*change.ChangeType)
	}
	if change.ChangeTrackingId != nil {
		out.changeTrackingID = *change.ChangeTrackingId
	}
	if item, ok := change.Item.(map[string]any); ok {
		if path, ok := item["path"].(string); ok {
			out.path = path
		}
		if isFolder, ok := item["isFolder"].(bool); ok && isFolder {
			out.isFolder = true
		}
		if objectType, ok := item["gitObjectType"].(string); ok && objectType == "tree" {
			out.isFolder = true
		}
	}

	// A deleted file has no current-side item: Azure DevOps sends a null
	// item path and names the file in originalPath (seen live). Without
	// this every deleted file was skipped and vanished from the review.
	if out.path == "" && isDeleteChange(out.changeType) {
		out.path = out.originalPath
	}
	return out
}

// isDeleteChange reports whether an ADO change type deletes the file
// ("delete", possibly combined), as opposed to "undelete".
func isDeleteChange(kind string) bool {
	return strings.Contains(kind, "delete") && !strings.Contains(kind, "undelete")
}

// GetDiff synthesizes the PR's cumulative unified diff: Azure DevOps has no
// text-diff endpoint, so the latest iteration's change list supplies the
// paths and change types, blob pairs are fetched at the base and head
// commits, and an internal Myers diff renders git-style hunks. When the
// local checkout already has both SHAs, `git diff` answers directly.
func (d *Driver) GetDiff(ctx context.Context, pr *forge.PullRequestDetails) (string, error) {
	const op = "get_diff"
	if diff, ok := d.localRangeDiff(pr.BaseSHA, pr.HeadSHA); ok {
		return diff, nil
	}
	project, repoName := d.coords(pr.Repository)
	prID := int(pr.Number) //nolint:gosec // G115: pull request numbers are small forge-assigned integers

	iterationID := decodePayload(pr).IterationID
	if iterationID == 0 {
		payload, _, err := d.iterationPayload(ctx, op, project, repoName, prID)
		if err != nil {
			return "", err
		}
		iterationID = payload.IterationID
	}
	if iterationID == 0 {
		return "", d.err(op, forge.ErrorValidation, 0, "",
			fmt.Errorf("merge request #%d has no iterations to diff", pr.Number))
	}
	changes, err := d.iterationChanges(ctx, op, project, repoName, prID, iterationID)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	for _, change := range changes {
		if change.isFolder || change.path == "" {
			continue
		}
		fileDiff, err := d.synthesizeFileDiff(ctx, op, project, repoName, pr, change)
		if err != nil {
			return "", err
		}
		sb.WriteString(fileDiff)
	}
	return sb.String(), nil
}

// synthesizeFileDiff fetches the blob pair for one change entry and renders
// its unified diff.
func (d *Driver) synthesizeFileDiff(ctx context.Context, op, project, repoName string, pr *forge.PullRequestDetails, change iterationChange) (string, error) {
	newPath := strings.TrimPrefix(change.path, "/")
	oldPath := newPath
	if change.originalPath != "" {
		oldPath = strings.TrimPrefix(change.originalPath, "/")
	}
	kind := change.changeType
	isAdd := strings.Contains(kind, "add")
	isDelete := isDeleteChange(kind)

	oldContent, newContent := "", ""
	if isAdd {
		oldPath = ""
	} else {
		content, err := d.blobAt(ctx, op, project, repoName, pr.BaseSHA, oldPath)
		if err != nil {
			return "", err
		}
		oldContent = content
	}
	if isDelete {
		// A delete's path is the removed file's (digestChange fills it
		// from originalPath); it has no current side.
		oldPath = newPath
		newPath = ""
	} else {
		content, err := d.blobAt(ctx, op, project, repoName, pr.HeadSHA, newPath)
		if err != nil {
			return "", err
		}
		newContent = content
	}
	return diffgen.UnifiedFileDiff(oldPath, newPath, oldContent, newContent, diffgen.Options{}), nil
}

// GetCommitRangeDiff is unsupported: Azure DevOps serves no text diff for
// arbitrary commit ranges (Capabilities.CommitRangeDiff is false, so the
// app never calls this; the error is defensive).
func (d *Driver) GetCommitRangeDiff(context.Context, *forge.PullRequestDetails, string, string) (string, error) {
	const op = "get_commit_range_diff"
	return "", d.err(op, forge.ErrorUnsupported, 0, hintNoRangeDiff,
		errs.Unsupportedf("azure devops serves no commit-range diff"))
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

// ListCommits returns the PR's commits oldest-first. The API returns them
// newest-first with header-based continuation tokens; the SDK wrapper
// exposes the token but cannot send one, so follow-up pages go through a
// raw call on the same client.
func (d *Driver) ListCommits(ctx context.Context, pr *forge.PullRequestDetails) ([]forge.Commit, error) {
	const op = "list_commits"
	project, repoName := d.coords(pr.Repository)
	prID := int(pr.Number) //nolint:gosec // G115: pull request numbers are small forge-assigned integers
	page, err := d.gitClient.GetPullRequestCommits(ctx, git.GetPullRequestCommitsArgs{
		RepositoryId:  &repoName,
		Project:       &project,
		PullRequestId: &prID,
	})
	if err != nil {
		return nil, d.wrap(op, err)
	}
	rows := page.Value
	token := page.ContinuationToken
	for range maxCommitPages {
		if token == "" {
			break
		}
		more, next, err := d.rawCommitPage(ctx, op, project, repoName, prID, token)
		if err != nil {
			return nil, err
		}
		rows = append(rows, more...)
		token = next
	}
	commits := make([]forge.Commit, 0, len(rows))
	for i := range rows {
		// Reverse to oldest-first to match the neutral contract.
		commits = append(commits, convertCommit(&rows[len(rows)-1-i]))
	}
	return commits, nil
}

// rawCommitPage fetches one continuation page of PR commits, which the SDK
// wrapper cannot request.
func (d *Driver) rawCommitPage(ctx context.Context, op, project, repoName string, prID int, token string) ([]git.GitCommitRef, string, error) {
	requestURL := fmt.Sprintf("%s/%s/_apis/git/repositories/%s/pullrequests/%d/commits?continuationToken=%s",
		d.orgURL, url.PathEscape(project), url.PathEscape(repoName), prID, url.QueryEscape(token))
	req, err := d.core.CreateRequestMessage(ctx, http.MethodGet, requestURL, "7.1",
		nil, "", azuredevops.MediaTypeApplicationJson, nil)
	if err != nil {
		return nil, "", d.wrap(op, err)
	}
	resp, err := d.core.SendRequest(req) //nolint:bodyclose // closed by d.core.UnmarshalCollectionBody
	if err != nil {
		return nil, "", d.wrap(op, err)
	}
	var rows []git.GitCommitRef
	if err := d.core.UnmarshalCollectionBody(resp, &rows); err != nil {
		return nil, "", d.wrap(op, err)
	}
	return rows, resp.Header.Get(azuredevops.HeaderKeyContinuationToken), nil
}

// convertCommit maps one commit row to the neutral commit type.
func convertCommit(row *git.GitCommitRef) forge.Commit {
	sha := deref(row.CommitId)
	shortOID := sha
	if len(shortOID) > 7 {
		shortOID = shortOID[:7]
	}
	summary, _, _ := strings.Cut(deref(row.Comment), "\n")
	author := "unknown"
	var timestamp *time.Time
	if row.Author != nil {
		switch {
		case deref(row.Author.Name) != "":
			author = *row.Author.Name
		case deref(row.Author.Email) != "":
			author = *row.Author.Email
		}
		if row.Author.Date != nil {
			t := row.Author.Date.Time
			timestamp = &t
		}
	}
	return forge.Commit{
		OID:       sha,
		ShortOID:  shortOID,
		Summary:   summary,
		Author:    author,
		Timestamp: timestamp,
	}
}

// summary maps a GitPullRequest onto the neutral summary type. Azure
// DevOps exposes no updated-at timestamp on pull requests, so the creation
// date stands in (closed date when the PR is closed).
func (d *Driver) summary(repo forgetypes.Repository, project, repoName string, pr *git.GitPullRequest) forge.PullRequestSummary {
	number := uint64(0)
	if pr.PullRequestId != nil {
		number = uint64(*pr.PullRequestId) //nolint:gosec // G115: the forge never returns a negative id
	}
	updated := timeOf(pr.CreationDate)
	if t := timeOf(pr.ClosedDate); t != nil {
		updated = t
	}
	return forge.PullRequestSummary{
		Repository:  repo,
		Number:      number,
		Title:       deref(pr.Title),
		Author:      identityLabel(pr.CreatedBy),
		HeadRefName: strings.TrimPrefix(deref(pr.SourceRefName), "refs/heads/"),
		BaseRefName: strings.TrimPrefix(deref(pr.TargetRefName), "refs/heads/"),
		UpdatedAt:   updated,
		URL:         d.prWebURL(project, repoName, number),
		State:       stateLabel(prStatus(pr)),
		IsDraft:     pr.IsDraft != nil && *pr.IsDraft,
	}
}

// stateLabel renders a PR status as the display state string.
func stateLabel(status git.PullRequestStatus) string {
	switch status {
	case git.PullRequestStatusValues.Active:
		return "open"
	case git.PullRequestStatusValues.Completed:
		return "merged"
	case git.PullRequestStatusValues.Abandoned:
		return "closed"
	}
	return string(status)
}

// timeOf converts an SDK timestamp into *time.Time.
func timeOf(ts *azuredevops.Time) *time.Time {
	if ts == nil {
		return nil
	}
	t := ts.Time
	return &t
}

// prStatus returns the PR's status, defaulting to notSet.
func prStatus(pr *git.GitPullRequest) git.PullRequestStatus {
	if pr.Status == nil {
		return git.PullRequestStatusValues.NotSet
	}
	return *pr.Status
}

// mergedAt approximates the merge time: Azure DevOps records only a closed
// date, which for completed PRs is the completion time.
func mergedAt(pr *git.GitPullRequest) *time.Time {
	if prStatus(pr) != git.PullRequestStatusValues.Completed || pr.ClosedDate == nil {
		return nil
	}
	t := pr.ClosedDate.Time
	return &t
}

// deref returns the pointed-to string, "" for nil.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// derefInt returns the pointed-to int, 0 for nil.
func derefInt(n *int) int {
	if n == nil {
		return 0
	}
	return *n
}

// identityLabel returns the best human label for an identity reference.
func identityLabel(ref *webapi.IdentityRef) string {
	if ref == nil {
		return ""
	}
	if name := deref(ref.DisplayName); name != "" {
		return name
	}
	return deref(ref.UniqueName)
}
