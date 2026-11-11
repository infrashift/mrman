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

// hintNotOwnThread explains a refused resolution.
const hintNotOwnThread = "Only threads whose first comment this token's identity wrote can be " +
	"resolved from the command line; resolve other people's threads in the web UI."

var _ forge.ThreadResolver = (*Driver)(nil)

// ResolveOwnThread sets an open thread's status to fixed (Azure DevOps'
// "Resolved"), after posting reply as a comment in it when reply is not
// empty. The thread's root comment must be the viewer's own: the author's
// identity id is compared with the token's, not a display name.
func (d *Driver) ResolveOwnThread(ctx context.Context, pr *forge.PullRequestDetails, threadID, reply string) (forge.ResolveOutcome, error) {
	const op = "resolve_thread"
	id, err := strconv.Atoi(threadID)
	if err != nil || id <= 0 {
		return "", d.err(op, forge.ErrorValidation, 0, "", fmt.Errorf("thread id %q is not an Azure DevOps thread id", threadID))
	}
	viewer, err := d.viewerUser(ctx, op)
	if err != nil {
		return "", err
	}
	project, repoName := d.coords(pr.Repository)
	prID := int(pr.Number) //nolint:gosec // G115: pull request numbers are small forge-assigned integers
	thread, err := d.gitClient.GetPullRequestThread(ctx, git.GetPullRequestThreadArgs{
		RepositoryId: &repoName, Project: &project, PullRequestId: &prID, ThreadId: &id,
	})
	if err != nil {
		return "", d.wrap(op, err)
	}
	if thread == nil || (thread.IsDeleted != nil && *thread.IsDeleted) {
		return "", d.err(op, forge.ErrorNotFound, 0, "", fmt.Errorf("thread %d not found on pull request %d", id, prID))
	}
	root := humanComments(thread)
	if len(root) == 0 || root[0].Author == nil || root[0].Author.Id == nil ||
		!strings.EqualFold(*root[0].Author.Id, viewer.ID) {
		return "", d.err(op, forge.ErrorForbidden, 0, hintNotOwnThread,
			errors.New("thread "+threadID+" was not started by this token's identity"))
	}
	if isResolvedStatus(thread.Status) {
		return forge.ResolveAlreadyResolved, nil
	}
	if reply != "" {
		parent := derefInt(root[0].Id)
		text := git.CommentTypeValues.Text
		if _, err := d.gitClient.CreateComment(ctx, git.CreateCommentArgs{
			Comment:      &git.Comment{Content: &reply, ParentCommentId: &parent, CommentType: &text},
			RepositoryId: &repoName, Project: &project, PullRequestId: &prID, ThreadId: &id,
		}); err != nil {
			return "", d.wrap(op, err)
		}
	}
	fixed := git.CommentThreadStatusValues.Fixed
	if _, err := d.gitClient.UpdateThread(ctx, git.UpdateThreadArgs{
		CommentThread: &git.GitPullRequestCommentThread{Status: &fixed},
		RepositoryId:  &repoName, Project: &project, PullRequestId: &prID, ThreadId: &id,
	}); err != nil {
		return "", d.wrap(op, err)
	}
	return forge.ResolveResolved, nil
}
