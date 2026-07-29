package diffparser

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

// TestParseRealGitOutput feeds genuine `git diff` output through the parser
// end-to-end. Skipped when git is unavailable.
func TestParseRealGitOutput(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	oldFile := filepath.Join(dir, "f1.txt")
	newFile := filepath.Join(dir, "f2.txt")
	if err := os.WriteFile(oldFile, []byte("line one\nline two\nline three\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newFile, []byte("line one\nline 2\nline three\nline four\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// --no-index exits 1 when files differ; that's expected.
	out, _ := exec.Command("git", "diff", "--no-index", oldFile, newFile).Output()
	if len(out) == 0 {
		t.Fatal("git produced no diff")
	}

	files, err := Parse(string(out), GitStyle, nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files", len(files))
	}
	f := files[0]
	adds, dels := f.Stat()
	if adds != 2 || dels != 1 {
		t.Errorf("stat = +%d -%d, want +2 -1", adds, dels)
	}
	if len(f.Hunks) != 1 || f.Status != model.StatusRenamed && f.Status != model.StatusModified {
		t.Errorf("hunks=%d status=%s", len(f.Hunks), f.Status)
	}
	if f.ContentHash == 0 {
		t.Error("content hash must be computed")
	}
}
