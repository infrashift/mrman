package ignore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The core tuicr scenario: .gitignore excludes *.lock, .mrmanignore
// un-ignores Cargo.lock — Cargo.lock stays, yarn.lock goes.
func TestMrmanignoreCanUnignore(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".gitignore", "*.lock\n")
	writeFile(t, dir, FileName, "!Cargo.lock\n")

	f := Load(dir)
	if !f.HasRules() {
		t.Fatal("rules must be loaded")
	}
	if f.Ignored("Cargo.lock") {
		t.Error("Cargo.lock must be un-ignored by .mrmanignore")
	}
	if !f.Ignored("yarn.lock") {
		t.Error("yarn.lock must stay ignored")
	}
	if f.Ignored("src/main.go") {
		t.Error("unrelated files must pass")
	}
}

func TestFilterDiffFilesMatchesDeletedOnOldPath(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, FileName, "generated/\nsecret.txt\n")

	gen, kept, deleted := "generated/out.js", "src/app.go", "secret.txt"
	files := []model.DiffFile{
		{NewPath: &gen, Status: model.StatusAdded},
		{NewPath: &kept, Status: model.StatusModified},
		{OldPath: &deleted, Status: model.StatusDeleted},
	}
	got := Load(dir).FilterDiffFiles(files)
	if len(got) != 1 || got[0].DisplayPath() != "src/app.go" {
		t.Fatalf("got %d files", len(got))
	}
}

func TestFilterPaths(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".gitignore", "dist/\n# a comment\n\n")

	got := Load(dir).FilterPaths([]string{"dist/bundle.js", "src/x.go"})
	if len(got) != 1 || got[0] != "src/x.go" {
		t.Fatalf("got %v", got)
	}
}

func TestNoRuleFilesMeansNoFiltering(t *testing.T) {
	f := Load(t.TempDir())
	if f.HasRules() {
		t.Fatal("no files → no rules")
	}
	paths := []string{"anything.lock", "dist/x"}
	if got := f.FilterPaths(paths); len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	if f.Ignored("anything.lock") {
		t.Fatal("nothing is ignored without rules")
	}
}

func TestNestedIgnoreFilesAreNotConsulted(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "sub"), ".gitignore", "*.go\n")
	f := Load(dir)
	if f.Ignored("sub/main.go") {
		t.Fatal("nested .gitignore must not be consulted")
	}
}
