package azdof

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"

	"github.com/infrashift/mrman/internal/forge"
)

// hintNotOwnSummary explains a refused edit.
const hintNotOwnSummary = "Only pull-request-level comments this token's identity wrote can be " +
	"edited from the command line."

var _ forge.SummaryEditor = (*Driver)(nil)

// EditOwnSummary replaces the content of the first comment of the
// context-less thread summaryID, the form ListReviewSummaries reports a
// review summary in. That comment must be the viewer's own: its author's
// identity id is compared with the token's, not a display name. A thread
// anchored to a file is not a summary and is refused.
func (d *Driver) EditOwnSummary(ctx context.Context, pr *forge.PullRequestDetails, summaryID, body string) error {
	const op = "edit_summary"
	id, err := strconv.Atoi(summaryID)
	if err != nil || id <= 0 {
		return d.err(op, forge.ErrorValidation, 0, "", fmt.Errorf("summary id %q is not an Azure DevOps thread id", summaryID))
	}
	if strings.TrimSpace(body) == "" {
		return d.err(op, forge.ErrorValidation, 0, "", errors.New("an empty body"))
	}
	viewer, err := d.viewerUser(ctx, op)
	if err != nil {
		return err
	}
	project, repoName := d.coords(pr.Repository)
	prID := int(pr.Number) //nolint:gosec // G115: pull request numbers are small forge-assigned integers
	thread, err := d.gitClient.GetPullRequestThread(ctx, git.GetPullRequestThreadArgs{
		RepositoryId: &repoName, Project: &project, PullRequestId: &prID, ThreadId: &id,
	})
	if err != nil {
		return d.wrap(op, err)
	}
	if thread == nil || (thread.IsDeleted != nil && *thread.IsDeleted) {
		return d.err(op, forge.ErrorNotFound, 0, "", fmt.Errorf("thread %d not found on pull request %d", id, prID))
	}
	if threadFilePath(thread) != "" {
		return d.err(op, forge.ErrorValidation, 0, "",
			fmt.Errorf("thread %d is anchored to a file, not a pull-request-level comment", id))
	}
	root := humanComments(thread)
	if len(root) == 0 || root[0].Author == nil || root[0].Author.Id == nil ||
		!strings.EqualFold(*root[0].Author.Id, viewer.ID) {
		return d.err(op, forge.ErrorForbidden, 0, hintNotOwnSummary,
			errors.New("summary "+summaryID+" was not written by this token's identity"))
	}
	commentID := derefInt(root[0].Id)
	if _, err := d.gitClient.UpdateComment(ctx, git.UpdateCommentArgs{
		Comment:      &git.Comment{Content: &body},
		RepositoryId: &repoName, Project: &project, PullRequestId: &prID, ThreadId: &id, CommentId: &commentID,
	}); err != nil {
		return d.wrap(op, err)
	}
	return nil
}
