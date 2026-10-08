package githubf

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
)

// errorServer answers every request with one canned status/body/headers.
func errorServer(status int, body string, headers map[string]string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
	})
}

func TestGenericErrorMapping(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		headers  map[string]string
		wantKind forge.ErrorKind
		wantHint string
	}{
		{
			name:   "401 maps to auth with token hint",
			status: http.StatusUnauthorized, body: `{"message":"Bad credentials"}`,
			wantKind: forge.ErrorAuth, wantHint: "GITHUB_TOKEN",
		},
		{
			name:   "403 maps to forbidden",
			status: http.StatusForbidden, body: `{"message":"Forbidden"}`,
			wantKind: forge.ErrorForbidden, wantHint: "scopes",
		},
		{
			name:   "403 with exhausted rate limit maps to rate-limited",
			status: http.StatusForbidden, body: `{"message":"API rate limit exceeded"}`,
			headers: map[string]string{
				"X-RateLimit-Remaining": "0",
				"X-RateLimit-Limit":     "60",
				"X-RateLimit-Reset":     "1750000000",
			},
			wantKind: forge.ErrorRateLimited, wantHint: "rate limit",
		},
		{
			name:   "404 maps to not-found",
			status: http.StatusNotFound, body: `{"message":"Not Found"}`,
			wantKind: forge.ErrorNotFound, wantHint: "not found",
		},
		{
			name:   "409 maps to conflict",
			status: http.StatusConflict, body: `{"message":"Conflict"}`,
			wantKind: forge.ErrorConflict,
		},
		{
			name:   "422 maps to validation",
			status: http.StatusUnprocessableEntity, body: `{"message":"Validation Failed"}`,
			wantKind: forge.ErrorValidation,
		},
		{
			name:   "429 maps to rate-limited",
			status: http.StatusTooManyRequests, body: `{"message":"Too many requests"}`,
			wantKind: forge.ErrorRateLimited, wantHint: "rate limit",
		},
		{
			name:   "500 maps to server",
			status: http.StatusInternalServerError, body: `{"message":"boom"}`,
			wantKind: forge.ErrorServer,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := newTestDriver(t, errorServer(c.status, c.body, c.headers))
			repo := testRepo()
			_, err := d.GetPullRequest(context.Background(), forge.Target{Repository: &repo, Number: 42})
			fe := mustForgeErr(t, err)
			if fe.Kind != c.wantKind {
				t.Fatalf("error kind = %v, want %v (err: %v)", fe.Kind, c.wantKind, fe)
			}
			if c.wantHint != "" && !strings.Contains(strings.ToLower(fe.Hint), strings.ToLower(c.wantHint)) {
				t.Errorf("hint = %q, want it to mention %q", fe.Hint, c.wantHint)
			}
			if c.wantKind != forge.ErrorRateLimited && fe.Status != c.status {
				t.Errorf("status = %d, want %d", fe.Status, c.status)
			}
		})
	}
}

func TestCreateReviewErrorMapping(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		wantKind forge.ErrorKind
		wantHint string
	}{
		{
			name:     "pending review conflict",
			status:   http.StatusUnprocessableEntity,
			body:     `{"message":"Validation Failed","errors":["User can only have one pending review per pull request"]}`,
			wantKind: forge.ErrorConflict, wantHint: "pending review on this MR",
		},
		{
			name:     "unknown commit after force-push",
			status:   http.StatusUnprocessableEntity,
			body:     `{"message":"Unprocessable Entity","errors":[{"message":"The commitOID is not part of the pull request"}]}`,
			wantKind: forge.ErrorConflict, wantHint: "not part of this MR",
		},
		{
			name:     "resource not accessible",
			status:   http.StatusForbidden,
			body:     `{"message":"Resource not accessible by integration"}`,
			wantKind: forge.ErrorForbidden, wantHint: "merge request write permission",
		},
		{
			name:     "integration without write scope on 422",
			status:   http.StatusUnprocessableEntity,
			body:     `{"message":"Validation Failed","errors":["User must have pull request write access"]}`,
			wantKind: forge.ErrorForbidden, wantHint: "merge request write permission",
		},
		{
			name:     "401 falls through to auth mapping",
			status:   http.StatusUnauthorized,
			body:     `{"message":"Bad credentials"}`,
			wantKind: forge.ErrorAuth, wantHint: "GITHUB_TOKEN",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := newTestDriver(t, errorServer(c.status, c.body, nil))
			_, err := d.CreateReview(context.Background(), testPR(), forge.CreateReviewRequest{
				Event: forge.SubmitComment, CommitID: "sha",
			})
			fe := mustForgeErr(t, err)
			if fe.Kind != c.wantKind {
				t.Fatalf("error kind = %v, want %v (err: %v)", fe.Kind, c.wantKind, fe)
			}
			if !strings.Contains(strings.ToLower(fe.Hint), strings.ToLower(c.wantHint)) {
				t.Errorf("hint = %q, want it to mention %q", fe.Hint, c.wantHint)
			}
			if fe.Status != c.status {
				t.Errorf("status = %d, want %d", fe.Status, c.status)
			}
		})
	}
}

func TestCanceledContextMapsToCanceled(t *testing.T) {
	d := newTestDriver(t, errorServer(http.StatusOK, `{}`, nil))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := d.GetDiff(ctx, testPR())
	wantForgeErr(t, err, forge.ErrorCanceled)
}

func TestTransportFailureMapsToNetwork(t *testing.T) {
	d, err := New(Options{APIBase: "http://127.0.0.1:1"}) // nothing listens here
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, gerr := d.GetDiff(context.Background(), testPR())
	wantForgeErr(t, gerr, forge.ErrorNetwork)
}

// TestGraphQLErrorMapping: ListReviewThreads is the first GraphQL call a PR
// load makes. githubv4 reports a non-200 only as a formatted string, so a
// bad token there used to read as "network error" with no auth hint.
func TestGraphQLErrorMapping(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		wantKind forge.ErrorKind
		wantHint string
	}{
		{"401", http.StatusUnauthorized, `{"message":"Bad credentials"}`, forge.ErrorAuth, "GITHUB_TOKEN"},
		{"403", http.StatusForbidden, `{"message":"Forbidden"}`, forge.ErrorForbidden, "scopes"},
		{"502", http.StatusBadGateway, `bad gateway`, forge.ErrorServer, ""},
		{"unresolvable repository", http.StatusOK,
			`{"data":null,"errors":[{"type":"NOT_FOUND","message":"Could not resolve to a Repository with the name 'o/r'."}]}`,
			forge.ErrorNotFound, "not found"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := newTestDriver(t, errorServer(c.status, c.body, nil))
			_, err := d.ListReviewThreads(context.Background(), testPR())
			wantForgeErr(t, err, c.wantKind)
			fe := mustForgeErr(t, err)
			if c.wantHint != "" && !strings.Contains(fe.Hint, c.wantHint) {
				t.Errorf("hint = %q, want it to mention %q", fe.Hint, c.wantHint)
			}
		})
	}
}
