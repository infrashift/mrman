package git

import (
	"errors"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs"
)

func TestChangeStatusTrackedChanges(t *testing.T) {
	backend, runner := newTestBackend(t, "/repo", map[string]response{
		"git diff --quiet --cached --": {err: exitError(1)},
		"git diff --quiet --":          {err: exitError(1)},
	})

	status, err := backend.ChangeStatus()
	if err != nil {
		t.Fatalf("ChangeStatus failed: %v", err)
	}

	if status != (vcs.ChangeStatus{Staged: true, Unstaged: true}) {
		t.Errorf("status = %+v", status)
	}
	if runner.called("git ls-files --others --exclude-standard -z --directory") {
		t.Error("untracked scan should be skipped when tracked unstaged changes exist")
	}
}

func TestChangeStatusUntrackedOnly(t *testing.T) {
	backend, _ := newTestBackend(t, "/repo", map[string]response{
		"git diff --quiet --cached --":                            {},
		"git diff --quiet --":                                     {},
		"git sparse-checkout list":                                {err: exitError(1)},
		"git ls-files --others --exclude-standard -z --directory": {stdout: "u.txt\x00"},
	})

	status, err := backend.ChangeStatus()
	if err != nil {
		t.Fatalf("ChangeStatus failed: %v", err)
	}

	if status != (vcs.ChangeStatus{Staged: false, Unstaged: true}) {
		t.Errorf("status = %+v", status)
	}
}

func TestChangeStatusClean(t *testing.T) {
	backend, _ := newTestBackend(t, "/repo", map[string]response{
		"git diff --quiet --cached --":                            {},
		"git diff --quiet --":                                     {},
		"git sparse-checkout list":                                {err: exitError(1)},
		"git ls-files --others --exclude-standard -z --directory": {},
	})

	status, err := backend.ChangeStatus()
	if err != nil {
		t.Fatalf("ChangeStatus failed: %v", err)
	}

	if status != (vcs.ChangeStatus{}) {
		t.Errorf("status = %+v", status)
	}
}

func TestChangeStatusSparseConePathspecs(t *testing.T) {
	backend, runner := newTestBackend(t, "/repo", map[string]response{
		"git diff --quiet --cached --": {},
		"git diff --quiet --":          {},
		"git sparse-checkout list":     {stdout: "/keep/\n"},
		"git ls-files --others --exclude-standard -z --directory -- keep": {},
	})

	status, err := backend.ChangeStatus()
	if err != nil {
		t.Fatalf("ChangeStatus failed: %v", err)
	}

	if status.Unstaged {
		t.Errorf("status = %+v", status)
	}
	if !runner.called("git ls-files --others --exclude-standard -z --directory -- keep") {
		t.Error("untracked scan should be narrowed to the sparse cone")
	}
}

func TestChangeStatusProbeError(t *testing.T) {
	backend, _ := newTestBackend(t, "/repo", map[string]response{
		"git diff --quiet --cached --": {stderr: "fatal: broken index", err: exitError(129)},
	})

	_, err := backend.ChangeStatus()

	var vcsErr *errs.VcsCommand
	if !errors.As(err, &vcsErr) || vcsErr.Detail != "fatal: broken index" {
		t.Fatalf("err = %v", err)
	}
}

func TestListChangedPathsStaged(t *testing.T) {
	backend, _ := newTestBackend(t, "/repo", map[string]response{
		"git diff --cached --name-only -z --": {stdout: "a.txt\x00dir/b.txt\x00"},
	})

	paths, err := backend.ListChangedPaths(vcs.ChangeStaged)
	if err != nil {
		t.Fatalf("ListChangedPaths failed: %v", err)
	}

	if len(paths) != 2 || paths[0] != "a.txt" || paths[1] != "dir/b.txt" {
		t.Errorf("paths = %v", paths)
	}
}

func TestListChangedPathsUnstagedIncludesUntracked(t *testing.T) {
	backend, _ := newTestBackend(t, "/repo", map[string]response{
		"git diff --name-only -z --":                               {stdout: "mod.txt\x00"},
		"git sparse-checkout list":                                 {stdout: "/keep/\n/also\n"},
		"git ls-files --others --exclude-standard -z -- keep also": {stdout: "keep/new.txt\x00"},
	})

	paths, err := backend.ListChangedPaths(vcs.ChangeUnstaged)
	if err != nil {
		t.Fatalf("ListChangedPaths failed: %v", err)
	}

	if len(paths) != 2 || paths[0] != "mod.txt" || paths[1] != "keep/new.txt" {
		t.Errorf("paths = %v", paths)
	}
}

func TestSparsePathspecsComplexPatternFallsBackToFullScan(t *testing.T) {
	backend, _ := newTestBackend(t, "/repo", map[string]response{
		"git sparse-checkout list": {stdout: "/keep/\n!*.log\n"},
	})

	pathspecs, err := backend.sparseCheckoutUntrackedPathspecs()
	if err != nil {
		t.Fatalf("sparseCheckoutUntrackedPathspecs failed: %v", err)
	}

	if pathspecs != nil {
		t.Errorf("pathspecs = %v, want nil for complex patterns", pathspecs)
	}
}

func TestSparsePathspecsSpawnFailure(t *testing.T) {
	backend, _ := newTestBackend(t, "/repo", map[string]response{
		"git sparse-checkout list": {err: errors.New("no git binary")},
	})

	_, err := backend.sparseCheckoutUntrackedPathspecs()

	var vcsErr *errs.VcsCommand
	if !errors.As(err, &vcsErr) {
		t.Fatalf("err = %v, want *errs.VcsCommand", err)
	}
}

func TestFetchContextLinesFromRef(t *testing.T) {
	backend, _ := newTestBackend(t, "/repo", map[string]response{
		"git show abc:file.txt": {stdout: "one\ntwo\nthree\nfour\n"},
	})
	ref := "abc"

	lines, err := backend.FetchContextLines("file.txt", model.StatusModified, &ref, 2, 3)
	if err != nil {
		t.Fatalf("FetchContextLines failed: %v", err)
	}

	if len(lines) != 2 || lines[0].Content != "two" || lines[1].Content != "three" {
		t.Fatalf("lines = %+v", lines)
	}
	if lines[0].Origin != model.OriginContext {
		t.Errorf("origin = %v", lines[0].Origin)
	}
	if *lines[0].OldLineno != 2 || *lines[0].NewLineno != 2 {
		t.Errorf("linenos = %v %v", lines[0].OldLineno, lines[0].NewLineno)
	}
}

func TestFetchContextLinesGuards(t *testing.T) {
	backend, runner := newTestBackend(t, "/repo", nil)

	if lines, err := backend.FetchContextLines("f", model.StatusModified, nil, 0, 5); err != nil || lines != nil {
		t.Errorf("start 0: %v %v", lines, err)
	}
	if lines, err := backend.FetchContextLines("f", model.StatusModified, nil, 4, 2); err != nil || lines != nil {
		t.Errorf("start > end: %v %v", lines, err)
	}
	if len(runner.calls) != 0 {
		t.Errorf("guards should not invoke git: %v", runner.calls)
	}
}

func TestFetchContextLinesDeletedReadsHead(t *testing.T) {
	backend, _ := newTestBackend(t, "/repo", map[string]response{
		"git show HEAD:gone.txt": {stdout: "a\nb\n"},
	})

	lines, err := backend.FetchContextLines("gone.txt", model.StatusDeleted, nil, 1, 2)
	if err != nil {
		t.Fatalf("FetchContextLines failed: %v", err)
	}
	if len(lines) != 2 || lines[0].Content != "a" {
		t.Fatalf("lines = %+v", lines)
	}
}

func TestFetchContextLinesDeletedMissingFromHead(t *testing.T) {
	backend, _ := newTestBackend(t, "/repo", map[string]response{
		"git show HEAD:gone.txt": {stderr: "fatal: path does not exist", err: exitError(128)},
	})

	_, err := backend.FetchContextLines("gone.txt", model.StatusDeleted, nil, 1, 2)

	var vcsErr *errs.VcsCommand
	if !errors.As(err, &vcsErr) || vcsErr.Detail != "failed to read deleted file from HEAD" {
		t.Fatalf("err = %v", err)
	}
}

func TestFetchContextLinesRefReadFailure(t *testing.T) {
	backend, _ := newTestBackend(t, "/repo", map[string]response{
		"git show abc:file.txt": {stderr: "fatal: bad object", err: exitError(128)},
	})
	ref := "abc"

	_, err := backend.FetchContextLines("file.txt", model.StatusModified, &ref, 1, 2)

	var vcsErr *errs.VcsCommand
	if !errors.As(err, &vcsErr) || vcsErr.Detail != "failed to read file.txt at abc" {
		t.Fatalf("err = %v", err)
	}
}

func TestFetchContextLinesFromWorkdir(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "wd.txt", "one\ntwo\n")
	backend, _ := newTestBackend(t, root, nil)

	lines, err := backend.FetchContextLines("wd.txt", model.StatusModified, nil, 1, 5)
	if err != nil {
		t.Fatalf("FetchContextLines failed: %v", err)
	}
	if len(lines) != 2 || lines[1].Content != "two" {
		t.Fatalf("lines = %+v", lines)
	}
}

func TestFileLineCount(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "three.txt", "a\nb\nc\n")
	backend, _ := newTestBackend(t, root, map[string]response{
		"git show abc:ref.txt": {stdout: "x\ny"},
	})

	count, err := backend.FileLineCount("three.txt", model.StatusModified, nil)
	if err != nil || count != 3 {
		t.Errorf("workdir count = %d, %v", count, err)
	}

	ref := "abc"
	count, err = backend.FileLineCount("ref.txt", model.StatusModified, &ref)
	if err != nil || count != 2 {
		t.Errorf("ref count = %d, %v", count, err)
	}
}

func TestFileLineCountMissingWorkdirFile(t *testing.T) {
	backend, _ := newTestBackend(t, t.TempDir(), nil)

	_, err := backend.FileLineCount("missing.txt", model.StatusModified, nil)

	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestCountLines(t *testing.T) {
	tests := []struct {
		content string
		want    uint32
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

func TestStageFile(t *testing.T) {
	backend, runner := newTestBackend(t, "/repo", map[string]response{
		"git add -- dir/file.txt": {},
	})

	if err := backend.StageFile("dir/file.txt"); err != nil {
		t.Fatalf("StageFile failed: %v", err)
	}
	if !runner.called("git add -- dir/file.txt") {
		t.Error("git add not invoked")
	}
}

func TestStageFileFailure(t *testing.T) {
	backend, _ := newTestBackend(t, "/repo", map[string]response{
		"git add -- nope.txt": {stderr: "fatal: pathspec 'nope.txt' did not match any files\n", err: exitError(128)},
	})

	err := backend.StageFile("nope.txt")

	var vcsErr *errs.VcsCommand
	if !errors.As(err, &vcsErr) || vcsErr.Detail != "fatal: pathspec 'nope.txt' did not match any files" {
		t.Fatalf("err = %v", err)
	}
}

func TestStageFileSpawnFailure(t *testing.T) {
	backend, _ := newTestBackend(t, "/repo", map[string]response{
		"git add -- f.txt": {err: errors.New("no git binary")},
	})

	err := backend.StageFile("f.txt")

	var vcsErr *errs.VcsCommand
	if !errors.As(err, &vcsErr) || !strings.Contains(vcsErr.Detail, "Failed to run git") {
		t.Fatalf("err = %v", err)
	}
}

func TestSplitNulPaths(t *testing.T) {
	paths := splitNulPaths("a.txt\x00\x00b.txt\x00")
	if len(paths) != 2 || paths[0] != "a.txt" || paths[1] != "b.txt" {
		t.Errorf("paths = %v", paths)
	}
	if got := splitNulPaths(""); got != nil {
		t.Errorf("empty = %v", got)
	}
}
