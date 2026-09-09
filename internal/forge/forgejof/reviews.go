package forgejof

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	forgejo "codeberg.org/mvdkleijn/forgejo-sdk/forgejo/v3"

	"github.com/infrashift/mrman/internal/forge"
)

// maxReviewPages bounds ListPullReviews pagination.
const maxReviewPages = 10

// listReviews pages through every review of the PR.
func (d *Driver) listReviews(api *forgejo.Client, op string, pr *forge.PullRequestDetails) ([]*forgejo.PullReview, error) {
	var reviews []*forgejo.PullReview
	page := 1
	for range maxReviewPages {
		rows, resp, err := api.ListPullReviews(pr.Repository.Owner, pr.Repository.Name,
			int64(pr.Number), forgejo.ListPullReviewsOptions{ //nolint:gosec // G115: pull request numbers are small forge-assigned integers
				ListOptions: forgejo.ListOptions{Page: page, PageSize: 100},
			})
		if err != nil {
			return nil, d.wrap(op, resp, err)
		}
		reviews = append(reviews, rows...)
		if resp == nil || resp.NextPage == 0 {
			break
		}
		page = resp.NextPage
	}
	return reviews, nil
}

// ListReviewThreads synthesizes discussion threads from Forgejo's flat
// review comments: every review's code comments are fetched and grouped by
// (path, original_position, position) — replies land on the same anchor, so
// the group is the thread. Resolution is approximated by any comment in the
// group carrying a resolver; outdated by both positions being zero or the
// root's commit drifting from its original commit.
func (d *Driver) ListReviewThreads(ctx context.Context, pr *forge.PullRequestDetails) ([]forge.RemoteReviewThread, error) {
	const op = "list_review_threads"
	api, err := d.api(ctx)
	if err != nil {
		return nil, d.wrap(op, nil, err)
	}
	reviews, err := d.listReviews(api, op, pr)
	if err != nil {
		return nil, err
	}
	groups := make(map[string][]*forgejo.PullReviewComment)
	for _, review := range reviews {
		if review.CodeCommentsCount == 0 {
			continue
		}
		comments, resp, err := api.ListPullReviewComments(pr.Repository.Owner, pr.Repository.Name,
			int64(pr.Number), review.ID) //nolint:gosec // G115: pull request numbers are small forge-assigned integers
		if err != nil {
			return nil, d.wrap(op, resp, err)
		}
		for _, comment := range comments {
			key := fmt.Sprintf("%s\x00%d\x00%d", comment.Path, comment.OldLineNum, comment.LineNum)
			groups[key] = append(groups[key], comment)
		}
	}
	threads := make([]forge.RemoteReviewThread, 0, len(groups))
	for _, group := range groups {
		threads = append(threads, synthesizeThread(group))
	}
	// Posted order: threads by root creation time, id as tiebreaker.
	slices.SortFunc(threads, func(a, b forge.RemoteReviewThread) int {
		ra, rb := a.Root(), b.Root()
		if ra != nil && rb != nil && ra.CreatedAt != nil && rb.CreatedAt != nil {
			if c := ra.CreatedAt.Compare(*rb.CreatedAt); c != 0 {
				return c
			}
		}
		return strings.Compare(a.ID, b.ID)
	})
	return threads, nil
}

// synthesizeThread builds one neutral thread from a group of comments that
// share an anchor. The earliest comment is the root; later ones become
// replies to it.
func synthesizeThread(group []*forgejo.PullReviewComment) forge.RemoteReviewThread {
	slices.SortFunc(group, func(a, b *forgejo.PullReviewComment) int {
		if c := a.Created.Compare(b.Created); c != 0 {
			return c
		}
		return int(a.ID - b.ID)
	})
	root := group[0]
	side := forge.SideNew
	var line *uint32
	switch {
	case root.OldLineNum != 0:
		// Old-side comments report original_position only.
		side = forge.SideOld
		anchor := uint32(root.OldLineNum) //nolint:gosec // G115: line numbers fit uint32
		line = &anchor
	case root.LineNum != 0:
		anchor := uint32(root.LineNum) //nolint:gosec // G115: line numbers fit uint32
		line = &anchor
	}
	thread := forge.RemoteReviewThread{
		ID:   strconv.FormatInt(root.ID, 10),
		Path: root.Path,
		Line: line,
		Side: side,
		IsOutdated: (root.LineNum == 0 && root.OldLineNum == 0) ||
			(root.CommitID != "" && root.OrigCommitID != "" && root.CommitID != root.OrigCommitID),
	}
	for i, raw := range group {
		comment := forge.RemoteReviewComment{
			ID:   strconv.FormatInt(raw.ID, 10),
			Body: raw.Body,
			URL:  raw.HTMLURL,
		}
		if !raw.Created.IsZero() {
			created := raw.Created
			comment.CreatedAt = &created
		}
		if raw.Reviewer != nil {
			comment.Author = raw.Reviewer.UserName
		}
		if i > 0 {
			comment.InReplyTo = thread.ID
		}
		if raw.Resolver != nil {
			thread.IsResolved = true
		}
		thread.Comments = append(thread.Comments, comment)
	}
	return thread
}

// ListReviewSummaries fetches review-level bodies. Reviews with empty
// bodies (bare approvals, review-request records) are dropped — they have
// nothing to display. ReviewMetadata keeps them.
func (d *Driver) ListReviewSummaries(ctx context.Context, pr *forge.PullRequestDetails) ([]forge.RemoteReviewSummary, error) {
	const op = "list_review_summaries"
	api, err := d.api(ctx)
	if err != nil {
		return nil, d.wrap(op, nil, err)
	}
	reviews, err := d.listReviews(api, op, pr)
	if err != nil {
		return nil, err
	}
	summaries := make([]forge.RemoteReviewSummary, 0, len(reviews))
	for _, review := range reviews {
		if strings.TrimSpace(review.Body) == "" {
			continue
		}
		summary := forge.RemoteReviewSummary{
			ID:    strconv.FormatInt(review.ID, 10),
			Body:  review.Body,
			State: reviewState(review),
			URL:   review.HTMLURL,
		}
		if review.Reviewer != nil {
			summary.Author = review.Reviewer.UserName
		}
		if !review.Submitted.IsZero() {
			submitted := review.Submitted
			summary.CreatedAt = &submitted
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}

// reviewState maps a Forgejo review record onto the neutral review state.
func reviewState(review *forgejo.PullReview) forge.ReviewState {
	if review.Dismissed {
		return forge.ReviewDismissed
	}
	switch review.State {
	case forgejo.ReviewStateApproved:
		return forge.ReviewApproved
	case forgejo.ReviewStateRequestChanges:
		return forge.ReviewChangesRequested
	case forgejo.ReviewStatePending:
		return forge.ReviewPending
	}
	return forge.ReviewCommented
}

// ReviewMetadata fetches the viewer login plus every review's author, time,
// and commit id for commit-scope inference. Empty-body reviews count;
// REQUEST_REVIEW rows do not — they record a reviewer being asked, not a
// review happening.
func (d *Driver) ReviewMetadata(ctx context.Context, pr *forge.PullRequestDetails) (*forge.ReviewMetadata, error) {
	const op = "review_metadata"
	api, err := d.api(ctx)
	if err != nil {
		return nil, d.wrap(op, nil, err)
	}
	viewer, resp, err := api.GetMyUserInfo()
	if err != nil {
		return nil, d.wrap(op, resp, err)
	}
	reviews, err := d.listReviews(api, op, pr)
	if err != nil {
		return nil, err
	}
	metadata := &forge.ReviewMetadata{ViewerLogin: viewer.UserName}
	for _, review := range reviews {
		if review.State == forgejo.ReviewStateRequestReview {
			continue
		}
		record := forge.ReviewRecord{CommitOID: review.CommitID}
		if review.Reviewer != nil {
			record.Author = review.Reviewer.UserName
		}
		if !review.Submitted.IsZero() {
			submitted := review.Submitted
			record.SubmittedAt = &submitted
		}
		metadata.Reviews = append(metadata.Reviews, record)
	}
	return metadata, nil
}
