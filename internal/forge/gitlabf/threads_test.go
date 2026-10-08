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

// TestListReviewSummariesAreTheGeneralNotes: GitLab has no review object,
// so a review body lands as a general MR note. Those notes come back as
// summaries; positioned discussions and system notes do not.
func TestListReviewSummariesAreTheGeneralNotes(t *testing.T) {
	mux := newFixtureMux(t)
	mux.JSON("GET "+projectPrefix+"/discussions", discussionsBody)
	d := newTestDriver(t, mux)
	summaries, err := d.ListReviewSummaries(context.Background(), testPR())
	if err != nil {
		t.Fatal(err)
	}
	threads, _ := d.ListReviewThreads(context.Background(), testPR())
	want := 0
	for _, th := range threads {
		if th.Path == "" {
			want++
		}
	}
	if want == 0 || len(summaries) != want {
		t.Fatalf("summaries = %+v, want one per general note (%d)", summaries, want)
	}
	for _, s := range summaries {
		if s.Body == "" || s.Author == "" {
			t.Errorf("summary %+v lacks its author or body", s)
		}
	}
}

// TestReviewMetadataStampsEachReviewWithItsVersion: an approval given
// before a later push covers only the commits that existed then. Every
// approval used to be stamped with the latest head, so "commits since your
// last review" claimed there were none.
func TestReviewMetadataStampsEachReviewWithItsVersion(t *testing.T) {
	mux := newFixtureMux(t)
	mux.JSON("GET /api/v4/user", `{"username": "ronen"}`)
	mux.JSON("GET "+projectPrefix+"/versions", `[
		{"id": 3, "head_commit_sha": "ccc333", "created_at": "2026-06-03T09:00:00Z"},
		{"id": 2, "head_commit_sha": "bbb222", "created_at": "2026-06-02T09:00:00Z"}
	]`)
	mux.JSON("GET "+projectPrefix+"/notes", `[
		{"id": 1, "system": true, "body": "approved this merge request",
		 "author": {"username": "ronen"}, "created_at": "2026-06-02T10:00:00Z"},
		{"id": 2, "system": false, "body": "looks good",
		 "author": {"username": "alice"}, "created_at": "2026-06-03T10:00:00Z"},
		{"id": 3, "system": true, "body": "added 1 commit",
		 "author": {"username": "bob"}, "created_at": "2026-06-03T08:59:00Z"},
		{"id": 4, "system": false, "body": "before any version",
		 "author": {"username": "eve"}, "created_at": "2026-06-01T00:00:00Z"},
		{"id": 5, "system": true, "body": "requested changes",
		 "author": {"username": "rex"}, "created_at": "2026-06-03T11:00:00Z"}
	]`)
	d := newTestDriver(t, mux)

	metadata, err := d.ReviewMetadata(context.Background(), testPR())
	if err != nil {
		t.Fatalf("ReviewMetadata: %v", err)
	}
	if metadata.ViewerLogin != "ronen" {
		t.Errorf("ViewerLogin = %q", metadata.ViewerLogin)
	}
	got := map[string]string{}
	for _, r := range metadata.Reviews {
		got[r.Author] = r.CommitOID
		if r.SubmittedAt == nil {
			t.Errorf("%s: no SubmittedAt", r.Author)
		}
	}
	want := map[string]string{"ronen": "bbb222", "alice": "ccc333", "rex": "ccc333"}
	if len(got) != len(want) || got["ronen"] != want["ronen"] || got["alice"] != want["alice"] || got["rex"] != want["rex"] {
		t.Errorf("reviews = %v, want %v (system events and unplaceable notes dropped)", got, want)
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
	mux.Handle("GET "+projectPrefix+"/notes", fail)
	d := newTestDriver(t, mux)

	metadata, err := d.ReviewMetadata(context.Background(), testPR())
	if err != nil {
		t.Fatalf("ReviewMetadata must degrade, got error: %v", err)
	}
	if metadata.ViewerLogin != "" || len(metadata.Reviews) != 0 {
		t.Errorf("metadata = %+v, want empty", metadata)
	}
}

// TestListReviewThreadsReadsLineRanges: a range comment used to come back
// as a single line, its end, so the TUI drew it on one line and the live
// test could assert only the end.
func TestListReviewThreadsReadsLineRanges(t *testing.T) {
	mux := newFixtureMux(t)
	mux.JSON("GET "+projectPrefix+"/discussions", `[
		{"id": "new-range", "notes": [{"id": 1, "body": "b", "author": {"username": "a"},
			"position": {"position_type": "text", "new_path": "f.go", "new_line": 18, "old_line": 0,
				"line_range": {"start": {"type": "new", "new_line": 16}, "end": {"type": "new", "new_line": 18}}}}]},
		{"id": "context-range", "notes": [{"id": 2, "body": "b", "author": {"username": "a"},
			"position": {"position_type": "text", "new_path": "f.go", "new_line": 19, "old_line": 16,
				"line_range": {"start": {"old_line": 5, "new_line": 5}, "end": {"old_line": 16, "new_line": 19}}}}]},
		{"id": "old-range", "notes": [{"id": 3, "body": "b", "author": {"username": "a"},
			"position": {"position_type": "text", "old_path": "f.go", "old_line": 9,
				"line_range": {"start": {"type": "old", "old_line": 7}, "end": {"type": "old", "old_line": 9}}}}]},
		{"id": "mixed-range", "notes": [{"id": 4, "body": "b", "author": {"username": "a"},
			"position": {"position_type": "text", "new_path": "f.go", "new_line": 4,
				"line_range": {"start": {"type": "old", "old_line": 3}, "end": {"type": "new", "new_line": 4}}}}]},
		{"id": "single", "notes": [{"id": 5, "body": "b", "author": {"username": "a"},
			"position": {"position_type": "text", "new_path": "f.go", "new_line": 2,
				"line_range": {"start": {"type": "new", "new_line": 2}, "end": {"type": "new", "new_line": 2}}}}]}
	]`)
	d := newTestDriver(t, mux)
	threads, err := d.ListReviewThreads(context.Background(), testPR())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]uint32{"new-range": 16, "context-range": 5, "old-range": 7}
	for _, th := range threads {
		w, isRange := want[th.ID]
		switch {
		case isRange && (th.StartLine == nil || *th.StartLine != w):
			t.Errorf("%s: StartLine = %v, want %d", th.ID, th.StartLine, w)
		case !isRange && th.StartLine != nil:
			t.Errorf("%s: StartLine = %d, want none", th.ID, *th.StartLine)
		}
	}
	if len(threads) != 5 {
		t.Fatalf("got %d threads, want 5", len(threads))
	}
}
