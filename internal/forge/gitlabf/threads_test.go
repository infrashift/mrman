package gitlabf

import (
	"context"
	"net/http"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
)

// discussionsBody exercises the whole conversion table: a positioned
// new-side thread with a reply, a resolved old-side thread, an individual
// note, a system note (skipped), a positionless discussion (skipped), and
// a non-text position (skipped).
const discussionsBody = `[
	{
		"id": "disc-new",
		"individual_note": false,
		"notes": [
			{
				"id": 100,
				"body": "root comment",
				"author": {"username": "alice"},
				"created_at": "2026-06-01T10:00:00Z",
				"system": false,
				"resolved": false,
				"position": {
					"position_type": "text",
					"head_sha": "headsha1",
					"new_path": "src/lib.rs",
					"new_line": 12,
					"old_path": "src/lib.rs",
					"old_line": 11
				}
			},
			{
				"id": 101,
				"body": "a reply",
				"author": {"username": "bob"},
				"system": false
			}
		]
	},
	{
		"id": "disc-old",
		"individual_note": false,
		"notes": [{
			"id": 102,
			"body": "old side",
			"author": {"username": "carol"},
			"system": false,
			"resolved": true,
			"position": {
				"position_type": "text",
				"old_path": "old/name.rs",
				"old_line": 7
			}
		}]
	},
	{
		"id": "disc-note",
		"individual_note": true,
		"notes": [
			{"id": 200, "body": "general MR note", "author": {"username": "dave"}, "system": false},
			{"id": 201, "body": "requested review from @x", "system": true},
			{"id": 202, "body": "", "system": false}
		]
	},
	{
		"id": "disc-system",
		"individual_note": true,
		"notes": [{"id": 300, "body": "requested review from @bob", "system": true}]
	},
	{
		"id": "disc-no-position",
		"individual_note": false,
		"notes": [{"id": 400, "body": "context-less", "system": false}]
	},
	{
		"id": "disc-image",
		"individual_note": false,
		"notes": [{
			"id": 500, "body": "on an image", "system": false,
			"position": {"position_type": "image", "new_path": "logo.png"}
		}]
	}
]`

func TestListReviewThreadsConversionTable(t *testing.T) {
	mux := newFixtureMux(t)
	mux.JSON("GET "+projectPrefix+"/discussions", discussionsBody)
	d := newTestDriver(t, mux)

	threads, err := d.ListReviewThreads(context.Background(), testPR())
	if err != nil {
		t.Fatalf("ListReviewThreads: %v", err)
	}
	if len(threads) != 3 {
		t.Fatalf("threads = %d, want 3 (got %+v)", len(threads), threads)
	}

	newSide := threads[0]
	if newSide.ID != "disc-new" || newSide.Path != "src/lib.rs" {
		t.Errorf("thread 0 = %+v", newSide)
	}
	if newSide.Line == nil || *newSide.Line != 12 || newSide.Side != forge.SideNew {
		t.Errorf("thread 0 anchor = %v/%v", newSide.Line, newSide.Side)
	}
	if newSide.IsResolved || newSide.IsOutdated {
		t.Errorf("thread 0 state = %+v", newSide)
	}
	if len(newSide.Comments) != 2 || newSide.Comments[0].ID != "100" ||
		newSide.Comments[0].Author != "alice" || newSide.Comments[1].Body != "a reply" {
		t.Errorf("thread 0 comments = %+v", newSide.Comments)
	}
	if newSide.Comments[0].CreatedAt == nil {
		t.Error("thread 0 root CreatedAt missing")
	}

	oldSide := threads[1]
	if oldSide.Path != "old/name.rs" || oldSide.Side != forge.SideOld ||
		oldSide.Line == nil || *oldSide.Line != 7 {
		t.Errorf("thread 1 = %+v", oldSide)
	}
	if !oldSide.IsResolved {
		t.Error("thread 1 must be resolved")
	}

	note := threads[2]
	if note.ID != "disc-note" || note.Path != "" || note.Line != nil {
		t.Errorf("thread 2 = %+v", note)
	}
	if len(note.Comments) != 1 || note.Comments[0].Body != "general MR note" {
		t.Errorf("thread 2 comments = %+v (system/empty notes must be dropped)", note.Comments)
	}
}

func TestListReviewThreadsPaginates(t *testing.T) {
	mux := newFixtureMux(t)
	mux.Handle("GET "+projectPrefix+"/discussions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			w.Header().Set("X-Next-Page", "2")
			_, _ = w.Write([]byte(`[{
				"id": "d1", "individual_note": true,
				"notes": [{"id": 1, "body": "first", "system": false}]
			}]`))
		default:
			_, _ = w.Write([]byte(`[{
				"id": "d2", "individual_note": true,
				"notes": [{"id": 2, "body": "second", "system": false}]
			}]`))
		}
	})
	d := newTestDriver(t, mux)

	threads, err := d.ListReviewThreads(context.Background(), testPR())
	if err != nil {
		t.Fatalf("ListReviewThreads: %v", err)
	}
	if len(threads) != 2 || threads[0].ID != "d1" || threads[1].ID != "d2" {
		t.Errorf("threads = %+v", threads)
	}
}

func TestListReviewSummariesIsEmpty(t *testing.T) {
	d := newTestDriver(t, forbidNetwork(t))
	summaries, err := d.ListReviewSummaries(context.Background(), testPR())
	if err != nil || summaries != nil {
		t.Errorf("summaries = %v err = %v, want nil/nil", summaries, err)
	}
}

func TestReviewMetadataViewerAndApprovals(t *testing.T) {
	mux := newFixtureMux(t)
	mux.JSON("GET /api/v4/user", `{"username": "ronen"}`)
	mux.JSON("GET "+projectPrefix+"/versions", `[
		{"id": 3, "head_commit_sha": "ccc333", "created_at": "2026-06-03T09:00:00Z"},
		{"id": 2, "head_commit_sha": "bbb222", "created_at": "2026-06-02T09:00:00Z"}
	]`)
	mux.JSON("GET "+projectPrefix+"/approvals", `{
		"approved_by": [
			{"user": {"username": "ronen"}},
			{"user": {"username": "alice"}},
			{}
		]
	}`)
	d := newTestDriver(t, mux)

	metadata, err := d.ReviewMetadata(context.Background(), testPR())
	if err != nil {
		t.Fatalf("ReviewMetadata: %v", err)
	}
	if metadata.ViewerLogin != "ronen" {
		t.Errorf("ViewerLogin = %q", metadata.ViewerLogin)
	}
	if len(metadata.Reviews) != 2 {
		t.Fatalf("reviews = %+v, want 2 (nil approver dropped)", metadata.Reviews)
	}
	if metadata.Reviews[0].Author != "ronen" || metadata.Reviews[0].CommitOID != "ccc333" {
		t.Errorf("review 0 = %+v", metadata.Reviews[0])
	}
	if metadata.Reviews[1].Author != "alice" || metadata.Reviews[1].CommitOID != "ccc333" {
		t.Errorf("review 1 = %+v", metadata.Reviews[1])
	}
}

func TestReviewMetadataDegradesGracefully(t *testing.T) {
	mux := newFixtureMux(t)
	fail := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message": "403 Forbidden"}`))
	}
	mux.Handle("GET /api/v4/user", fail)
	mux.Handle("GET "+projectPrefix+"/versions", fail)
	mux.Handle("GET "+projectPrefix+"/approvals", fail)
	d := newTestDriver(t, mux)

	metadata, err := d.ReviewMetadata(context.Background(), testPR())
	if err != nil {
		t.Fatalf("ReviewMetadata must degrade, got error: %v", err)
	}
	if metadata.ViewerLogin != "" || len(metadata.Reviews) != 0 {
		t.Errorf("metadata = %+v, want empty", metadata)
	}
}
