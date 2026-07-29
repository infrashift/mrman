package forgejof

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	forgejo "codeberg.org/mvdkleijn/forgejo-sdk/forgejo/v3"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/submit"
)

// CreateReview submits a review atomically via
// POST /repos/{owner}/{repo}/pulls/{index}/reviews. Draft maps to the
// PENDING event, which keeps the review pending server-side — it is
// finished (or discarded) in the Forgejo web UI, mirroring the GitHub UX.
//
// Forgejo inline comments are single-line (one old_position/new_position);
// the submit.DowngradeMultiline pass collapses ranges upstream, and a range
// that still reaches the driver is rejected before any network call.
func (d *Driver) CreateReview(ctx context.Context, pr *forge.PullRequestDetails, req forge.CreateReviewRequest) (*forge.SubmitResult, error) {
	const op = "create_review"
	state, err := forgejoEvent(req.Event)
	if err != nil {
		return nil, d.err(op, forge.ErrorValidation, 0, "", err)
	}
	comments := make([]forgejo.CreatePullReviewComment, 0, len(req.Comments))
	for i := range req.Comments {
		comment, err := encodeComment(&req.Comments[i])
		if err != nil {
			return nil, d.err(op, forge.ErrorValidation, 0, "", err)
		}
		comments = append(comments, comment)
	}
	opt := forgejo.CreatePullReviewOptions{
		State:    state,
		Body:     req.Body,
		CommitID: req.CommitID,
		Comments: comments,
	}
	// The SDK validates client-side too, but its plain error would classify
	// as a network failure; catching it here keeps the kind honest.
	if err := opt.Validate(); err != nil {
		return nil, d.err(op, forge.ErrorValidation, 0, "", err)
	}
	api, err := d.api(ctx)
	if err != nil {
		return nil, d.wrap(op, nil, err)
	}
	review, resp, err := api.CreatePullReview(pr.Repository.Owner, pr.Repository.Name,
		int64(pr.Number), opt)
	if err != nil {
		return nil, d.wrapCreateReview(resp, err)
	}
	result := &forge.SubmitResult{
		ReviewID: strconv.FormatInt(review.ID, 10),
		URL:      review.HTMLURL,
		State:    string(review.State),
	}
	if result.URL == "" {
		result.URL = pr.URL
	}
	return result, nil
}

// forgejoEvent maps the neutral submit event onto Forgejo's review event.
func forgejoEvent(event forge.SubmitEvent) (forgejo.ReviewStateType, error) {
	switch event {
	case forge.SubmitComment:
		return forgejo.ReviewStateComment, nil
	case forge.SubmitApprove:
		return forgejo.ReviewStateApproved, nil
	case forge.SubmitRequestChanges:
		return forgejo.ReviewStateRequestChanges, nil
	case forge.SubmitDraft:
		return forgejo.ReviewStatePending, nil
	default:
		return forgejo.ReviewStateUnknown, fmt.Errorf("unknown submit event %d", event)
	}
}

// encodeComment converts one neutral inline comment into Forgejo's wire
// shape: the anchor line lands in old_position or new_position depending on
// the side, with the other left zero (Forgejo rejects both being set).
// CounterpartLine and OldPath have no Forgejo encoding and are not sent.
func encodeComment(c *submit.InlineComment) (forgejo.CreatePullReviewComment, error) {
	if c.StartLine != nil {
		return forgejo.CreatePullReviewComment{}, fmt.Errorf(
			"comment %q spans lines %d-%d, but Forgejo comments are single-line; "+
				"the multiline downgrade pass must run before submit",
			c.CommentID, *c.StartLine, c.Line)
	}
	comment := forgejo.CreatePullReviewComment{Path: c.Path, Body: c.Body}
	if c.Side == submit.SideOld {
		comment.OldLineNum = int64(c.Line)
	} else {
		comment.NewLineNum = int64(c.Line)
	}
	return comment, nil
}

// wrapCreateReview layers submit-specific hints over the generic
// translator: permission failures name the missing write scope and payload
// rejections point at stale anchors.
func (d *Driver) wrapCreateReview(resp *forgejo.Response, err error) error {
	const op = "create_review"
	var fe *forge.Error
	if errors.As(err, &fe) {
		return err
	}
	switch status := responseStatus(resp); status {
	case http.StatusForbidden:
		return d.err(op, forge.ErrorForbidden, status, hintReviewForbidden, err)
	case http.StatusUnprocessableEntity:
		return d.err(op, forge.ErrorValidation, status, hintReviewRejected, err)
	}
	return d.wrap(op, resp, err)
}
