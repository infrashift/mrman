package githubf

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/infrashift/mrman/internal/vcs/diffparser"
)

// TestGetDiffFallsBackToTheFilesListing: GitHub answers a raw-diff request
// for a pull request of more than 300 files with 406 too_large, and the PR
// could not be opened at all (seen live on infrashift/scratch#3). The files
// listing still serves every file's patch, page by page.
func TestGetDiffFallsBackToTheFilesListing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/octo/hello/pulls/42", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotAcceptable)
		_, _ = fmt.Fprint(w, `{"message":"Sorry, the diff exceeded the maximum number of files (300).",
			"errors":[{"resource":"PullRequest","field":"diff","code":"too_large"}]}`)
	})
	mux.HandleFunc("GET /repos/octo/hello/pulls/42/files", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "" {
			w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/octo/hello/pulls/42/files?page=2>; rel="next"`, r.Host))
			_, _ = fmt.Fprint(w, `[
				{"filename":"src/a.go","status":"modified","additions":1,"deletions":1,"changes":2,
				 "patch":"@@ -1,2 +1,2 @@\n ctx\n-old\n+new"},
				{"filename":"docs/new.md","status":"added","additions":1,"deletions":0,"changes":1,
				 "patch":"@@ -0,0 +1 @@\n+hello"},
				{"filename":"gone.txt","status":"removed","additions":0,"deletions":1,"changes":1,
				 "patch":"@@ -1 +0,0 @@\n-bye"}
			]`)
			return
		}
		_, _ = fmt.Fprint(w, `[
			{"filename":"new name.txt","previous_filename":"old name.txt","status":"renamed","additions":0,"deletions":0,"changes":0},
			{"filename":"logo.png","status":"modified","additions":0,"deletions":0,"changes":0},
			{"filename":"huge.json","status":"modified","additions":9000,"deletions":10,"changes":9010}
		]`)
	})
	d := newTestDriver(t, mux)

	diff, err := d.GetDiff(context.Background(), testPR())
	if err != nil {
		t.Fatalf("GetDiff: %v", err)
	}
	files, err := diffparser.Parse(diff, diffparser.GitStyle, nil)
	if err != nil {
		t.Fatalf("Parse: %v\n%s", err, diff)
	}
	type want struct {
		status         string
		hunks          int
		binary, tooBig bool
	}
	wants := map[string]want{
		"src/a.go":     {"modified", 1, false, false},
		"docs/new.md":  {"added", 1, false, false},
		"gone.txt":     {"deleted", 1, false, false},
		"new name.txt": {"renamed", 0, false, false},
		"logo.png":     {"modified", 0, true, false},
		"huge.json":    {"modified", 0, false, true},
	}
	if len(files) != len(wants) {
		t.Fatalf("parsed %d files, want %d:\n%s", len(files), len(wants), diff)
	}
	for _, f := range files {
		w, ok := wants[f.DisplayPath()]
		if !ok {
			t.Errorf("unexpected file %q", f.DisplayPath())
			continue
		}
		if got := string(f.Status); got != w.status || len(f.Hunks) != w.hunks || f.IsBinary != w.binary || f.IsTooLarge != w.tooBig {
			t.Errorf("%s: status %s, %d hunks, binary %v, too large %v; want %+v",
				f.DisplayPath(), got, len(f.Hunks), f.IsBinary, f.IsTooLarge, w)
		}
	}
}
