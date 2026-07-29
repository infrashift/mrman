package forgejof

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

func targetFor(repo forgetypes.Repository) forge.Target {
	return forge.Target{Repository: &repo, Number: 42, Original: "octo/hello#42"}
}

func TestGetPullRequestUsesMergeBase(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/repos/octo/hello/pulls/42", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{
			"number":42,"title":"Add feature","body":"the description","state":"open",
			"html_url":"https://fj/octo/hello/pulls/42","user":{"login":"alice"},
			"merge_base":"mergebasesha",
			"head":{"ref":"feat","sha":"headsha"},
			"base":{"ref":"main","sha":"basetipsha"},
			"updated_at":"2026-07-10T12:00:00Z"
		}`)
	})
	d := newTestDriver(t, mux)

	pr, err := d.GetPullRequest(context.Background(), targetFor(testRepo()))
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if pr.HeadSHA != "headsha" {
		t.Errorf("HeadSHA = %q", pr.HeadSHA)
	}
	// The diff base is the merge base, not the moving base branch tip.
	if pr.BaseSHA != "mergebasesha" {
		t.Errorf("BaseSHA = %q, want mergebasesha", pr.BaseSHA)
	}
	if pr.Number != 42 || pr.Title != "Add feature" || pr.Body != "the description" ||
		pr.Author != "alice" || pr.HeadRefName != "feat" || pr.BaseRefName != "main" ||
		pr.URL != "https://fj/octo/hello/pulls/42" || pr.State != "open" {
		t.Errorf("pr = %+v", pr)
	}
	if pr.Closed || pr.MergedAt != nil || pr.IsReadOnly() {
		t.Errorf("open PR reported read-only: %+v", pr)
	}
	if pr.UpdatedAt == nil {
		t.Error("UpdatedAt missing")
	}
}

func TestGetPullRequestClosedAndMerged(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/repos/octo/hello/pulls/42", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{
			"number":42,"title":"Done","state":"closed","merged":true,
			"merged_at":"2026-07-11T08:00:00Z","merge_base":"mb",
			"head":{"ref":"feat","sha":"h"},"base":{"ref":"main","sha":"b"}
		}`)
	})
	d := newTestDriver(t, mux)

	pr, err := d.GetPullRequest(context.Background(), targetFor(testRepo()))
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if !pr.Closed || pr.MergedAt == nil {
		t.Errorf("closed=%v mergedAt=%v", pr.Closed, pr.MergedAt)
	}
	if pr.ReadOnlyReason() != "merged" {
		t.Errorf("ReadOnlyReason = %q", pr.ReadOnlyReason())
	}
}

func TestGetPullRequestFallsBackToBaseTip(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/repos/octo/hello/pulls/42", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"number":42,"state":"open",
			"head":{"ref":"feat","sha":"h"},"base":{"ref":"main","sha":"basetip"}}`)
	})
	d := newTestDriver(t, mux)

	pr, err := d.GetPullRequest(context.Background(), targetFor(testRepo()))
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if pr.BaseSHA != "basetip" {
		t.Errorf("BaseSHA = %q, want basetip fallback", pr.BaseSHA)
	}
}

func TestGetPullRequestValidatesTarget(t *testing.T) {
	d := newTestDriver(t, forbidNetwork(t))

	_, err := d.GetPullRequest(context.Background(), forge.Target{Original: "42"})
	wantForgeErr(t, err, forge.ErrorValidation)

	repo := testRepo()
	_, err = d.GetPullRequest(context.Background(), forge.Target{Repository: &repo, Original: "octo/hello"})
	wantForgeErr(t, err, forge.ErrorValidation)
}

func TestGetPullRequestMissingSHAs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/repos/octo/hello/pulls/42", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"number":42,"state":"open"}`)
	})
	d := newTestDriver(t, mux)

	_, err := d.GetPullRequest(context.Background(), targetFor(testRepo()))
	wantForgeErr(t, err, forge.ErrorValidation)
}

func TestGetPullRequestNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/repos/octo/hello/pulls/42", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"pull request does not exist"}`, http.StatusNotFound)
	})
	d := newTestDriver(t, mux)

	_, err := d.GetPullRequest(context.Background(), targetFor(testRepo()))
	fe := mustForgeErr(t, err)
	if fe.Kind != forge.ErrorNotFound || fe.Hint != hintNotFound {
		t.Errorf("err = %+v", fe)
	}
}

func TestGetCommitRangeDiffLocalFastPath(t *testing.T) {
	runner := &fakeRunner{out: map[string]string{
		"git cat-file -e sha1": "",
		"git cat-file -e sha2": "",
		"git diff sha1..sha2":  "diff --git a/x b/x\n",
	}}
	d := newLocalDriver(t, forbidNetwork(t), runner)

	diff, err := d.GetCommitRangeDiff(context.Background(), testPR(), "sha1", "sha2")
	if err != nil {
		t.Fatalf("GetCommitRangeDiff: %v", err)
	}
	if diff != "diff --git a/x b/x\n" {
		t.Errorf("diff = %q", diff)
	}
}

func TestGetCommitRangeDiffUnsupportedWithoutLocal(t *testing.T) {
	// No checkout at all.
	d := newTestDriver(t, forbidNetwork(t))
	_, err := d.GetCommitRangeDiff(context.Background(), testPR(), "sha1", "sha2")
	fe := mustForgeErr(t, err)
	if fe.Kind != forge.ErrorUnsupported || fe.Hint != hintRangeDiff {
		t.Errorf("err = %+v", fe)
	}

	// Checkout present but missing one SHA.
	runner := &fakeRunner{out: map[string]string{"git cat-file -e sha1": ""}}
	d = newLocalDriver(t, forbidNetwork(t), runner)
	_, err = d.GetCommitRangeDiff(context.Background(), testPR(), "sha1", "sha2")
	wantForgeErr(t, err, forge.ErrorUnsupported)
}

func TestListCommitsReversesToOldestFirst(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/repos/octo/hello/pulls/42/commits", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			// Forgejo serves git-log order: newest first.
			w.Header().Set("Link", fmt.Sprintf("<http://%s/api/v1/repos/octo/hello/pulls/42/commits?page=2&limit=100>; rel=\"next\"", r.Host))
			_, _ = fmt.Fprint(w, `[
				{"sha":"ccc3333","created":"2026-07-03T10:00:00Z",
				 "commit":{"message":"third commit\n\nlong body","author":{"name":"Alice","email":"a@example.com","date":"2026-07-03T10:00:00Z"}},
				 "author":{"login":"alice"}},
				{"sha":"bbb2222","created":"2026-07-02T10:00:00Z",
				 "commit":{"message":"second commit","author":{"name":"","email":"bob@example.com","date":"2026-07-02T10:00:00Z"}}}
			]`)
		case "2":
			_, _ = fmt.Fprint(w, `[
				{"sha":"aaa1111","created":"2026-07-01T10:00:00Z",
				 "commit":{"message":"first commit"},
				 "author":{"login":"carol"}}
			]`)
		default:
			t.Errorf("unexpected page %q", r.URL.Query().Get("page"))
		}
	})
	d := newTestDriver(t, mux)

	commits, err := d.ListCommits(context.Background(), testPR())
	if err != nil {
		t.Fatalf("ListCommits: %v", err)
	}
	if len(commits) != 3 {
		t.Fatalf("commits = %+v", commits)
	}
	if commits[0].OID != "aaa1111" || commits[1].OID != "bbb2222" || commits[2].OID != "ccc3333" {
		t.Errorf("order = %s %s %s, want oldest first", commits[0].OID, commits[1].OID, commits[2].OID)
	}
	if commits[2].ShortOID != "ccc3333"[:7] || commits[2].Summary != "third commit" ||
		commits[2].Author != "Alice" || commits[2].Timestamp == nil {
		t.Errorf("newest = %+v", commits[2])
	}
	// Author fallbacks: email when name empty, login when no commit author,
	// timestamp from top-level created when the author date is absent.
	if commits[1].Author != "bob@example.com" {
		t.Errorf("email fallback = %q", commits[1].Author)
	}
	if commits[0].Author != "carol" || commits[0].Timestamp == nil {
		t.Errorf("login fallback = %+v", commits[0])
	}
}

func TestGetDiffError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/repos/octo/hello/pulls/42.diff", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
	})
	d := newTestDriver(t, mux)

	_, err := d.GetDiff(context.Background(), testPR())
	wantForgeErr(t, err, forge.ErrorServer)
}
