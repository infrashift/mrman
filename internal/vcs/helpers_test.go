package vcs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/syntax"
)

func TestTabify(t *testing.T) {
	if got := Tabify("a\tb\tc"); got != "a    b    c" {
		t.Fatalf("got %q", got)
	}
}

func TestSliceContextLines(t *testing.T) {
	content := "one\ttab\ntwo\nthree\n"
	lines := SliceContextLines(content, 1, 3)
	if len(lines) != 3 {
		t.Fatalf("got %d lines", len(lines))
	}
	if lines[0].Content != "one    tab" || lines[2].Content != "three" {
		t.Fatalf("contents wrong: %+v", lines)
	}
	if *lines[1].OldLineno != 2 || *lines[1].NewLineno != 2 || lines[1].Origin != model.OriginContext {
		t.Fatalf("line 2 wrong: %+v", lines[1])
	}
	// Past EOF truncates; invalid ranges return nil.
	if got := SliceContextLines(content, 2, 99); len(got) != 2 {
		t.Fatalf("EOF truncation got %d", len(got))
	}
	if SliceContextLines(content, 0, 2) != nil || SliceContextLines(content, 5, 2) != nil {
		t.Fatal("invalid ranges must return nil")
	}
}

func TestParseBatchedFiles(t *testing.T) {
	out := "\n" + BatchBoundary + "\na.go\npackage a\nvar X = 1\n" +
		"\n" + BatchBoundary + "\nb/c.vue\n<template></template>"
	files := ParseBatchedFiles(out)
	if len(files) != 2 {
		t.Fatalf("got %d files", len(files))
	}
	if files["a.go"] != "package a\nvar X = 1\n" {
		t.Fatalf("a.go = %q", files["a.go"])
	}
	if files["b/c.vue"] != "<template></template>" {
		t.Fatalf("c.vue = %q", files["b/c.vue"])
	}
	if len(ParseBatchedFiles("")) != 0 {
		t.Fatal("empty output must parse empty")
	}
}

func TestReadWorkdirFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, ok := ReadWorkdirFile(dir, "x.txt"); !ok || got != "data" {
		t.Fatalf("got %q, %v", got, ok)
	}
	if _, ok := ReadWorkdirFile(dir, "missing.txt"); ok {
		t.Fatal("missing file must return ok=false")
	}
}

func TestContainerFilePaths(t *testing.T) {
	vuePath, goPath, oldOnly := "app.vue", "main.go", "gone.svelte"
	files := []model.DiffFile{
		{NewPath: &vuePath, Hunks: []model.DiffHunk{{}}},
		{NewPath: &goPath, Hunks: []model.DiffHunk{{}}},
		{OldPath: &oldOnly, Status: model.StatusDeleted, Hunks: []model.DiffHunk{{}}},
		{NewPath: &vuePath, IsBinary: true, Hunks: []model.DiffHunk{{}}},
	}
	needs := syntax.NeedsFullFileHighlight

	newSide := ContainerFilePaths(files, model.LineSideNew, needs)
	if len(newSide) != 1 || newSide[0] != "app.vue" {
		t.Fatalf("new side = %v", newSide)
	}
	oldSide := ContainerFilePaths(files, model.LineSideOld, needs)
	if len(oldSide) != 1 || oldSide[0] != "gone.svelte" {
		t.Fatalf("old side = %v", oldSide)
	}
}

type failRunner struct{}

func (failRunner) Run(string, string, ...string) ([]byte, []byte, error) {
	return []byte("body detail"), []byte("status line"), errors.New("boom")
}

func TestCommandError(t *testing.T) {
	if CommandError("git", nil, nil, nil, nil) != nil {
		t.Fatal("nil error must map to nil")
	}
	stdout, stderr, err := failRunner{}.Run("", "git", "status")
	mapped := CommandError("git", []string{"status"}, stdout, stderr, err)
	var vcsErr *errs.VcsCommand
	if !errors.As(mapped, &vcsErr) {
		t.Fatalf("got %T", mapped)
	}
	// Both streams must be present, stderr first.
	if want := "status line\nbody detail"; !contains(vcsErr.Detail, want) {
		t.Fatalf("detail %q missing %q", vcsErr.Detail, want)
	}
}

func TestUnsupportedBaseDefaults(t *testing.T) {
	var base UnsupportedBase
	if _, err := base.StagedDiff(nil); !errors.Is(err, errs.ErrUnsupported) {
		t.Fatal("StagedDiff must be unsupported")
	}
	if _, err := base.ChangeStatus(); !errors.Is(err, errs.ErrUnsupported) {
		t.Fatal("ChangeStatus must be unsupported")
	}
	if err := base.StageFile("x"); !errors.Is(err, errs.ErrUnsupported) {
		t.Fatal("StageFile must be unsupported")
	}
	if commits, err := base.RecentCommits(0, 10); err != nil || commits != nil {
		t.Fatal("RecentCommits defaults to empty")
	}
	if base.SupportsSparseCheckout() || base.StartupWarnings() != nil {
		t.Fatal("defaults wrong")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestUnquoteGitPath(t *testing.T) {
	cases := map[string]string{
		`plain.c`:         `plain.c`,
		`"quoted.c"`:      `quoted.c`,
		`"with\tspace.c"`: "with\tspace.c",
		`"caf\303\251.c"`: "café.c",
		`"back\\slash.c"`: `back\slash.c`,
	}
	for in, want := range cases {
		if got := UnquoteGitPath(in); got != want {
			t.Errorf("UnquoteGitPath(%q) = %q, want %q", in, got, want)
		}
	}
}
