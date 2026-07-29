package azdof

import (
	"context"
	"net/http"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/model"
)

// fileLinesRequest builds a head-side request for src/main.go.
func fileLinesRequest(start, end uint32) forge.FileLinesRequest {
	return forge.FileLinesRequest{
		Repository: testRepo(),
		BaseSHA:    "basesha",
		HeadSHA:    "headsha",
		Path:       "src/main.go",
		Status:     model.StatusModified,
		Side:       forge.FileSideHead,
		StartLine:  start,
		EndLine:    end,
	}
}

// itemsHandler serves one canned blob and records the requested query.
func itemsHandler(t *testing.T, content string, gotQuery *map[string]string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != itemsPath {
			t.Errorf("unexpected call %s %s", r.Method, r.URL)
			return
		}
		if gotQuery != nil {
			q := map[string]string{}
			for k := range r.URL.Query() {
				q[k] = r.URL.Query().Get(k)
			}
			*gotQuery = q
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte(content))
	})
}

func TestFetchFileLinesViaAPI(t *testing.T) {
	var query map[string]string
	d := newTestDriver(t, itemsHandler(t, "l1\nl2\nl3\nl4\n", &query))
	lines, err := d.FetchFileLines(context.Background(), fileLinesRequest(2, 3))
	if err != nil {
		t.Fatalf("FetchFileLines: %v", err)
	}
	if len(lines) != 2 || lines[0].Content != "l2" || lines[1].Content != "l3" {
		t.Fatalf("lines = %+v", lines)
	}
	if lines[0].Origin != model.OriginContext || *lines[0].OldLineno != 2 || *lines[0].NewLineno != 2 {
		t.Errorf("line shape = %+v", lines[0])
	}
	if query["path"] != "/src/main.go" {
		t.Errorf("path = %q, want ADO item path with leading slash", query["path"])
	}
	if query["versionDescriptor.version"] != "headsha" ||
		query["versionDescriptor.versionType"] != "commit" {
		t.Errorf("version descriptor = %v", query)
	}
}

func TestFetchFileLinesBaseSideUsesBaseSHA(t *testing.T) {
	var query map[string]string
	d := newTestDriver(t, itemsHandler(t, "old\n", &query))
	req := fileLinesRequest(1, 1)
	req.Side = forge.FileSideBase
	req.Status = model.StatusDeleted
	if _, err := d.FetchFileLines(context.Background(), req); err != nil {
		t.Fatalf("FetchFileLines: %v", err)
	}
	if query["versionDescriptor.version"] != "basesha" {
		t.Errorf("version = %q, want basesha", query["versionDescriptor.version"])
	}
}

func TestFetchFileLinesInvalidRange(t *testing.T) {
	d := newTestDriver(t, forbidNetwork(t))
	for _, req := range []forge.FileLinesRequest{fileLinesRequest(0, 5), fileLinesRequest(5, 2)} {
		lines, err := d.FetchFileLines(context.Background(), req)
		if err != nil || lines != nil {
			t.Errorf("invalid range must return nil,nil; got %v, %v", lines, err)
		}
	}
}

func TestFetchFileLinesLocalFastPath(t *testing.T) {
	runner := &fakeRunner{out: map[string]string{
		"git cat-file -e headsha:src/main.go": "",
		"git show headsha:src/main.go":        "a\nb\nc\n",
	}}
	d := newLocalDriver(t, forbidNetwork(t), runner)
	lines, err := d.FetchFileLines(context.Background(), fileLinesRequest(1, 2))
	if err != nil {
		t.Fatalf("FetchFileLines: %v", err)
	}
	if len(lines) != 2 || lines[0].Content != "a" {
		t.Fatalf("lines = %+v", lines)
	}
}

func TestFetchFileLinesLocalMissFallsBack(t *testing.T) {
	runner := &fakeRunner{out: map[string]string{}} // cat-file fails
	d := newLocalDriver(t, itemsHandler(t, "x\ny\n", nil), runner)
	lines, err := d.FetchFileLines(context.Background(), fileLinesRequest(1, 2))
	if err != nil {
		t.Fatalf("FetchFileLines: %v", err)
	}
	if len(lines) != 2 || lines[1].Content != "y" {
		t.Fatalf("lines = %+v", lines)
	}
}

func TestFileLineCount(t *testing.T) {
	d := newTestDriver(t, itemsHandler(t, "a\nb\nc", nil))
	count, err := d.FileLineCount(context.Background(), fileLinesRequest(1, 1))
	if err != nil {
		t.Fatalf("FileLineCount: %v", err)
	}
	if count != 3 {
		t.Errorf("count = %d, want 3 (no trailing newline)", count)
	}
}

func TestFileLineCountLocal(t *testing.T) {
	runner := &fakeRunner{out: map[string]string{
		"git cat-file -e headsha:src/main.go": "",
		"git show headsha:src/main.go":        "a\nb\n",
	}}
	d := newLocalDriver(t, forbidNetwork(t), runner)
	count, err := d.FileLineCount(context.Background(), fileLinesRequest(1, 1))
	if err != nil {
		t.Fatalf("FileLineCount: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestFileLineCountError(t *testing.T) {
	d := newTestDriver(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, `{"message": "TF401174"}`)
	}))
	_, err := d.FileLineCount(context.Background(), fileLinesRequest(1, 1))
	wantForgeErr(t, err, forge.ErrorNotFound)
}

func TestCountLines(t *testing.T) {
	tests := []struct {
		content string
		want    int
	}{
		{"", 0},
		{"a", 1},
		{"a\n", 1},
		{"a\nb", 2},
		{"a\nb\n", 2},
	}
	for _, tt := range tests {
		if got := countLines(tt.content); got != tt.want {
			t.Errorf("countLines(%q) = %d, want %d", tt.content, got, tt.want)
		}
	}
}
