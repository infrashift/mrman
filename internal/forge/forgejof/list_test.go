package forgejof

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
)

func TestListPullRequestsOpen(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/repos/octo/hello/pulls", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != "open" || q.Get("sort") != "recentupdate" {
			t.Errorf("query = %v", q)
		}
		if q.Get("page") != "1" || q.Get("limit") != "30" {
			t.Errorf("pagination = page %q limit %q", q.Get("page"), q.Get("limit"))
		}
		w.Header().Set("Link", fmt.Sprintf("<http://%s/api/v1/repos/octo/hello/pulls?page=2&limit=30>; rel=\"next\"", r.Host))
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[
			{"number":7,"title":"Add feature","state":"open","html_url":"https://fj/octo/hello/pulls/7",
			 "user":{"login":"alice"},"head":{"ref":"feat","sha":"h7"},"base":{"ref":"main","sha":"b7"},
			 "updated_at":"2026-07-01T10:00:00Z"},
			{"number":8,"title":"WIP: half-done","state":"open","html_url":"https://fj/octo/hello/pulls/8",
			 "user":{"login":"bob"},"head":{"ref":"wip","sha":"h8"},"base":{"ref":"main","sha":"b8"},
			 "updated_at":"2026-07-02T10:00:00Z"}
		]`)
	})
	d := newTestDriver(t, mux)

	page, err := d.ListPullRequests(context.Background(), forge.ListQuery{Repository: testRepo()})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %+v", page.Items)
	}
	first := page.Items[0]
	if first.Number != 7 || first.Title != "Add feature" || first.Author != "alice" ||
		first.HeadRefName != "feat" || first.BaseRefName != "main" ||
		first.URL != "https://fj/octo/hello/pulls/7" || first.State != "open" {
		t.Errorf("first = %+v", first)
	}
	if first.UpdatedAt == nil || first.IsDraft {
		t.Errorf("first updated=%v draft=%v", first.UpdatedAt, first.IsDraft)
	}
	if !page.Items[1].IsDraft {
		t.Error("WIP-titled PR must be marked draft")
	}
	if page.NextPageToken != "2" {
		t.Errorf("NextPageToken = %q, want 2", page.NextPageToken)
	}
}

func TestListPullRequestsPageTokenAndLastPage(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/repos/octo/hello/pulls", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("page"); got != "3" {
			t.Errorf("page = %q, want 3", got)
		}
		if got := r.URL.Query().Get("limit"); got != "10" {
			t.Errorf("limit = %q, want 10", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[]`) // no Link header: last page
	})
	d := newTestDriver(t, mux)

	page, err := d.ListPullRequests(context.Background(), forge.ListQuery{
		Repository: testRepo(), PageToken: "3", PageSize: 10,
	})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	if len(page.Items) != 0 || page.NextPageToken != "" {
		t.Errorf("page = %+v", page)
	}
}

func TestListPullRequestsRejectsBadPageToken(t *testing.T) {
	d := newTestDriver(t, forbidNetwork(t))
	for _, token := range []string{"x", "0", "-2"} {
		_, err := d.ListPullRequests(context.Background(), forge.ListQuery{
			Repository: testRepo(), PageToken: token,
		})
		wantForgeErr(t, err, forge.ErrorValidation)
	}
}

func TestListPullRequestsReviewRequested(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/repos/issues/search", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "token "+testToken {
			t.Errorf("Authorization = %q", got)
		}
		q := r.URL.Query()
		if q.Get("type") != "pulls" || q.Get("review_requested") != "true" ||
			q.Get("state") != "open" || q.Get("owner") != "octo" {
			t.Errorf("query = %v", q)
		}
		if q.Get("page") != "1" || q.Get("limit") != "30" {
			t.Errorf("pagination = %v", q)
		}
		w.Header().Set("Link", fmt.Sprintf("<http://%s/api/v1/repos/issues/search?page=2&limit=30>; rel=\"next\", <http://%s/api/v1/repos/issues/search?page=5&limit=30>; rel=\"last\"", r.Host, r.Host))
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[
			{"number":11,"title":"Please review","state":"open","html_url":"https://fj/octo/hello/pulls/11",
			 "user":{"login":"carol"},"updated_at":"2026-07-03T09:00:00Z",
			 "repository":{"full_name":"octo/hello"}},
			{"number":12,"title":"Other repo","state":"open","html_url":"https://fj/octo/other/pulls/12",
			 "user":{"login":"dave"},"repository":{"full_name":"octo/other"}}
		]`)
	})
	d := newTestDriver(t, mux)

	page, err := d.ListPullRequests(context.Background(), forge.ListQuery{
		Repository: testRepo(), Scope: forge.ScopeReviewRequested,
	})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("sibling-repo rows must be filtered out: %+v", page.Items)
	}
	item := page.Items[0]
	if item.Number != 11 || item.Author != "carol" || item.Title != "Please review" ||
		item.URL != "https://fj/octo/hello/pulls/11" || item.Repository != testRepo() {
		t.Errorf("item = %+v", item)
	}
	if page.NextPageToken != "2" {
		t.Errorf("NextPageToken = %q, want 2", page.NextPageToken)
	}
}

func TestListPullRequestsReviewRequestedAuthError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/repos/issues/search", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"token is required"}`, http.StatusUnauthorized)
	})
	d := newTestDriver(t, mux)

	_, err := d.ListPullRequests(context.Background(), forge.ListQuery{
		Repository: testRepo(), Scope: forge.ScopeReviewRequested,
	})
	fe := mustForgeErr(t, err)
	if fe.Kind != forge.ErrorAuth || fe.Status != http.StatusUnauthorized {
		t.Errorf("err = %+v", fe)
	}
	if fe.Hint != hintAuth {
		t.Errorf("hint = %q", fe.Hint)
	}
}

func TestListPullRequestsReviewRequestedBadJSON(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/repos/issues/search", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"not":"a list"}`)
	})
	d := newTestDriver(t, mux)

	_, err := d.ListPullRequests(context.Background(), forge.ListQuery{
		Repository: testRepo(), Scope: forge.ScopeReviewRequested,
	})
	wantForgeErr(t, err, forge.ErrorValidation)
}

func TestNextPageFromLink(t *testing.T) {
	cases := []struct {
		link string
		want int
	}{
		{"", 0},
		{`<https://fj/api/v1/x?page=4&limit=30>; rel="next"`, 4},
		{`<https://fj/x?page=2>; rel="prev", <https://fj/x?page=9>; rel="next"`, 9},
		{`<https://fj/x?page=9>; rel="last"`, 0},
		{`<https://fj/x>; rel="next"`, 0},
	}
	for _, tc := range cases {
		if got := nextPageFromLink(tc.link); got != tc.want {
			t.Errorf("nextPageFromLink(%q) = %d, want %d", tc.link, got, tc.want)
		}
	}
}

// TestReviewRequestedSkipsPagesTheRepoFilterEmpties: the search pages by
// owner and the repository filter runs afterwards, so page 1 can hold only
// sibling repositories while page 2 holds this one's PR. The caller must get
// that PR, not an empty page that says there is more. The server's casing
// of the repository name need not match the remote's.
func TestReviewRequestedSkipsPagesTheRepoFilterEmpties(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/repos/issues/search", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			w.Header().Set("Link", fmt.Sprintf("<http://%s/api/v1/repos/issues/search?page=2>; rel=\"next\"", r.Host))
			_, _ = fmt.Fprint(w, `[{"number":1,"title":"x","repository":{"full_name":"octo/other"}}]`)
		case "2":
			_, _ = fmt.Fprint(w, `[{"number":7,"title":"mine","repository":{"full_name":"Octo/Hello"}}]`)
		default:
			t.Errorf("unexpected page %q", r.URL.Query().Get("page"))
		}
	})
	d := newTestDriver(t, mux)

	page, err := d.ListPullRequests(context.Background(), forge.ListQuery{
		Repository: testRepo(), Scope: forge.ScopeReviewRequested,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Number != 7 {
		t.Fatalf("items = %+v, want PR 7 from page 2", page.Items)
	}
	if page.NextPageToken != "" {
		t.Errorf("NextPageToken = %q, want none after the last page", page.NextPageToken)
	}
}
