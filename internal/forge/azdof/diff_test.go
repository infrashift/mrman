package azdof

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs/diffparser"
)

// diffChangesJSON covers every change type the synthesizer handles.
const diffChangesJSON = `{
	"changeEntries": [
		{"changeTrackingId": 1, "changeType": "edit", "item": {"path": "/src/main.go"}},
		{"changeTrackingId": 2, "changeType": "add", "item": {"path": "/added.txt"}},
		{"changeTrackingId": 3, "changeType": "delete", "item": {"path": "/removed.txt"}},
		{"changeTrackingId": 4, "changeType": "rename, edit", "originalPath": "/old/name.txt", "item": {"path": "/new/name.txt"}},
		{"changeTrackingId": 5, "changeType": "edit", "item": {"path": "/img.bin"}},
		{"changeTrackingId": 6, "changeType": "edit", "item": {"path": "/dir", "isFolder": true}}
	],
	"nextSkip": 0,
	"nextTop": 0
}`

// blobFixtures maps "version path" to canned item content.
var blobFixtures = map[string]string{
	"basesha /src/main.go":  "package main\n\nfunc main() {\n\told()\n}\n",
	"headsha /src/main.go":  "package main\n\nfunc main() {\n\tnew()\n}\n",
	"headsha /added.txt":    "fresh\n",
	"basesha /removed.txt":  "stale\n",
	"basesha /old/name.txt": "keep\nold line\n",
	"headsha /new/name.txt": "keep\nnew line\n",
	"basesha /img.bin":      "\x00\x01",
	"headsha /img.bin":      "\x00\x02",
}

// diffHandler serves iteration changes and item content for GetDiff.
func diffHandler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case changesPath:
			writeJSON(w, http.StatusOK, diffChangesJSON)
		case itemsPath:
			version := r.URL.Query().Get("versionDescriptor.version")
			path := r.URL.Query().Get("path")
			if r.URL.Query().Get("versionDescriptor.versionType") != "commit" {
				t.Errorf("items call must pin versionType=commit: %s", r.URL.RawQuery)
			}
			content, ok := blobFixtures[version+" "+path]
			if !ok {
				t.Errorf("unexpected item fetch %q at %q", path, version)
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte(content))
		case iterationsPath:
			writeJSON(w, http.StatusOK, iterationsJSON)
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL)
		}
	})
}

// wantSynthesizedDiff is assembled line-wise because the empty context
// line carries a significant trailing space ("<space>\n") that editors
// strip from raw string literals. "→" stands in for a tab.
var wantSynthesizedDiff = strings.Join([]string{
	"diff --git a/src/main.go b/src/main.go",
	"--- a/src/main.go",
	"+++ b/src/main.go",
	"@@ -1,5 +1,5 @@",
	" package main",
	" ",
	" func main() {",
	"-→old()",
	"+→new()",
	" }",
	wantSynthesizedDiffTail,
}, "\n")

const wantSynthesizedDiffTail = `diff --git a/added.txt b/added.txt
new file mode 100644
--- /dev/null
+++ b/added.txt
@@ -0,0 +1 @@
+fresh
diff --git a/removed.txt b/removed.txt
deleted file mode 100644
--- a/removed.txt
+++ /dev/null
@@ -1 +0,0 @@
-stale
diff --git a/old/name.txt b/new/name.txt
rename from old/name.txt
rename to new/name.txt
--- a/old/name.txt
+++ b/new/name.txt
@@ -1,2 +1,2 @@
 keep
-old line
+new line
diff --git a/img.bin b/img.bin
Binary files a/img.bin and b/img.bin differ
`

func TestGetDiffSynthesizes(t *testing.T) {
	d := newTestDriver(t, diffHandler(t))
	got, err := d.GetDiff(context.Background(), testPR())
	if err != nil {
		t.Fatalf("GetDiff: %v", err)
	}
	// The fixtures use real tabs; the golden marks them with "→".
	want := strings.ReplaceAll(wantSynthesizedDiff, "→", "\t")
	if got != want {
		t.Fatalf("synthesized diff mismatch.\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// TestGetDiffParsesWithDiffparser guards the contract that matters most:
// the app feeds GetDiff output into the git-style diff parser.
func TestGetDiffParsesWithDiffparser(t *testing.T) {
	d := newTestDriver(t, diffHandler(t))
	diff, err := d.GetDiff(context.Background(), testPR())
	if err != nil {
		t.Fatalf("GetDiff: %v", err)
	}
	files, err := diffparser.Parse(diff, diffparser.GitStyle, nil)
	if err != nil {
		t.Fatalf("diffparser.Parse: %v", err)
	}
	if len(files) != 5 {
		t.Fatalf("parsed files = %d, want 5", len(files))
	}
	wantStatus := []model.FileStatus{
		model.StatusModified, model.StatusAdded, model.StatusDeleted,
		model.StatusRenamed, model.StatusModified,
	}
	for i, want := range wantStatus {
		if files[i].Status != want {
			t.Errorf("file %d status = %q, want %q", i, files[i].Status, want)
		}
	}
	if !files[4].IsBinary {
		t.Error("binary stub must parse as binary")
	}
	if old, ok := files[3].OldPath, files[3].NewPath; old == nil || ok == nil ||
		*old != "old/name.txt" || *ok != "new/name.txt" {
		t.Errorf("rename paths = %v -> %v", files[3].OldPath, files[3].NewPath)
	}
}

// TestGetDiffWithoutPayload falls back to fetching the latest iteration.
func TestGetDiffWithoutPayload(t *testing.T) {
	d := newTestDriver(t, diffHandler(t))
	pr := testPR()
	pr.ForgePayload = nil
	diff, err := d.GetDiff(context.Background(), pr)
	if err != nil {
		t.Fatalf("GetDiff: %v", err)
	}
	if !strings.Contains(diff, "diff --git a/src/main.go b/src/main.go") {
		t.Errorf("diff missing files:\n%s", diff)
	}
}

func TestGetDiffNoIterations(t *testing.T) {
	d := newTestDriver(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == iterationsPath {
			writeJSON(w, http.StatusOK, `{"count":0,"value":[]}`)
			return
		}
		t.Errorf("unexpected call %s %s", r.Method, r.URL)
	}))
	pr := testPR()
	pr.ForgePayload = nil
	_, err := d.GetDiff(context.Background(), pr)
	wantForgeErr(t, err, forge.ErrorValidation)
}

// TestGetDiffLocalFastPath answers from the checkout when both SHAs are
// present, without any API call.
func TestGetDiffLocalFastPath(t *testing.T) {
	runner := &fakeRunner{out: map[string]string{
		"git cat-file -e basesha":   "",
		"git cat-file -e headsha":   "",
		"git diff basesha..headsha": "diff --git a/x b/x\n",
	}}
	d := newLocalDriver(t, forbidNetwork(t), runner)
	diff, err := d.GetDiff(context.Background(), testPR())
	if err != nil {
		t.Fatalf("GetDiff: %v", err)
	}
	if diff != "diff --git a/x b/x\n" {
		t.Errorf("diff = %q", diff)
	}
}

// TestGetDiffLocalFallsBack degrades to the API when a SHA is absent
// locally.
func TestGetDiffLocalFallsBack(t *testing.T) {
	runner := &fakeRunner{out: map[string]string{
		"git cat-file -e basesha": "",
		// headsha missing → API path
	}}
	d := newLocalDriver(t, diffHandler(t), runner)
	diff, err := d.GetDiff(context.Background(), testPR())
	if err != nil {
		t.Fatalf("GetDiff: %v", err)
	}
	if !strings.Contains(diff, "+fresh") {
		t.Errorf("expected synthesized diff, got:\n%s", diff)
	}
}

func TestGetDiffBlobError(t *testing.T) {
	d := newTestDriver(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case changesPath:
			writeJSON(w, http.StatusOK, diffChangesJSON)
		case itemsPath:
			writeJSON(w, http.StatusNotFound, `{"message": "TF401174: item not found"}`)
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL)
		}
	}))
	_, err := d.GetDiff(context.Background(), testPR())
	wantForgeErr(t, err, forge.ErrorNotFound)
}

// TestGetDiffFetchesFilesConcurrentlyInOrder: blob pairs used to be fetched
// one file after another. They now overlap, bounded, and the synthesized
// diff still lists files in the iteration's order.
func TestGetDiffFetchesFilesConcurrentlyInOrder(t *testing.T) {
	const n = 12
	var entries []string
	for i := range n {
		entries = append(entries, fmt.Sprintf(`{"changeTrackingId": %d, "changeType": "add", "item": {"path": "/f%02d.txt"}}`, i+1, i))
	}
	changes := `{"changeEntries": [` + strings.Join(entries, ",") + `], "nextSkip": 0}`
	var inFlight, peak atomic.Int32
	d := newTestDriver(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case changesPath:
			writeJSON(w, http.StatusOK, changes)
		case itemsPath:
			now := inFlight.Add(1)
			for {
				if old := peak.Load(); now <= old || peak.CompareAndSwap(old, now) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			inFlight.Add(-1)
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte(r.URL.Query().Get("path") + "\n"))
		case iterationsPath:
			writeJSON(w, http.StatusOK, iterationsJSON)
		default:
			t.Errorf("unexpected call %s %s", r.Method, r.URL)
		}
	}))
	diff, err := d.GetDiff(context.Background(), testPR())
	if err != nil {
		t.Fatalf("GetDiff: %v", err)
	}
	if p := peak.Load(); p < 2 || p > blobFetchConcurrency {
		t.Errorf("peak concurrent fetches = %d, want 2..%d", p, blobFetchConcurrency)
	}
	last := -1
	for i := range n {
		at := strings.Index(diff, fmt.Sprintf("diff --git a/f%02d.txt", i))
		if at < 0 || at < last {
			t.Fatalf("file f%02d.txt missing or out of order in:\n%s", i, diff)
		}
		last = at
	}
}
