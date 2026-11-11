package azdof

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"

	"github.com/infrashift/mrman/internal/forge"
)

var _ forge.ThreadReplier = (*Driver)(nil)

// ReplyToThread posts body as a text comment in thread threadID, under
// comment parentID (the root comment when parentID is empty). A reply adds
// to a discussion and changes no one's comment or status, so it is not
// limited to the token's own threads.
func (d *Driver) ReplyToThread(ctx context.Context, pr *forge.PullRequestDetails, threadID, parentID, body string) (string, error) {
	const op = "reply_to_thread"
	id, err := strconv.Atoi(threadID)
	if err != nil || id <= 0 {
		return "", d.err(op, forge.ErrorValidation, 0, "", fmt.Errorf("thread id %q is not an Azure DevOps thread id", threadID))
	}
	if strings.TrimSpace(body) == "" {
		return "", d.err(op, forge.ErrorValidation, 0, "", fmt.Errorf("an empty reply"))
	}
	project, repoName := d.coords(pr.Repository)
	prID := int(pr.Number) //nolint:gosec // G115: pull request numbers are small forge-assigned integers
	parent := 0
	if parentID != "" {
		if parent, err = strconv.Atoi(parentID); err != nil || parent <= 0 {
			return "", d.err(op, forge.ErrorValidation, 0, "", fmt.Errorf("comment id %q is not an Azure DevOps comment id", parentID))
		}
	} else {
		thread, gerr := d.gitClient.GetPullRequestThread(ctx, git.GetPullRequestThreadArgs{
			RepositoryId: &repoName, Project: &project, PullRequestId: &prID, ThreadId: &id,
		})
		if gerr != nil {
			return "", d.wrap(op, gerr)
		}
		if thread == nil {
			return "", d.err(op, forge.ErrorNotFound, 0, "", fmt.Errorf("thread %d not found", id))
		}
		root := humanComments(thread)
		if len(root) == 0 {
			return "", d.err(op, forge.ErrorNotFound, 0, "", fmt.Errorf("thread %d has no comment to reply to", id))
		}
		parent = derefInt(root[0].Id)
	}
	text := git.CommentTypeValues.Text
	created, err := d.gitClient.CreateComment(ctx, git.CreateCommentArgs{
		Comment:      &git.Comment{Content: &body, ParentCommentId: &parent, CommentType: &text},
		RepositoryId: &repoName, Project: &project, PullRequestId: &prID, ThreadId: &id,
	})
	if err != nil {
		return "", d.wrap(op, err)
	}
	if created == nil {
		return "", nil
	}
	return strconv.Itoa(derefInt(created.Id)), nil
}
