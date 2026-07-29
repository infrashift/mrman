package azdof

import (
	"context"
	"strconv"
	"strings"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"

	"github.com/infrashift/mrman/internal/forge"
)

// ListReviewThreads fetches the PR's comment threads and keeps the
// file-anchored ones. Thread status maps onto resolution (fixed, wontFix,
// closed, and byDesign count as resolved); outdated-ness is approximated:
// a thread the server no longer tracks against the pull request (no
// pullRequestThreadContext) is assumed stale.
func (d *Driver) ListReviewThreads(ctx context.Context, pr *forge.PullRequestDetails) ([]forge.RemoteReviewThread, error) {
	const op = "list_review_threads"
	rows, err := d.threads(ctx, op, pr)
	if err != nil {
		return nil, err
	}
	var threads []forge.RemoteReviewThread
	for i := range rows {
		thread := &rows[i]
		if !isHumanThread(thread) || threadFilePath(thread) == "" {
			continue
		}
		comments := convertThreadComments(thread)
		if len(comments) == 0 {
			continue
		}
		converted := forge.RemoteReviewThread{
			ID:         strconv.Itoa(derefInt(thread.Id)),
			Path:       strings.TrimPrefix(threadFilePath(thread), "/"),
			IsResolved: isResolvedStatus(thread.Status),
			IsOutdated: thread.PullRequestThreadContext == nil,
			Comments:   comments,
		}
		converted.Side, converted.Line, converted.StartLine = threadAnchor(thread.ThreadContext)
		threads = append(threads, converted)
	}
	return threads, nil
}

// ListReviewSummaries approximates review summaries from context-less
// threads: Azure DevOps has no review-level bodies, so a human thread
// without a file anchor is the closest thing to a review summary comment.
// The ReviewSummaries capability stays false to mark the approximation.
func (d *Driver) ListReviewSummaries(ctx context.Context, pr *forge.PullRequestDetails) ([]forge.RemoteReviewSummary, error) {
	const op = "list_review_summaries"
	rows, err := d.threads(ctx, op, pr)
	if err != nil {
		return nil, err
	}
	var summaries []forge.RemoteReviewSummary
	for i := range rows {
		thread := &rows[i]
		if !isHumanThread(thread) || threadFilePath(thread) != "" {
			continue
		}
		comments := convertThreadComments(thread)
		if len(comments) == 0 || strings.TrimSpace(comments[0].Body) == "" {
			continue
		}
		root := comments[0]
		summaries = append(summaries, forge.RemoteReviewSummary{
			ID:        strconv.Itoa(derefInt(thread.Id)),
			Author:    root.Author,
			Body:      root.Body,
			State:     forge.ReviewCommented,
			CreatedAt: root.CreatedAt,
		})
	}
	return summaries, nil
}

// ReviewMetadata reports the viewer identity and one review record per
// reviewer who cast a vote (10/5 approve, -5 waiting, -10 reject). Azure
// DevOps votes carry neither timestamps nor commit OIDs, so records have
// only authors and the CommitScopedReviews capability is false.
func (d *Driver) ReviewMetadata(ctx context.Context, pr *forge.PullRequestDetails) (*forge.ReviewMetadata, error) {
	const op = "review_metadata"
	viewer, err := d.viewerUser(ctx, op)
	if err != nil {
		return nil, err
	}
	prID := int(pr.Number)
	project, _ := d.coords(pr.Repository)
	record, err := d.gitClient.GetPullRequestById(ctx, git.GetPullRequestByIdArgs{
		PullRequestId: &prID,
		Project:       &project,
	})
	if err != nil {
		return nil, d.wrap(op, err)
	}
	metadata := &forge.ReviewMetadata{ViewerLogin: viewer.displayName()}
	if record.Reviewers != nil {
		for i := range *record.Reviewers {
			reviewer := &(*record.Reviewers)[i]
			if reviewer.Vote == nil || *reviewer.Vote == 0 {
				continue
			}
			metadata.Reviews = append(metadata.Reviews, forge.ReviewRecord{
				Author: reviewerLabel(reviewer),
			})
		}
	}
	return metadata, nil
}

// threads fetches all comment threads for the PR.
func (d *Driver) threads(ctx context.Context, op string, pr *forge.PullRequestDetails) ([]git.GitPullRequestCommentThread, error) {
	project, repoName := d.coords(pr.Repository)
	prID := int(pr.Number)
	rows, err := d.gitClient.GetThreads(ctx, git.GetThreadsArgs{
		RepositoryId:  &repoName,
		Project:       &project,
		PullRequestId: &prID,
	})
	if err != nil {
		return nil, d.wrap(op, err)
	}
	if rows == nil {
		return nil, nil
	}
	return *rows, nil
}

// isHumanThread reports whether the thread is live and carries at least one
// human comment (Azure DevOps files vote changes and pushes as system
// comment threads).
func isHumanThread(thread *git.GitPullRequestCommentThread) bool {
	if thread.IsDeleted != nil && *thread.IsDeleted {
		return false
	}
	return len(humanComments(thread)) > 0
}

// humanComments returns the thread's live, non-system comments.
func humanComments(thread *git.GitPullRequestCommentThread) []git.Comment {
	if thread.Comments == nil {
		return nil
	}
	var out []git.Comment
	for _, comment := range *thread.Comments {
		if comment.IsDeleted != nil && *comment.IsDeleted {
			continue
		}
		if comment.CommentType != nil && *comment.CommentType == git.CommentTypeValues.System {
			continue
		}
		out = append(out, comment)
	}
	return out
}

// convertThreadComments maps the thread's human comments onto the neutral
// comment type, root first as delivered.
func convertThreadComments(thread *git.GitPullRequestCommentThread) []forge.RemoteReviewComment {
	rows := humanComments(thread)
	comments := make([]forge.RemoteReviewComment, 0, len(rows))
	for i := range rows {
		comment := &rows[i]
		converted := forge.RemoteReviewComment{
			ID:        strconv.Itoa(derefInt(comment.Id)),
			Author:    identityLabel(comment.Author),
			Body:      deref(comment.Content),
			CreatedAt: timeOf(comment.PublishedDate),
		}
		if comment.ParentCommentId != nil && *comment.ParentCommentId > 0 {
			converted.InReplyTo = strconv.Itoa(*comment.ParentCommentId)
		}
		comments = append(comments, converted)
	}
	return comments
}

// threadFilePath returns the thread's anchored file path, "" for
// context-less threads.
func threadFilePath(thread *git.GitPullRequestCommentThread) string {
	if thread.ThreadContext == nil {
		return ""
	}
	return deref(thread.ThreadContext.FilePath)
}

// threadAnchor derives the neutral side, anchor line, and range start from
// a thread context: right-file positions anchor to the new side, left-file
// positions to the old side. The anchor line is the span's end; the start
// is reported only for real multi-line spans.
func threadAnchor(tc *git.CommentThreadContext) (forge.Side, *uint32, *uint32) {
	if tc == nil {
		return forge.SideNew, nil, nil
	}
	side := forge.SideNew
	start, end := tc.RightFileStart, tc.RightFileEnd
	if start == nil && end == nil {
		side = forge.SideOld
		start, end = tc.LeftFileStart, tc.LeftFileEnd
	}
	if end == nil {
		end = start
	}
	if start == nil {
		start = end
	}
	if end == nil || end.Line == nil {
		return side, nil, nil
	}
	line := uint32(*end.Line)
	var startLine *uint32
	if start != nil && start.Line != nil && *start.Line < *end.Line {
		s := uint32(*start.Line)
		startLine = &s
	}
	return side, &line, startLine
}

// isResolvedStatus reports whether a thread status counts as resolved.
func isResolvedStatus(status *git.CommentThreadStatus) bool {
	if status == nil {
		return false
	}
	switch *status {
	case git.CommentThreadStatusValues.Fixed,
		git.CommentThreadStatusValues.WontFix,
		git.CommentThreadStatusValues.Closed,
		git.CommentThreadStatusValues.ByDesign:
		return true
	}
	return false
}

// reviewerLabel returns the best human label for a reviewer identity.
func reviewerLabel(reviewer *git.IdentityRefWithVote) string {
	if reviewer == nil {
		return ""
	}
	if name := deref(reviewer.DisplayName); name != "" {
		return name
	}
	return deref(reviewer.UniqueName)
}
