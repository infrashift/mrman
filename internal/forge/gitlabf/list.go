package gitlabf

import (
	"context"
	"fmt"
	"strconv"

	gitlab "gitlab.com/gitlab-org/api/client-go"

	"github.com/infrashift/mrman/internal/forge"
)

// defaultPageSize is used when the query does not specify one.
const defaultPageSize = 30

// ListPullRequests lists open merge requests, either all of them or only
// those with review requested from the viewer (reviewer_username filter).
// PageToken is the stringified REST page number; "" means the first page.
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
	opts := &gitlab.ListProjectMergeRequestsOptions{
		State:   new("opened"),
		OrderBy: new("updated_at"),
		Sort:    new("desc"),
		ListOptions: gitlab.ListOptions{
			Page:    int64(page),
			PerPage: int64(size),
		},
	}
	if q.Scope == forge.ScopeReviewRequested {
		viewer, err := d.currentUsername(ctx, op)
		if err != nil {
			return nil, err
		}
		opts.ReviewerUsername = new(viewer)
	}
	rows, resp, err := d.client.MergeRequests.ListProjectMergeRequests(
		projectID(q.Repository), opts, gitlab.WithContext(ctx))
	if err != nil {
		return nil, d.wrap(op, err)
	}
	items := make([]forge.PullRequestSummary, 0, len(rows))
	for _, row := range rows {
		summary := forge.PullRequestSummary{
			Repository:  q.Repository,
			Number:      uint64(row.IID), //nolint:gosec // G115: the forge never returns a negative id
			Title:       row.Title,
			HeadRefName: row.SourceBranch,
			BaseRefName: row.TargetBranch,
			UpdatedAt:   row.UpdatedAt,
			URL:         row.WebURL,
			State:       normalizeState(row.State),
			IsDraft:     row.Draft,
		}
		if row.Author != nil {
			summary.Author = row.Author.Username
		}
		items = append(items, summary)
	}
	return &forge.PullRequestPage{Items: items, NextPageToken: nextPageToken(resp)}, nil
}

// nextPageToken converts the SDK's next page number into the opaque token,
// "" when there is no further page.
func nextPageToken(resp *gitlab.Response) string {
	if resp == nil || resp.NextPage == 0 {
		return ""
	}
	return strconv.FormatInt(resp.NextPage, 10)
}

// currentUsername returns the authenticated user's login, cached for the
// driver's lifetime.
func (d *Driver) currentUsername(ctx context.Context, op string) (string, error) {
	d.mu.Lock()
	cached := d.viewer
	d.mu.Unlock()
	if cached != "" {
		return cached, nil
	}
	user, _, err := d.client.Users.CurrentUser(gitlab.WithContext(ctx))
	if err != nil {
		return "", d.wrap(op, err)
	}
	if user == nil || user.Username == "" {
		return "", d.err(op, forge.ErrorValidation, 0, "",
			fmt.Errorf("GitLab did not report a username for the current user"))
	}
	d.mu.Lock()
	d.viewer = user.Username
	d.mu.Unlock()
	return user.Username, nil
}
