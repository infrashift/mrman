package githubf

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/go-github/v76/github"

	"github.com/infrashift/mrman/internal/forge"
)

// defaultPageSize is used when the query does not specify one.
const defaultPageSize = 30

// ListPullRequests lists open PRs, either all of them or only those with
// review requested from the viewer. PageToken is the stringified REST page
// number; "" means the first page.
func (d *Driver) ListPullRequests(ctx context.Context, q forge.ListQuery) (*forge.PullRequestPage, error) {
	const op = "list_pull_requests"
	page := 1
	if q.PageToken != "" {
		parsed, err := strconv.Atoi(q.PageToken)
		if err != nil || parsed < 1 {
			return nil, d.err(op, forge.ErrorValidation, 0, "",
				fmt.Errorf("invalid page token %q", q.PageToken))
		}
		page = parsed
	}
	size := q.PageSize
	if size <= 0 {
		size = defaultPageSize
	}
	if q.Scope == forge.ScopeReviewRequested {
		return d.listReviewRequested(ctx, q, page, size)
	}
	return d.listOpen(ctx, q, page, size)
}

// listOpen pages through the plain REST PR listing, newest-updated first.
func (d *Driver) listOpen(ctx context.Context, q forge.ListQuery, page, size int) (*forge.PullRequestPage, error) {
	const op = "list_pull_requests"
	prs, resp, err := d.rest.PullRequests.List(ctx, q.Repository.Owner, q.Repository.Name,
		&github.PullRequestListOptions{
			State:     "open",
			Sort:      "updated",
			Direction: "desc",
			ListOptions: github.ListOptions{
				Page:    page,
				PerPage: size,
			},
		})
	if err != nil {
		return nil, d.wrap(op, err)
	}
	items := make([]forge.PullRequestSummary, 0, len(prs))
	for _, pr := range prs {
		items = append(items, forge.PullRequestSummary{
			Repository:  q.Repository,
			Number:      uint64(pr.GetNumber()),
			Title:       pr.GetTitle(),
			Author:      pr.GetUser().GetLogin(),
			HeadRefName: pr.GetHead().GetRef(),
			BaseRefName: pr.GetBase().GetRef(),
			UpdatedAt:   timePtr(pr.UpdatedAt),
			URL:         pr.GetHTMLURL(),
			State:       pr.GetState(),
			IsDraft:     pr.GetDraft(),
		})
	}
	return &forge.PullRequestPage{Items: items, NextPageToken: nextPageToken(resp)}, nil
}

// listReviewRequested uses the search API with the review-requested:@me
// qualifier. Search rows are issues, so head/base ref names are not
// available and stay empty on the summaries.
func (d *Driver) listReviewRequested(ctx context.Context, q forge.ListQuery, page, size int) (*forge.PullRequestPage, error) {
	const op = "list_pull_requests"
	query := fmt.Sprintf("is:pr is:open review-requested:@me repo:%s/%s",
		q.Repository.Owner, q.Repository.Name)
	result, resp, err := d.rest.Search.Issues(ctx, query, &github.SearchOptions{
		Sort: "updated",
		ListOptions: github.ListOptions{
			Page:    page,
			PerPage: size,
		},
	})
	if err != nil {
		return nil, d.wrap(op, err)
	}
	items := make([]forge.PullRequestSummary, 0, len(result.Issues))
	for _, issue := range result.Issues {
		items = append(items, forge.PullRequestSummary{
			Repository: q.Repository,
			Number:     uint64(issue.GetNumber()),
			Title:      issue.GetTitle(),
			Author:     issue.GetUser().GetLogin(),
			UpdatedAt:  timePtr(issue.UpdatedAt),
			URL:        issue.GetHTMLURL(),
			State:      issue.GetState(),
			IsDraft:    issue.GetDraft(),
		})
	}
	return &forge.PullRequestPage{Items: items, NextPageToken: nextPageToken(resp)}, nil
}

// nextPageToken converts go-github's Link-header page number into the
// opaque token, "" when there is no further page.
func nextPageToken(resp *github.Response) string {
	if resp == nil || resp.NextPage == 0 {
		return ""
	}
	return strconv.Itoa(resp.NextPage)
}

// timePtr converts a go-github timestamp into *time.Time.
func timePtr(ts *github.Timestamp) *time.Time {
	if ts == nil {
		return nil
	}
	t := ts.Time
	return &t
}
