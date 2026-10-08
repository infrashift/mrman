package githubf

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
)

// gqlRequest is the wire shape the GraphQL client posts.
type gqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

// graphqlHandler dispatches fixture pages keyed by the $after cursor.
// pages[""] serves the first page (null cursor).
func graphqlHandler(t *testing.T, pages map[string]string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /graphql", func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode graphql request: %v", err)
		}
		if req.Variables["owner"] != "octo" || req.Variables["name"] != "hello" {
			t.Errorf("variables = %v", req.Variables)
		}
		if n, ok := req.Variables["number"].(float64); !ok || n != 42 {
			t.Errorf("number variable = %v", req.Variables["number"])
		}
		cursor, _ := req.Variables["after"].(string)
		page, ok := pages[cursor]
		if !ok {
			t.Errorf("no fixture for cursor %q", cursor)
			page = `{"data":null}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, page)
	})
	return mux
}

const threadsPage1 = `{"data":{"repository":{"pullRequest":{"reviewThreads":{
  "pageInfo":{"hasNextPage":true,"endCursor":"C1"},
  "nodes":[
    {"id":"PRRT_1","isResolved":false,"isOutdated":false,"path":"src/lib.rs",
     "line":42,"originalLine":42,"startLine":null,"diffSide":"RIGHT",
     "comments":{"nodes":[
       {"id":"PRRC_1","body":"Can this be simplified?","author":{"login":"alice"},
        "createdAt":"2026-05-12T18:30:00Z","url":"https://example.com/1","replyTo":null},
       {"id":"PRRC_2","body":"Done.","author":{"login":"bob"},
        "createdAt":"2026-05-12T19:00:00Z","url":"https://example.com/2","replyTo":{"id":"PRRC_1"}}
     ]}},
    {"id":"PRRT_outdated","isResolved":false,"isOutdated":true,"path":"src/main.rs",
     "line":null,"originalLine":19,"startLine":null,"diffSide":"LEFT",
     "comments":{"nodes":[
       {"id":"PRRC_old","body":"Moved line.","author":null,
        "createdAt":"2026-05-10T00:00:00Z","url":"https://example.com/o","replyTo":null}
     ]}}
  ]}}}}}`

const threadsPage2 = `{"data":{"repository":{"pullRequest":{"reviewThreads":{
  "pageInfo":{"hasNextPage":false,"endCursor":null},
  "nodes":[
    {"id":"PRRT_resolved","isResolved":true,"isOutdated":false,"path":"src/lib.rs",
     "line":7,"originalLine":7,"startLine":5,"diffSide":"RIGHT",
     "comments":{"nodes":[
       {"id":"PRRC_r","body":"Old resolved comment.","author":{"login":"bob"},
        "createdAt":"2026-05-01T00:00:00Z","url":"https://example.com/r","replyTo":null}
     ]}}
  ]}}}}}`

func TestListReviewThreadsPaginatesAndMaps(t *testing.T) {
	d := newTestDriver(t, graphqlHandler(t, map[string]string{
		"": threadsPage1, "C1": threadsPage2,
	}))

	threads, err := d.ListReviewThreads(context.Background(), testPR())
	if err != nil {
		t.Fatalf("ListReviewThreads: %v", err)
	}
	if len(threads) != 3 {
		t.Fatalf("threads = %d, want 3", len(threads))
	}

	first := threads[0]
	if first.ID != "PRRT_1" || first.Path != "src/lib.rs" || first.Side != forge.SideNew ||
		first.Line == nil || *first.Line != 42 || first.IsResolved || first.IsOutdated {
		t.Errorf("first thread = %+v", first)
	}
	if len(first.Comments) != 2 {
		t.Fatalf("comments = %d, want 2", len(first.Comments))
	}
	root, reply := first.Comments[0], first.Comments[1]
	if root.Author != "alice" || root.Body != "Can this be simplified?" ||
		root.InReplyTo != "" || root.CreatedAt == nil || root.URL != "https://example.com/1" {
		t.Errorf("root comment = %+v", root)
	}
	if reply.InReplyTo != "PRRC_1" || reply.Author != "bob" {
		t.Errorf("reply comment = %+v", reply)
	}
	if first.Root() == nil || first.Root().ID != "PRRC_1" {
		t.Errorf("Root() = %+v", first.Root())
	}

	// Outdated thread: null line falls back to originalLine; LEFT → old
	// side; nil author tolerated.
	outdated := threads[1]
	if !outdated.IsOutdated || outdated.Side != forge.SideOld ||
		outdated.Line == nil || *outdated.Line != 19 {
		t.Errorf("outdated thread = %+v", outdated)
	}
	if outdated.Comments[0].Author != "" {
		t.Errorf("nil author must map to empty login: %+v", outdated.Comments[0])
	}

	resolved := threads[2]
	if !resolved.IsResolved || resolved.StartLine == nil || *resolved.StartLine != 5 {
		t.Errorf("resolved thread = %+v", resolved)
	}
}

const reviewsPage1 = `{"data":{"repository":{"pullRequest":{"reviews":{
  "pageInfo":{"hasNextPage":true,"endCursor":"R1"},
  "nodes":[
    {"id":"PRR_1","state":"COMMENTED","body":"Overall this looks tight.",
     "author":{"login":"alice"},"submittedAt":"2026-05-12T18:30:00Z","url":"https://example.com/r/1"},
    {"id":"PRR_empty","state":"APPROVED","body":"",
     "author":{"login":"ci-bot"},"submittedAt":"2026-05-12T20:00:00Z","url":"https://example.com/r/e"},
    {"id":"PRR_ws","state":"COMMENTED","body":"   \n\t",
     "author":{"login":"ci-bot"},"submittedAt":"2026-05-12T20:05:00Z","url":"https://example.com/r/w"}
  ]}}}}}`

const reviewsPage2 = `{"data":{"repository":{"pullRequest":{"reviews":{
  "pageInfo":{"hasNextPage":false,"endCursor":null},
  "nodes":[
    {"id":"PRR_2","state":"CHANGES_REQUESTED","body":"Needs a test.",
     "author":null,"submittedAt":null,"url":"https://example.com/r/2"}
  ]}}}}}`

func TestListReviewSummariesDropsEmptyBodiesAndPaginates(t *testing.T) {
	d := newTestDriver(t, graphqlHandler(t, map[string]string{
		"": reviewsPage1, "R1": reviewsPage2,
	}))

	summaries, err := d.ListReviewSummaries(context.Background(), testPR())
	if err != nil {
		t.Fatalf("ListReviewSummaries: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("summaries = %d, want 2 (empty bodies dropped)", len(summaries))
	}
	first := summaries[0]
	if first.ID != "PRR_1" || first.Author != "alice" || first.State != forge.ReviewCommented ||
		first.Body != "Overall this looks tight." || first.CreatedAt == nil {
		t.Errorf("first summary = %+v", first)
	}
	second := summaries[1]
	if second.State != forge.ReviewChangesRequested || second.Author != "" || second.CreatedAt != nil {
		t.Errorf("second summary = %+v", second)
	}
}

const metadataPage1 = `{"data":{
  "viewer":{"login":"ronen-hoffer"},
  "repository":{"pullRequest":{"reviews":{
    "pageInfo":{"hasNextPage":true,"endCursor":"M1"},
    "nodes":[
      {"author":{"login":"alice"},"submittedAt":"2026-06-01T18:59:23Z","commit":{"oid":"aaa111"}}
    ]}}}}}`

const metadataPage2 = `{"data":{
  "viewer":{"login":"ronen-hoffer"},
  "repository":{"pullRequest":{"reviews":{
    "pageInfo":{"hasNextPage":false,"endCursor":null},
    "nodes":[
      {"author":{"login":"ronen-hoffer"},"submittedAt":"2026-06-02T06:32:29Z","commit":{"oid":"bbb222"}},
      {"author":null,"submittedAt":null,"commit":null}
    ]}}}}}`

func TestReviewMetadataKeepsBareReviewsAndViewer(t *testing.T) {
	d := newTestDriver(t, graphqlHandler(t, map[string]string{
		"": metadataPage1, "M1": metadataPage2,
	}))

	metadata, err := d.ReviewMetadata(context.Background(), testPR())
	if err != nil {
		t.Fatalf("ReviewMetadata: %v", err)
	}
	if metadata.ViewerLogin != "ronen-hoffer" {
		t.Errorf("ViewerLogin = %q", metadata.ViewerLogin)
	}
	// All three reviews survive — bare approvals count for "commits since
	// my last review" even without a body.
	if len(metadata.Reviews) != 3 {
		t.Fatalf("reviews = %d, want 3", len(metadata.Reviews))
	}
	if metadata.Reviews[1].Author != "ronen-hoffer" || metadata.Reviews[1].CommitOID != "bbb222" ||
		metadata.Reviews[1].SubmittedAt == nil {
		t.Errorf("review record = %+v", metadata.Reviews[1])
	}
	if metadata.Reviews[2].Author != "" || metadata.Reviews[2].CommitOID != "" {
		t.Errorf("nil author/commit must map to empty: %+v", metadata.Reviews[2])
	}
}

func TestGraphQLErrorsAreWrapped(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /graphql", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"data":null,"errors":[{"message":"Something went wrong"}]}`)
	})
	d := newTestDriver(t, mux)

	_, err := d.ListReviewThreads(context.Background(), testPR())
	fe := mustForgeErr(t, err)
	if fe.Kind != forge.ErrorNetwork {
		t.Fatalf("error kind = %v, want network (err: %v)", fe.Kind, fe)
	}
	if !strings.Contains(fe.Error(), "Something went wrong") {
		t.Errorf("error should carry the GraphQL message: %v", fe)
	}
}

// TestListReviewThreadsPagesLongThreads: a thread's comments came from one
// comments(first: 100) page, so a longer discussion silently ended at the
// hundredth comment.
func TestListReviewThreadsPagesLongThreads(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /graphql", func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode graphql request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(req.Query, "reviewThreads"):
			_, _ = fmt.Fprint(w, `{"data":{"repository":{"pullRequest":{"reviewThreads":{
			  "pageInfo":{"hasNextPage":false,"endCursor":null},
			  "nodes":[{"id":"PRRT_long","path":"a.go","line":3,"diffSide":"RIGHT",
			    "comments":{"pageInfo":{"hasNextPage":true,"endCursor":"T1"},
			      "nodes":[{"id":"c1","body":"first page"}]}}]}}}}}`)
		case strings.Contains(req.Query, "node(id: $id)"):
			if req.Variables["id"] != "PRRT_long" || req.Variables["after"] != "T1" {
				t.Errorf("continuation variables = %v", req.Variables)
			}
			_, _ = fmt.Fprint(w, `{"data":{"node":{"comments":{
			  "pageInfo":{"hasNextPage":false,"endCursor":null},
			  "nodes":[{"id":"c2","body":"second page"}]}}}}`)
		default:
			t.Errorf("unexpected query %s", req.Query)
		}
	})
	d := newTestDriver(t, mux)

	threads, err := d.ListReviewThreads(context.Background(), testPR())
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 1 || len(threads[0].Comments) != 2 || threads[0].Comments[1].Body != "second page" {
		t.Fatalf("threads = %+v, want one thread with both pages of comments", threads)
	}
}
