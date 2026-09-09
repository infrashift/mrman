//go:build unix

package patchbackend

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/infrashift/mrman/internal/patch"
)

// TestRejectsNonRegularFiles covers `--patch <(git format-patch --stdout …)`,
// which reads like it should work and cannot: the backend re-reads the
// artifact on every reload — that is how :e works — and a pipe can only be
// read once. Without this the second read comes back empty and a perfectly
// good patch is reported as unparseable.
func TestRejectsNonRegularFiles(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe.patch")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("cannot create a fifo here: %v", err)
	}

	_, err := New(fifo, patch.Options{})
	if err == nil {
		t.Fatal("a pipe should be refused, not half-read")
	}
	if !strings.Contains(err.Error(), "regular file") {
		t.Errorf("err = %q, want it to name the problem", err)
	}
	if !strings.Contains(err.Error(), "redirect to a file") {
		t.Errorf("err = %q, want it to say what to do instead", err)
	}
}
