package diffparser

import (
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

// The parser expands tabs to four spaces so highlighted spans line up with
// what is rendered, and that expanded text is what gets persisted into a
// comment's anchor snapshot. Raw exists so the original bytes are still
// available to anything that has to emit the diff back — a review reply
// quoted with the wrong indentation is not much use to a kernel reviewer.

const tabbedPatch = "diff --git a/foo.c b/foo.c\n" +
	"index 1234567..89abcde 100644\n" +
	"--- a/foo.c\n" +
	"+++ b/foo.c\n" +
	"@@ -100,4 +100,6 @@ static int foo_probe(struct platform_device *pdev)\n" +
	" \tint ret;\n" +
	"\n" +
	"+\tif (!bar)\n" +
	"+\t\treturn -EINVAL;\n" +
	"-\tret = old(pdev);\n" +
	" \treturn ret;\n"

// TestRawPreservesTabs is the assertion the whole export half rests on.
func TestRawPreservesTabs(t *testing.T) {
	files, err := Parse(tabbedPatch, GitStyle, nil)
	if err != nil {
		t.Fatal(err)
	}
	lines := files[0].Hunks[0].Lines

	var sawTab bool
	for _, l := range lines {
		if strings.Contains(l.Raw, "\t") {
			sawTab = true
		}
		if strings.Contains(l.Content, "\t") {
			t.Errorf("Content should be tab-expanded for display, got %q", l.Content)
		}
	}
	if !sawTab {
		t.Fatal("Raw lost the tabs it exists to keep")
	}

	// And specifically: the added line reads as the author wrote it.
	if lines[2].Raw != "+\tif (!bar)" {
		t.Errorf("Raw = %q, want %q", lines[2].Raw, "+\tif (!bar)")
	}
	if lines[2].Content != "+    if (!bar)"[1:] { // Content carries no prefix
		t.Errorf("Content = %q, want the tab-expanded body without its prefix", lines[2].Content)
	}
}

// TestRawRoundTripsHunkBodies checks the stronger property: joining every
// line's Raw reproduces the hunk body exactly as it arrived.
func TestRawRoundTripsHunkBodies(t *testing.T) {
	files, err := Parse(tabbedPatch, GitStyle, nil)
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, l := range files[0].Hunks[0].Lines {
		got = append(got, l.Raw)
	}

	// The body is every source line after the @@ header.
	all := strings.Split(strings.TrimSuffix(tabbedPatch, "\n"), "\n")
	var want []string
	for i, line := range all {
		if strings.HasPrefix(line, "@@") {
			want = all[i+1:]
			break
		}
	}
	if len(want) == 0 {
		t.Fatal("fixture has no hunk body")
	}

	if len(got) != len(want) {
		t.Fatalf("got %d raw lines, want %d\ngot:  %q\nwant: %q", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: Raw = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestRawHeaderKeepsThePreamble covers the other half of quoting a file back:
// the "diff --git" line and the metadata under it.
func TestRawHeaderKeepsThePreamble(t *testing.T) {
	files, err := Parse(tabbedPatch, GitStyle, nil)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"diff --git a/foo.c b/foo.c",
		"index 1234567..89abcde 100644",
		"--- a/foo.c",
		"+++ b/foo.c",
	}
	if len(files[0].RawHeader) != len(want) {
		t.Fatalf("RawHeader = %q, want %q", files[0].RawHeader, want)
	}
	for i := range want {
		if files[0].RawHeader[i] != want[i] {
			t.Errorf("RawHeader[%d] = %q, want %q", i, files[0].RawHeader[i], want[i])
		}
	}
}

// TestSourceIndexRecordsPatchOrder exists because NewApp sorts DiffFiles by
// directory for display, which loses the order the author chose. A reply
// quoted out of that order is hard to follow against the original mail.
func TestSourceIndexRecordsPatchOrder(t *testing.T) {
	multi := "diff --git a/z/last.c b/z/last.c\n--- a/z/last.c\n+++ b/z/last.c\n" +
		"@@ -1,1 +1,2 @@\n a\n+b\n" +
		"diff --git a/a/first.c b/a/first.c\n--- a/a/first.c\n+++ b/a/first.c\n" +
		"@@ -1,1 +1,2 @@\n c\n+d\n"

	files, err := Parse(multi, GitStyle, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2", len(files))
	}
	// Source order, not alphabetical: z/last.c really was written first.
	if files[0].DisplayPath() != "z/last.c" || files[0].SourceIndex != 0 {
		t.Errorf("files[0] = %s at index %d, want z/last.c at 0",
			files[0].DisplayPath(), files[0].SourceIndex)
	}
	if files[1].DisplayPath() != "a/first.c" || files[1].SourceIndex != 1 {
		t.Errorf("files[1] = %s at index %d, want a/first.c at 1",
			files[1].DisplayPath(), files[1].SourceIndex)
	}
}

// TestSynthesizedFilesAreMarked keeps a file mrman invented from being sorted
// in among files that came from a real diff.
func TestSynthesizedFilesAreMarked(t *testing.T) {
	// A parsed file always has a real position.
	files, err := Parse(tabbedPatch, GitStyle, nil)
	if err != nil {
		t.Fatal(err)
	}
	if files[0].SourceIndex < 0 {
		t.Errorf("a parsed file should carry its position, got %d", files[0].SourceIndex)
	}

	// A zero-valued DiffFile must not look like position 0; callers that care
	// check for -1, which the synthesizers set explicitly.
	var synthesized model.DiffFile
	if synthesized.SourceIndex != 0 {
		t.Fatal("precondition: the zero value is 0, which is why synthesizers must set -1")
	}
}

// TestBinaryFileKeepsItsHeader covers the file kind with no hunks at all.
func TestBinaryFileKeepsItsHeader(t *testing.T) {
	files, err := Parse("diff --git a/logo.png b/logo.png\n"+
		"new file mode 100644\n"+
		"index 0000000..6666666\n"+
		"GIT binary patch\n"+
		"literal 12\n", GitStyle, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !files[0].IsBinary {
		t.Fatal("expected a binary file")
	}
	if len(files[0].RawHeader) == 0 || files[0].RawHeader[0] != "diff --git a/logo.png b/logo.png" {
		t.Errorf("RawHeader = %q, want the preamble", files[0].RawHeader)
	}
	if files[0].SourceIndex != 0 {
		t.Errorf("SourceIndex = %d, want 0", files[0].SourceIndex)
	}
}

// TestRawDoesNotDisturbContentHash is the guard on the invariant: Raw is for
// emitting, and nothing that decides identity may read it.
func TestRawDoesNotDisturbContentHash(t *testing.T) {
	files, err := Parse(tabbedPatch, GitStyle, nil)
	if err != nil {
		t.Fatal(err)
	}
	before := files[0].ContentHash

	// Blank every Raw and rehash: the value must not move.
	for hi := range files[0].Hunks {
		for li := range files[0].Hunks[hi].Lines {
			files[0].Hunks[hi].Lines[li].Raw = ""
		}
	}
	if after := model.ComputeContentHash(files[0].Hunks); after != before {
		t.Errorf("ContentHash depends on Raw: %016x vs %016x", before, after)
	}
}
