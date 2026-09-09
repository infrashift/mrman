package diffparser

import (
	"errors"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/syntax"
)

func pathVal(t *testing.T, p *string) string {
	t.Helper()
	if p == nil {
		return "<nil>"
	}
	return *p
}

// checkPath compares a *string path against want, where "" means nil.
func checkPath(t *testing.T, label string, got *string, want string) {
	t.Helper()
	if want == "" {
		if got != nil {
			t.Errorf("%s = %q, want nil", label, *got)
		}
		return
	}
	if got == nil || *got != want {
		t.Errorf("%s = %s, want %q", label, pathVal(t, got), want)
	}
}

func checkLineno(t *testing.T, label string, got *uint32, want int) {
	t.Helper()
	if want < 0 {
		if got != nil {
			t.Errorf("%s = %d, want nil", label, *got)
		}
		return
	}
	if got == nil {
		t.Errorf("%s = nil, want %d", label, want)
		return
	}
	if *got != uint32(want) {
		t.Errorf("%s = %d, want %d", label, *got, want)
	}
}

func TestParseHunkHeader(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		ok       bool
		oldStart uint32
		oldCount uint32
		newStart uint32
		newCount uint32
	}{
		{name: "basic", line: "@@ -1,3 +1,4 @@", ok: true, oldStart: 1, oldCount: 3, newStart: 1, newCount: 4},
		{name: "with context", line: "@@ -10,5 +20,8 @@ context", ok: true, oldStart: 10, oldCount: 5, newStart: 20, newCount: 8},
		{name: "without count", line: "@@ -5 +10 @@", ok: true, oldStart: 5, oldCount: 1, newStart: 10, newCount: 1},
		{name: "not a header", line: "not a hunk header", ok: false},
		{name: "truncated", line: "@@ invalid", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, ok := parseHunkHeader(tt.line)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if !ok {
				return
			}
			if b.OldStart != tt.oldStart || b.OldCount != tt.oldCount ||
				b.NewStart != tt.newStart || b.NewCount != tt.newCount {
				t.Fatalf("got (%d,%d,%d,%d), want (%d,%d,%d,%d)",
					b.OldStart, b.OldCount, b.NewStart, b.NewCount,
					tt.oldStart, tt.oldCount, tt.newStart, tt.newCount)
			}
			// Every header in this table is well formed, so its counts are
			// trustworthy as a body-length budget.
			if !b.Counted {
				t.Errorf("Counted = false for a well-formed header %q", tt.line)
			}
		})
	}
}

func TestParseRange(t *testing.T) {
	tests := []struct {
		in    string
		start uint32
		count uint32
		ok    bool
	}{
		{in: "10,5", start: 10, count: 5, ok: true},
		{in: "1,100", start: 1, count: 100, ok: true},
		{in: "42", start: 42, count: 1, ok: true},
		{in: "1", start: 1, count: 1, ok: true},
		// Unparsable numbers still fall back to 1, but say so, so the caller
		// does not budget a hunk body against a number it invented.
		{in: "abc", start: 1, count: 1, ok: false},
		{in: "abc,def", start: 1, count: 1, ok: false},
		{in: "10,def", start: 10, count: 1, ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			start, count, ok := parseRange(tt.in)
			if start != tt.start || count != tt.count {
				t.Fatalf("parseRange(%q) = (%d,%d), want (%d,%d)", tt.in, start, count, tt.start, tt.count)
			}
			if ok != tt.ok {
				t.Errorf("parseRange(%q) ok = %v, want %v", tt.in, ok, tt.ok)
			}
		})
	}
}

func TestParseEmptyDiffReturnsNoChanges(t *testing.T) {
	for _, format := range []Format{Hg, GitStyle} {
		if _, err := Parse("", format, nil); !errors.Is(err, errs.ErrNoChanges) {
			t.Errorf("format %d: err = %v, want ErrNoChanges", format, err)
		}
	}
}

// wantFile describes the expected shape of one parsed DiffFile. Empty path
// strings mean nil.
type wantFile struct {
	oldPath  string
	newPath  string
	status   model.FileStatus
	isBinary bool
	hunks    int
}

func TestParseDiffs(t *testing.T) {
	tests := []struct {
		name   string
		format Format
		diff   string
		want   []wantFile
		check  func(t *testing.T, files []model.DiffFile)
	}{
		{
			name:   "hg simple diff",
			format: Hg,
			diff: "diff -r abc123 test.rs\n" +
				"--- a/test.rs\tThu Jan 01 00:00:00 1970 +0000\n" +
				"+++ b/test.rs\tThu Jan 01 00:00:00 1970 +0000\n" +
				"@@ -1,3 +1,4 @@\n" +
				" fn main() {\n" +
				"+    println!(\"hello\");\n" +
				"     println!(\"world\");\n" +
				" }\n",
			want: []wantFile{{oldPath: "test.rs", newPath: "test.rs", status: model.StatusModified, hunks: 1}},
			check: func(t *testing.T, files []model.DiffFile) {
				if got := len(files[0].Hunks[0].Lines); got != 4 {
					t.Errorf("lines = %d, want 4", got)
				}
			},
		},
		{
			name:   "hg tab expansion in hunk lines",
			format: Hg,
			diff: "diff -r abc123 test.rs\n" +
				"--- a/test.rs\tThu Jan 01 00:00:00 1970 +0000\n" +
				"+++ b/test.rs\tThu Jan 01 00:00:00 1970 +0000\n" +
				"@@ -1,2 +1,2 @@\n" +
				"-\told\n" +
				"+\tnew\n",
			want: []wantFile{{oldPath: "test.rs", newPath: "test.rs", status: model.StatusModified, hunks: 1}},
			check: func(t *testing.T, files []model.DiffFile) {
				lines := files[0].Hunks[0].Lines
				if lines[0].Content != "    old" || lines[1].Content != "    new" {
					t.Errorf("contents = %q, %q", lines[0].Content, lines[1].Content)
				}
				for i, l := range lines {
					if strings.ContainsRune(l.Content, '\t') {
						t.Errorf("line %d still contains a tab: %q", i, l.Content)
					}
				}
			},
		},
		{
			name:   "hg new file",
			format: Hg,
			diff: "diff -r 000000000000 new_file.rs\n" +
				"--- /dev/null\n" +
				"+++ b/new_file.rs\n" +
				"@@ -0,0 +1,2 @@\n" +
				"+fn new() {\n" +
				"+}\n",
			want: []wantFile{{newPath: "new_file.rs", status: model.StatusAdded, hunks: 1}},
		},
		{
			name:   "hg deleted file",
			format: Hg,
			diff: "diff -r abc123 old_file.rs\n" +
				"--- a/old_file.rs\n" +
				"+++ /dev/null\n" +
				"@@ -1,2 +0,0 @@\n" +
				"-fn old() {\n" +
				"-}\n",
			want: []wantFile{{oldPath: "old_file.rs", status: model.StatusDeleted, hunks: 1}},
		},
		{
			name:   "hg multiple files",
			format: Hg,
			diff: "diff -r abc123 file1.rs\n" +
				"--- a/file1.rs\n" +
				"+++ b/file1.rs\n" +
				"@@ -1,1 +1,2 @@\n" +
				" line1\n" +
				"+line2\n" +
				"diff -r abc123 file2.rs\n" +
				"--- a/file2.rs\n" +
				"+++ b/file2.rs\n" +
				"@@ -1,2 +1,1 @@\n" +
				" keep\n" +
				"-remove\n",
			want: []wantFile{
				{oldPath: "file1.rs", newPath: "file1.rs", status: model.StatusModified, hunks: 1},
				{oldPath: "file2.rs", newPath: "file2.rs", status: model.StatusModified, hunks: 1},
			},
		},
		{
			name:   "hg multiple hunks",
			format: Hg,
			diff: "diff -r abc123 multi.rs\n" +
				"--- a/multi.rs\n" +
				"+++ b/multi.rs\n" +
				"@@ -1,3 +1,4 @@\n" +
				" fn first() {\n" +
				"+    // added\n" +
				" }\n" +
				"\n" +
				"@@ -10,3 +11,4 @@\n" +
				" fn second() {\n" +
				"+    // also added\n" +
				" }\n",
			want: []wantFile{{oldPath: "multi.rs", newPath: "multi.rs", status: model.StatusModified, hunks: 2}},
			check: func(t *testing.T, files []model.DiffFile) {
				if files[0].Hunks[0].OldStart != 1 || files[0].Hunks[1].OldStart != 10 {
					t.Errorf("old starts = %d, %d, want 1, 10", files[0].Hunks[0].OldStart, files[0].Hunks[1].OldStart)
				}
			},
		},
		{
			name:   "hg renamed file",
			format: Hg,
			diff: "diff -r abc123 new_name.rs\n" +
				"rename from old_name.rs\n" +
				"rename to new_name.rs\n" +
				"--- a/old_name.rs\n" +
				"+++ b/new_name.rs\n" +
				"@@ -1,1 +1,1 @@\n" +
				"-old content\n" +
				"+new content\n",
			want: []wantFile{{oldPath: "old_name.rs", newPath: "new_name.rs", status: model.StatusRenamed, hunks: 1}},
		},
		{
			name:   "hg binary file",
			format: Hg,
			diff: "diff -r abc123 image.png\n" +
				"Binary file image.png has changed\n",
			want: []wantFile{{oldPath: "image.png", newPath: "image.png", status: model.StatusModified, isBinary: true}},
		},
		{
			name:   "hg renamed file without content changes",
			format: Hg,
			diff: "diff -r abc123 new_name.rs\n" +
				"rename from old_name.rs\n" +
				"rename to new_name.rs\n",
			want: []wantFile{{oldPath: "old_name.rs", newPath: "new_name.rs", status: model.StatusRenamed}},
		},
		{
			name:   "hg copied file without content changes",
			format: Hg,
			diff: "diff -r abc123 dest.rs\n" +
				"copy from source.rs\n" +
				"copy to dest.rs\n",
			want: []wantFile{{oldPath: "source.rs", newPath: "dest.rs", status: model.StatusCopied}},
		},
		{
			name:   "hg copied file with content changes",
			format: Hg,
			diff: "diff -r abc123 dest.rs\n" +
				"copy from source.rs\n" +
				"copy to dest.rs\n" +
				"--- a/source.rs\tThu Jan 01 00:00:00 1970 +0000\n" +
				"+++ b/dest.rs\tThu Jan 01 00:00:00 1970 +0000\n" +
				"@@ -1 +1,2 @@\n" +
				" original\n" +
				"+added line\n",
			want: []wantFile{{oldPath: "source.rs", newPath: "dest.rs", status: model.StatusCopied, hunks: 1}},
		},
		{
			name:   "hg no newline marker",
			format: Hg,
			diff: "diff -r abc123 no_newline.rs\n" +
				"--- a/no_newline.rs\n" +
				"+++ b/no_newline.rs\n" +
				"@@ -1,1 +1,1 @@\n" +
				"-old\n" +
				"\\ No newline at end of file\n" +
				"+new\n" +
				"\\ No newline at end of file\n",
			want: []wantFile{{oldPath: "no_newline.rs", newPath: "no_newline.rs", status: model.StatusModified, hunks: 1}},
			check: func(t *testing.T, files []model.DiffFile) {
				if got := len(files[0].Hunks[0].Lines); got != 2 {
					t.Errorf("lines = %d, want 2", got)
				}
			},
		},
		{
			name:   "hg line numbers",
			format: Hg,
			diff: "diff -r abc123 nums.rs\n" +
				"--- a/nums.rs\n" +
				"+++ b/nums.rs\n" +
				"@@ -5,4 +5,5 @@\n" +
				" context at 5\n" +
				"-deleted at 6\n" +
				"+added at 6\n" +
				"+added at 7\n" +
				" context at 7->8\n",
			want: []wantFile{{oldPath: "nums.rs", newPath: "nums.rs", status: model.StatusModified, hunks: 1}},
			check: func(t *testing.T, files []model.DiffFile) {
				lines := files[0].Hunks[0].Lines
				wantLines := []struct {
					origin model.LineOrigin
					oldLn  int
					newLn  int
				}{
					{origin: model.OriginContext, oldLn: 5, newLn: 5},
					{origin: model.OriginDeletion, oldLn: 6, newLn: -1},
					{origin: model.OriginAddition, oldLn: -1, newLn: 6},
					{origin: model.OriginAddition, oldLn: -1, newLn: 7},
					{origin: model.OriginContext, oldLn: 7, newLn: 8},
				}
				if len(lines) != len(wantLines) {
					t.Fatalf("lines = %d, want %d", len(lines), len(wantLines))
				}
				for i, w := range wantLines {
					if lines[i].Origin != w.origin {
						t.Errorf("line %d origin = %d, want %d", i, lines[i].Origin, w.origin)
					}
					checkLineno(t, "old lineno", lines[i].OldLineno, w.oldLn)
					checkLineno(t, "new lineno", lines[i].NewLineno, w.newLn)
				}
			},
		},
		{
			name:   "jj simple diff",
			format: GitStyle,
			diff: "diff --git a/file.txt b/file.txt\n" +
				"--- a/file.txt\n" +
				"+++ b/file.txt\n" +
				"@@ -1,3 +1,4 @@\n" +
				" line1\n" +
				"+added\n" +
				" line2\n" +
				" line3\n",
			want: []wantFile{{oldPath: "file.txt", newPath: "file.txt", status: model.StatusModified, hunks: 1}},
			check: func(t *testing.T, files []model.DiffFile) {
				if got := len(files[0].Hunks[0].Lines); got != 4 {
					t.Errorf("lines = %d, want 4", got)
				}
			},
		},
		{
			name:   "jj tab expansion in hunk lines",
			format: GitStyle,
			diff: "diff --git a/file.txt b/file.txt\n" +
				"--- a/file.txt\n" +
				"+++ b/file.txt\n" +
				"@@ -1,2 +1,2 @@\n" +
				"-\told\n" +
				"+\tnew\n",
			want: []wantFile{{oldPath: "file.txt", newPath: "file.txt", status: model.StatusModified, hunks: 1}},
			check: func(t *testing.T, files []model.DiffFile) {
				lines := files[0].Hunks[0].Lines
				if lines[0].Content != "    old" || lines[1].Content != "    new" {
					t.Errorf("contents = %q, %q", lines[0].Content, lines[1].Content)
				}
			},
		},
		{
			name:   "jj new file",
			format: GitStyle,
			diff: "diff --git a/new.txt b/new.txt\n" +
				"new file mode 100644\n" +
				"--- /dev/null\n" +
				"+++ b/new.txt\n" +
				"@@ -0,0 +1,2 @@\n" +
				"+line1\n" +
				"+line2\n",
			want: []wantFile{{newPath: "new.txt", status: model.StatusAdded, hunks: 1}},
		},
		{
			name:   "jj deleted file",
			format: GitStyle,
			diff: "diff --git a/old.txt b/old.txt\n" +
				"deleted file mode 100644\n" +
				"--- a/old.txt\n" +
				"+++ /dev/null\n" +
				"@@ -1,2 +0,0 @@\n" +
				"-line1\n" +
				"-line2\n",
			want: []wantFile{{oldPath: "old.txt", status: model.StatusDeleted, hunks: 1}},
		},
		{
			name:   "jj renamed file without content changes",
			format: GitStyle,
			diff: "diff --git a/old.txt b/new.txt\n" +
				"rename from old.txt\n" +
				"rename to new.txt\n",
			want: []wantFile{{oldPath: "old.txt", newPath: "new.txt", status: model.StatusRenamed}},
		},
		{
			name:   "jj renamed file with content changes",
			format: GitStyle,
			diff: "diff --git a/old.txt b/new.txt\n" +
				"rename from old.txt\n" +
				"rename to new.txt\n" +
				"--- a/old.txt\n" +
				"+++ b/new.txt\n" +
				"@@ -1 +1 @@\n" +
				"-old content\n" +
				"+new content\n",
			want: []wantFile{{oldPath: "old.txt", newPath: "new.txt", status: model.StatusRenamed, hunks: 1}},
		},
		{
			name:   "jj rename with similarity index",
			format: GitStyle,
			diff: "diff --git a/old.txt b/new.txt\n" +
				"similarity index 95%\n" +
				"rename from old.txt\n" +
				"rename to new.txt\n",
			want: []wantFile{{oldPath: "old.txt", newPath: "new.txt", status: model.StatusRenamed}},
		},
		{
			name:   "jj copied file without content changes",
			format: GitStyle,
			diff: "diff --git a/source.txt b/dest.txt\n" +
				"copy from source.txt\n" +
				"copy to dest.txt\n",
			want: []wantFile{{oldPath: "source.txt", newPath: "dest.txt", status: model.StatusCopied}},
		},
		{
			name:   "jj copied file with content changes",
			format: GitStyle,
			diff: "diff --git a/source.txt b/dest.txt\n" +
				"copy from source.txt\n" +
				"copy to dest.txt\n" +
				"--- a/source.txt\n" +
				"+++ b/dest.txt\n" +
				"@@ -1 +1,2 @@\n" +
				" original\n" +
				"+added line\n",
			want: []wantFile{{oldPath: "source.txt", newPath: "dest.txt", status: model.StatusCopied, hunks: 1}},
		},
		{
			name:   "jj binary file added",
			format: GitStyle,
			diff: "diff --git a/image.png b/image.png\n" +
				"new file mode 100644\n" +
				"index 0000000000..abc1234567\n" +
				"Binary files /dev/null and b/image.png differ\n",
			want: []wantFile{{newPath: "image.png", status: model.StatusAdded, isBinary: true}},
		},
		{
			name:   "jj binary file deleted",
			format: GitStyle,
			diff: "diff --git a/image.png b/image.png\n" +
				"deleted file mode 100644\n" +
				"index abc1234567..0000000000\n" +
				"Binary files a/image.png and /dev/null differ\n",
			want: []wantFile{{oldPath: "image.png", status: model.StatusDeleted, isBinary: true}},
		},
		{
			name:   "jj binary file modified",
			format: GitStyle,
			diff: "diff --git a/image.png b/image.png\n" +
				"index abc1234567..def7890123 100644\n" +
				"Binary files a/image.png and b/image.png differ\n",
			want: []wantFile{{oldPath: "image.png", newPath: "image.png", status: model.StatusModified, isBinary: true}},
		},
		{
			name:   "jj hunk context containing Binary is not a binary marker",
			format: GitStyle,
			diff: "diff --git a/f.txt b/f.txt\n" +
				"index 1111111..2222222 100644\n" +
				"--- a/f.txt\n" +
				"+++ b/f.txt\n" +
				"@@ -1,2 +1,3 @@ someBinaryThing(\n" +
				" aaa\n" +
				"+bbb\n",
			want: []wantFile{{oldPath: "f.txt", newPath: "f.txt", status: model.StatusModified, hunks: 1}},
			check: func(t *testing.T, files []model.DiffFile) {
				if got := files[0].Hunks[0].Header; got != "@@ -1,2 +1,3 @@ someBinaryThing(" {
					t.Errorf("header = %q, trailing context must be preserved", got)
				}
			},
		},
		{
			name:   "jj multiple files",
			format: GitStyle,
			diff: "diff --git a/a.txt b/a.txt\n" +
				"--- a/a.txt\n" +
				"+++ b/a.txt\n" +
				"@@ -1 +1 @@\n" +
				"-old\n" +
				"+new\n" +
				"diff --git a/b.txt b/b.txt\n" +
				"--- a/b.txt\n" +
				"+++ b/b.txt\n" +
				"@@ -1 +1 @@\n" +
				"-foo\n" +
				"+bar\n",
			want: []wantFile{
				{oldPath: "a.txt", newPath: "a.txt", status: model.StatusModified, hunks: 1},
				{oldPath: "b.txt", newPath: "b.txt", status: model.StatusModified, hunks: 1},
			},
		},
		{
			name:   "jj line numbers",
			format: GitStyle,
			diff: "diff --git a/file.txt b/file.txt\n" +
				"--- a/file.txt\n" +
				"+++ b/file.txt\n" +
				"@@ -5,4 +5,5 @@\n" +
				" context\n" +
				"-deleted\n" +
				"+added1\n" +
				"+added2\n" +
				" more\n",
			want: []wantFile{{oldPath: "file.txt", newPath: "file.txt", status: model.StatusModified, hunks: 1}},
			check: func(t *testing.T, files []model.DiffFile) {
				lines := files[0].Hunks[0].Lines
				wantOld := []int{5, 6, -1, -1, 7}
				wantNew := []int{5, -1, 6, 7, 8}
				for i := range lines {
					checkLineno(t, "old lineno", lines[i].OldLineno, wantOld[i])
					checkLineno(t, "new lineno", lines[i].NewLineno, wantNew[i])
				}
			},
		},
		{
			name:   "jj empty new file",
			format: GitStyle,
			diff: "diff --git a/empty.toml b/empty.toml\n" +
				"new file mode 100644\n" +
				"index 0000000000..e69de29bb2\n",
			want: []wantFile{{newPath: "empty.toml", status: model.StatusAdded}},
			check: func(t *testing.T, files []model.DiffFile) {
				if got := files[0].DisplayPath(); got != "empty.toml" {
					t.Errorf("DisplayPath = %q", got)
				}
			},
		},
		{
			name:   "jj mode only change",
			format: GitStyle,
			diff: "diff --git a/script.sh b/script.sh\n" +
				"old mode 100644\n" +
				"new mode 100755\n",
			want: []wantFile{{oldPath: "script.sh", newPath: "script.sh", status: model.StatusModified}},
			check: func(t *testing.T, files []model.DiffFile) {
				if got := files[0].DisplayPath(); got != "script.sh" {
					t.Errorf("DisplayPath = %q", got)
				}
			},
		},
		{
			name:   "jj paths with spaces in fallback header",
			format: GitStyle,
			diff: "diff --git a/my file.txt b/my file.txt\n" +
				"old mode 100644\n" +
				"new mode 100755\n",
			want: []wantFile{{oldPath: "my file.txt", newPath: "my file.txt", status: model.StatusModified}},
		},
		{
			name:   "git binary patch",
			format: GitStyle,
			diff: "diff --git a/image.bin b/image.bin\n" +
				"index 1234567..89abcde 100644\n" +
				"GIT binary patch\n" +
				"literal 4\n" +
				"LcmeZB000M*0RR91\n" +
				"\n" +
				"literal 4\n" +
				"LcmeZB000M*0RR91\n",
			want: []wantFile{{oldPath: "image.bin", newPath: "image.bin", status: model.StatusModified, isBinary: true}},
		},
		{
			name:   "malformed hunk header is skipped",
			format: GitStyle,
			diff: "diff --git a/x.txt b/x.txt\n" +
				"--- a/x.txt\n" +
				"+++ b/x.txt\n" +
				"@@ garbage\n" +
				"-old\n" +
				"+new\n" +
				"@@ -1 +1 @@\n" +
				"-a\n" +
				"+b\n",
			want: []wantFile{{oldPath: "x.txt", newPath: "x.txt", status: model.StatusModified, hunks: 1}},
			check: func(t *testing.T, files []model.DiffFile) {
				if got := files[0].Hunks[0].Header; got != "@@ -1 +1 @@" {
					t.Errorf("header = %q, want the second (valid) hunk", got)
				}
			},
		},
		{
			name:   "garbage before and between file headers is ignored",
			format: GitStyle,
			diff: "commit deadbeef\n" +
				"Author: someone\n" +
				"\n" +
				"diff --git a/a.txt b/a.txt\n" +
				"--- a/a.txt\n" +
				"+++ b/a.txt\n" +
				"@@ -1 +1 @@\n" +
				"-old\n" +
				"+new\n" +
				"random trailing junk\n",
			want: []wantFile{{oldPath: "a.txt", newPath: "a.txt", status: model.StatusModified, hunks: 1}},
		},
		{
			name:   "crlf line endings",
			format: GitStyle,
			diff: "diff --git a/a.txt b/a.txt\r\n" +
				"--- a/a.txt\r\n" +
				"+++ b/a.txt\r\n" +
				"@@ -1 +1 @@\r\n" +
				"-old\r\n" +
				"+new\r\n",
			want: []wantFile{{oldPath: "a.txt", newPath: "a.txt", status: model.StatusModified, hunks: 1}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, err := Parse(tt.diff, tt.format, nil)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if len(files) != len(tt.want) {
				t.Fatalf("files = %d, want %d", len(files), len(tt.want))
			}
			for i, w := range tt.want {
				f := &files[i]
				checkPath(t, "old path", f.OldPath, w.oldPath)
				checkPath(t, "new path", f.NewPath, w.newPath)
				if f.Status != w.status {
					t.Errorf("file %d status = %q, want %q", i, f.Status, w.status)
				}
				if f.IsBinary != w.isBinary {
					t.Errorf("file %d isBinary = %v, want %v", i, f.IsBinary, w.isBinary)
				}
				if len(f.Hunks) != w.hunks {
					t.Errorf("file %d hunks = %d, want %d", i, len(f.Hunks), w.hunks)
				}
				if f.IsTooLarge || f.IsCommitMessage {
					t.Errorf("file %d unexpectedly flagged too-large/commit-message", i)
				}
				if w.isBinary {
					if f.ContentHash != 0 {
						t.Errorf("file %d binary content hash = %d, want 0", i, f.ContentHash)
					}
				} else if got, want := f.ContentHash, model.ComputeContentHash(f.Hunks); got != want {
					t.Errorf("file %d content hash = %d, want %d", i, got, want)
				}
			}
			if tt.check != nil {
				tt.check(t, files)
			}
		})
	}
}

func TestParseKeepsHighlightingForInterleavedTypescriptHunk(t *testing.T) {
	diff := "diff --git a/file.ts b/file.ts\n" +
		"--- a/file.ts\n" +
		"+++ b/file.ts\n" +
		"@@ -1,3 +1,4 @@\n" +
		" const msg = getMsg(\n" +
		"-    \"old argument\"\n" +
		"+    \"new argument\",\n" +
		"+    { extra: true }\n" +
		" );\n"

	h := syntax.NewHighlighter("monokai", "#00230c", "#2d0000")
	files, err := Parse(diff, GitStyle, h)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	lines := files[0].Hunks[0].Lines
	if len(lines) != 5 {
		t.Fatalf("lines = %d, want 5", len(lines))
	}
	for i, line := range lines {
		if len(line.HighlightedSpans) == 0 {
			t.Errorf("line %d should retain highlighting", i)
			continue
		}
		wantBG := ""
		switch line.Origin {
		case model.OriginAddition:
			wantBG = "#00230c"
		case model.OriginDeletion:
			wantBG = "#2d0000"
		case model.OriginContext:
		}
		for _, span := range line.HighlightedSpans {
			if span.Style.BG != wantBG {
				t.Errorf("line %d span BG = %q, want %q", i, span.Style.BG, wantBG)
			}
		}
	}
}

func TestParseSkipsHighlightingForContainerGrammars(t *testing.T) {
	diff := "diff --git a/app.vue b/app.vue\n" +
		"--- a/app.vue\n" +
		"+++ b/app.vue\n" +
		"@@ -1 +1 @@\n" +
		"-<template>old</template>\n" +
		"+<template>new</template>\n"

	h := syntax.NewHighlighter("monokai", "#00230c", "#2d0000")
	files, err := Parse(diff, GitStyle, h)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for i, line := range files[0].Hunks[0].Lines {
		if line.HighlightedSpans != nil {
			t.Errorf("line %d should defer to the full-file highlight pass", i)
		}
	}
}

func linesIterator(lines []string) func() (string, bool, error) {
	i := 0
	return func() (string, bool, error) {
		if i >= len(lines) {
			return "", false, nil
		}
		line := lines[i]
		i++
		return line, true, nil
	}
}

func TestParseLinesStreaming(t *testing.T) {
	next := linesIterator([]string{
		"diff --git a/file.txt b/file.txt",
		"--- a/file.txt",
		"+++ b/file.txt",
		"@@ -1 +1 @@",
		"-old",
		"+new",
	})
	files, err := ParseLines(next, GitStyle, nil)
	if err != nil {
		t.Fatalf("ParseLines: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %d, want 1", len(files))
	}
	checkPath(t, "new path", files[0].NewPath, "file.txt")
	if got := len(files[0].Hunks[0].Lines); got != 2 {
		t.Errorf("lines = %d, want 2", got)
	}
}

func TestParseLinesEmptyStream(t *testing.T) {
	if _, err := ParseLines(linesIterator(nil), GitStyle, nil); !errors.Is(err, errs.ErrNoChanges) {
		t.Fatalf("err = %v, want ErrNoChanges", err)
	}
}

func TestParseLinesErrorMidStream(t *testing.T) {
	streamErr := errors.New("stream broke")
	lines := []string{
		"diff --git a/file.txt b/file.txt",
		"--- a/file.txt",
		"+++ b/file.txt",
		"@@ -1 +1 @@",
		"-old",
	}
	i := 0
	next := func() (string, bool, error) {
		if i >= len(lines) {
			return "", false, streamErr
		}
		line := lines[i]
		i++
		return line, true, nil
	}
	files, err := ParseLines(next, GitStyle, nil)
	if !errors.Is(err, streamErr) {
		t.Fatalf("err = %v, want %v", err, streamErr)
	}
	if files != nil {
		t.Fatalf("files = %v, want nil on stream error", files)
	}
}

func TestParseLinesErrorOnFirstRead(t *testing.T) {
	streamErr := errors.New("boom")
	next := func() (string, bool, error) { return "", false, streamErr }
	if _, err := ParseLines(next, GitStyle, nil); !errors.Is(err, streamErr) {
		t.Fatalf("err = %v, want %v", err, streamErr)
	}
}

func TestParseLinesErrorDuringHeaderParse(t *testing.T) {
	streamErr := errors.New("header read failed")
	lines := []string{
		"diff --git a/file.txt b/file.txt",
		"--- a/file.txt",
	}
	i := 0
	next := func() (string, bool, error) {
		if i >= len(lines) {
			return "", false, streamErr
		}
		line := lines[i]
		i++
		return line, true, nil
	}
	if _, err := ParseLines(next, GitStyle, nil); !errors.Is(err, streamErr) {
		t.Fatalf("err = %v, want %v", err, streamErr)
	}
}

func TestSplitDiffLinesForHighlighting(t *testing.T) {
	contents := []string{"ctx1", "del", "add", "ctx2"}
	origins := []model.LineOrigin{
		model.OriginContext, model.OriginDeletion, model.OriginAddition, model.OriginContext,
	}
	seq := splitDiffLinesForHighlighting(contents, origins)

	if want := []string{"ctx1", "del", "ctx2"}; !equalStrings(seq.oldLines, want) {
		t.Errorf("old lines = %v, want %v", seq.oldLines, want)
	}
	if want := []string{"ctx1", "add", "ctx2"}; !equalStrings(seq.newLines, want) {
		t.Errorf("new lines = %v, want %v", seq.newLines, want)
	}
	if want := []int{0, 1, -1, 2}; !equalInts(seq.oldLineIndices, want) {
		t.Errorf("old indices = %v, want %v", seq.oldLineIndices, want)
	}
	if want := []int{0, -1, 1, 2}; !equalInts(seq.newLineIndices, want) {
		t.Errorf("new indices = %v, want %v", seq.newLineIndices, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestParseBinaryFileLine(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		ok      bool
		oldPath string
		newPath string
	}{
		{name: "git modified", line: "Binary files a/img.png and b/img.png differ", ok: true, oldPath: "img.png", newPath: "img.png"},
		{name: "git added", line: "Binary files /dev/null and b/img.png differ", ok: true, newPath: "img.png"},
		{name: "git deleted", line: "Binary files a/img.png and /dev/null differ", ok: true, oldPath: "img.png"},
		{name: "hg changed", line: "Binary file img.png has changed", ok: true, oldPath: "img.png", newPath: "img.png"},
		{name: "missing differ suffix", line: "Binary files a/x and b/y", ok: false},
		{name: "missing and separator", line: "Binary files a/x differ", ok: false},
		{name: "missing hg suffix", line: "Binary file img.png", ok: false},
		{name: "unrelated", line: "GIT binary patch", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldPath, newPath, ok := parseBinaryFileLine(tt.line)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if !ok {
				return
			}
			checkPath(t, "old path", oldPath, tt.oldPath)
			checkPath(t, "new path", newPath, tt.newPath)
		})
	}
}

func TestParseDiffGitHeader(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		ok      bool
		oldPath string
		newPath string
	}{
		{name: "simple", line: "diff --git a/x.txt b/x.txt", ok: true, oldPath: "x.txt", newPath: "x.txt"},
		{name: "spaces in paths", line: "diff --git a/my file.txt b/my file.txt", ok: true, oldPath: "my file.txt", newPath: "my file.txt"},
		{name: "rename", line: "diff --git a/old.txt b/new.txt", ok: true, oldPath: "old.txt", newPath: "new.txt"},
		{name: "no b separator", line: "diff --git something", ok: false},
		{name: "not a git header", line: "diff -r abc file.txt", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldPath, newPath, ok := parseDiffGitHeader(tt.line)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if !ok {
				return
			}
			if oldPath != tt.oldPath || newPath != tt.newPath {
				t.Fatalf("got (%q, %q), want (%q, %q)", oldPath, newPath, tt.oldPath, tt.newPath)
			}
		})
	}
}

// TestParseScrubsTerminalControls pins that a diff is scrubbed on the way
// in: every backend and every forge feeds this parser, so a hostile line in
// a pull request or a mailed patch cannot restyle the terminal.
func TestParseScrubsTerminalControls(t *testing.T) {
	diff := "diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+\x1b[31mfoo\x1b[0m\x07\n"
	files, err := Parse(diff, GitStyle, nil)
	if err != nil {
		t.Fatal(err)
	}
	line := files[0].Hunks[0].Lines[1]
	if line.Content != "foo" || line.Raw != "+foo" {
		t.Fatalf("line = %+v, want the escapes and the bell gone", line)
	}
}
