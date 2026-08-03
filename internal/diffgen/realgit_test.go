package diffgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs/diffparser"
)

// TestMatchesRealGit uses git as an oracle for this package's Myers output.
//
// mrman does not shell out to `git diff --no-index` for two-path reviews —
// it ignores .gitignore, mangles absolute paths, and returns exit 1 both
// when files differ and when it cannot open them. But git is still the
// reference implementation of what a correct diff of two blobs looks like,
// so both are parsed and the resulting models compared.
//
// The comparison is on the parsed model, never the raw text: index lines,
// mode lines and the exact placement of "\ No newline at end of file"
// legitimately differ between the two producers without either being wrong.
func TestMatchesRealGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	tests := []struct {
		name             string
		oldText, newText string
	}{
		{"replace in the middle", "one\ntwo\nthree\n", "one\n2\nthree\n"},
		{"pure insert", "one\ntwo\n", "one\ninserted\ntwo\n"},
		{"pure delete", "one\ntwo\nthree\n", "one\nthree\n"},
		{"append at the end", "one\n", "one\ntwo\nthree\n"},
		{"prepend at the start", "one\n", "zero\none\n"},
		{"empty to content", "", "hello\nworld\n"},
		{"content to empty", "hello\nworld\n", ""},
		{"no trailing newline on the new side", "a\nb\n", "a\nb"},
		{"no trailing newline on the old side", "a\nb", "a\nb\n"},
		{"crlf against lf", "a\r\nb\r\n", "a\nb\n"},
		{"every line changed", "a\nb\nc\n", "x\ny\nz\n"},
		{"hunks far enough apart to split", "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n", "X\n2\n3\n4\n5\n6\n7\n8\n9\nY\n"},
		{"blank lines preserved", "a\n\n\nb\n", "a\n\nb\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			oldFile := filepath.Join(dir, "old.txt")
			newFile := filepath.Join(dir, "new.txt")
			if err := os.WriteFile(oldFile, []byte(tt.oldText), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(newFile, []byte(tt.newText), 0o644); err != nil {
				t.Fatal(err)
			}

			// --no-index exits 1 when the files differ, so the error is
			// deliberately ignored and the output judged on its own.
			out, _ := exec.Command("git", "diff", "--no-index",
				"--src-prefix=a/", "--dst-prefix=b/", oldFile, newFile).Output()

			// Rename headers are suppressed on both sides: git does not emit
			// them for --no-index on a plain pair, and old.txt/new.txt are
			// two things being compared, not one file's history.
			ours := UnifiedFileDiff("old.txt", "new.txt", tt.oldText, tt.newText,
				Options{NoRenameHeaders: true})

			gitFiles := parseOrEmpty(t, string(out))
			ourFiles := parseOrEmpty(t, ours)

			if len(gitFiles) != len(ourFiles) {
				t.Fatalf("file count: git %d, diffgen %d\ngit:\n%s\ndiffgen:\n%s",
					len(gitFiles), len(ourFiles), out, ours)
			}
			if len(gitFiles) == 0 {
				return // both agree there is nothing to show
			}
			assertSameDiff(t, &gitFiles[0], &ourFiles[0], string(out), ours)
		})
	}
}

// parseOrEmpty parses diff text, treating "no changes" as zero files rather
// than an error so identical inputs compare cleanly.
func parseOrEmpty(t *testing.T, text string) []model.DiffFile {
	t.Helper()
	if text == "" {
		return nil
	}
	files, err := diffparser.Parse(text, diffparser.GitStyle, nil)
	if err != nil {
		return nil
	}
	return files
}

// assertSameDiff compares the reviewable substance of two parsed diffs:
// hunk structure, per-line origins and the line numbers the gutter shows.
func assertSameDiff(t *testing.T, want, got *model.DiffFile, wantText, gotText string) {
	t.Helper()

	wantAdds, wantDels := want.Stat()
	gotAdds, gotDels := got.Stat()
	if wantAdds != gotAdds || wantDels != gotDels {
		t.Errorf("stat: git +%d -%d, diffgen +%d -%d", wantAdds, wantDels, gotAdds, gotDels)
	}
	if len(want.Hunks) != len(got.Hunks) {
		t.Fatalf("hunk count: git %d, diffgen %d\ngit:\n%s\ndiffgen:\n%s",
			len(want.Hunks), len(got.Hunks), wantText, gotText)
	}

	for hi := range want.Hunks {
		wh, gh := &want.Hunks[hi], &got.Hunks[hi]
		if wh.OldStart != gh.OldStart || wh.OldCount != gh.OldCount ||
			wh.NewStart != gh.NewStart || wh.NewCount != gh.NewCount {
			t.Errorf("hunk %d header: git @@ -%d,%d +%d,%d @@, diffgen @@ -%d,%d +%d,%d @@",
				hi, wh.OldStart, wh.OldCount, wh.NewStart, wh.NewCount,
				gh.OldStart, gh.OldCount, gh.NewStart, gh.NewCount)
			continue
		}
		if len(wh.Lines) != len(gh.Lines) {
			t.Errorf("hunk %d line count: git %d, diffgen %d", hi, len(wh.Lines), len(gh.Lines))
			continue
		}
		for li := range wh.Lines {
			wl, gl := &wh.Lines[li], &gh.Lines[li]
			if wl.Origin != gl.Origin || wl.Content != gl.Content {
				t.Errorf("hunk %d line %d: git (%v, %q), diffgen (%v, %q)",
					hi, li, wl.Origin, wl.Content, gl.Origin, gl.Content)
			}
			if !sameLineno(wl.OldLineno, gl.OldLineno) || !sameLineno(wl.NewLineno, gl.NewLineno) {
				t.Errorf("hunk %d line %d: line numbers differ (old %v/%v, new %v/%v)",
					hi, li, deref(wl.OldLineno), deref(gl.OldLineno),
					deref(wl.NewLineno), deref(gl.NewLineno))
			}
		}
	}
}

func sameLineno(a, b *uint32) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func deref(p *uint32) any {
	if p == nil {
		return nil
	}
	return *p
}
