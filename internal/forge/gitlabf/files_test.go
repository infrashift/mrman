package gitlabf

import (
	"context"
	"net/http"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
)

func linesRequest() forge.FileLinesRequest {
	return forge.FileLinesRequest{
		Repository: testRepo(),
		BaseSHA:    "basesha1",
		HeadSHA:    "headsha1",
		Path:       "src/main.go",
		Side:       forge.FileSideHead,
		StartLine:  2,
		EndLine:    3,
	}
}

func TestFetchFileLinesViaAPI(t *testing.T) {
	mux := newFixtureMux(t)
	var gotRef string
	mux.Handle("GET /api/v4/projects/group%2Fsub%2Frepo/repository/files/src%2Fmain%2Ego/raw",
		func(w http.ResponseWriter, r *http.Request) {
			gotRef = r.URL.Query().Get("ref")
			_, _ = w.Write([]byte("one\ntwo\nthree\nfour\n"))
		})
	d := newTestDriver(t, mux)

	lines, err := d.FetchFileLines(context.Background(), linesRequest())
	if err != nil {
		t.Fatalf("FetchFileLines: %v", err)
	}
	if gotRef != "headsha1" {
		t.Errorf("ref = %q, want headsha1", gotRef)
	}
	if len(lines) != 2 || lines[0].Content != "two" || lines[1].Content != "three" {
		t.Errorf("lines = %+v", lines)
	}
}

func TestFetchFileLinesOutOfRangeReturnsEmpty(t *testing.T) {
	d := newTestDriver(t, forbidNetwork(t))
	req := linesRequest()
	req.StartLine = 0
	lines, err := d.FetchFileLines(context.Background(), req)
	if err != nil || lines != nil {
		t.Errorf("lines = %v err = %v, want nil/nil", lines, err)
	}
}

func TestFetchFileLinesLocalFastPath(t *testing.T) {
	runner := &fakeRunner{out: map[string]string{
		"git cat-file -e headsha1:src/main.go": "",
		"git show headsha1:src/main.go":        "one\ntwo\nthree\n",
	}}
	d := newLocalDriver(t, forbidNetwork(t), runner)

	lines, err := d.FetchFileLines(context.Background(), linesRequest())
	if err != nil {
		t.Fatalf("FetchFileLines: %v", err)
	}
	if len(lines) != 2 || lines[0].Content != "two" {
		t.Errorf("lines = %+v", lines)
	}
}

func TestFileLineCountBaseSide(t *testing.T) {
	mux := newFixtureMux(t)
	var gotRef string
	mux.Handle("GET /api/v4/projects/group%2Fsub%2Frepo/repository/files/src%2Fmain%2Ego/raw",
		func(w http.ResponseWriter, r *http.Request) {
			gotRef = r.URL.Query().Get("ref")
			_, _ = w.Write([]byte("one\ntwo\nthree"))
		})
	d := newTestDriver(t, mux)

	req := linesRequest()
	req.Side = forge.FileSideBase
	count, err := d.FileLineCount(context.Background(), req)
	if err != nil {
		t.Fatalf("FileLineCount: %v", err)
	}
	if gotRef != "basesha1" {
		t.Errorf("ref = %q, want basesha1", gotRef)
	}
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
}

func TestFileLineCountNotFound(t *testing.T) {
	mux := newFixtureMux(t)
	mux.Handle("GET /api/v4/projects/group%2Fsub%2Frepo/repository/files/src%2Fmain%2Ego/raw",
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message": "404 File Not Found"}`))
		})
	d := newTestDriver(t, mux)

	_, err := d.FileLineCount(context.Background(), linesRequest())
	wantForgeErr(t, err, forge.ErrorNotFound)
}

func TestCountLines(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"one", 1},
		{"one\n", 1},
		{"one\ntwo", 2},
		{"one\ntwo\n", 2},
	}
	for _, c := range cases {
		if got := countLines(c.in); got != c.want {
			t.Errorf("countLines(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
