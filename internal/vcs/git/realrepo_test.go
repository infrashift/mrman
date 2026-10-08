package git

import (
	"os"
	"os/exec"
	"path/filepath"
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

// TestBinaryChangeIsMarkedWithoutBinaryPatch: git diff runs without
// --binary, which made git emit the base85 contents of every changed binary
// file only for the parser to throw them away. Git still reports "Binary
// files ... differ", and that marks the file.
func TestBinaryChangeIsMarkedWithoutBinaryPatch(t *testing.T) {
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
	writeFile(t, dir, "blob.bin", "\x00\x01binary\x00")
	writeFile(t, dir, "a.txt", "one\n")
	git("add", ".")
	git("commit", "-q", "-m", "first")
	writeFile(t, dir, "blob.bin", "\x00\x02binary\x00")
	writeFile(t, dir, "a.txt", "two\n")

	backend, err := Discover(dir, vcs.WhitespaceNormal, vcs.SystemRunner{})
	if err != nil {
		t.Fatal(err)
	}
	files, err := backend.WorkingTreeDiff(nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, f := range files {
		switch f.DisplayPath() {
		case "blob.bin":
			seen++
			if !f.IsBinary {
				t.Error("blob.bin is not marked binary")
			}
		case "a.txt":
			seen++
			if len(f.Hunks) != 1 {
				t.Errorf("a.txt has %d hunks, want 1", len(f.Hunks))
			}
		}
	}
	if seen != 2 {
		t.Fatalf("parsed files %v, want blob.bin and a.txt", files)
	}
}
