package ui

import (
	"os"
	"os/exec"
	"testing"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/cli"
	"github.com/infrashift/mrman/internal/config"
)

// gitRepo makes a repository with two commits and an unstaged edit.
func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(dir+"/src", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dir+"/"+name, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q", "-b", "main")
	write("src/a.go", "package a\n\nvar A = 1\n")
	write("docs.md", "# docs\n")
	git("add", ".")
	git("commit", "-q", "-m", "first")
	write("src/a.go", "package a\n\nvar A = 2\n")
	git("commit", "-q", "-am", "second")
	write("docs.md", "# docs\n\nmore\n")
	return dir
}

// TestLocalStartupLoadsTheRequestedDiff drives the part of Run that does
// not need a terminal: detect the repository, load the diff the flags ask
// for, and apply --path. No test reached it before.
func TestLocalStartupLoadsTheRequestedDiff(t *testing.T) {
	dir := gitRepo(t)
	cfg := config.Default()
	cases := []struct {
		name  string
		opts  cli.TuiOptions
		kind  app.DiffSourceKind
		paths []string
		strip int
	}{
		{"working tree", cli.TuiOptions{WorkingTree: true}, app.DiffSourceWorkingTree, []string{"docs.md"}, 0},
		{"commit range", cli.TuiOptions{Revisions: "HEAD~1..HEAD"}, app.DiffSourceCommitRange, []string{"src/a.go"}, 1},
		{"path filter", cli.TuiOptions{Revisions: "HEAD~1..HEAD", Path: "docs"}, app.DiffSourceCommitRange, nil, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend, patchBackend, diffBackend, err := openLocalBackend(tc.opts, cfg, dir)
			if err != nil {
				t.Fatal(err)
			}
			files, source, commits, err := loadInitialDiff(tc.opts, backend, patchBackend, diffBackend, false, nil)
			if err != nil {
				t.Fatal(err)
			}
			if source.Kind != tc.kind || len(commits) != tc.strip {
				t.Errorf("source %v with %d strip commits, want %v with %d", source.Kind, len(commits), tc.kind, tc.strip)
			}
			files, err = filterLocalFiles(files, backend.Info().RootPath, tc.opts.Path)
			if tc.paths == nil {
				if err == nil {
					t.Errorf("a path filter that leaves nothing must refuse the review, kept %d files", len(files))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, f := range files {
				got = append(got, f.DisplayPath())
			}
			if len(got) != len(tc.paths) || got[0] != tc.paths[0] {
				t.Errorf("files = %v, want %v", got, tc.paths)
			}
		})
	}
}

func TestLocalStartupOutsideARepositoryExplainsItself(t *testing.T) {
	_, _, _, err := openLocalBackend(cli.TuiOptions{WorkingTree: true}, config.Default(), t.TempDir())
	if err == nil {
		t.Fatal("a directory with no repository must be refused")
	}
}
