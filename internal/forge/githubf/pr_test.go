package githubf

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
)

const prDetailsJSON = `{
  "number":42,"title":"Widen the gizmo","state":"open","draft":true,
  "body":"Widens the gizmo by two flanges.",
  "html_url":"https://github.com/octo/hello/pull/42",
  "updated_at":"2026-06-05T08:00:00Z","merged_at":null,
  "user":{"login":"alice"},
  "head":{"ref":"widen","sha":"headsha"},
  "base":{"ref":"main","sha":"basesha"}
}`

func TestGetPullRequestMapsDetails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/octo/hello/pulls/42", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, prDetailsJSON)
	})
	d := newTestDriver(t, mux)

	repo := testRepo()
	pr, err := d.GetPullRequest(context.Background(), forge.Target{Repository: &repo, Number: 42})
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if pr.Number != 42 || pr.Title != "Widen the gizmo" || pr.Author != "alice" ||
		pr.HeadRefName != "widen" || pr.BaseRefName != "main" ||
		pr.HeadSHA != "headsha" || pr.BaseSHA != "basesha" ||
		pr.Body != "Widens the gizmo by two flanges." ||
		pr.URL != "https://github.com/octo/hello/pull/42" ||
		!pr.IsDraft || pr.Closed || pr.State != "open" || pr.MergedAt != nil {
		t.Errorf("unexpected details: %+v", pr)
	}
	if pr.UpdatedAt == nil {
		t.Error("UpdatedAt must be set")
	}
	if pr.Repository != repo {
		t.Errorf("Repository = %+v", pr.Repository)
	}
}

func TestGetPullRequestClosedAndMerged(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/octo/hello/pulls/7", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"number":7,"state":"closed","merged_at":"2026-06-01T12:00:00Z",
			"user":{"login":"bob"},
			"head":{"ref":"h","sha":"aaa"},"base":{"ref":"b","sha":"bbb"}}`)
	})
	d := newTestDriver(t, mux)

	repo := testRepo()
	pr, err := d.GetPullRequest(context.Background(), forge.Target{Repository: &repo, Number: 7})
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if !pr.Closed || pr.MergedAt == nil {
		t.Errorf("closed/merged not mapped: %+v", pr)
	}
	if !pr.IsReadOnly() {
		t.Error("merged PR must be read-only")
	}
}

func TestGetPullRequestValidatesTarget(t *testing.T) {
	d := newTestDriver(t, forbidNetwork(t))
	repo := testRepo()

	_, err := d.GetPullRequest(context.Background(), forge.Target{Number: 42, Original: "42"})
	wantForgeErr(t, err, forge.ErrorValidation)

	_, err = d.GetPullRequest(context.Background(), forge.Target{Repository: &repo, Number: 0})
	wantForgeErr(t, err, forge.ErrorValidation)
}

func TestGetPullRequestRequiresSHAs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/octo/hello/pulls/42", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"number":42,"state":"open","head":{"ref":"h"},"base":{"ref":"b","sha":"bbb"}}`)
	})
	d := newTestDriver(t, mux)
	repo := testRepo()
	_, err := d.GetPullRequest(context.Background(), forge.Target{Repository: &repo, Number: 42})
	wantForgeErr(t, err, forge.ErrorValidation)
}

const simpleDiff = `diff --git a/src/lib.rs b/src/lib.rs
index 1111111..2222222 100644
--- a/src/lib.rs
+++ b/src/lib.rs
@@ -1,3 +1,3 @@
 pub fn answer() -> u32 {
-    41
+    42
 }
`

func TestGetDiffReturnsRawDiff(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/octo/hello/pulls/42", func(w http.ResponseWriter, r *http.Request) {
		if accept := r.Header.Get("Accept"); !strings.Contains(accept, "diff") {
			t.Errorf("Accept = %q, want a diff media type", accept)
		}
		_, _ = fmt.Fprint(w, simpleDiff)
	})
	d := newTestDriver(t, mux)

	diff, err := d.GetDiff(context.Background(), testPR())
	if err != nil {
		t.Fatalf("GetDiff: %v", err)
	}
	if diff != simpleDiff {
		t.Errorf("diff = %q", diff)
	}
}

func TestGetCommitRangeDiffFallsBackToCompareAPI(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/octo/hello/compare/sha1...sha2", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, simpleDiff)
	})
	d := newTestDriver(t, mux)

	diff, err := d.GetCommitRangeDiff(context.Background(), testPR(), "sha1", "sha2")
	if err != nil {
		t.Fatalf("GetCommitRangeDiff: %v", err)
	}
	if diff != simpleDiff {
		t.Errorf("diff = %q", diff)
	}
}

func TestGetCommitRangeDiffUsesLocalCheckout(t *testing.T) {
	runner := &fakeRunner{out: map[string]string{
		"git cat-file -e sha1": "",
		"git cat-file -e sha2": "",
		"git diff sha1..sha2":  simpleDiff,
	}}
	d := newLocalDriver(t, forbidNetwork(t), runner)

	diff, err := d.GetCommitRangeDiff(context.Background(), testPR(), "sha1", "sha2")
	if err != nil {
		t.Fatalf("GetCommitRangeDiff: %v", err)
	}
	if diff != simpleDiff {
		t.Errorf("diff = %q", diff)
	}
	if len(runner.calls) != 3 {
		t.Errorf("calls = %v", runner.calls)
	}
}

func TestGetCommitRangeDiffLocalMissFallsBack(t *testing.T) {
	// sha2 is absent locally: cat-file fails, so the compare API answers.
	runner := &fakeRunner{out: map[string]string{
		"git cat-file -e sha1": "",
	}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/octo/hello/compare/sha1...sha2", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, simpleDiff)
	})
	d := newLocalDriver(t, mux, runner)

	diff, err := d.GetCommitRangeDiff(context.Background(), testPR(), "sha1", "sha2")
	if err != nil {
		t.Fatalf("GetCommitRangeDiff: %v", err)
	}
	if diff != simpleDiff {
		t.Errorf("diff = %q", diff)
	}
}

func TestListCommitsPaginatesAndMaps(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/octo/hello/pulls/42/commits", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "2" {
			_, _ = fmt.Fprint(w, `[
				{"sha":"ccc3333333333","commit":{"message":"third","author":{"date":"2026-06-03T00:00:00Z"}}}
			]`)
			return
		}
		if r.URL.Query().Get("per_page") != "100" {
			t.Errorf("per_page = %q", r.URL.Query().Get("per_page"))
		}
		w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/octo/hello/pulls/42/commits?page=2>; rel="next"`, r.Host))
		_, _ = fmt.Fprint(w, `[
			{"sha":"aaa1111111111","commit":{
				"message":"first line\n\nbody text","author":{"name":"Alice","email":"a@x","date":"2026-06-01T00:00:00Z"}}},
			{"sha":"bbb2222222222","commit":{
				"message":"second","author":{"email":"bob@x","date":"2026-06-02T00:00:00Z"}}}
		]`)
	})
	d := newTestDriver(t, mux)

	commits, err := d.ListCommits(context.Background(), testPR())
	if err != nil {
		t.Fatalf("ListCommits: %v", err)
	}
	if len(commits) != 3 {
		t.Fatalf("commits = %d, want 3", len(commits))
	}
	first := commits[0]
	if first.OID != "aaa1111111111" || first.ShortOID != "aaa1111" ||
		first.Summary != "first line" || first.Author != "Alice" || first.Timestamp == nil {
		t.Errorf("first commit = %+v", first)
	}
	if commits[1].Author != "bob@x" {
		t.Errorf("email fallback author = %q", commits[1].Author)
	}
	if commits[2].Author != "unknown" {
		t.Errorf("missing author = %q, want unknown", commits[2].Author)
	}
}
