package diffparser

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

// TestFormatPatchMatchesPlainDiff is the strongest statement available about
// the hunk budget: for the same commits, `git format-patch` output and
// `git diff` output must parse to the same hunks.
//
// format-patch wraps each commit in mail headers, a changelog, a diffstat and
// a trailing "-- \n<version>" signature. `git diff` has none of that. If any
// of it leaks into a hunk, the two disagree — and the disagreement lands in
// ContentHash, which decides whether a reviewed file counts as changed.
//
// Skipped when git is unavailable, matching TestParseRealGitOutput.
func TestFormatPatchMatchesPlainDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git("init", "-q", "-b", "main")
	// Tab-indented, so the fixture looks like the kernel C this feature targets.
	write("foo.c", "int main(void)\n{\n\treturn 0;\n}\n")
	write("bar.c", "void bar(void)\n{\n}\n")
	git("add", ".")
	git("commit", "-q", "-m", "base")

	// Two commits, the second touching a file the first also touched — the
	// series shape that used to swallow inter-patch junk.
	write("foo.c", "int main(void)\n{\n\tint ret = 0;\n\treturn ret;\n}\n")
	git("add", ".")
	git("commit", "-q", "-m", "first: rework main\n\nChanges since v1:\n- use a local\n- drop the cast\n")

	write("foo.c", "int main(void)\n{\n\tint ret = 1;\n\treturn ret;\n}\n")
	write("bar.c", "void bar(void)\n{\n\t/* now with a body */\n}\n")
	git("add", ".")
	git("commit", "-q", "-m", "second: adjust\n\n- another bullet that looks like a deletion\n")

	// The two views of the same two commits.
	plain := git("diff", "HEAD~2", "HEAD")
	series := git("format-patch", "--stdout", "HEAD~2..HEAD")

	if !strings.Contains(series, "\n-- \n") {
		t.Fatal("precondition: format-patch output should carry a signature")
	}

	fromPlain, err := Parse(plain, GitStyle, nil)
	if err != nil {
		t.Fatalf("parse git diff: %v", err)
	}
	fromSeries, err := Parse(series, GitStyle, nil)
	if err != nil {
		t.Fatalf("parse format-patch: %v", err)
	}

	// The series touches foo.c twice (once per patch) and bar.c once, so it
	// yields three file entries against the squashed diff's two. Compare the
	// union of hunk content per path instead of entry counts.
	plainRows := rowsByPath(t, fromPlain)
	seriesRows := rowsByPath(t, fromSeries)

	for path, want := range plainRows {
		got, ok := seriesRows[path]
		if !ok {
			t.Errorf("%s missing from the format-patch parse", path)
			continue
		}
		// Every row the squashed diff produced must appear in the series, and
		// the series must contribute no row that is not real diff content.
		for _, row := range want {
			if !containsRow(got, row) {
				t.Errorf("%s: row %q missing from the format-patch parse", path, row)
			}
		}
	}

	// No row anywhere may be a signature, a diffstat entry or a changelog
	// bullet. This is the assertion that fails without the hunk budget.
	for path, rows := range seriesRows {
		for _, row := range rows {
			switch {
			case row == "- ", strings.HasPrefix(row, "-- "):
				t.Errorf("%s: signature leaked into a hunk as %q", path, row)
			case strings.Contains(row, "file changed"), strings.Contains(row, "insertion"):
				t.Errorf("%s: diffstat leaked into a hunk as %q", path, row)
			case strings.HasSuffix(row, "another bullet that looks like a deletion"),
				strings.HasSuffix(row, "use a local"), strings.HasSuffix(row, "drop the cast"):
				t.Errorf("%s: changelog bullet leaked into a hunk as %q", path, row)
			}
		}
	}

	// And the tabs the fixture was written with survive, since that is what
	// the export half of this feature has to quote.
	var sawTab bool
	for _, rows := range seriesRows {
		for _, row := range rows {
			if strings.Contains(row, "    ") { // Tabify turns \t into 4 spaces
				sawTab = true
			}
		}
	}
	if !sawTab {
		t.Error("expected the tab-indented fixture to survive into parsed rows")
	}
}

// rowsByPath renders each file's hunk rows as "<origin><content>" strings.
func rowsByPath(t *testing.T, files []model.DiffFile) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for i := range files {
		path := files[i].DisplayPath()
		for _, h := range files[i].Hunks {
			for _, l := range h.Lines {
				var marker string
				switch l.Origin {
				case model.OriginAddition:
					marker = "+"
				case model.OriginDeletion:
					marker = "-"
				default:
					marker = " "
				}
				out[path] = append(out[path], marker+l.Content)
			}
		}
	}
	return out
}

func containsRow(rows []string, want string) bool {
	for _, r := range rows {
		if r == want {
			return true
		}
	}
	return false
}
