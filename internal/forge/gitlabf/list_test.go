package gitlabf

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
)

const listBody = `[
	{
		"iid": 7,
		"title": "Add feature",
		"author": {"username": "alice"},
		"source_branch": "feature",
		"target_branch": "main",
		"updated_at": "2026-06-01T10:00:00Z",
		"web_url": "https://gitlab.com/group/sub/repo/-/merge_requests/7",
		"state": "opened",
		"draft": true
	}
]`

func TestListPullRequestsOpen(t *testing.T) {
	mux := newFixtureMux(t)
	var gotQuery string
	mux.Handle("GET /api/v4/projects/group%2Fsub%2Frepo/merge_requests",
		func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.RawQuery
			w.Header().Set("X-Page", "1")
			w.Header().Set("X-Next-Page", "2")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(listBody))
		})
	d := newTestDriver(t, mux)

	page, err := d.ListPullRequests(context.Background(), forge.ListQuery{
		Repository: testRepo(), Scope: forge.ScopeOpen, PageSize: 30,
	})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(page.Items))
	}
	item := page.Items[0]
	if item.Number != 7 || item.Title != "Add feature" || item.Author != "alice" {
		t.Errorf("unexpected summary: %+v", item)
	}
	if item.HeadRefName != "feature" || item.BaseRefName != "main" {
		t.Errorf("refs = %q/%q", item.HeadRefName, item.BaseRefName)
	}
	if item.State != "open" || !item.IsDraft {
		t.Errorf("state = %q draft = %v", item.State, item.IsDraft)
	}
	if page.NextPageToken != "2" {
		t.Errorf("NextPageToken = %q, want 2", page.NextPageToken)
	}
	for _, want := range []string{"state=opened", "order_by=updated_at", "sort=desc", "per_page=30", "page=1"} {
		if !containsParam(gotQuery, want) {
			t.Errorf("query %q missing %q", gotQuery, want)
		}
	}
	if containsParam(gotQuery, "reviewer_username") {
		t.Errorf("open scope must not filter by reviewer: %q", gotQuery)
	}
}

func TestListPullRequestsReviewRequestedFiltersByViewer(t *testing.T) {
	mux := newFixtureMux(t)
	userCalls := 0
	mux.Handle("GET /api/v4/user", func(w http.ResponseWriter, _ *http.Request) {
		userCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"username": "ronen"}`))
	})
	var gotQuery string
	mux.Handle("GET /api/v4/projects/group%2Fsub%2Frepo/merge_requests",
		func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.RawQuery
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		})
	d := newTestDriver(t, mux)

	for range 2 { // second call must hit the viewer cache
		if _, err := d.ListPullRequests(context.Background(), forge.ListQuery{
			Repository: testRepo(), Scope: forge.ScopeReviewRequested,
		}); err != nil {
			t.Fatalf("ListPullRequests: %v", err)
		}
	}
	if !containsParam(gotQuery, "reviewer_username=ronen") {
		t.Errorf("query %q missing reviewer_username=ronen", gotQuery)
	}
	if userCalls != 1 {
		t.Errorf("CurrentUser calls = %d, want 1 (cached)", userCalls)
	}
}

func TestListPullRequestsPageToken(t *testing.T) {
	mux := newFixtureMux(t)
	var gotQuery string
	mux.Handle("GET /api/v4/projects/group%2Fsub%2Frepo/merge_requests",
		func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.RawQuery
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		})
	d := newTestDriver(t, mux)

	page, err := d.ListPullRequests(context.Background(), forge.ListQuery{
		Repository: testRepo(), PageToken: "3",
	})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	if !containsParam(gotQuery, "page=3") {
		t.Errorf("query %q missing page=3", gotQuery)
	}
	if page.NextPageToken != "" {
		t.Errorf("NextPageToken = %q, want empty on last page", page.NextPageToken)
	}
}

func TestListPullRequestsRejectsBadPageToken(t *testing.T) {
	d := newTestDriver(t, forbidNetwork(t))
	_, err := d.ListPullRequests(context.Background(), forge.ListQuery{
		Repository: testRepo(), PageToken: "zero",
	})
	wantForgeErr(t, err, forge.ErrorValidation)
}

// containsParam reports whether the raw query contains the "key=value"
// pair (or, for a bare "key", the key at all).
func containsParam(query, want string) bool {
	values, err := url.ParseQuery(query)
	if err != nil {
		return false
	}
	key, value, hasValue := strings.Cut(want, "=")
	got, ok := values[key]
	if !ok {
		return false
	}
	if !hasValue {
		return true
	}
	return slices.Contains(got, value)
}
