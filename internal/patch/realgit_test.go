package patch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs/diffparser"
)

// TestRealFormatPatchSeries feeds genuine `git format-patch --stdout` output
// through the reader.
//
// Checked-in fixtures can only encode shapes I thought to write down; this
// asserts against whatever git actually emits — signature, diffstat,
// changelog, encoded headers and all. Skipped when git is unavailable.
func TestRealFormatPatchSeries(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Dev Eloper", "GIT_AUTHOR_EMAIL=dev@example.org",
			"GIT_COMMITTER_NAME=Dev Eloper", "GIT_COMMITTER_EMAIL=dev@example.org",
			"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z")
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
	// Tab-indented, like the kernel C this feature exists for.
	write("foo.c", "int main(void)\n{\n\treturn 0;\n}\n")
	git("add", ".")
	git("commit", "-q", "-m", "base")

	write("foo.c", "int main(void)\n{\n\tint ret = 0;\n\treturn ret;\n}\n")
	git("add", ".")
	git("commit", "-q", "-m", "foo: use a local\n\nThe return value wants a name.\n\n"+
		"Changes since v1:\n- renamed the local\n\nSigned-off-by: Dev Eloper <dev@example.org>\n")

	// A second patch touching the same file — the collision case.
	write("foo.c", "int main(void)\n{\n\tint ret = 1;\n\treturn ret;\n}\n")
	git("add", ".")
	git("commit", "-q", "-m", "foo: adjust the value\n\nSigned-off-by: Dev Eloper <dev@example.org>\n")

	out, err := exec.Command("git", "-C", dir, "format-patch", "--stdout", "HEAD~2..HEAD").Output()
	if err != nil {
		t.Fatalf("format-patch: %v", err)
	}

	series, err := Load(string(out), Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if series.Kind != KindMbox {
		t.Errorf("Kind = %v, want KindMbox", series.Kind)
	}
	if series.Len() != 2 {
		t.Fatalf("got %d patches, want 2", series.Len())
	}

	// Each patch is its own review target, both touching foo.c.
	for i, p := range series.Patches {
		if p.SeriesPos != i+1 || p.SeriesLen != 2 {
			t.Errorf("patch %d: position %d/%d, want %d/2", i, p.SeriesPos, p.SeriesLen, i+1)
		}
		if !strings.Contains(p.Author, "Dev Eloper") {
			t.Errorf("patch %d: Author = %q", i, p.Author)
		}
		// Plain `git format-patch` writes no Message-Id — only --thread does,
		// or mail that has actually been sent. A review reply can therefore
		// only be threaded when the patch came from a list, which is why the
		// exporter must never invent one.
		if p.MessageID != "" {
			t.Errorf("patch %d: unthreaded format-patch should carry no Message-Id, got %q",
				i, p.MessageID)
		}
		if p.Date.IsZero() {
			t.Errorf("patch %d: Date was not parsed", i)
		}
		if !strings.Contains(p.Changelog, "Signed-off-by") {
			t.Errorf("patch %d: trailer missing from the changelog:\n%s", i, p.Changelog)
		}
		if strings.Contains(p.Changelog, "file changed") {
			t.Errorf("patch %d: diffstat leaked into the changelog:\n%s", i, p.Changelog)
		}

		files, err := diffparser.Parse(p.DiffText, diffparser.GitStyle, nil)
		if err != nil {
			t.Fatalf("patch %d: parse: %v\n%s", i, err, p.DiffText)
		}
		if len(files) != 1 || files[0].DisplayPath() != "foo.c" {
			t.Fatalf("patch %d: parsed %d files, want just foo.c", i, len(files))
		}

		// Nothing from around the diff may have become diff content.
		for _, h := range files[0].Hunks {
			for _, l := range h.Lines {
				switch {
				case l.Content == "2.43.0" || strings.HasPrefix(l.Content, "- "):
					t.Errorf("patch %d: signature leaked as %q", i, l.Content)
				case strings.Contains(l.Content, "file changed"):
					t.Errorf("patch %d: diffstat leaked as %q", i, l.Content)
				case strings.Contains(l.Content, "renamed the local"):
					t.Errorf("patch %d: changelog leaked as %q", i, l.Content)
				}
			}
		}
	}

	// The changelog of patch 1 keeps the bullet that looks like a deletion.
	if !strings.Contains(series.Patches[0].Changelog, "- renamed the local") {
		t.Errorf("the changelog bullet belongs in the changelog:\n%s", series.Patches[0].Changelog)
	}

	// Tabs survive the whole path, which the export half depends on.
	if !strings.Contains(series.Patches[0].DiffText, "\t") {
		t.Error("tabs were lost between git and the normalised diff")
	}
}

// TestRealThreadedSeriesCarriesMessageIDs covers `format-patch --thread`,
// which is what a contributor about to `git send-email` actually runs — and
// the only locally-produced artifact a review reply can thread against.
//
// It also pins the header-name casing: git spells it "Message-ID" while mail
// clients spell it "Message-Id", so the reader must not be case-sensitive.
func TestRealThreadedSeriesCarriesMessageIDs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Dev", "GIT_AUTHOR_EMAIL=dev@example.org",
			"GIT_COMMITTER_NAME=Dev", "GIT_COMMITTER_EMAIL=dev@example.org")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	git("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.c"), []byte("int a;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-q", "-m", "base")
	if err := os.WriteFile(filepath.Join(dir, "a.c"), []byte("int a;\nint b;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-q", "-m", "add b")

	out, err := exec.Command("git", "-C", dir, "format-patch", "--thread",
		"--stdout", "HEAD~1..HEAD").Output()
	if err != nil {
		t.Fatalf("format-patch --thread: %v", err)
	}
	if !strings.Contains(string(out), "Message-ID:") {
		t.Skip("this git spells the header differently; nothing to assert")
	}

	series, err := Load(string(out), Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if series.Patches[0].MessageID == "" {
		t.Error("a threaded series must yield a Message-Id to reply against")
	}
	if strings.ContainsAny(series.Patches[0].MessageID, "<>") {
		t.Errorf("MessageID should have its angle brackets removed, got %q",
			series.Patches[0].MessageID)
	}
}

// TestRealNoPrefixDiff covers `git diff --no-prefix`, whose paths carry no
// a//b/ and would be corrupted by a blind one-component strip.
func TestRealNoPrefixDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) []byte {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return out
	}

	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "thing.c"), []byte("int x;\nint z;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "base")
	if err := os.WriteFile(filepath.Join(dir, "thing.c"), []byte("int x;\nint y;\nint z;\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := run("diff", "--no-prefix")
	series, err := Load(string(out), Options{StripLevel: -1})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	files, err := diffparser.Parse(series.Patches[0].DiffText, diffparser.GitStyle, nil)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, series.Patches[0].DiffText)
	}
	if len(files) != 1 || files[0].DisplayPath() != "thing.c" {
		t.Errorf("parsed paths = %v, want [thing.c]", pathsOf(files))
	}
}

func pathsOf(files []model.DiffFile) []string {
	out := make([]string, 0, len(files))
	for i := range files {
		out = append(out, files[i].DisplayPath())
	}
	return out
}
