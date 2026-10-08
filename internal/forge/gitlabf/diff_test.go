package gitlabf

import (
	"context"
	"net/http"
	"testing"

	"github.com/infrashift/mrman/internal/vcs/diffparser"
)

func TestSynthesizeUnifiedDiffGolden(t *testing.T) {
	files := []fileDiff{
		{ // modified file
			OldPath: "src/lib.rs", NewPath: "src/lib.rs",
			AMode: "100644", BMode: "100644",
			Body: "@@ -1,2 +1,2 @@\n-old\n+new\n context\n",
		},
		{ // rename with hunks
			OldPath: "old/name.go", NewPath: "new/name.go",
			Renamed: true,
			Body:    "@@ -3 +3 @@\n-a\n+b\n",
		},
		{ // new file
			OldPath: "", NewPath: "added.txt",
			BMode: "100755", New: true,
			Body: "@@ -0,0 +1 @@\n+hello\n",
		},
		{ // deleted file, mode fallback
			OldPath: "gone.txt", NewPath: "gone.txt",
			Deleted: true,
			Body:    "@@ -1 +0,0 @@\n-bye",
		},
		{ // rename-only entry: headers, no hunks
			OldPath: "a.txt", NewPath: "b.txt", Renamed: true,
		},
	}
	want := "diff --git a/src/lib.rs b/src/lib.rs\n" +
		"--- a/src/lib.rs\n" +
		"+++ b/src/lib.rs\n" +
		"@@ -1,2 +1,2 @@\n-old\n+new\n context\n" +
		"diff --git a/old/name.go b/new/name.go\n" +
		"rename from old/name.go\n" +
		"rename to new/name.go\n" +
		"--- a/old/name.go\n" +
		"+++ b/new/name.go\n" +
		"@@ -3 +3 @@\n-a\n+b\n" +
		"diff --git a/added.txt b/added.txt\n" +
		"new file mode 100755\n" +
		"--- /dev/null\n" +
		"+++ b/added.txt\n" +
		"@@ -0,0 +1 @@\n+hello\n" +
		"diff --git a/gone.txt b/gone.txt\n" +
		"deleted file mode 100644\n" +
		"--- a/gone.txt\n" +
		"+++ /dev/null\n" +
		"@@ -1 +0,0 @@\n-bye\n" + // newline appended
		"diff --git a/a.txt b/b.txt\n" +
		"rename from a.txt\n" +
		"rename to b.txt\n"
	if got := synthesizeUnifiedDiff(files); got != want {
		t.Errorf("synthesized diff mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestSynthesizeUnifiedDiffKeepsExistingHeaders(t *testing.T) {
	files := []fileDiff{{
		OldPath: "x.txt", NewPath: "x.txt",
		Body: "--- a/x.txt\n+++ b/x.txt\n@@ -1 +1 @@\n-a\n+b\n",
	}}
	want := "diff --git a/x.txt b/x.txt\n" +
		"--- a/x.txt\n+++ b/x.txt\n@@ -1 +1 @@\n-a\n+b\n"
	if got := synthesizeUnifiedDiff(files); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestSynthesizeUnifiedDiffSkipsPathlessEntries(t *testing.T) {
	if got := synthesizeUnifiedDiff([]fileDiff{{Body: "@@ -1 +1 @@\n"}}); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestGetDiffPagesAndSynthesizes(t *testing.T) {
	mux := newFixtureMux(t)
	mux.Handle("GET "+projectPrefix+"/diffs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			w.Header().Set("X-Next-Page", "2")
			_, _ = w.Write([]byte(`[{
				"old_path": "src/lib.rs", "new_path": "src/lib.rs",
				"a_mode": "100644", "b_mode": "100644",
				"diff": "@@ -1 +1 @@\n-old\n+new\n",
				"new_file": false, "renamed_file": false, "deleted_file": false
			}]`))
		default:
			_, _ = w.Write([]byte(`[{
				"old_path": "added.txt", "new_path": "added.txt",
				"b_mode": "100644",
				"diff": "@@ -0,0 +1 @@\n+hi\n",
				"new_file": true
			}]`))
		}
	})
	d := newTestDriver(t, mux)

	diff, err := d.GetDiff(context.Background(), testPR())
	if err != nil {
		t.Fatalf("GetDiff: %v", err)
	}
	want := "diff --git a/src/lib.rs b/src/lib.rs\n" +
		"--- a/src/lib.rs\n+++ b/src/lib.rs\n@@ -1 +1 @@\n-old\n+new\n" +
		"diff --git a/added.txt b/added.txt\n" +
		"new file mode 100644\n" +
		"--- /dev/null\n+++ b/added.txt\n@@ -0,0 +1 @@\n+hi\n"
	if diff != want {
		t.Errorf("diff:\n%s\nwant:\n%s", diff, want)
	}
}

// TestGetDiffMarksTooLargeFiles: GitLab sends a file over its diff limits
// with an empty diff and too_large (or collapsed) set. Synthesized as a bare
// header, it parsed as a file with no changes; it must read as too large.
func TestGetDiffMarksTooLargeFiles(t *testing.T) {
	mux := newFixtureMux(t)
	mux.Handle("GET "+projectPrefix+"/diffs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"old_path": "big.json", "new_path": "big.json", "diff": "", "too_large": true},
			{"old_path": "gen.lock", "new_path": "gen.lock", "diff": "", "collapsed": true},
			{"old_path": "small.go", "new_path": "small.go", "diff": "@@ -1 +1 @@\n-a\n+b\n", "collapsed": true},
			{"old_path": "old.txt", "new_path": "new.txt", "diff": "", "renamed_file": true}
		]`))
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
	want := map[string]bool{"big.json": true, "gen.lock": true, "small.go": false, "new.txt": false}
	if len(files) != len(want) {
		t.Fatalf("parsed %d files, want %d:\n%s", len(files), len(want), diff)
	}
	for _, f := range files {
		if f.IsTooLarge != want[f.DisplayPath()] {
			t.Errorf("%s: IsTooLarge = %v, want %v", f.DisplayPath(), f.IsTooLarge, want[f.DisplayPath()])
		}
	}
	for _, f := range files {
		if f.DisplayPath() == "small.go" && len(f.Hunks) != 1 {
			t.Errorf("a collapsed file that still carries its diff keeps its hunks")
		}
	}
}
