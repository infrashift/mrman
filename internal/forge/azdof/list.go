package azdof

import (
	"context"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"

	"github.com/infrashift/mrman/internal/forge"
)

// defaultPageSize is used when the query does not specify one.
const defaultPageSize = 30

// ListPullRequests lists active PRs, either all of them or only those where
// the viewer is a requested reviewer. Azure DevOps has no listing cursor,
// so PageToken is the stringified $skip offset ("" means the first page) —
// stable enough for the active-PR window this lists.
func (d *Driver) ListPullRequests(ctx context.Context, q forge.ListQuery) (*forge.PullRequestPage, error) {
	const op = "list_pull_requests"
	skip := 0
	if q.PageToken != "" {
		parsed, err := strconv.Atoi(q.PageToken)
		if err != nil || parsed < 0 {
			return nil, d.err(op, forge.ErrorValidation, 0, "",
				fmt.Errorf("invalid page token %q", q.PageToken))
		}
		skip = parsed
	}
	size := q.PageSize
	if size <= 0 {
		size = defaultPageSize
	}
	project, repoName := d.coords(q.Repository)

	criteria := git.GitPullRequestSearchCriteria{
		Status: &git.PullRequestStatusValues.Active,
	}
	if q.Scope == forge.ScopeReviewRequested {
		viewer, err := d.viewerUser(ctx, op)
		if err != nil {
			return nil, err
		}
		viewerID, err := uuid.Parse(viewer.ID)
		if err != nil {
			return nil, d.err(op, forge.ErrorValidation, 0, "",
				fmt.Errorf("connectionData returned a non-UUID user id %q: %w", viewer.ID, err))
		}
		criteria.ReviewerId = &viewerID
	}

	prs, err := d.gitClient.GetPullRequests(ctx, git.GetPullRequestsArgs{
		RepositoryId:   &repoName,
		Project:        &project,
		SearchCriteria: &criteria,
		Top:            &size,
		Skip:           &skip,
	})
	if err != nil {
		return nil, d.wrap(op, err)
	}

	var rows []git.GitPullRequest
	if prs != nil {
		rows = *prs
	}
	items := make([]forge.PullRequestSummary, 0, len(rows))
	for i := range rows {
		items = append(items, d.summary(q.Repository, project, repoName, &rows[i]))
	}
	next := ""
	if len(rows) == size {
		next = strconv.Itoa(skip + size)
	}
	return &forge.PullRequestPage{Items: items, NextPageToken: next}, nil
}
