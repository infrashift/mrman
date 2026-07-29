package filebackend

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/infrashift/mrman/internal/errs"
)

// cannedRunner responds to "name arg1 arg2 ..." keys with canned stdout, and
// errors for any other command.
type cannedRunner struct {
	responses map[string]string
	failAll   bool
	calls     []string
}

func (r *cannedRunner) Run(_ string, name string, args ...string) ([]byte, []byte, error) {
	key := name
	for _, a := range args {
		key += " " + a
	}
	r.calls = append(r.calls, key)
	if r.failAll {
		return nil, []byte("fatal: not a git repository"), errors.New("exit status 128")
	}
	out, ok := r.responses[key]
	if !ok {
		return nil, []byte("unknown command"), errors.New("exit status 1")
	}
	return []byte(out), nil, nil
}

func TestCollectTrackedPathsErrorsWhenNotAGitRepo(t *testing.T) {
	run := &cannedRunner{failAll: true}
	if _, err := CollectTrackedPaths(t.TempDir(), run); !errors.Is(err, errs.ErrNotARepository) {
		t.Fatalf("expected ErrNotARepository, got %v", err)
	}
}

func TestCollectTrackedPathsErrorsWhenRepoHasNoTrackedFiles(t *testing.T) {
	run := &cannedRunner{responses: map[string]string{"git ls-files -z": ""}}
	if _, err := CollectTrackedPaths(t.TempDir(), run); !errors.Is(err, errs.ErrNoChanges) {
		t.Fatalf("expected ErrNoChanges, got %v", err)
	}
}

func TestCollectTrackedPathsListsOnlyTrackedFiles(t *testing.T) {
	dir := canonTempDir(t)
	mustWrite(t, filepath.Join(dir, "kept.txt"), "hello\n")
	mustWrite(t, filepath.Join(dir, ".gitignore"), "ignored.txt\n")
	mustWrite(t, filepath.Join(dir, "ignored.txt"), "skip me\n")
	mustWrite(t, filepath.Join(dir, "untracked.txt"), "untracked\n")

	// git only reports tracked files; ignored/untracked never appear.
	run := &cannedRunner{responses: map[string]string{
		"git ls-files -z": ".gitignore\x00kept.txt\x00",
	}}
	paths, err := CollectTrackedPaths(dir, run)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, ".gitignore"), filepath.Join(dir, "kept.txt")}
	if len(paths) != 2 || paths[0] != want[0] || paths[1] != want[1] {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
}

func TestCollectTrackedPathsDropsDeletedButTrackedEntries(t *testing.T) {
	dir := canonTempDir(t)
	mustWrite(t, filepath.Join(dir, "kept.txt"), "k\n")
	// removed.txt is tracked in the index but absent from disk.
	run := &cannedRunner{responses: map[string]string{
		"git ls-files -z": "kept.txt\x00removed.txt\x00",
	}}
	paths, err := CollectTrackedPaths(dir, run)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != filepath.Join(dir, "kept.txt") {
		t.Fatalf("paths = %v", paths)
	}
}

func TestHeadShortSHA(t *testing.T) {
	dir := t.TempDir()

	ok := &cannedRunner{responses: map[string]string{"git rev-parse --short HEAD": "abc1234\n"}}
	if got := HeadShortSHA(dir, ok); got != "abc1234" {
		t.Fatalf("got %q", got)
	}

	failing := &cannedRunner{failAll: true}
	if got := HeadShortSHA(dir, failing); got != "none" {
		t.Fatalf("failure sentinel = %q, want none", got)
	}

	empty := &cannedRunner{responses: map[string]string{"git rev-parse --short HEAD": "\n"}}
	if got := HeadShortSHA(dir, empty); got != "none" {
		t.Fatalf("empty sentinel = %q, want none", got)
	}
}
