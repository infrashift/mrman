package forgejof

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	forgejo "codeberg.org/mvdkleijn/forgejo-sdk/forgejo/v3"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

// defaultPageSize is used when the query does not specify one.
const defaultPageSize = 30

// ListPullRequests lists open PRs, either all of them or only those with
// review requested from the viewer. PageToken is the stringified page
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

// listOpen pages through the repository PR listing, newest-updated first.
func (d *Driver) listOpen(ctx context.Context, q forge.ListQuery, page, size int) (*forge.PullRequestPage, error) {
	const op = "list_pull_requests"
	api, err := d.api(ctx)
	if err != nil {
		return nil, d.wrap(op, nil, err)
	}
	prs, resp, err := api.ListRepoPullRequests(q.Repository.Owner, q.Repository.Name,
		forgejo.ListPullRequestsOptions{
			ListOptions: forgejo.ListOptions{Page: page, PageSize: size},
			State:       forgejo.StateOpen,
			Sort:        "recentupdate",
		})
	if err != nil {
		return nil, d.wrap(op, resp, err)
	}
	items := make([]forge.PullRequestSummary, 0, len(prs))
	for _, pr := range prs {
		items = append(items, prSummary(q.Repository, pr))
	}
	next := ""
	if resp != nil && resp.NextPage != 0 {
		next = strconv.Itoa(resp.NextPage)
	}
	return &forge.PullRequestPage{Items: items, NextPageToken: next}, nil
}

// prSummary maps one SDK pull request to the neutral summary type.
func prSummary(repo forgetypes.Repository, pr *forgejo.PullRequest) forge.PullRequestSummary {
	s := forge.PullRequestSummary{
		Repository: repo,
		Title:      pr.Title,
		UpdatedAt:  pr.Updated,
		URL:        pr.HTMLURL,
		State:      string(pr.State),
		IsDraft:    isDraftTitle(pr.Title),
	}
	if pr.Index > 0 {
		s.Number = uint64(pr.Index)
	}
	if pr.Poster != nil {
		s.Author = pr.Poster.UserName
	}
	if pr.Head != nil {
		s.HeadRefName = pr.Head.Ref
	}
	if pr.Base != nil {
		s.BaseRefName = pr.Base.Ref
	}
	return s
}

// isDraftTitle approximates draft state from the work-in-progress title
// prefixes; the Forgejo API does not expose a draft flag on PR records.
func isDraftTitle(title string) bool {
	upper := strings.ToUpper(strings.TrimSpace(title))
	return strings.HasPrefix(upper, "WIP:") || strings.HasPrefix(upper, "[WIP]")
}

// searchIssue is the slice of the issues-search row the listing consumes.
type searchIssue struct {
	Number  uint64     `json:"number"`
	Title   string     `json:"title"`
	HTMLURL string     `json:"html_url"`
	State   string     `json:"state"`
	Updated *time.Time `json:"updated_at"`
	User    *struct {
		Login string `json:"login"`
	} `json:"user"`
	Repository *struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

// maxFilteredPages bounds how many search pages one listing call reads while
// the client-side repository filter keeps emptying them.
const maxFilteredPages = 10

// listReviewRequested lists open PRs whose review was requested from the
// viewer via GET /repos/issues/search with review_requested=true — the SDK
// exposes no such filter, so this is a raw call against the same API base
// with the same token header. The endpoint filters by owner but not by
// repository, so rows from sibling repositories are dropped client-side.
//
// Because that filter runs after the server paged, a page can come back
// with nothing for this repository while later pages still hold matches.
// Such pages are skipped, up to maxFilteredPages, so the caller never sees
// an empty page that claims there is more.
func (d *Driver) listReviewRequested(ctx context.Context, q forge.ListQuery, page, size int) (*forge.PullRequestPage, error) {
	for range maxFilteredPages {
		result, next, err := d.searchReviewRequested(ctx, q, page, size)
		if err != nil {
			return nil, err
		}
		if len(result.Items) > 0 || next == 0 {
			return result, nil
		}
		page = next
	}
	return &forge.PullRequestPage{NextPageToken: strconv.Itoa(page)}, nil
}

// searchReviewRequested fetches one page of the issues search and keeps the
// rows for q's repository. next is the following page number, 0 at the end.
func (d *Driver) searchReviewRequested(ctx context.Context, q forge.ListQuery, page, size int) (*forge.PullRequestPage, int, error) {
	const op = "list_pull_requests"
	query := url.Values{}
	query.Set("type", "pulls")
	query.Set("review_requested", "true")
	query.Set("state", "open")
	query.Set("owner", q.Repository.Owner)
	query.Set("page", strconv.Itoa(page))
	query.Set("limit", strconv.Itoa(size))
	endpoint := d.base + "/api/v1/repos/issues/search?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, d.err(op, forge.ErrorValidation, 0, "", err)
	}
	req.Header.Set("Accept", "application/json")
	if d.token != "" {
		req.Header.Set("Authorization", "token "+d.token)
	}
	client := d.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, d.err(op, forge.Classify(err), 0, "", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, d.err(op, forge.Classify(err), 0, "", err)
	}
	if resp.StatusCode >= 400 {
		return nil, 0, d.err(op, forge.FromHTTPStatus(resp.StatusCode), resp.StatusCode,
			statusHint(resp.StatusCode),
			fmt.Errorf("issues search failed: %s", strings.TrimSpace(string(body))))
	}
	var rows []searchIssue
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, 0, d.err(op, forge.ErrorValidation, 0, "",
			fmt.Errorf("decode issues search response: %w", err))
	}
	fullName := q.Repository.Owner + "/" + q.Repository.Name
	items := make([]forge.PullRequestSummary, 0, len(rows))
	for _, row := range rows {
		// Forgejo owner and repository names are case-insensitive, and the
		// remote's casing need not match the server's.
		if row.Repository != nil && !strings.EqualFold(row.Repository.FullName, fullName) {
			continue
		}
		item := forge.PullRequestSummary{
			Repository: q.Repository,
			Number:     row.Number,
			Title:      row.Title,
			UpdatedAt:  row.Updated,
			URL:        row.HTMLURL,
			State:      row.State,
			IsDraft:    isDraftTitle(row.Title),
		}
		if row.User != nil {
			item.Author = row.User.Login
		}
		items = append(items, item)
	}
	next := nextPageFromLink(resp.Header.Get("Link"))
	token := ""
	if next != 0 {
		token = strconv.Itoa(next)
	}
	return &forge.PullRequestPage{Items: items, NextPageToken: token}, next, nil
}

// nextPageFromLink extracts the rel="next" page number from a Link header,
// 0 when there is no further page.
func nextPageFromLink(link string) int {
	for part := range strings.SplitSeq(link, ",") {
		target, params, ok := strings.Cut(part, ";")
		if !ok || !strings.Contains(params, `rel="next"`) {
			continue
		}
		parsed, err := url.Parse(strings.Trim(strings.TrimSpace(target), "<>"))
		if err != nil {
			continue
		}
		page, err := strconv.Atoi(parsed.Query().Get("page"))
		if err != nil || page < 1 {
			continue
		}
		return page
	}
	return 0
}
