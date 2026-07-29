package azdof

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
)

// listPRJSON renders one PR row for the listing fixture.
func listPRJSON(id int, title string) string {
	return fmt.Sprintf(`{
		"pullRequestId": %d,
		"title": %q,
		"status": "active",
		"isDraft": false,
		"createdBy": {"displayName": "Alice", "uniqueName": "alice@example.com"},
		"creationDate": "2026-07-01T10:00:00Z",
		"sourceRefName": "refs/heads/feature",
		"targetRefName": "refs/heads/main"
	}`, id, title)
}

const listPath = "/proj/_apis/git/repositories/repo/pullrequests"

func TestListPullRequestsOpen(t *testing.T) {
	var queries []string
	d := newTestDriver(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != listPath {
			t.Errorf("unexpected call %s %s", r.Method, r.URL)
			return
		}
		queries = append(queries, r.URL.RawQuery)
		if r.URL.Query().Get("$skip") == "0" {
			writeJSON(w, http.StatusOK, fmt.Sprintf(`{"count":2,"value":[%s,%s]}`,
				listPRJSON(7, "First"), listPRJSON(8, "Second")))
			return
		}
		writeJSON(w, http.StatusOK, fmt.Sprintf(`{"count":1,"value":[%s]}`, listPRJSON(9, "Third")))
	}))

	page, err := d.ListPullRequests(context.Background(), forge.ListQuery{
		Repository: testRepo(),
		Scope:      forge.ScopeOpen,
		PageSize:   2,
	})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(page.Items))
	}
	first := page.Items[0]
	if first.Number != 7 || first.Title != "First" || first.Author != "Alice" {
		t.Errorf("first item = %+v", first)
	}
	if first.HeadRefName != "feature" || first.BaseRefName != "main" {
		t.Errorf("ref names = %q/%q", first.HeadRefName, first.BaseRefName)
	}
	if first.State != "open" {
		t.Errorf("state = %q", first.State)
	}
	if wantURL := "/org/proj/_git/repo/pullrequest/7"; !strings.HasSuffix(first.URL, wantURL) &&
		!strings.HasSuffix(first.URL, "/proj/_git/repo/pullrequest/7") {
		t.Errorf("URL = %q", first.URL)
	}
	if first.UpdatedAt == nil {
		t.Error("UpdatedAt must be set from creationDate")
	}
	if page.NextPageToken != "2" {
		t.Fatalf("NextPageToken = %q, want 2 (skip offset)", page.NextPageToken)
	}

	// Second page via the returned skip token; a short page ends paging.
	page, err = d.ListPullRequests(context.Background(), forge.ListQuery{
		Repository: testRepo(),
		Scope:      forge.ScopeOpen,
		PageSize:   2,
		PageToken:  page.NextPageToken,
	})
	if err != nil {
		t.Fatalf("ListPullRequests page 2: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Number != 9 {
		t.Fatalf("page 2 items = %+v", page.Items)
	}
	if page.NextPageToken != "" {
		t.Errorf("NextPageToken = %q, want empty on short page", page.NextPageToken)
	}

	if len(queries) != 2 {
		t.Fatalf("request count = %d", len(queries))
	}
	for _, want := range []string{"searchCriteria.status=active", "%24top=2", "%24skip=0"} {
		if !strings.Contains(queries[0], want) {
			t.Errorf("first query %q missing %q", queries[0], want)
		}
	}
	if !strings.Contains(queries[1], "%24skip=2") {
		t.Errorf("second query %q missing skip=2", queries[1])
	}
}

func TestListPullRequestsReviewRequested(t *testing.T) {
	var listQuery string
	connectionHits := 0
	d := newTestDriver(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.EqualFold(r.URL.Path, "/_apis/connectionData"):
			connectionHits++
			writeJSON(w, http.StatusOK, connectionDataJSON())
		case r.URL.Path == listPath:
			listQuery = r.URL.RawQuery
			writeJSON(w, http.StatusOK, fmt.Sprintf(`{"count":1,"value":[%s]}`, listPRJSON(5, "Mine")))
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL)
		}
	}))

	query := forge.ListQuery{Repository: testRepo(), Scope: forge.ScopeReviewRequested, PageSize: 10}
	for range 2 {
		page, err := d.ListPullRequests(context.Background(), query)
		if err != nil {
			t.Fatalf("ListPullRequests: %v", err)
		}
		if len(page.Items) != 1 || page.Items[0].Number != 5 {
			t.Fatalf("items = %+v", page.Items)
		}
	}
	if !strings.Contains(listQuery, "searchCriteria.reviewerId="+testViewerID) {
		t.Errorf("query %q missing reviewerId filter", listQuery)
	}
	if connectionHits != 1 {
		t.Errorf("connectionData hits = %d, want 1 (cached)", connectionHits)
	}
}

func TestListPullRequestsBadPageToken(t *testing.T) {
	d := newTestDriver(t, forbidNetwork(t))
	for _, token := range []string{"nope", "-3"} {
		_, err := d.ListPullRequests(context.Background(), forge.ListQuery{
			Repository: testRepo(),
			PageToken:  token,
		})
		wantForgeErr(t, err, forge.ErrorValidation)
	}
}

func TestListPullRequestsBadViewerID(t *testing.T) {
	d := newTestDriver(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(r.URL.Path, "/_apis/connectionData") {
			writeJSON(w, http.StatusOK, `{"authorizedUser": {"id": "not-a-uuid"}}`)
			return
		}
		t.Errorf("unexpected call %s %s", r.Method, r.URL)
	}))
	_, err := d.ListPullRequests(context.Background(), forge.ListQuery{
		Repository: testRepo(),
		Scope:      forge.ScopeReviewRequested,
	})
	wantForgeErr(t, err, forge.ErrorValidation)
}

func TestListPullRequestsHTTPError(t *testing.T) {
	d := newTestDriver(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusUnauthorized, `{"message": "TF400813: not authorized"}`)
	}))
	_, err := d.ListPullRequests(context.Background(), forge.ListQuery{Repository: testRepo()})
	fe := mustForgeErr(t, err)
	if fe.Kind != forge.ErrorAuth || fe.Status != http.StatusUnauthorized {
		t.Errorf("kind=%v status=%d", fe.Kind, fe.Status)
	}
	if !strings.Contains(fe.Hint, "Code (Read & Write)") {
		t.Errorf("hint %q must name the PAT scope", fe.Hint)
	}
}
