package githubf

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
)

const pullsPage1 = `[
  {"number":101,"title":"Fix crash","state":"open","draft":true,
   "html_url":"https://github.com/octo/hello/pull/101",
   "updated_at":"2026-05-12T18:30:00Z","user":{"login":"alice"},
   "head":{"ref":"fix-crash","sha":"h1"},"base":{"ref":"main","sha":"b1"}},
  {"number":102,"title":"Add feature","state":"open","draft":false,
   "html_url":"https://github.com/octo/hello/pull/102",
   "updated_at":"2026-05-13T09:00:00Z","user":{"login":"bob"},
   "head":{"ref":"feature","sha":"h2"},"base":{"ref":"main","sha":"b2"}}
]`

const pullsPage2 = `[
  {"number":103,"title":"Last one","state":"open","draft":false,
   "html_url":"https://github.com/octo/hello/pull/103",
   "updated_at":"2026-05-14T10:00:00Z","user":{"login":"carol"},
   "head":{"ref":"last","sha":"h3"},"base":{"ref":"main","sha":"b3"}}
]`

func TestListPullRequestsOpenPaginates(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/octo/hello/pulls", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != "open" || q.Get("sort") != "updated" || q.Get("direction") != "desc" {
			t.Errorf("unexpected listing params: %v", q)
		}
		if q.Get("per_page") != "2" {
			t.Errorf("per_page = %q, want 2", q.Get("per_page"))
		}
		w.Header().Set("Content-Type", "application/json")
		if q.Get("page") == "2" {
			_, _ = fmt.Fprint(w, pullsPage2)
			return
		}
		w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/octo/hello/pulls?page=2&per_page=2>; rel="next"`, r.Host))
		_, _ = fmt.Fprint(w, pullsPage1)
	})
	d := newTestDriver(t, mux)

	page, err := d.ListPullRequests(context.Background(), forge.ListQuery{
		Repository: testRepo(), Scope: forge.ScopeOpen, PageSize: 2,
	})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(page.Items))
	}
	if page.NextPageToken != "2" {
		t.Errorf("NextPageToken = %q, want 2", page.NextPageToken)
	}
	first := page.Items[0]
	if first.Number != 101 || first.Title != "Fix crash" || first.Author != "alice" ||
		first.HeadRefName != "fix-crash" || first.BaseRefName != "main" ||
		first.URL != "https://github.com/octo/hello/pull/101" ||
		first.State != "open" || !first.IsDraft {
		t.Errorf("unexpected first summary: %+v", first)
	}
	if first.UpdatedAt == nil || first.UpdatedAt.UTC().Format("2006-01-02") != "2026-05-12" {
		t.Errorf("UpdatedAt = %v", first.UpdatedAt)
	}
	if first.Repository != testRepo() {
		t.Errorf("Repository = %+v", first.Repository)
	}

	page, err = d.ListPullRequests(context.Background(), forge.ListQuery{
		Repository: testRepo(), Scope: forge.ScopeOpen, PageSize: 2, PageToken: page.NextPageToken,
	})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Number != 103 {
		t.Fatalf("second page items = %+v", page.Items)
	}
	if page.NextPageToken != "" {
		t.Errorf("NextPageToken = %q, want empty at end", page.NextPageToken)
	}
}

func TestListPullRequestsReviewRequestedUsesSearch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /search/issues", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != "is:pr is:open review-requested:@me repo:octo/hello" {
			t.Errorf("search q = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Link", fmt.Sprintf(`<http://%s/search/issues?q=x&page=2>; rel="next"`, r.Host))
		_, _ = fmt.Fprint(w, `{"total_count":1,"incomplete_results":false,"items":[
			{"number":7,"title":"Please review","state":"open","draft":false,
			 "html_url":"https://github.com/octo/hello/pull/7",
			 "updated_at":"2026-06-01T00:00:00Z","user":{"login":"dave"}}
		]}`)
	})
	d := newTestDriver(t, mux)

	page, err := d.ListPullRequests(context.Background(), forge.ListQuery{
		Repository: testRepo(), Scope: forge.ScopeReviewRequested, PageSize: 10,
	})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(page.Items))
	}
	item := page.Items[0]
	if item.Number != 7 || item.Author != "dave" || item.Title != "Please review" ||
		item.State != "open" || item.URL != "https://github.com/octo/hello/pull/7" {
		t.Errorf("unexpected item: %+v", item)
	}
	// Search rows are issues: ref names are unavailable by design.
	if item.HeadRefName != "" || item.BaseRefName != "" {
		t.Errorf("ref names must be empty from search: %+v", item)
	}
	if page.NextPageToken != "2" {
		t.Errorf("NextPageToken = %q, want 2", page.NextPageToken)
	}
}

func TestListPullRequestsRejectsBadPageToken(t *testing.T) {
	d := newTestDriver(t, forbidNetwork(t))
	for _, token := range []string{"abc", "0", "-1"} {
		_, err := d.ListPullRequests(context.Background(), forge.ListQuery{
			Repository: testRepo(), PageToken: token,
		})
		wantForgeErr(t, err, forge.ErrorValidation)
	}
}
