package githubf

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
)

func fileReq(side forge.FileSide, start, end uint32) forge.FileLinesRequest {
	return forge.FileLinesRequest{
		Repository: testRepo(),
		BaseSHA:    "basesha",
		HeadSHA:    "headsha",
		Path:       "src/lib.rs",
		Side:       side,
		StartLine:  start,
		EndLine:    end,
	}
}

func TestFetchFileLinesUsesLocalCheckout(t *testing.T) {
	runner := &fakeRunner{out: map[string]string{
		"git cat-file -e headsha:src/lib.rs": "",
		"git show headsha:src/lib.rs":        "one\ntwo\nthree\nfour\n",
	}}
	d := newLocalDriver(t, forbidNetwork(t), runner)

	lines, err := d.FetchFileLines(context.Background(), fileReq(forge.FileSideHead, 2, 3))
	if err != nil {
		t.Fatalf("FetchFileLines: %v", err)
	}
	if len(lines) != 2 || lines[0].Content != "two" || lines[1].Content != "three" {
		t.Fatalf("lines = %+v", lines)
	}
	// Context rows carry the source line number on both sides.
	if lines[0].OldLineno == nil || *lines[0].OldLineno != 2 ||
		lines[0].NewLineno == nil || *lines[0].NewLineno != 2 {
		t.Errorf("line numbers = %+v", lines[0])
	}
}

func TestFetchFileLinesBaseSideUsesBaseSHA(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/octo/hello/contents/src/lib.rs", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("ref"); got != "basesha" {
			t.Errorf("ref = %q, want basesha", got)
		}
		w.Header().Set("Content-Type", "application/json")
		content := base64.StdEncoding.EncodeToString([]byte("alpha\nbeta\n"))
		_, _ = fmt.Fprintf(w, `{"type":"file","encoding":"base64","size":11,"path":"src/lib.rs","sha":"blobsha","content":"%s"}`, content)
	})
	d := newTestDriver(t, mux)

	lines, err := d.FetchFileLines(context.Background(), fileReq(forge.FileSideBase, 1, 2))
	if err != nil {
		t.Fatalf("FetchFileLines: %v", err)
	}
	if len(lines) != 2 || lines[0].Content != "alpha" || lines[1].Content != "beta" {
		t.Fatalf("lines = %+v", lines)
	}
}

func TestFetchFileLinesLargeFileFallsBackToBlobRaw(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/octo/hello/contents/src/lib.rs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// >1 MB files come back with encoding "none" and no content.
		_, _ = fmt.Fprint(w, `{"type":"file","encoding":"none","size":2000000,"path":"src/lib.rs","sha":"bigblobsha","content":""}`)
	})
	mux.HandleFunc("GET /repos/octo/hello/git/blobs/bigblobsha", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "big one\nbig two\n")
	})
	d := newTestDriver(t, mux)

	lines, err := d.FetchFileLines(context.Background(), fileReq(forge.FileSideHead, 1, 2))
	if err != nil {
		t.Fatalf("FetchFileLines: %v", err)
	}
	if len(lines) != 2 || lines[0].Content != "big one" {
		t.Fatalf("lines = %+v", lines)
	}
}

func TestFetchFileLinesRejectsDirectories(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/octo/hello/contents/src/lib.rs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[{"type":"file","name":"a.rs"}]`)
	})
	d := newTestDriver(t, mux)

	_, err := d.FetchFileLines(context.Background(), fileReq(forge.FileSideHead, 1, 2))
	wantForgeErr(t, err, forge.ErrorValidation)
}

func TestFetchFileLinesOutOfRangeReturnsEmpty(t *testing.T) {
	d := newTestDriver(t, forbidNetwork(t))
	for _, bounds := range [][2]uint32{{0, 5}, {5, 2}} {
		lines, err := d.FetchFileLines(context.Background(), fileReq(forge.FileSideHead, bounds[0], bounds[1]))
		if err != nil || lines != nil {
			t.Errorf("bounds %v: lines=%v err=%v, want empty and nil", bounds, lines, err)
		}
	}
}

func TestFetchFileLinesLocalMissFallsBackToAPI(t *testing.T) {
	runner := &fakeRunner{out: map[string]string{}} // cat-file always fails
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/octo/hello/contents/src/lib.rs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		content := base64.StdEncoding.EncodeToString([]byte("remote\n"))
		_, _ = fmt.Fprintf(w, `{"type":"file","encoding":"base64","size":7,"path":"src/lib.rs","sha":"s","content":"%s"}`, content)
	})
	d := newLocalDriver(t, mux, runner)

	lines, err := d.FetchFileLines(context.Background(), fileReq(forge.FileSideHead, 1, 1))
	if err != nil {
		t.Fatalf("FetchFileLines: %v", err)
	}
	if len(lines) != 1 || lines[0].Content != "remote" {
		t.Fatalf("lines = %+v", lines)
	}
}

func TestFileLineCountLocalAndRemote(t *testing.T) {
	runner := &fakeRunner{out: map[string]string{
		"git cat-file -e headsha:src/lib.rs": "",
		"git show headsha:src/lib.rs":        "one\ntwo\nthree",
	}}
	d := newLocalDriver(t, forbidNetwork(t), runner)
	n, err := d.FileLineCount(context.Background(), fileReq(forge.FileSideHead, 1, 1))
	if err != nil || n != 3 {
		t.Errorf("local count = %d, err %v, want 3", n, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/octo/hello/contents/src/lib.rs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		content := base64.StdEncoding.EncodeToString([]byte("a\nb\nc\nd\n"))
		_, _ = fmt.Fprintf(w, `{"type":"file","encoding":"base64","size":8,"path":"src/lib.rs","sha":"s","content":"%s"}`, content)
	})
	remote := newTestDriver(t, mux)
	n, err = remote.FileLineCount(context.Background(), fileReq(forge.FileSideHead, 1, 1))
	if err != nil || n != 4 {
		t.Errorf("remote count = %d, err %v, want 4", n, err)
	}
}

func TestCountLines(t *testing.T) {
	cases := []struct {
		content string
		want    int
	}{
		{"", 0},
		{"a", 1},
		{"a\n", 1},
		{"a\nb", 2},
		{"a\nb\n", 2},
	}
	for _, c := range cases {
		if got := countLines(c.content); got != c.want {
			t.Errorf("countLines(%q) = %d, want %d", c.content, got, c.want)
		}
	}
}
