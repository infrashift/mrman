package githubf

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/shurcooL/githubv4"

	"github.com/infrashift/mrman/internal/forge"
)

// hintNotOwnSummary explains a refused edit.
const hintNotOwnSummary = "Only pull-request-level comments this token's identity wrote can be " +
	"edited from the command line."

var _ forge.SummaryEditor = (*Driver)(nil)

// gqlReviewTarget is what a summary edit needs to know before it acts: who
// the token is, and the review's author and pull request.
type gqlReviewTarget struct {
	Viewer struct {
		Login string
	}
	Node struct {
		Review struct {
			ID          string
			Author      *gqlActor
			PullRequest struct {
				Number     int
				Repository struct {
					Name  string
					Owner struct {
						Login string
					}
				}
			}
		} `graphql:"... on PullRequestReview"`
	} `graphql:"node(id: $id)"`
}

// EditOwnSummary replaces the body of review summaryID (the PRR_… node id
// ListReviewSummaries reports) with updatePullRequestReview. The review
// must belong to pr, and its author must be the login the token
// authenticates as, both read live in one query.
func (d *Driver) EditOwnSummary(ctx context.Context, pr *forge.PullRequestDetails, summaryID, body string) error {
	const op = "edit_summary"
	if strings.TrimSpace(summaryID) == "" {
		return d.err(op, forge.ErrorValidation, 0, "", errors.New("an empty summary id"))
	}
	if strings.TrimSpace(body) == "" {
		return d.err(op, forge.ErrorValidation, 0, "", errors.New("an empty body"))
	}
	var q gqlReviewTarget
	if err := d.gql.Query(ctx, &q, map[string]any{"id": githubv4.ID(summaryID)}); err != nil {
		return d.wrap(op, err)
	}
	rv := &q.Node.Review
	// Review ids are global node ids: without this check a review of another
	// pull request (or repository) would be edited, though the grant or
	// policy that allowed the edit named this one.
	if rv.ID == "" || rv.PullRequest.Number != int(pr.Number) || //nolint:gosec // G115: pull request numbers are small forge-assigned integers
		!strings.EqualFold(rv.PullRequest.Repository.Owner.Login, pr.Repository.Owner) ||
		!strings.EqualFold(rv.PullRequest.Repository.Name, pr.Repository.Name) {
		return d.err(op, forge.ErrorNotFound, 0, "",
			fmt.Errorf("%q is not a review of %s/%s#%d", summaryID,
				pr.Repository.Owner, pr.Repository.Name, pr.Number))
	}
	if rv.Author == nil || !sameLogin(rv.Author.Login, q.Viewer.Login) {
		return d.err(op, forge.ErrorForbidden, 0, hintNotOwnSummary,
			errors.New("summary "+summaryID+" was not written by this token's identity"))
	}
	var m struct {
		UpdatePullRequestReview struct {
			PullRequestReview *struct {
				ID string
			}
		} `graphql:"updatePullRequestReview(input: $input)"`
	}
	input := githubv4.UpdatePullRequestReviewInput{
		PullRequestReviewID: githubv4.ID(summaryID),
		Body:                githubv4.String(body),
	}
	if err := d.gql.Mutate(ctx, &m, input, nil); err != nil {
		return d.wrap(op, err)
	}
	return nil
}
