package githubf

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/go-github/v76/github"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/submit"
)

// CreateReview submits a review atomically via
// POST /repos/{owner}/{repo}/pulls/{number}/reviews. Draft omits the
// `event` field entirely so GitHub creates a PENDING review.
func (d *Driver) CreateReview(ctx context.Context, pr *forge.PullRequestDetails, req forge.CreateReviewRequest) (*forge.SubmitResult, error) {
	const op = "create_review"
	event, err := githubEvent(req.Event)
	if err != nil {
		return nil, d.err(op, forge.ErrorValidation, 0, "", err)
	}
	reviewReq := &github.PullRequestReviewRequest{
		CommitID: new(req.CommitID),
		Body:     new(req.Body),
		Event:    event,
		Comments: draftComments(req.Comments),
	}
	review, _, err := d.rest.PullRequests.CreateReview(ctx, pr.Repository.Owner, pr.Repository.Name,
		int(pr.Number), reviewReq) //nolint:gosec // G115: pull request numbers are small forge-assigned integers
	if err != nil {
		return nil, d.wrapCreateReview(err)
	}
	return &forge.SubmitResult{
		ReviewID: strconv.FormatInt(review.GetID(), 10),
		URL:      review.GetHTMLURL(),
		State:    review.GetState(),
	}, nil
}

// githubEvent maps the neutral submit event onto GitHub's review event.
// Draft returns nil: omitting `event` is how the API creates a PENDING
// review.
func githubEvent(event forge.SubmitEvent) (*string, error) {
	switch event {
	case forge.SubmitComment:
		return new("COMMENT"), nil
	case forge.SubmitApprove:
		return new("APPROVE"), nil
	case forge.SubmitRequestChanges:
		return new("REQUEST_CHANGES"), nil
	case forge.SubmitDraft:
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown submit event %d", event)
	}
}

// draftComments encodes the neutral inline comments for GitHub: neutral
// sides become LEFT/RIGHT, start_line/start_side are attached only for
// range comments. CounterpartLine and OldPath are GitLab/Forgejo concerns
// and are intentionally not sent.
func draftComments(comments []submit.InlineComment) []*github.DraftReviewComment {
	if len(comments) == 0 {
		return nil
	}
	out := make([]*github.DraftReviewComment, 0, len(comments))
	for i := range comments {
		c := &comments[i]
		dc := &github.DraftReviewComment{
			Path: new(c.Path),
			Body: new(c.Body),
			Line: new(int(c.Line)),
			Side: new(githubSide(c.Side)),
		}
		if c.StartLine != nil {
			dc.StartLine = new(int(*c.StartLine))
		}
		if c.StartSide != nil {
			dc.StartSide = new(githubSide(*c.StartSide))
		}
		out = append(out, dc)
	}
	return out
}

// githubSide converts a neutral side to GitHub's LEFT/RIGHT encoding.
func githubSide(side submit.Side) string {
	if side == submit.SideOld {
		return "LEFT"
	}
	return "RIGHT"
}

// wrapCreateReview layers the submit-specific error mapping (ported from
// tuicr's gh.rs) over the generic translator: pending-review and
// unknown-commit 422s become conflicts with actionable hints, and
// permission failures name the missing PR write scope.
func (d *Driver) wrapCreateReview(err error) error {
	const op = "create_review"
	if ghErr, ok := errors.AsType[*github.ErrorResponse](err); ok {
		status := 0
		if ghErr.Response != nil {
			status = ghErr.Response.StatusCode
		}
		haystack := strings.ToLower(ghErr.Error())
		switch {
		case strings.Contains(haystack, "only have one pending review per pull request"):
			return d.err(op, forge.ErrorConflict, status, hintPendingReview, err)
		case strings.Contains(haystack, "commitoid is not part of the pull request"),
			strings.Contains(haystack, "commit_id is not part of the pull request"):
			return d.err(op, forge.ErrorConflict, status, hintUnknownCommit, err)
		case status == http.StatusForbidden,
			strings.Contains(haystack, "resource not accessible by integration"),
			strings.Contains(haystack, "must have pull request write"):
			return d.err(op, forge.ErrorForbidden, status, hintReviewForbidden, err)
		}
	}
	return d.wrap(op, err)
}
