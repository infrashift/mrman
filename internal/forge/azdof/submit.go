package azdof

import (
	"context"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/submit"
)

// Azure DevOps votes; the reviewer endpoint records them per identity.
const (
	voteApprove        = 10
	voteRequestChanges = -10
)

// CreateReview posts a review as Azure DevOps' N+1 sequence: the review
// body becomes a context-less thread, each inline comment its own
// file-anchored thread (with iteration context and change-tracking id from
// the PR's ForgePayload), and approve/request-changes cast a vote on the
// viewer's reviewer record. There is no server-side review object, so
// SubmitResult.ReviewID stays empty. A mid-sequence failure returns a
// result with Partial set (and a nil error) naming the comments that made
// it, so the app flips only those to submitted.
func (d *Driver) CreateReview(ctx context.Context, pr *forge.PullRequestDetails, req forge.CreateReviewRequest) (*forge.SubmitResult, error) {
	const op = "create_review"
	if req.Event == forge.SubmitDraft {
		// Capabilities.DraftReviews is false, so the app never sends this;
		// defensive only.
		return nil, d.err(op, forge.ErrorUnsupported, 0, hintNoDraft,
			errs.Unsupportedf("azure devops has no draft reviews"))
	}

	project, repoName := d.coords(pr.Repository)
	prID := int(pr.Number)
	payload := decodePayload(pr)
	result := &forge.SubmitResult{
		URL:   pr.URL,
		State: string(submitState(req.Event)),
	}

	if req.Body != "" {
		if _, err := d.createThread(ctx, project, repoName, prID, bodyThread(req.Body)); err != nil {
			// Nothing has been posted yet; fail the submit outright.
			return nil, d.wrap(op, err)
		}
	}

	succeeded := make([]string, 0, len(req.Comments))
	for i := range req.Comments {
		comment := &req.Comments[i]
		if _, err := d.createThread(ctx, project, repoName, prID, inlineThread(comment, payload)); err != nil {
			result.Partial = &forge.PartialFailure{
				SucceededCommentIDs: succeeded,
				FailedAt:            i,
				Cause:               d.wrap(op, err),
			}
			return result, nil
		}
		succeeded = append(succeeded, comment.CommentID)
	}

	if req.Event == forge.SubmitApprove || req.Event == forge.SubmitRequestChanges {
		if err := d.castVote(ctx, op, project, repoName, prID, req.Event); err != nil {
			if req.Body == "" && len(req.Comments) == 0 {
				// Pure vote submit: nothing landed, so fail outright.
				return nil, err
			}
			result.Partial = &forge.PartialFailure{
				SucceededCommentIDs: succeeded,
				FailedAt:            len(req.Comments),
				Cause:               err,
			}
			return result, nil
		}
	}
	return result, nil
}

// createThread posts one comment thread.
func (d *Driver) createThread(ctx context.Context, project, repoName string, prID int, thread *git.GitPullRequestCommentThread) (*git.GitPullRequestCommentThread, error) {
	return d.gitClient.CreateThread(ctx, git.CreateThreadArgs{
		CommentThread: thread,
		RepositoryId:  &repoName,
		Project:       &project,
		PullRequestId: &prID,
	})
}

// castVote records the viewer's vote on their own reviewer entry.
func (d *Driver) castVote(ctx context.Context, op, project, repoName string, prID int, event forge.SubmitEvent) error {
	viewer, err := d.viewerUser(ctx, op)
	if err != nil {
		return err
	}
	vote := voteApprove
	if event == forge.SubmitRequestChanges {
		vote = voteRequestChanges
	}
	viewerID := viewer.ID
	reviewer := git.IdentityRefWithVote{Id: &viewerID, Vote: &vote}
	_, err = d.gitClient.CreatePullRequestReviewer(ctx, git.CreatePullRequestReviewerArgs{
		Reviewer:      &reviewer,
		RepositoryId:  &repoName,
		Project:       &project,
		PullRequestId: &prID,
		ReviewerId:    &viewerID,
	})
	if err != nil {
		return d.wrap(op, err)
	}
	return nil
}

// submitState maps the neutral event onto the resulting review state.
func submitState(event forge.SubmitEvent) forge.ReviewState {
	switch event {
	case forge.SubmitApprove:
		return forge.ReviewApproved
	case forge.SubmitRequestChanges:
		return forge.ReviewChangesRequested
	case forge.SubmitComment, forge.SubmitDraft:
		return forge.ReviewCommented
	}
	return forge.ReviewCommented
}

// bodyThread builds the context-less thread holding the review body.
func bodyThread(body string) *git.GitPullRequestCommentThread {
	return &git.GitPullRequestCommentThread{
		Status:   &git.CommentThreadStatusValues.Active,
		Comments: &[]git.Comment{newComment(body)},
	}
}

// inlineThread builds a file-anchored thread for one inline comment. The
// position spans [StartLine, Line] with character offset 1 on both ends
// (official samples and the web UI anchor line comments at offset 1);
// new-side comments fill the right file positions, old-side the left. When
// the ForgePayload carries iteration data, the pull-request thread context
// pins the comment to iteration 1..latest and the file's change-tracking
// id so Azure DevOps can track it across future iterations.
func inlineThread(comment *submit.InlineComment, payload forgePayload) *git.GitPullRequestCommentThread {
	path := comment.Path
	if comment.Side == submit.SideOld && comment.OldPath != nil {
		path = *comment.OldPath
	}
	filePath := "/" + path

	end := position(comment.Line)
	start := end
	if comment.StartLine != nil {
		start = position(*comment.StartLine)
	}
	threadContext := &git.CommentThreadContext{FilePath: &filePath}
	if comment.Side == submit.SideOld {
		threadContext.LeftFileStart = start
		threadContext.LeftFileEnd = end
	} else {
		threadContext.RightFileStart = start
		threadContext.RightFileEnd = end
	}

	thread := &git.GitPullRequestCommentThread{
		Status:        &git.CommentThreadStatusValues.Active,
		Comments:      &[]git.Comment{newComment(comment.Body)},
		ThreadContext: threadContext,
	}
	if payload.IterationID > 0 {
		first := 1
		second := payload.IterationID
		prContext := &git.GitPullRequestCommentThreadContext{
			IterationContext: &git.CommentIterationContext{
				FirstComparingIteration:  &first,
				SecondComparingIteration: &second,
			},
		}
		if trackingID, ok := payload.ChangeTracking[filePath]; ok {
			prContext.ChangeTrackingId = &trackingID
		}
		thread.PullRequestThreadContext = prContext
	}
	return thread
}

// newComment builds a root text comment.
func newComment(content string) git.Comment {
	parent := 0
	return git.Comment{
		ParentCommentId: &parent,
		Content:         &content,
		CommentType:     &git.CommentTypeValues.Text,
	}
}

// position builds a line-anchored comment position at character offset 1.
func position(line uint32) *git.CommentPosition {
	l := int(line)
	offset := 1
	return &git.CommentPosition{Line: &l, Offset: &offset}
}
