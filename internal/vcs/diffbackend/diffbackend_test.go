package diffbackend

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs"
	"github.com/infrashift/mrman/internal/vcs/filebackend"
)

// write creates dir/name (making parents) with the given content and
// returns its path.
func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// filePair writes two files into a fresh temp dir and returns their paths.
func filePair(t *testing.T, oldContent, newContent string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	return write(t, dir, "old/a.txt", oldContent), write(t, dir, "new/a.txt", newContent)
}

func TestNewRejections(t *testing.T) {
	dir := t.TempDir()
	file := write(t, dir, "a.txt", "x\n")
	subdir := filepath.Join(dir, "sub")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name           string
		oldPath        string
		newPath        string
		wantInvalid    bool
		wantNotExist   bool
		wantNoChanges  bool
		detailContains string
	}{
		{
			name: "file against directory", oldPath: file, newPath: subdir,
			wantInvalid: true, detailContains: "both be files or both be directories",
		},
		{
			name: "directory against file", oldPath: subdir, newPath: file,
			wantInvalid: true, detailContains: "both be files or both be directories",
		},
		{
			name: "same file both sides", oldPath: file, newPath: file,
			wantInvalid: true, detailContains: "same file",
		},
		{
			name: "missing old path", oldPath: filepath.Join(dir, "nope"), newPath: file,
			wantNotExist: true,
		},
		{
			name: "missing new path", oldPath: file, newPath: filepath.Join(dir, "nope"),
			wantNotExist: true,
		},
		{
			name: "empty directories", oldPath: subdir, newPath: mkdir(t, dir, "sub2"),
			wantNoChanges: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.oldPath, tt.newPath, vcs.WhitespaceNormal)
			if err == nil {
				t.Fatal("want an error, got nil")
			}
			var invalid *errs.InvalidInput
			switch {
			case tt.wantInvalid:
				if !errors.As(err, &invalid) {
					t.Fatalf("want *errs.InvalidInput, got %T: %v", err, err)
				}
				if !strings.Contains(invalid.Detail, tt.detailContains) {
					t.Errorf("detail %q does not mention %q", invalid.Detail, tt.detailContains)
				}
			case tt.wantNotExist:
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("want a not-exist error, got %v", err)
				}
			case tt.wantNoChanges:
				if !errors.Is(err, errs.ErrNoChanges) {
					t.Fatalf("want errs.ErrNoChanges, got %v", err)
				}
			}
		})
	}
}

func mkdir(t *testing.T, parent, name string) string {
	t.Helper()
	path := filepath.Join(parent, name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInfo(t *testing.T) {
	oldFile, newFile := filePair(t, "one\ntwo\n", "one\n2\n")
	b, err := New(oldFile, newFile, vcs.WhitespaceNormal)
	if err != nil {
		t.Fatal(err)
	}
	info := b.Info()

	if want := filepath.Dir(newFile); info.RootPath != want {
		t.Errorf("RootPath = %q, want the new side's directory %q", info.RootPath, want)
	}
	if info.Type != vcs.TypeDiff {
		t.Errorf("Type = %q, want %q", info.Type, vcs.TypeDiff)
	}
	// Load-bearing: slug.Parse cuts on the first colon and then demands a
	// known forge prefix, so a colon here makes the session unparseable.
	if strings.Contains(info.HeadCommit, ":") {
		t.Errorf("HeadCommit %q must not contain a colon", info.HeadCommit)
	}
	if len(info.HeadCommit) != 16 {
		t.Errorf("HeadCommit = %q, want 16 hex characters", info.HeadCommit)
	}
	if info.BranchName == nil {
		t.Fatal("BranchName must be set; it names the session file")
	}
	if !strings.Contains(*info.BranchName, "-vs-") {
		t.Errorf("BranchName = %q, want it to name both sides", *info.BranchName)
	}
}

// TestIdentityIsPathKeyed is the session-resumption contract: the same two
// paths must resolve to the same session even after their contents change,
// because editing a file and reopening is the central workflow.
func TestIdentityIsPathKeyed(t *testing.T) {
	oldFile, newFile := filePair(t, "one\ntwo\n", "one\n2\n")

	identity := func(o, n string) string {
		t.Helper()
		b, err := New(o, n, vcs.WhitespaceNormal)
		if err != nil {
			t.Fatal(err)
		}
		return b.Info().HeadCommit
	}

	first := identity(oldFile, newFile)
	if again := identity(oldFile, newFile); again != first {
		t.Errorf("identity is not stable: %q then %q", first, again)
	}
	if swapped := identity(newFile, oldFile); swapped == first {
		t.Error("swapping the two sides must be a different comparison")
	}

	if err := os.WriteFile(newFile, []byte("wholly different\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if edited := identity(oldFile, newFile); edited != first {
		t.Errorf("editing a file changed the identity: %q became %q", first, edited)
	}
}

func TestWorkingTreeDiffTwoFiles(t *testing.T) {
	oldFile, newFile := filePair(t, "one\ntwo\nthree\n", "one\n2\nthree\nfour\n")
	b, err := New(oldFile, newFile, vcs.WhitespaceNormal)
	if err != nil {
		t.Fatal(err)
	}
	files, err := b.WorkingTreeDiff(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	f := files[0]

	if got := f.DisplayPath(); got != "a.txt" {
		t.Errorf("DisplayPath = %q, want the new side's basename", got)
	}
	// Regression guard for NoRenameHeaders: two compared files are not a
	// rename, and StatusRenamed would paint a meaningless R badge.
	if f.Status != model.StatusModified {
		t.Errorf("Status = %q, want %q", f.Status, model.StatusModified)
	}
	if adds, dels := f.Stat(); adds != 2 || dels != 1 {
		t.Errorf("stat = +%d -%d, want +2 -1", adds, dels)
	}
	if f.ContentHash == 0 {
		t.Error("ContentHash must be computed")
	}
	if len(f.Hunks) == 0 {
		t.Fatal("want at least one hunk")
	}
	// A real old side is the whole point of this mode: unlike --file, both
	// gutters carry line numbers.
	var sawOld, sawNew bool
	for _, line := range f.Hunks[0].Lines {
		sawOld = sawOld || line.OldLineno != nil
		sawNew = sawNew || line.NewLineno != nil
	}
	if !sawOld || !sawNew {
		t.Errorf("want both gutters populated, got old=%v new=%v", sawOld, sawNew)
	}
}

func TestWorkingTreeDiffIdenticalFiles(t *testing.T) {
	oldFile, newFile := filePair(t, "same\n", "same\n")
	b, err := New(oldFile, newFile, vcs.WhitespaceNormal)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.WorkingTreeDiff(nil); !errors.Is(err, errs.ErrNoChanges) {
		t.Fatalf("want errs.ErrNoChanges, got %v", err)
	}
}

func TestWhitespaceIgnoreAll(t *testing.T) {
	oldFile, newFile := filePair(t, "func f() {\n    return 1\n}\n", "func f() {\n\treturn 1\n}\n")

	b, err := New(oldFile, newFile, vcs.WhitespaceNormal)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.WorkingTreeDiff(nil); err != nil {
		t.Fatalf("a reindent should be a difference by default: %v", err)
	}

	ignoring, err := New(oldFile, newFile, vcs.WhitespaceIgnoreAll)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ignoring.WorkingTreeDiff(nil); !errors.Is(err, errs.ErrNoChanges) {
		t.Fatalf("want the reindent ignored, got %v", err)
	}
}

func TestTreePairing(t *testing.T) {
	dir := t.TempDir()
	oldRoot, newRoot := mkdir(t, dir, "old"), mkdir(t, dir, "new")

	write(t, oldRoot, "shared.txt", "one\ntwo\n")
	write(t, newRoot, "shared.txt", "one\n2\n")
	write(t, oldRoot, "removed.txt", "gone\n")
	write(t, newRoot, "added.txt", "brand new\n")
	write(t, oldRoot, "same.txt", "identical\n")
	write(t, newRoot, "same.txt", "identical\n")
	write(t, oldRoot, "nested/deep.txt", "a\n")
	write(t, newRoot, "nested/deep.txt", "b\n")

	b, err := New(oldRoot, newRoot, vcs.WhitespaceNormal)
	if err != nil {
		t.Fatal(err)
	}
	if b.Mode() != Trees {
		t.Fatalf("Mode = %v, want Trees", b.Mode())
	}
	files, err := b.WorkingTreeDiff(nil)
	if err != nil {
		t.Fatal(err)
	}

	got := make(map[string]model.FileStatus, len(files))
	for _, f := range files {
		got[f.DisplayPath()] = f.Status
	}
	want := map[string]model.FileStatus{
		"shared.txt":      model.StatusModified,
		"removed.txt":     model.StatusDeleted,
		"added.txt":       model.StatusAdded,
		"nested/deep.txt": model.StatusModified,
	}
	for path, status := range want {
		if got[path] != status {
			t.Errorf("%s: status %q, want %q", path, got[path], status)
		}
	}
	if _, listed := got["same.txt"]; listed {
		t.Error("an identical file has nothing to review and must not be listed")
	}
	if len(got) != len(want) {
		t.Errorf("got %d entries %v, want %d", len(got), got, len(want))
	}
}

// TestTreeWalkHonoursGitignore is the test that encodes why this backend
// does not shell out to `git diff --no-index`: --no-index is a plain
// recursive readdir, so comparing two directories in any real project walks
// node_modules and buries the review in thousands of vendored files.
func TestTreeWalkHonoursGitignore(t *testing.T) {
	dir := t.TempDir()
	oldRoot, newRoot := mkdir(t, dir, "old"), mkdir(t, dir, "new")

	for _, root := range []string{oldRoot, newRoot} {
		write(t, root, ".gitignore", "node_modules/\n*.log\n")
		write(t, root, "node_modules/dep/index.js", "vendored "+filepath.Base(root)+"\n")
		write(t, root, "debug.log", "noise "+filepath.Base(root)+"\n")
	}
	write(t, oldRoot, "src/app.js", "one\n")
	write(t, newRoot, "src/app.js", "two\n")

	b, err := New(oldRoot, newRoot, vcs.WhitespaceNormal)
	if err != nil {
		t.Fatal(err)
	}
	files, err := b.WorkingTreeDiff(nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range files {
		path := f.DisplayPath()
		if strings.Contains(path, "node_modules") || strings.HasSuffix(path, ".log") {
			t.Errorf("ignored path %q reached the review", path)
		}
	}
	if len(files) != 1 || files[0].DisplayPath() != "src/app.js" {
		t.Errorf("want only src/app.js, got %d files", len(files))
	}
}

func TestBinaryAndTooLargeStubs(t *testing.T) {
	// Naming a binary file explicitly is a request to compare it, so it is
	// listed as a stub rather than silently producing "no changes".
	t.Run("an explicitly named binary is listed but not rendered", func(t *testing.T) {
		dir := t.TempDir()
		oldFile := write(t, dir, "old/img.png", "\x00\x01binary old\n")
		newFile := write(t, dir, "new/img.png", "\x00\x01binary new\n")

		b, err := New(oldFile, newFile, vcs.WhitespaceNormal)
		if err != nil {
			t.Fatal(err)
		}
		files, err := b.WorkingTreeDiff(nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 1 {
			t.Fatalf("got %d files, want 1", len(files))
		}
		if !files[0].IsBinary {
			t.Error("want IsBinary")
		}
		if len(files[0].Hunks) != 0 {
			t.Error("a binary stub must carry no hunks")
		}
		if got := files[0].DisplayPath(); got != "img.png" {
			t.Errorf("DisplayPath = %q, want img.png", got)
		}
	})

	// A tree walk drops binaries before they ever become a pair, which is
	// what `--file <dir>` does too. Both directory walks must agree about
	// what counts as reviewable.
	t.Run("a tree walk skips binaries entirely", func(t *testing.T) {
		dir := t.TempDir()
		oldRoot, newRoot := mkdir(t, dir, "old"), mkdir(t, dir, "new")
		write(t, oldRoot, "img.png", "\x00\x01binary old\n")
		write(t, newRoot, "img.png", "\x00\x01binary new\n")
		write(t, oldRoot, "a.txt", "one\n")
		write(t, newRoot, "a.txt", "two\n")

		b, err := New(oldRoot, newRoot, vcs.WhitespaceNormal)
		if err != nil {
			t.Fatal(err)
		}
		files, err := b.WorkingTreeDiff(nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 1 || files[0].DisplayPath() != "a.txt" {
			t.Fatalf("want only a.txt, got %d files", len(files))
		}
	})

	t.Run("oversized text is listed but not rendered", func(t *testing.T) {
		dir := t.TempDir()
		oldRoot, newRoot := mkdir(t, dir, "old"), mkdir(t, dir, "new")
		write(t, oldRoot, "big.txt", "small\n")
		// Text in the sniffed prefix so it is not mistaken for binary, then
		// extended past the ceiling without writing the bytes.
		big := write(t, newRoot, "big.txt", strings.Repeat("text line\n", 1000))
		if err := os.Truncate(big, filebackendMaxPlusOne()); err != nil {
			t.Fatal(err)
		}

		b, err := New(oldRoot, newRoot, vcs.WhitespaceNormal)
		if err != nil {
			t.Fatal(err)
		}
		files, err := b.WorkingTreeDiff(nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 1 {
			t.Fatalf("got %d files, want 1", len(files))
		}
		if !files[0].IsTooLarge {
			t.Error("want IsTooLarge")
		}
		if files[0].IsBinary {
			t.Error("a large text file must not be reported as binary")
		}
	})
}

func TestCRLFMismatchWarns(t *testing.T) {
	t.Run("mismatch is reported", func(t *testing.T) {
		oldFile, newFile := filePair(t, "a\r\nb\r\n", "a\nb\n")
		b, err := New(oldFile, newFile, vcs.WhitespaceNormal)
		if err != nil {
			t.Fatal(err)
		}
		warnings := b.StartupWarnings()
		if len(warnings) != 1 {
			t.Fatalf("got %d warnings, want 1: %v", len(warnings), warnings)
		}
		if !strings.Contains(warnings[0], "CRLF") || !strings.Contains(warnings[0], "a.txt") {
			t.Errorf("warning does not explain the mismatch: %q", warnings[0])
		}
	})

	t.Run("matching line endings are silent", func(t *testing.T) {
		oldFile, newFile := filePair(t, "a\nb\n", "a\nc\n")
		b, err := New(oldFile, newFile, vcs.WhitespaceNormal)
		if err != nil {
			t.Fatal(err)
		}
		if got := b.StartupWarnings(); len(got) != 0 {
			t.Errorf("want no warnings, got %v", got)
		}
	})
}

func TestFetchContextLines(t *testing.T) {
	dir := t.TempDir()
	oldRoot, newRoot := mkdir(t, dir, "old"), mkdir(t, dir, "new")
	write(t, oldRoot, "a.txt", "old1\nold2\nold3\n")
	write(t, newRoot, "a.txt", "new1\nnew2\nnew3\nnew4\n")
	write(t, oldRoot, "deleted.txt", "d1\nd2\n")

	b, err := New(oldRoot, newRoot, vcs.WhitespaceNormal)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("reads the new side", func(t *testing.T) {
		lines, err := b.FetchContextLines("a.txt", model.StatusModified, nil, 1, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(lines) != 2 || lines[0].Content != "new1" || lines[1].Content != "new2" {
			t.Fatalf("got %+v, want the new file's first two lines", lines)
		}
		if lines[0].Origin != model.OriginContext {
			t.Errorf("context lines must have OriginContext, got %v", lines[0].Origin)
		}
	})

	t.Run("a deletion reads the old side", func(t *testing.T) {
		lines, err := b.FetchContextLines("deleted.txt", model.StatusDeleted, nil, 1, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(lines) != 2 || lines[0].Content != "d1" {
			t.Fatalf("got %+v, want the old file's lines", lines)
		}
	})

	t.Run("clamps past end of file", func(t *testing.T) {
		lines, err := b.FetchContextLines("a.txt", model.StatusModified, nil, 3, 99)
		if err != nil {
			t.Fatal(err)
		}
		if len(lines) != 2 {
			t.Fatalf("got %d lines, want the 2 that exist", len(lines))
		}
	})

	t.Run("rejects an inverted or zero range", func(t *testing.T) {
		for _, r := range [][2]uint32{{5, 1}, {0, 3}} {
			lines, err := b.FetchContextLines("a.txt", model.StatusModified, nil, r[0], r[1])
			if err != nil || lines != nil {
				t.Errorf("range %v: got (%v, %v), want (nil, nil)", r, lines, err)
			}
		}
	})

	// The confinement guarantee: paths are resolved through an enumerated
	// map, so a stale or hostile session path finds nothing rather than
	// escaping the compared roots.
	t.Run("unknown and escaping paths read nothing", func(t *testing.T) {
		for _, path := range []string{"nope.txt", "../../etc/passwd", "/etc/passwd", ""} {
			lines, err := b.FetchContextLines(path, model.StatusModified, nil, 1, 5)
			if err != nil || lines != nil {
				t.Errorf("%q: got (%v, %v), want (nil, nil)", path, lines, err)
			}
		}
	})
}

func TestFileLineCount(t *testing.T) {
	dir := t.TempDir()
	oldRoot, newRoot := mkdir(t, dir, "old"), mkdir(t, dir, "new")
	write(t, oldRoot, "a.txt", "1\n2\n")
	write(t, newRoot, "a.txt", "1\n2\n3\n")

	b, err := New(oldRoot, newRoot, vcs.WhitespaceNormal)
	if err != nil {
		t.Fatal(err)
	}

	if got, _ := b.FileLineCount("a.txt", model.StatusModified, nil); got != 3 {
		t.Errorf("new side count = %d, want 3", got)
	}
	if got, _ := b.FileLineCount("a.txt", model.StatusDeleted, nil); got != 2 {
		t.Errorf("old side count = %d, want 2", got)
	}
	if got, _ := b.FileLineCount("missing.txt", model.StatusModified, nil); got != 0 {
		t.Errorf("unknown path count = %d, want 0", got)
	}
}

// TestChangeStatusAnswersPlainly guards the reason ChangeStatus is
// implemented at all: an ErrUnsupported here makes resolveChangeStatus fall
// back to WorkingTreeDiff and report this backend's own files as unstaged
// changes, producing a selector row that fails when chosen.
func TestChangeStatusAnswersPlainly(t *testing.T) {
	oldFile, newFile := filePair(t, "a\n", "b\n")
	b, err := New(oldFile, newFile, vcs.WhitespaceNormal)
	if err != nil {
		t.Fatal(err)
	}
	status, err := b.ChangeStatus()
	if err != nil {
		t.Fatalf("want a plain answer, got %v", err)
	}
	if status.Staged || status.Unstaged {
		t.Errorf("want no changes reported, got %+v", status)
	}
}

func TestSanitizeStem(t *testing.T) {
	tests := []struct{ in, want string }{
		{"a.txt", "a.txt"},
		{"my file.txt", "my-file.txt"},
		{"weird//name", "weird-name"},
		{"v1.2.3", "v1.2.3"},
		{"under_score", "under_score"},
		{"-leading-and-trailing-", "leading-and-trailing"},
		{"...", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := SanitizeStem(tt.in); got != tt.want {
			t.Errorf("SanitizeStem(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCountLines(t *testing.T) {
	tests := []struct {
		in   string
		want uint32
	}{
		{"", 0},
		{"a\n", 1},
		{"a", 1},
		{"a\nb\n", 2},
		{"a\nb", 2},
		{"\n", 1},
	}
	for _, tt := range tests {
		if got := countLines(tt.in); got != tt.want {
			t.Errorf("countLines(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

// filebackendMaxPlusOne is one byte past the shared render ceiling.
func filebackendMaxPlusOne() int64 { return filebackend.MaxFileBytes + 1 }

func TestDistinguishingTails(t *testing.T) {
	tests := []struct {
		name         string
		a, b         string
		wantA, wantB string
	}{
		{"different basenames stop at one component", "/x/old.txt", "/y/new.txt", "old.txt", "new.txt"},
		{"shared basename walks up one", "/x/a.txt", "/y/a.txt", "x/a.txt", "y/a.txt"},
		{"shared tail walks up until it differs", "/p/staging/c.yaml", "/p/prod/c.yaml", "staging/c.yaml", "prod/c.yaml"},
		{"deeply shared tail", "/one/k/deep/f", "/two/k/deep/f", "one/k/deep/f", "two/k/deep/f"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotA, gotB := distinguishingTails(tt.a, tt.b)
			if gotA != tt.wantA || gotB != tt.wantB {
				t.Errorf("got (%q, %q), want (%q, %q)", gotA, gotB, tt.wantA, tt.wantB)
			}
		})
	}
}

// TestLabel covers the one thing nothing else in the UI can say: which of
// the two paths the reviewer is looking at.
func TestLabel(t *testing.T) {
	t.Run("two files sharing a name stay distinguishable", func(t *testing.T) {
		oldFile, newFile := filePair(t, "a\n", "b\n")
		b, err := New(oldFile, newFile, vcs.WhitespaceNormal)
		if err != nil {
			t.Fatal(err)
		}
		if got := b.Label(); got != "old/a.txt → new/a.txt" {
			t.Errorf("Label() = %q, want the parent directories included", got)
		}
	})

	t.Run("directories are marked as such", func(t *testing.T) {
		dir := t.TempDir()
		oldRoot, newRoot := mkdir(t, dir, "old"), mkdir(t, dir, "new")
		write(t, oldRoot, "a.txt", "one\n")
		write(t, newRoot, "a.txt", "two\n")

		b, err := New(oldRoot, newRoot, vcs.WhitespaceNormal)
		if err != nil {
			t.Fatal(err)
		}
		sep := string(filepath.Separator)
		if want := "old" + sep + " → new" + sep; b.Label() != want {
			t.Errorf("Label() = %q, want %q", b.Label(), want)
		}
	})
}

// TestSessionLabelIsFilesystemSafe guards the session filename: the slug
// grammar accepts only [A-Za-z0-9._-], so a separator reaching it would
// produce an unopenable path.
func TestSessionLabelIsFilesystemSafe(t *testing.T) {
	oldFile, newFile := filePair(t, "a\n", "b\n")
	b, err := New(oldFile, newFile, vcs.WhitespaceNormal)
	if err != nil {
		t.Fatal(err)
	}
	label := *b.Info().BranchName
	if label != "old-a.txt-vs-new-a.txt" {
		t.Errorf("BranchName = %q, want both sides named", label)
	}
	for _, r := range label {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '.' || r == '_' || r == '-'
		if !ok {
			t.Errorf("BranchName %q contains %q, which the slug grammar rejects", label, r)
		}
	}
}

// renameTrees builds two directory trees from maps of relative path to
// content and returns the two roots.
func renameTrees(t *testing.T, oldTree, newTree map[string]string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	oldRoot, newRoot := mkdir(t, dir, "old"), mkdir(t, dir, "new")
	for path, content := range oldTree {
		write(t, oldRoot, path, content)
	}
	for path, content := range newTree {
		write(t, newRoot, path, content)
	}
	return oldRoot, newRoot
}

// diffByPath runs the comparison and indexes the result by display path.
func diffByPath(t *testing.T, oldRoot, newRoot string) map[string]model.DiffFile {
	t.Helper()
	b, err := New(oldRoot, newRoot, vcs.WhitespaceNormal)
	if err != nil {
		t.Fatal(err)
	}
	files, err := b.WorkingTreeDiff(nil)
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]model.DiffFile, len(files))
	for _, f := range files {
		got[f.DisplayPath()] = f
	}
	return got
}

// TestRenameDetectionExact covers the case the feature exists for: a file
// that moved without changing would otherwise be read twice, once as a
// whole-file deletion and once as a whole-file addition, for content that
// is identical.
func TestRenameDetectionExact(t *testing.T) {
	oldRoot, newRoot := renameTrees(t,
		map[string]string{"src/foo.js": "line1\nline2\nline3\nline4\n"},
		map[string]string{"lib/foo.js": "line1\nline2\nline3\nline4\n"})

	got := diffByPath(t, oldRoot, newRoot)
	if len(got) != 1 {
		t.Fatalf("got %d entries %v, want a single rename", len(got), got)
	}
	f, ok := got["lib/foo.js"]
	if !ok {
		t.Fatalf("want the entry under its new path, got %v", got)
	}
	if f.Status != model.StatusRenamed {
		t.Errorf("Status = %q, want %q", f.Status, model.StatusRenamed)
	}
	if f.OldPath == nil || *f.OldPath != "src/foo.js" {
		t.Errorf("OldPath = %v, want src/foo.js", f.OldPath)
	}
	// A pure move has nothing to read: the headers carry the whole change.
	if adds, dels := f.Stat(); adds != 0 || dels != 0 {
		t.Errorf("stat = +%d -%d, want a pure move to show no line changes", adds, dels)
	}
}

// TestRenameDetectionSimilar is the case exact matching cannot reach, and
// the reason the similarity pass exists: a dependency that reorganises its
// layout usually edits the moved files too.
func TestRenameDetectionSimilar(t *testing.T) {
	oldRoot, newRoot := renameTrees(t,
		map[string]string{"src/foo.js": "line1\nline2\nline3\nline4\n"},
		map[string]string{"lib/foo.js": "line1\nCHANGED\nline3\nline4\n"})

	got := diffByPath(t, oldRoot, newRoot)
	f, ok := got["lib/foo.js"]
	if !ok {
		t.Fatalf("want a single renamed entry, got %v", got)
	}
	if f.Status != model.StatusRenamed {
		t.Errorf("Status = %q, want %q", f.Status, model.StatusRenamed)
	}
	if f.OldPath == nil || *f.OldPath != "src/foo.js" {
		t.Errorf("OldPath = %v, want src/foo.js", f.OldPath)
	}
	// The edit is still reviewable — a rename must not swallow the diff.
	if adds, dels := f.Stat(); adds != 1 || dels != 1 {
		t.Errorf("stat = +%d -%d, want +1 -1", adds, dels)
	}
}

func TestRenameDetectionLimits(t *testing.T) {
	t.Run("wholly different files stay an add and a delete", func(t *testing.T) {
		oldRoot, newRoot := renameTrees(t,
			map[string]string{"gone.txt": "alpha\nbravo\ncharlie\n"},
			map[string]string{"fresh.txt": "one\ntwo\nthree\n"})

		got := diffByPath(t, oldRoot, newRoot)
		if got["gone.txt"].Status != model.StatusDeleted {
			t.Errorf("gone.txt = %q, want deleted", got["gone.txt"].Status)
		}
		if got["fresh.txt"].Status != model.StatusAdded {
			t.Errorf("fresh.txt = %q, want added", got["fresh.txt"].Status)
		}
	})

	// Every empty file is byte-identical to every other, so pairing them
	// would be arbitrary and would tell the reviewer nothing.
	t.Run("empty files are never called renames", func(t *testing.T) {
		oldRoot, newRoot := renameTrees(t,
			map[string]string{"a.txt": "", "b.txt": ""},
			map[string]string{"x.txt": "", "y.txt": ""})

		got := diffByPath(t, oldRoot, newRoot)
		for path, f := range got {
			if f.Status == model.StatusRenamed {
				t.Errorf("%s was called a rename", path)
			}
		}
	})

	// An exact match is certain, so it must not be lost to a merely similar
	// candidate that happened to be considered first.
	t.Run("an exact match wins over a similar one", func(t *testing.T) {
		const content = "alpha\nbravo\ncharlie\ndelta\n"
		oldRoot, newRoot := renameTrees(t,
			map[string]string{"src/thing.txt": content},
			map[string]string{
				"a-similar.txt": "alpha\nbravo\ncharlie\nCHANGED\n",
				"z-exact.txt":   content,
			})

		got := diffByPath(t, oldRoot, newRoot)
		f, ok := got["z-exact.txt"]
		if !ok || f.Status != model.StatusRenamed {
			t.Fatalf("want the exact match claimed as the rename, got %v", got)
		}
		if got["a-similar.txt"].Status != model.StatusAdded {
			t.Errorf("a-similar.txt = %q, want added", got["a-similar.txt"].Status)
		}
	})

	// Detection must be stable: an unstable pairing would reshuffle the
	// review between runs of the same command.
	t.Run("pairing is deterministic", func(t *testing.T) {
		oldTree := map[string]string{"o1.txt": "alpha\nbravo\n", "o2.txt": "alpha\nbravo\n"}
		newTree := map[string]string{"n1.txt": "alpha\nbravo\n", "n2.txt": "alpha\nbravo\n"}

		var first string
		for range 5 {
			oldRoot, newRoot := renameTrees(t, oldTree, newTree)
			got := diffByPath(t, oldRoot, newRoot)
			var summary []string
			for path, f := range got {
				summary = append(summary, path+"="+string(f.Status))
				if f.OldPath != nil {
					summary[len(summary)-1] += "<-" + *f.OldPath
				}
			}
			sort.Strings(summary)
			joined := strings.Join(summary, ",")
			if first == "" {
				first = joined
			} else if joined != first {
				t.Fatalf("pairing changed between runs: %q then %q", first, joined)
			}
		}
	})
}

// TestFilesModeIsNeverARename guards the distinction the whole NoRenameHeaders
// option exists for: naming two files on the command line is a request to
// compare them, not a claim that one became the other.
func TestFilesModeIsNeverARename(t *testing.T) {
	dir := t.TempDir()
	oldFile := write(t, dir, "old/before.txt", "one\ntwo\n")
	newFile := write(t, dir, "new/after.txt", "one\n2\n")

	b, err := New(oldFile, newFile, vcs.WhitespaceNormal)
	if err != nil {
		t.Fatal(err)
	}
	files, err := b.WorkingTreeDiff(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	if files[0].Status != model.StatusModified {
		t.Errorf("Status = %q, want %q", files[0].Status, model.StatusModified)
	}
}

func TestSimilarity(t *testing.T) {
	tests := []struct {
		name   string
		a, b   string
		wantAt float64 // similarity must be at least this
		wantLt float64 // ...and below this
	}{
		{"identical", "a\nb\nc\n", "a\nb\nc\n", 1.0, 1.01},
		{"one line of four changed", "a\nb\nc\nd\n", "a\nX\nc\nd\n", 0.7, 0.8},
		{"half changed", "a\nb\nc\nd\n", "a\nb\nX\nY\n", 0.4, 0.6},
		{"nothing shared", "a\nb\n", "x\ny\n", 0.0, 0.01},
		{"repeats are counted, not deduped", "a\na\na\na\n", "a\n", 0.3, 0.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := similarity(lineCounts(tt.a), lineCounts(tt.b))
			if got < tt.wantAt || got >= tt.wantLt {
				t.Errorf("similarity = %v, want within [%v, %v)", got, tt.wantAt, tt.wantLt)
			}
		})
	}
}
