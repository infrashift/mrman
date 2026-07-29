package filebackend

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/syntax"
)

func highlighter() *syntax.Highlighter {
	return syntax.NewHighlighter("monokai", "#1c3d1c", "#3d1c1c")
}

// canonTempDir returns a symlink-resolved temp dir so path comparisons match
// the backend's canonicalized root.
func canonTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func diffPaths(t *testing.T, diffs []model.DiffFile) []string {
	t.Helper()
	names := make([]string, 0, len(diffs))
	for i := range diffs {
		if diffs[i].NewPath == nil {
			t.Fatalf("diff %d has nil NewPath", i)
		}
		names = append(names, *diffs[i].NewPath)
	}
	return names
}

func TestSingleFileModeReturnsOneDiffFile(t *testing.T) {
	dir := canonTempDir(t)
	path := filepath.Join(dir, "hello.txt")
	mustWrite(t, path, "alpha\nbeta\n")

	backend, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if backend.Mode() != Single {
		t.Fatalf("mode = %d, want Single", backend.Mode())
	}
	diffs, err := backend.WorkingTreeDiff(highlighter())
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 1 {
		t.Fatalf("got %d diffs", len(diffs))
	}
	if *diffs[0].NewPath != "hello.txt" {
		t.Fatalf("path = %q", *diffs[0].NewPath)
	}
	if got := len(diffs[0].Hunks[0].Lines); got != 2 {
		t.Fatalf("got %d lines", got)
	}
}

func TestDirectoryModeWalksAndRespectsGitignore(t *testing.T) {
	dir := canonTempDir(t)
	mustWrite(t, filepath.Join(dir, ".gitignore"), "ignored.txt\n")
	mustWrite(t, filepath.Join(dir, "kept.txt"), "hello\n")
	mustWrite(t, filepath.Join(dir, "ignored.txt"), "skip me\n")

	backend, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if backend.Mode() != Directory {
		t.Fatalf("mode = %d, want Directory", backend.Mode())
	}
	diffs, err := backend.WorkingTreeDiff(highlighter())
	if err != nil {
		t.Fatal(err)
	}
	if names := diffPaths(t, diffs); len(names) != 1 || names[0] != "kept.txt" {
		t.Fatalf("names = %v, want [kept.txt]", names)
	}
}

func TestDirectoryModeSkipsBinaryFiles(t *testing.T) {
	dir := canonTempDir(t)
	if err := os.WriteFile(filepath.Join(dir, "binary.bin"), []byte{0, 1, 2, 3, 0, 4}, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := New(dir)
	if !errors.Is(err, errs.ErrNoChanges) {
		t.Fatalf("expected ErrNoChanges, got %v", err)
	}
}

func TestDirectoryModePreservesRelativePathsForNestedFiles(t *testing.T) {
	dir := canonTempDir(t)
	mustWrite(t, filepath.Join(dir, "nested", "inner.txt"), "x\n")

	backend, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	diffs, err := backend.WorkingTreeDiff(highlighter())
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 1 || *diffs[0].NewPath != filepath.Join("nested", "inner.txt") {
		t.Fatalf("diffs = %v", diffPaths(t, diffs))
	}
}

func TestPristineDiffUsesContextOriginAndOneIndexedHunk(t *testing.T) {
	dir := canonTempDir(t)
	path := filepath.Join(dir, "hello.rs")
	mustWrite(t, path, "fn main() {}\nlet x = 1;\nlet y = 2;\nlet z = 3;\nlet w = 4;\n")

	backend, err := NewPristine([]string{path}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if backend.Mode() != Pristine {
		t.Fatalf("mode = %d, want Pristine", backend.Mode())
	}
	diffs, err := backend.WorkingTreeDiff(highlighter())
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 1 {
		t.Fatalf("got %d diffs", len(diffs))
	}
	hunk := &diffs[0].Hunks[0]
	if hunk.Header != "@@ -1,5 +1,5 @@" {
		t.Fatalf("header = %q", hunk.Header)
	}
	if hunk.OldStart != 1 || hunk.OldCount != 5 || hunk.NewStart != 1 || hunk.NewCount != 5 {
		t.Fatalf("hunk range = %+v", hunk)
	}
	for i := range hunk.Lines {
		l := &hunk.Lines[i]
		if l.Origin != model.OriginContext {
			t.Fatalf("line %d origin = %d, want context", i, l.Origin)
		}
		if l.OldLineno == nil || l.NewLineno == nil {
			t.Fatalf("line %d missing linenos", i)
		}
	}
	if diffs[0].Status != model.StatusModified {
		t.Fatalf("status = %q", diffs[0].Status)
	}
}

func TestDirectoryModeStillUsesAdditionOrigin(t *testing.T) {
	// Regression: pristine mode must not bleed back into the existing
	// `--file <dir>` directory-mode rendering.
	dir := canonTempDir(t)
	mustWrite(t, filepath.Join(dir, "a.txt"), "alpha\nbeta\n")

	backend, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	diffs, err := backend.WorkingTreeDiff(highlighter())
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 1 {
		t.Fatalf("got %d diffs", len(diffs))
	}
	hunk := &diffs[0].Hunks[0]
	if hunk.Header != "@@ -0,0 +1,2 @@" {
		t.Fatalf("header = %q", hunk.Header)
	}
	for i := range hunk.Lines {
		if hunk.Lines[i].Origin != model.OriginAddition {
			t.Fatalf("line %d origin = %d, want addition", i, hunk.Lines[i].Origin)
		}
		if hunk.Lines[i].OldLineno != nil {
			t.Fatalf("line %d has old lineno", i)
		}
	}
	if diffs[0].Status != model.StatusAdded {
		t.Fatalf("status = %q", diffs[0].Status)
	}
}

func TestPristineFiltersBinaryFiles(t *testing.T) {
	dir := canonTempDir(t)
	textPath := filepath.Join(dir, "text.txt")
	binPath := filepath.Join(dir, "bin.bin")
	mustWrite(t, textPath, "hello\n")
	if err := os.WriteFile(binPath, []byte{0, 1, 2, 3, 0, 4}, 0o644); err != nil {
		t.Fatal(err)
	}

	backend, err := NewPristine([]string{textPath, binPath}, dir)
	if err != nil {
		t.Fatal(err)
	}
	diffs, err := backend.WorkingTreeDiff(highlighter())
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 1 || *diffs[0].NewPath != "text.txt" {
		t.Fatalf("diffs = %v", diffPaths(t, diffs))
	}
}

func TestPristineAllFilteredIsNoChanges(t *testing.T) {
	dir := canonTempDir(t)
	binPath := filepath.Join(dir, "bin.bin")
	if err := os.WriteFile(binPath, []byte{0, 1}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPristine([]string{binPath, filepath.Join(dir, "missing.txt")}, dir); !errors.Is(err, errs.ErrNoChanges) {
		t.Fatalf("expected ErrNoChanges, got %v", err)
	}
	if _, err := NewPristine(nil, dir); !errors.Is(err, errs.ErrNoChanges) {
		t.Fatalf("empty input: expected ErrNoChanges, got %v", err)
	}
}

func TestPristineFetchContextResolvesPerFile(t *testing.T) {
	dir := canonTempDir(t)
	pathA := filepath.Join(dir, "a.txt")
	pathB := filepath.Join(dir, "b.txt")
	mustWrite(t, pathA, "alpha-1\nalpha-2\nalpha-3\n")
	mustWrite(t, pathB, "beta-1\nbeta-2\nbeta-3\n")

	backend, err := NewPristine([]string{pathA, pathB}, dir)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := backend.FetchContextLines("b.txt", model.StatusModified, nil, 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Fatalf("got %d lines", len(lines))
	}
	for i := range lines {
		if !strings.HasPrefix(lines[i].Content, "beta-") {
			t.Fatalf("line %d content = %q", i, lines[i].Content)
		}
		if lines[i].Origin != model.OriginContext {
			t.Fatalf("line %d origin = %d", i, lines[i].Origin)
		}
	}
}

func TestPristineFetchContextRejectsPathEscape(t *testing.T) {
	outer := canonTempDir(t)
	mustWrite(t, filepath.Join(outer, "secret.txt"), "TOP_SECRET\n")

	repo := filepath.Join(outer, "repo")
	kept := filepath.Join(repo, "kept.txt")
	mustWrite(t, kept, "ok\n")

	backend, err := NewPristine([]string{kept}, repo)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := backend.FetchContextLines("../secret.txt", model.StatusModified, nil, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 {
		t.Fatal("fetch must not read files outside the configured root")
	}
}

func TestFetchContextGuards(t *testing.T) {
	dir := canonTempDir(t)
	path := filepath.Join(dir, "a.txt")
	mustWrite(t, path, "one\ntwo\n")
	backend, err := New(path)
	if err != nil {
		t.Fatal(err)
	}

	// Invalid ranges return empty without error.
	if lines, err := backend.FetchContextLines("a.txt", model.StatusAdded, nil, 0, 3); err != nil || len(lines) != 0 {
		t.Fatalf("start=0: got %v, %v", lines, err)
	}
	if lines, err := backend.FetchContextLines("a.txt", model.StatusAdded, nil, 3, 1); err != nil || len(lines) != 0 {
		t.Fatalf("start>end: got %v, %v", lines, err)
	}
	// Missing files return empty without error.
	if lines, err := backend.FetchContextLines("missing.txt", model.StatusAdded, nil, 1, 1); err != nil || len(lines) != 0 {
		t.Fatalf("missing: got %v, %v", lines, err)
	}
	// Directories are not files.
	if lines, err := backend.FetchContextLines(".", model.StatusAdded, nil, 1, 1); err != nil || len(lines) != 0 {
		t.Fatalf("dir: got %v, %v", lines, err)
	}
	// Happy path.
	lines, err := backend.FetchContextLines("a.txt", model.StatusAdded, nil, 2, 2)
	if err != nil || len(lines) != 1 || lines[0].Content != "two" {
		t.Fatalf("happy path: got %v, %v", lines, err)
	}
}

func TestFileLineCount(t *testing.T) {
	dir := canonTempDir(t)
	path := filepath.Join(dir, "a.txt")
	mustWrite(t, path, "one\ntwo\nthree\n")
	backend, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	n, err := backend.FileLineCount("a.txt", model.StatusAdded, nil)
	if err != nil || n != 3 {
		t.Fatalf("got %d, %v", n, err)
	}
	if _, err := backend.FileLineCount("missing.txt", model.StatusAdded, nil); err == nil {
		t.Fatal("missing file must error")
	}
}

func TestTooLargeFileGetsPlaceholder(t *testing.T) {
	dir := canonTempDir(t)
	path := filepath.Join(dir, "big.txt")
	mustWrite(t, path, strings.Repeat("a", maxFileBytes+1))

	backend, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	diffs, err := backend.WorkingTreeDiff(highlighter())
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 1 {
		t.Fatalf("got %d diffs", len(diffs))
	}
	if !diffs[0].IsTooLarge || diffs[0].IsBinary || len(diffs[0].Hunks) != 0 {
		t.Fatalf("placeholder wrong: %+v", diffs[0])
	}
	if diffs[0].Status != model.StatusAdded {
		t.Fatalf("status = %q", diffs[0].Status)
	}
}

func TestEmptyFileIsSkipped(t *testing.T) {
	dir := canonTempDir(t)
	path := filepath.Join(dir, "empty.txt")
	mustWrite(t, path, "")
	backend, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.WorkingTreeDiff(highlighter()); !errors.Is(err, errs.ErrNoChanges) {
		t.Fatalf("expected ErrNoChanges, got %v", err)
	}
}

func TestInfoValues(t *testing.T) {
	dir := canonTempDir(t)
	path := filepath.Join(dir, "a.txt")
	mustWrite(t, path, "x\n")
	backend, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	info := backend.Info()
	if info.HeadCommit != "file" || info.BranchName != nil || info.Type != "file" {
		t.Fatalf("info = %+v", info)
	}
	if info.RootPath != dir {
		t.Fatalf("root = %q, want %q", info.RootPath, dir)
	}
}

func TestNewErrors(t *testing.T) {
	if _, err := New(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("nonexistent path must error")
	}
}

func TestUnsupportedDefaults(t *testing.T) {
	dir := canonTempDir(t)
	path := filepath.Join(dir, "a.txt")
	mustWrite(t, path, "x\n")
	backend, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.StagedDiff(nil); !errors.Is(err, errs.ErrUnsupported) {
		t.Fatal("StagedDiff must be unsupported")
	}
	if err := backend.StageFile("a.txt"); !errors.Is(err, errs.ErrUnsupported) {
		t.Fatal("StageFile must be unsupported")
	}
	if _, err := backend.ResolveRevisionRange("HEAD"); !errors.Is(err, errs.ErrUnsupported) {
		t.Fatal("ResolveRevisionRange must be unsupported")
	}
}

func TestHighlightingAppliesDiffBackground(t *testing.T) {
	dir := canonTempDir(t)
	path := filepath.Join(dir, "main.go")
	mustWrite(t, path, "package main\n\nfunc main() {}\n")

	backend, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	diffs, err := backend.WorkingTreeDiff(highlighter())
	if err != nil {
		t.Fatal(err)
	}
	first := diffs[0].Hunks[0].Lines[0]
	if len(first.HighlightedSpans) == 0 {
		t.Fatal("expected highlighted spans on Go source")
	}
	for _, span := range first.HighlightedSpans {
		if span.Style.BG != "#1c3d1c" {
			t.Fatalf("addition span BG = %q, want add background", span.Style.BG)
		}
	}
}

func TestIsProbablyBinaryDetectsAnyNullInSniffWindow(t *testing.T) {
	dir := canonTempDir(t)

	// No nulls anywhere within sniff window → text.
	for _, trial := range []int{0, 1, 100, 8000, binarySniffBytes, 16384} {
		p := filepath.Join(dir, "text.bin")
		mustWrite(t, p, strings.Repeat("a", trial))
		if isProbablyBinary(p) {
			t.Fatalf("no-null content of length %d must be classified text", trial)
		}
	}

	// A null at any position within the sniff window classifies as binary.
	for _, nullAt := range []int{0, 1, 100, binarySniffBytes - 1} {
		p := filepath.Join(dir, "null.bin")
		content := []byte(strings.Repeat("a", binarySniffBytes))
		content[nullAt] = 0
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatal(err)
		}
		if !isProbablyBinary(p) {
			t.Fatalf("null at offset %d must classify as binary", nullAt)
		}
	}

	// A null AFTER the sniff window is NOT detected (by design).
	p := filepath.Join(dir, "null-after.bin")
	content := []byte(strings.Repeat("a", binarySniffBytes+10))
	content[binarySniffBytes+5] = 0
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if isProbablyBinary(p) {
		t.Fatalf("null past the %d-byte sniff window must NOT be detected", binarySniffBytes)
	}

	// Unreadable files classify as binary.
	if !isProbablyBinary(filepath.Join(dir, "missing")) {
		t.Fatal("unreadable file must classify as binary")
	}
}

func TestGitignoreSemantics(t *testing.T) {
	dir := canonTempDir(t)
	mustWrite(t, filepath.Join(dir, ".gitignore"),
		"# comment\n\n*.log\n!keep.log\nbuild/\n/rooted.txt\ndocs/*.tmp\n**/deep.txt\n")
	mustWrite(t, filepath.Join(dir, "app.log"), "log\n")
	mustWrite(t, filepath.Join(dir, "keep.log"), "kept by negation\n")
	mustWrite(t, filepath.Join(dir, "build", "out.txt"), "in ignored dir\n")
	mustWrite(t, filepath.Join(dir, "src", "build"), "file named build, not dir-only match\n")
	mustWrite(t, filepath.Join(dir, "rooted.txt"), "anchored\n")
	mustWrite(t, filepath.Join(dir, "sub", "rooted.txt"), "not anchored here\n")
	mustWrite(t, filepath.Join(dir, "docs", "a.tmp"), "glob\n")
	mustWrite(t, filepath.Join(dir, "docs", "a.txt"), "kept\n")
	mustWrite(t, filepath.Join(dir, "x", "y", "deep.txt"), "double star\n")
	// Nested .gitignore overrides for its subtree.
	mustWrite(t, filepath.Join(dir, "nested", ".gitignore"), "inner.txt\n")
	mustWrite(t, filepath.Join(dir, "nested", "inner.txt"), "nested ignore\n")
	mustWrite(t, filepath.Join(dir, "nested", "other.txt"), "kept\n")

	backend, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	diffs, err := backend.WorkingTreeDiff(highlighter())
	if err != nil {
		t.Fatal(err)
	}
	got := diffPaths(t, diffs)
	want := []string{
		filepath.Join("docs", "a.txt"),
		"keep.log",
		filepath.Join("nested", "other.txt"),
		filepath.Join("src", "build"),
		filepath.Join("sub", "rooted.txt"),
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestSymlinksAreSkipped(t *testing.T) {
	dir := canonTempDir(t)
	mustWrite(t, filepath.Join(dir, "real.txt"), "real\n")
	if err := os.Symlink(filepath.Join(dir, "real.txt"), filepath.Join(dir, "link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	backend, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	diffs, err := backend.WorkingTreeDiff(highlighter())
	if err != nil {
		t.Fatal(err)
	}
	if names := diffPaths(t, diffs); len(names) != 1 || names[0] != "real.txt" {
		t.Fatalf("names = %v, want [real.txt]", names)
	}
}
