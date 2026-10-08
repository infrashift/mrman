package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs"
)

// TestBackendAgainstRealRepository exercises the backend against an actual
// git repository to prove the constructed argv works outside fakes.
// Skipped when git is unavailable.
func TestBackendAgainstRealRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git("init", "-q", "-b", "main")
	write("a.go", "package a\n\nvar X = 1\n")
	git("add", ".")
	git("commit", "-q", "-m", "first commit")
	write("a.go", "package a\n\nvar X = 2\nvar Y = 3\n")
	write("untracked.txt", "new file\n")

	backend, err := Discover(dir, vcs.WhitespaceNormal, vcs.SystemRunner{})
	if err != nil {
		t.Fatal(err)
	}
	info := backend.Info()
	if info.Type != vcs.TypeGit || info.BranchName == nil || *info.BranchName != "main" {
		t.Fatalf("info = %+v", info)
	}

	files, err := backend.WorkingTreeDiff(nil)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]*model.DiffFile{}
	for i := range files {
		byPath[files[i].DisplayPath()] = &files[i]
	}
	if f := byPath["a.go"]; f == nil || f.Status != model.StatusModified {
		t.Fatalf("a.go = %+v", f)
	}
	if f := byPath["untracked.txt"]; f == nil || f.Status != model.StatusAdded {
		t.Fatalf("untracked.txt = %+v", f)
	}

	status, err := backend.ChangeStatus()
	if err != nil || !status.Unstaged {
		t.Fatalf("status = %+v err=%v", status, err)
	}

	commits, err := backend.RecentCommits(0, 10)
	if err != nil || len(commits) != 1 || commits[0].Summary != "first commit" {
		t.Fatalf("commits = %+v err=%v", commits, err)
	}

	lines, err := backend.FetchContextLines("a.go", model.StatusModified, nil, 1, 2)
	if err != nil || len(lines) != 2 || lines[0].Content != "package a" {
		t.Fatalf("context = %+v err=%v", lines, err)
	}

	if err := backend.StageFile("a.go"); err != nil {
		t.Fatal(err)
	}
	status, err = backend.ChangeStatus()
	if err != nil || !status.Staged {
		t.Fatalf("post-stage status = %+v err=%v", status, err)
	}

	staged, err := backend.StagedDiff(nil)
	if err != nil || len(staged) != 1 {
		t.Fatalf("staged = %d files err=%v", len(staged), err)
	}
}

// TestBackendUnquotesUnusualPaths runs real git over files whose names git
// quotes or decorates in its diff headers, under a config that would also
// colour the output. Each file must come back under its real name, and its
// context lines must be readable through that name.
func TestBackendUnquotesUnusualPaths(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	names := []string{"café.go", "sp ace.txt", `quo"te.txt`}
	git("init", "-q", "-b", "main")
	git("config", "color.ui", "always")
	for _, name := range names {
		writeFile(t, dir, name, "one\ntwo\n")
	}
	git("add", ".")
	git("commit", "-q", "-m", "first")
	for _, name := range names {
		writeFile(t, dir, name, "one\nthree\n")
	}

	backend, err := Discover(dir, vcs.WhitespaceNormal, vcs.SystemRunner{})
	if err != nil {
		t.Fatal(err)
	}
	files, err := backend.WorkingTreeDiff(nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range files {
		got[f.DisplayPath()] = true
		if len(f.Hunks) != 1 {
			t.Errorf("%q: %d hunks, want 1 (colour codes in the output?)", f.DisplayPath(), len(f.Hunks))
		}
	}
	for _, name := range names {
		if !got[name] {
			t.Errorf("missing %q; parsed paths: %v", name, got)
			continue
		}
		lines, err := backend.FetchContextLines(name, model.StatusModified, nil, 1, 1)
		if err != nil || len(lines) != 1 || lines[0].Content != "one" {
			t.Errorf("context for %q = %+v, err %v", name, lines, err)
		}
	}
}

// TestIndexRefReadsTheStagedVersion: in a staged-only review the new side of
// the diff is the index. With further unstaged edits in the file, the
// working tree is the wrong snapshot to read context from.
func TestIndexRefReadsTheStagedVersion(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	writeFile(t, dir, "f.txt", "one\ntwo\nthree\n")
	writeFile(t, dir, "gone.txt", "kept\n")
	git("add", ".")
	git("commit", "-q", "-m", "first")
	writeFile(t, dir, "f.txt", "one\nTWO\nthree\n")
	git("add", "f.txt")
	git("rm", "-q", "gone.txt")
	writeFile(t, dir, "f.txt", "unstaged\none\nTWO\nthree\n")

	backend, err := Discover(dir, vcs.WhitespaceNormal, vcs.SystemRunner{})
	if err != nil {
		t.Fatal(err)
	}
	index := vcs.IndexRef
	lines, err := backend.FetchContextLines("f.txt", model.StatusModified, &index, 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, l := range lines {
		got = append(got, l.Content)
	}
	if strings.Join(got, ",") != "one,TWO,three" {
		t.Errorf("index context = %v, want the staged lines", got)
	}
	if n, err := backend.FileLineCount("f.txt", model.StatusModified, &index); err != nil || n != 3 {
		t.Errorf("index line count = %d, %v; want 3", n, err)
	}
	// A file deleted in the index has only its old side, read from HEAD.
	if n, err := backend.FileLineCount("gone.txt", model.StatusDeleted, &index); err != nil || n != 1 {
		t.Errorf("staged-deleted line count = %d, %v; want 1", n, err)
	}
}
