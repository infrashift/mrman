package gitlabf

import (
	"context"
	"net/http"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
)

const mrDetailsBody = `{
	"iid": 42,
	"title": "My MR",
	"web_url": "https://gitlab.com/group/sub/repo/-/merge_requests/42",
	"state": "opened",
	"draft": false,
	"author": {"username": "alice"},
	"source_branch": "feature",
	"target_branch": "main",
	"sha": "head111",
	"diff_refs": {
		"base_sha": "base000",
		"head_sha": "head111",
		"start_sha": "start222"
	},
	"description": "desc",
	"updated_at": "2026-06-01T10:00:00Z",
	"merged_at": null
}`

func target() forge.Target {
	repo := testRepo()
	return forge.Target{Repository: &repo, Number: 42, Original: "42"}
}

func TestGetPullRequestStashesStartSHA(t *testing.T) {
	mux := newFixtureMux(t)
	mux.JSON("GET "+projectPrefix, mrDetailsBody)
	d := newTestDriver(t, mux)

	pr, err := d.GetPullRequest(context.Background(), target())
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if pr.HeadSHA != "head111" || pr.BaseSHA != "base000" {
		t.Errorf("SHAs = %q/%q", pr.HeadSHA, pr.BaseSHA)
	}
	if string(pr.ForgePayload) != `{"start_sha":"start222"}` {
		t.Errorf("ForgePayload = %s", pr.ForgePayload)
	}
	if pr.Title != "My MR" || pr.Author != "alice" || pr.Body != "desc" {
		t.Errorf("fields = %q/%q/%q", pr.Title, pr.Author, pr.Body)
	}
	if pr.HeadRefName != "feature" || pr.BaseRefName != "main" {
		t.Errorf("refs = %q/%q", pr.HeadRefName, pr.BaseRefName)
	}
	if pr.State != "open" || pr.Closed || pr.MergedAt != nil {
		t.Errorf("state = %q closed=%v mergedAt=%v", pr.State, pr.Closed, pr.MergedAt)
	}
	if startSHA(pr) != "start222" {
		t.Errorf("startSHA = %q", startSHA(pr))
	}
}

func TestGetPullRequestFallsBackToSHA(t *testing.T) {
	mux := newFixtureMux(t)
	mux.JSON("GET "+projectPrefix, `{
		"iid": 42, "state": "closed", "sha": "fallback1",
		"closed_at": "2026-06-01T10:00:00Z"
	}`)
	d := newTestDriver(t, mux)

	pr, err := d.GetPullRequest(context.Background(), target())
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if pr.HeadSHA != "fallback1" {
		t.Errorf("HeadSHA = %q, want fallback1", pr.HeadSHA)
	}
	if len(pr.ForgePayload) != 0 {
		t.Errorf("ForgePayload = %s, want empty", pr.ForgePayload)
	}
	if !pr.Closed || pr.ReadOnlyReason() != "closed" {
		t.Errorf("closed = %v reason = %q", pr.Closed, pr.ReadOnlyReason())
	}
}

func TestGetPullRequestMerged(t *testing.T) {
	mux := newFixtureMux(t)
	mux.JSON("GET "+projectPrefix, `{
		"iid": 42, "state": "merged", "sha": "head111",
		"merged_at": "2026-06-01T10:00:00Z"
	}`)
	d := newTestDriver(t, mux)

	pr, err := d.GetPullRequest(context.Background(), target())
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if pr.MergedAt == nil || pr.ReadOnlyReason() != "merged" {
		t.Errorf("mergedAt = %v reason = %q", pr.MergedAt, pr.ReadOnlyReason())
	}
}

func TestGetPullRequestMissingSHAsFails(t *testing.T) {
	mux := newFixtureMux(t)
	mux.JSON("GET "+projectPrefix, `{"iid": 42, "state": "opened"}`)
	d := newTestDriver(t, mux)
	_, err := d.GetPullRequest(context.Background(), target())
	wantForgeErr(t, err, forge.ErrorValidation)
}

func TestGetPullRequestValidatesTarget(t *testing.T) {
	d := newTestDriver(t, forbidNetwork(t))
	if _, err := d.GetPullRequest(context.Background(), forge.Target{Original: "x"}); err == nil {
		t.Error("expected error for target without repository")
	} else {
		wantForgeErr(t, err, forge.ErrorValidation)
	}
	repo := testRepo()
	if _, err := d.GetPullRequest(context.Background(),
		forge.Target{Repository: &repo, Original: "0"}); err == nil {
		t.Error("expected error for target without number")
	} else {
		wantForgeErr(t, err, forge.ErrorValidation)
	}
}

func TestListCommitsReturnsOldestFirst(t *testing.T) {
	mux := newFixtureMux(t)
	page := 0
	mux.Handle("GET "+projectPrefix+"/commits", func(w http.ResponseWriter, r *http.Request) {
		page++
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			w.Header().Set("X-Next-Page", "2")
			// GitLab serves newest first.
			_, _ = w.Write([]byte(`[
				{"id": "ccc3", "short_id": "ccc", "title": "third",
				 "author_name": "Carol", "committed_date": "2026-06-03T09:00:00Z"},
				{"id": "bbb2", "short_id": "bbb", "title": "second", "author_name": "Bob"}
			]`))
		default:
			_, _ = w.Write([]byte(`[
				{"id": "aaa1111111", "title": "first", "author_name": "Alice"}
			]`))
		}
	})
	d := newTestDriver(t, mux)

	commits, err := d.ListCommits(context.Background(), testPR())
	if err != nil {
		t.Fatalf("ListCommits: %v", err)
	}
	if page != 2 {
		t.Errorf("pages fetched = %d, want 2", page)
	}
	if len(commits) != 3 {
		t.Fatalf("commits = %d, want 3", len(commits))
	}
	if commits[0].OID != "aaa1111111" || commits[1].OID != "bbb2" || commits[2].OID != "ccc3" {
		t.Errorf("order = %q,%q,%q, want oldest first",
			commits[0].OID, commits[1].OID, commits[2].OID)
	}
	if commits[0].ShortOID != "aaa1111" {
		t.Errorf("ShortOID fallback = %q, want aaa1111", commits[0].ShortOID)
	}
	if commits[2].Summary != "third" || commits[2].Author != "Carol" || commits[2].Timestamp == nil {
		t.Errorf("commit fields = %+v", commits[2])
	}
}

func TestGetCommitRangeDiffLocalFastPath(t *testing.T) {
	runner := &fakeRunner{out: map[string]string{
		"git cat-file -e start222": "",
		"git cat-file -e head111":  "",
		"git diff start222..head111": "diff --git a/x b/x\n" +
			"--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+b\n",
	}}
	d := newLocalDriver(t, forbidNetwork(t), runner)

	diff, err := d.GetCommitRangeDiff(context.Background(), testPR(), "start222", "head111")
	if err != nil {
		t.Fatalf("GetCommitRangeDiff: %v", err)
	}
	if diff == "" || diff[:10] != "diff --git" {
		t.Errorf("diff = %q", diff)
	}
}

func TestGetCommitRangeDiffFallsBackToCompare(t *testing.T) {
	mux := newFixtureMux(t)
	var gotQuery string
	mux.Handle("GET /api/v4/projects/group%2Fsub%2Frepo/repository/compare",
		func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.RawQuery
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"diffs": [{
					"old_path": "src/lib.rs", "new_path": "src/lib.rs",
					"diff": "@@ -1 +1 @@\n-a\n+b\n"
				}]
			}`))
		})
	runner := &fakeRunner{out: map[string]string{}} // SHAs missing locally
	d := newLocalDriver(t, mux, runner)

	diff, err := d.GetCommitRangeDiff(context.Background(), testPR(), "start222", "head111")
	if err != nil {
		t.Fatalf("GetCommitRangeDiff: %v", err)
	}
	want := "diff --git a/src/lib.rs b/src/lib.rs\n" +
		"--- a/src/lib.rs\n+++ b/src/lib.rs\n@@ -1 +1 @@\n-a\n+b\n"
	if diff != want {
		t.Errorf("diff = %q, want %q", diff, want)
	}
	if !containsParam(gotQuery, "from=start222") || !containsParam(gotQuery, "to=head111") {
		t.Errorf("query = %q", gotQuery)
	}
}
