package forgejof

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
)

// threadFixture serves two reviews with code comments (and one bare
// approval whose comments must never be fetched) exercising grouping,
// replies, resolution, and both outdated approximations.
func threadFixture(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/repos/octo/hello/pulls/42/reviews", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "1" {
			t.Errorf("page = %q", r.URL.Query().Get("page"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[
			{"id":1,"user":{"login":"alice"},"state":"COMMENT","body":"","commit_id":"headsha",
			 "comments_count":4,"submitted_at":"2026-07-01T10:00:00Z"},
			{"id":2,"user":{"login":"bob"},"state":"COMMENT","body":"","commit_id":"headsha",
			 "comments_count":1,"submitted_at":"2026-07-01T11:00:00Z"},
			{"id":3,"user":{"login":"carol"},"state":"APPROVED","body":"","commit_id":"headsha",
			 "comments_count":0,"submitted_at":"2026-07-01T12:00:00Z"}
		]`)
	})
	mux.HandleFunc("GET /api/v1/repos/octo/hello/pulls/42/reviews/1/comments", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[
			{"id":101,"body":"root comment","user":{"login":"alice"},"path":"src/lib.rs",
			 "position":10,"original_position":0,"commit_id":"headsha","original_commit_id":"headsha",
			 "created_at":"2026-07-01T10:00:00Z","html_url":"https://fj/c/101"},
			{"id":102,"body":"old side, resolved","user":{"login":"alice"},"path":"old.rs",
			 "position":0,"original_position":7,"commit_id":"headsha","original_commit_id":"headsha",
			 "resolver":{"login":"bob"},
			 "created_at":"2026-07-01T10:05:00Z","html_url":"https://fj/c/102"},
			{"id":103,"body":"fully outdated","user":{"login":"alice"},"path":"gone.rs",
			 "position":0,"original_position":0,"commit_id":"headsha","original_commit_id":"headsha",
			 "created_at":"2026-07-01T10:10:00Z","html_url":"https://fj/c/103"},
			{"id":104,"body":"drifted commit","user":{"login":"alice"},"path":"src/lib.rs",
			 "position":5,"original_position":0,"commit_id":"newsha","original_commit_id":"headsha",
			 "created_at":"2026-07-01T10:15:00Z","html_url":"https://fj/c/104"}
		]`)
	})
	mux.HandleFunc("GET /api/v1/repos/octo/hello/pulls/42/reviews/2/comments", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[
			{"id":201,"body":"reply to root","user":{"login":"bob"},"path":"src/lib.rs",
			 "position":10,"original_position":0,"commit_id":"headsha","original_commit_id":"headsha",
			 "created_at":"2026-07-01T11:00:00Z","html_url":"https://fj/c/201"}
		]`)
	})
	mux.HandleFunc("GET /api/v1/repos/octo/hello/pulls/42/reviews/3/comments", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("comments of a review with comments_count 0 must not be fetched")
	})
	return mux
}

func TestListReviewThreadsSynthesis(t *testing.T) {
	d := newTestDriver(t, threadFixture(t))

	threads, err := d.ListReviewThreads(context.Background(), testPR())
	if err != nil {
		t.Fatalf("ListReviewThreads: %v", err)
	}
	if len(threads) != 4 {
		t.Fatalf("threads = %d, want 4: %+v", len(threads), threads)
	}

	// Threads arrive in root-posted order.
	root := threads[0]
	if root.ID != "101" || root.Path != "src/lib.rs" || root.Side != forge.SideNew {
		t.Errorf("root thread = %+v", root)
	}
	if root.Line == nil || *root.Line != 10 || root.StartLine != nil {
		t.Errorf("root anchor = %+v", root)
	}
	if root.IsResolved || root.IsOutdated || !root.IsActive() {
		t.Errorf("root state = %+v", root)
	}
	// Cross-review grouping: bob's comment at the same anchor is a reply.
	if len(root.Comments) != 2 {
		t.Fatalf("root comments = %+v", root.Comments)
	}
	if root.Comments[0].ID != "101" || root.Comments[0].Author != "alice" ||
		root.Comments[0].InReplyTo != "" || root.Comments[0].URL != "https://fj/c/101" ||
		root.Comments[0].CreatedAt == nil {
		t.Errorf("root comment = %+v", root.Comments[0])
	}
	if root.Comments[1].ID != "201" || root.Comments[1].Author != "bob" ||
		root.Comments[1].InReplyTo != "101" {
		t.Errorf("reply = %+v", root.Comments[1])
	}

	oldSide := threads[1]
	if oldSide.ID != "102" || oldSide.Side != forge.SideOld || oldSide.Line == nil || *oldSide.Line != 7 {
		t.Errorf("old-side thread = %+v", oldSide)
	}
	if !oldSide.IsResolved || oldSide.IsOutdated {
		t.Errorf("old-side state = %+v", oldSide)
	}

	outdated := threads[2]
	if outdated.ID != "103" || !outdated.IsOutdated || outdated.Line != nil {
		t.Errorf("zero-position thread = %+v", outdated)
	}

	drifted := threads[3]
	if drifted.ID != "104" || !drifted.IsOutdated {
		t.Errorf("commit-drift thread = %+v", drifted)
	}
	if drifted.Line == nil || *drifted.Line != 5 || drifted.Side != forge.SideNew {
		t.Errorf("commit-drift anchor = %+v", drifted)
	}
}

func TestListReviewThreadsCommentsError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/repos/octo/hello/pulls/42/reviews", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[{"id":1,"state":"COMMENT","comments_count":1}]`)
	})
	mux.HandleFunc("GET /api/v1/repos/octo/hello/pulls/42/reviews/1/comments", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"nope"}`, http.StatusForbidden)
	})
	d := newTestDriver(t, mux)

	_, err := d.ListReviewThreads(context.Background(), testPR())
	wantForgeErr(t, err, forge.ErrorForbidden)
}

func TestListReviewSummariesStateMapping(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/repos/octo/hello/pulls/42/reviews", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[
			{"id":11,"user":{"login":"alice"},"state":"APPROVED","body":"LGTM",
			 "html_url":"https://fj/r/11","submitted_at":"2026-07-01T10:00:00Z"},
			{"id":12,"user":{"login":"bob"},"state":"REQUEST_CHANGES","body":"fix it",
			 "submitted_at":"2026-07-01T11:00:00Z"},
			{"id":13,"user":{"login":"carol"},"state":"COMMENT","body":"   "},
			{"id":14,"user":{"login":"me"},"state":"PENDING","body":"draft note"},
			{"id":15,"user":{"login":"dan"},"state":"COMMENT","body":"meh","dismissed":true}
		]`)
	})
	d := newTestDriver(t, mux)

	summaries, err := d.ListReviewSummaries(context.Background(), testPR())
	if err != nil {
		t.Fatalf("ListReviewSummaries: %v", err)
	}
	if len(summaries) != 4 {
		t.Fatalf("empty-body reviews must be dropped: %+v", summaries)
	}
	first := summaries[0]
	if first.ID != "11" || first.Author != "alice" || first.Body != "LGTM" ||
		first.State != forge.ReviewApproved || first.URL != "https://fj/r/11" ||
		first.CreatedAt == nil {
		t.Errorf("approved summary = %+v", first)
	}
	if summaries[1].State != forge.ReviewChangesRequested {
		t.Errorf("REQUEST_CHANGES → %v", summaries[1].State)
	}
	if summaries[2].State != forge.ReviewPending {
		t.Errorf("PENDING → %v", summaries[2].State)
	}
	if summaries[3].State != forge.ReviewDismissed {
		t.Errorf("dismissed → %v", summaries[3].State)
	}
}

func TestListReviewSummariesPagination(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/repos/octo/hello/pulls/42/reviews", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			w.Header().Set("Link", fmt.Sprintf("<http://%s/api/v1/repos/octo/hello/pulls/42/reviews?page=2&limit=100>; rel=\"next\"", r.Host))
			_, _ = fmt.Fprint(w, `[{"id":21,"state":"COMMENT","body":"page one"}]`)
		case "2":
			_, _ = fmt.Fprint(w, `[{"id":22,"state":"COMMENT","body":"page two"}]`)
		default:
			t.Errorf("unexpected page %q", r.URL.Query().Get("page"))
		}
	})
	d := newTestDriver(t, mux)

	summaries, err := d.ListReviewSummaries(context.Background(), testPR())
	if err != nil {
		t.Fatalf("ListReviewSummaries: %v", err)
	}
	if len(summaries) != 2 || summaries[0].Body != "page one" || summaries[1].Body != "page two" {
		t.Errorf("summaries = %+v", summaries)
	}
}

func TestReviewMetadata(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/user", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":1,"login":"me"}`)
	})
	mux.HandleFunc("GET /api/v1/repos/octo/hello/pulls/42/reviews", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[
			{"id":31,"user":{"login":"me"},"state":"APPROVED","body":"",
			 "commit_id":"oldhead","submitted_at":"2026-07-01T10:00:00Z"},
			{"id":32,"user":{"login":"bob"},"state":"REQUEST_REVIEW","body":"",
			 "submitted_at":"2026-07-01T11:00:00Z"},
			{"id":33,"user":{"login":"me"},"state":"COMMENT","body":"note",
			 "commit_id":"headsha","submitted_at":"2026-07-02T10:00:00Z"}
		]`)
	})
	d := newTestDriver(t, mux)

	metadata, err := d.ReviewMetadata(context.Background(), testPR())
	if err != nil {
		t.Fatalf("ReviewMetadata: %v", err)
	}
	if metadata.ViewerLogin != "me" {
		t.Errorf("ViewerLogin = %q", metadata.ViewerLogin)
	}
	// The REQUEST_REVIEW row records a request, not a review.
	if len(metadata.Reviews) != 2 {
		t.Fatalf("records = %+v", metadata.Reviews)
	}
	first := metadata.Reviews[0]
	if first.Author != "me" || first.CommitOID != "oldhead" || first.SubmittedAt == nil {
		t.Errorf("record = %+v", first)
	}
	if metadata.Reviews[1].CommitOID != "headsha" {
		t.Errorf("record = %+v", metadata.Reviews[1])
	}
}

func TestReviewMetadataUnauthenticated(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/user", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"token required"}`, http.StatusUnauthorized)
	})
	d := newTestDriver(t, mux)

	_, err := d.ReviewMetadata(context.Background(), testPR())
	fe := mustForgeErr(t, err)
	if fe.Kind != forge.ErrorAuth || fe.Hint != hintAuth {
		t.Errorf("err = %+v", fe)
	}
}
