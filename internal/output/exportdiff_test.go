package output

import (
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

// twoHunkDiff has two hunks whose old and new numbering diverge, so a test can
// tell a lookup that reads the side it was told from one that guesses.
//
// Hunk 1 carries old 10-12 and new 10-13; hunk 2 carries old 40-41 and new
// 41-43.
const twoHunkDiff = "diff --git a/src/main.rs b/src/main.rs\n" +
	"index 1234567..89abcde 100644\n" +
	"--- a/src/main.rs\n" +
	"+++ b/src/main.rs\n" +
	"@@ -10,3 +10,4 @@ fn main() {\n" +
	"     let a = 1;\n" +
	"-    let b = 2;\n" +
	"+    let b = 3;\n" +
	"+    let c = 4;\n" +
	"     println!(\"{}\", a);\n" +
	"@@ -40,2 +41,3 @@ fn helper() {\n" +
	"     let x = 0;\n" +
	"+    let y = 1;\n" +
	"     x\n"

// firstHunk and secondHunk are twoHunkDiff's hunks as the export should quote
// them: the header verbatim, then every line with its prefix intact.
const firstHunk = "@@ -10,3 +10,4 @@ fn main() {\n" +
	"     let a = 1;\n" +
	"-    let b = 2;\n" +
	"+    let b = 3;\n" +
	"+    let c = 4;\n" +
	"     println!(\"{}\", a);"

const secondHunk = "@@ -40,2 +41,3 @@ fn helper() {\n" +
	"     let x = 0;\n" +
	"+    let y = 1;\n" +
	"     x"

// diffSession builds a working-tree session over the given paths.
func diffSession(t *testing.T, paths ...string) *model.ReviewSession {
	t.Helper()
	s := newSession()
	for _, p := range paths {
		s.AddFile(p, model.StatusModified, 0)
	}
	return s
}

// addLine adds a line comment on the given side and returns it.
func addLine(t *testing.T, s *model.ReviewSession, path string, line uint32,
	side model.LineSide, body string) *model.Comment {
	t.Helper()
	c := model.NewComment(body, model.CommentTypeFromID("note"), &side)
	s.Files[path].AddLineComment(line, c)
	return c
}

// renderDiffExport renders the default template with diffs turned on.
func renderDiffExport(t *testing.T, s *model.ReviewSession, files []model.DiffFile) string {
	t.Helper()
	return renderExportWith(t, s, files, true)
}

// renderExportWith renders the default template, choosing whether diffs are
// quoted. It is the diff-aware sibling of renderDefault.
func renderExportWith(t *testing.T, s *model.ReviewSession, files []model.DiffFile, include bool) string {
	t.Helper()
	tmpl, warnings := LoadNotesTemplate("")
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings loading default template: %v", warnings)
	}
	data, err := BuildTemplateData(s, files, "", ExportOptions{
		CommentTypes: testLegend(),
		IncludeDiff:  include,
		// Unbadged, so these tests stay about the quoted hunk. See renderDefault.
		Author: AuthorVisibility{Username: model.DefaultAuthor},
	})
	if err != nil {
		t.Fatalf("BuildTemplateData: %v", err)
	}
	out, err := RenderNotes(tmpl, data)
	if err != nil {
		t.Fatalf("RenderNotes: %v", err)
	}
	return out
}

// fenced is the block the export should wrap a quoted hunk in.
func fenced(hunk string) string {
	return "```diff\n" + hunk + "\n```"
}

// TestExportDiffOffIsByteIdentical is the regression guard for every existing
// user: handing BuildTemplateData a diff must change nothing until the
// export_diff setting asks it to.
func TestExportDiffOffIsByteIdentical(t *testing.T) {
	files := parseFixture(t, twoHunkDiff)
	s := diffSession(t, "src/main.rs")
	addLine(t, s, "src/main.rs", 11, model.LineSideNew, "Magic number")

	withDiff := renderExportWith(t, s, files, false)
	withoutFiles := renderExportWith(t, s, nil, false)

	if withDiff != withoutFiles {
		t.Errorf("passing a diff changed the output with IncludeDiff off:\ngot:\n%q\nwant:\n%q",
			withDiff, withoutFiles)
	}
	if strings.Contains(withDiff, "```") {
		t.Errorf("unexpected fence with IncludeDiff off:\n%s", withDiff)
	}
}

func TestExportDiffQuotesContainingHunk(t *testing.T) {
	files := parseFixture(t, twoHunkDiff)
	s := diffSession(t, "src/main.rs")
	addLine(t, s, "src/main.rs", 11, model.LineSideNew, "Magic number")

	got := renderDiffExport(t, s, files)

	want := "I reviewed your code and have the following comments. Please address them.\n" +
		"\n" +
		"## Local mrman Comments\n" +
		"\n" +
		"\n" +
		fenced(firstHunk) + "\n" +
		"\n" +
		"1. **[NOTE]** `src/main.rs:11` - Magic number\n"
	if got != want {
		t.Errorf("golden mismatch:\ngot:\n%q\nwant:\n%q", got, want)
	}
}

// TestExportDiffQuotesRangeHunk covers a whole-hunk comment, which the app
// stores as a line range keyed by its end line.
func TestExportDiffQuotesRangeHunk(t *testing.T) {
	files := parseFixture(t, twoHunkDiff)
	span, side, ok := files[0].Hunks[1].CommentSpan()
	if !ok {
		t.Fatal("second hunk has no comment span")
	}
	s := diffSession(t, "src/main.rs")
	c := addLine(t, s, "src/main.rs", span.End, side, "Whole hunk")
	c.LineRange = &span

	got := renderDiffExport(t, s, files)

	if !strings.Contains(got, fenced(secondHunk)) {
		t.Errorf("range comment did not quote its hunk:\n%s", got)
	}
	if !strings.Contains(got, "`src/main.rs:41-43`") {
		t.Errorf("range anchor lost:\n%s", got)
	}
}

// TestExportDiffResolvesOldSide checks the lookup reads the side the comment
// was written on. Old line 11 is a deletion in the first hunk; new line 11 is
// an addition in the same hunk, so only a diff whose old/new numbering
// diverges would catch a lookup that read the wrong one — hence the second
// hunk's old 40, which is new 41.
func TestExportDiffResolvesOldSide(t *testing.T) {
	files := parseFixture(t, twoHunkDiff)
	s := diffSession(t, "src/main.rs")
	addLine(t, s, "src/main.rs", 40, model.LineSideOld, "Deleted context")

	got := renderDiffExport(t, s, files)

	if !strings.Contains(got, fenced(secondHunk)) {
		t.Errorf("old-side comment did not resolve to the second hunk:\n%s", got)
	}
	if !strings.Contains(got, "`src/main.rs:~40`") {
		t.Errorf("old-side anchor lost:\n%s", got)
	}
}

// TestExportDiffQuotesSharedHunkOnce keeps a run of comments in one hunk from
// repeating it, while a comment in a different hunk still gets its own block.
func TestExportDiffQuotesSharedHunkOnce(t *testing.T) {
	files := parseFixture(t, twoHunkDiff)
	s := diffSession(t, "src/main.rs")
	addLine(t, s, "src/main.rs", 11, model.LineSideNew, "First")
	addLine(t, s, "src/main.rs", 12, model.LineSideNew, "Second")
	addLine(t, s, "src/main.rs", 42, model.LineSideNew, "Third")

	got := renderDiffExport(t, s, files)

	if n := strings.Count(got, firstHunk); n != 1 {
		t.Errorf("first hunk quoted %d times, want 1:\n%s", n, got)
	}
	if n := strings.Count(got, secondHunk); n != 1 {
		t.Errorf("second hunk quoted %d times, want 1:\n%s", n, got)
	}
	// The suppressed comment still lists, immediately under the shared block.
	if !strings.Contains(got, "1. **[NOTE]** `src/main.rs:11` - First\n2. **[NOTE]** `src/main.rs:12` - Second\n") {
		t.Errorf("shared-hunk comments should list together:\n%s", got)
	}
}

// TestExportDiffRepeatsHunkAfterAnother checks the suppression is only of
// consecutive repeats. It drives the quoter directly: reaching this ordering
// through a session needs a diff whose old and new numbering have drifted far
// enough for the ascending line keys to revisit a hunk, which the mechanism
// itself does not care about.
func TestExportDiffRepeatsHunkAfterAnother(t *testing.T) {
	files := parseFixture(t, twoHunkDiff)
	q := newHunkQuoter(files, true)
	side := model.LineSideNew
	c := model.NewComment("x", model.CommentTypeFromID("note"), &side)

	if got := q.quote("src/main.rs", 11, c); got != firstHunk {
		t.Fatalf("first quote:\ngot:\n%q\nwant:\n%q", got, firstHunk)
	}
	if got := q.quote("src/main.rs", 42, c); got != secondHunk {
		t.Fatalf("second quote:\ngot:\n%q\nwant:\n%q", got, secondHunk)
	}
	if got := q.quote("src/main.rs", 12, c); got != firstHunk {
		t.Errorf("a hunk revisited after another must quote again, got:\n%q", got)
	}
}

// TestExportDiffResetsBetweenFiles keeps the repeat suppression from reaching
// across a file boundary, where the hunk index would otherwise collide.
func TestExportDiffResetsBetweenFiles(t *testing.T) {
	files := parseFixture(t, twoHunkDiff+
		strings.ReplaceAll(twoHunkDiff, "src/main.rs", "src/other.rs"))
	s := diffSession(t, "src/main.rs", "src/other.rs")
	addLine(t, s, "src/main.rs", 11, model.LineSideNew, "First file")
	addLine(t, s, "src/other.rs", 11, model.LineSideNew, "Second file")

	got := renderDiffExport(t, s, files)

	if n := strings.Count(got, firstHunk); n != 2 {
		t.Errorf("each file should quote its own hunk, got %d blocks:\n%s", n, got)
	}
}

// TestExportDiffSkipsUnquotableComments covers everything that has no hunk to
// show: review and file comments, which are not anchored to one, and files the
// diff cannot supply.
func TestExportDiffSkipsUnquotableComments(t *testing.T) {
	files := parseFixture(t, twoHunkDiff)

	t.Run("review_and_file_comments", func(t *testing.T) {
		s := diffSession(t, "src/main.rs")
		s.ReviewComments = append(s.ReviewComments, model.NewComment(
			"Overall", model.CommentTypeFromID("note"), nil))
		s.Files["src/main.rs"].AddFileComment(model.NewComment(
			"Whole file", model.CommentTypeFromID("note"), nil))

		got := renderDiffExport(t, s, files)

		if strings.Contains(got, "```") {
			t.Errorf("unanchored comments must not quote a hunk:\n%s", got)
		}
		if !strings.Contains(got, "2. **[NOTE]** `src/main.rs` - Whole file") {
			t.Errorf("file comment lost its bare path reference:\n%s", got)
		}
	})

	unquotable := []struct {
		name  string
		files []model.DiffFile
	}{
		{"absent", nil},
		{"binary", []model.DiffFile{{NewPath: strPtr("src/main.rs"), IsBinary: true}}},
		{"too_large", []model.DiffFile{{NewPath: strPtr("src/main.rs"), IsTooLarge: true}}},
		{"no_hunks", []model.DiffFile{{NewPath: strPtr("src/main.rs")}}},
		{"line_gone", files},
	}
	for _, tc := range unquotable {
		t.Run(tc.name, func(t *testing.T) {
			line := uint32(11)
			if tc.name == "line_gone" {
				line = 999
			}
			s := diffSession(t, "src/main.rs")
			addLine(t, s, "src/main.rs", line, model.LineSideNew, "Note")

			got := renderDiffExport(t, s, tc.files)

			if strings.Contains(got, "```") {
				t.Errorf("expected no fence:\n%s", got)
			}
			if !strings.Contains(got, "- Note\n") {
				t.Errorf("comment must still be listed:\n%s", got)
			}
		})
	}
}

// TestExportDiffKeepsSeriesPatchesApart covers a patch series touching one
// path twice: each comment must quote the hunk from the patch it was written
// on, not whichever entry sorted first.
func TestExportDiffKeepsSeriesPatchesApart(t *testing.T) {
	files := parseFixture(t, twoHunkDiff)
	first := files[0]
	first.CommitID = "aaa1111"
	first.Hunks = first.Hunks[:1]

	second := files[0]
	second.CommitID = "bbb2222"
	second.Hunks = files[0].Hunks[1:]

	s := diffSession(t, "src/main.rs")
	c := addLine(t, s, "src/main.rs", 42, model.LineSideNew, "Second patch")
	commit := "bbb2222"
	c.CommitID = &commit

	got := renderDiffExport(t, s, []model.DiffFile{first, second})

	if !strings.Contains(got, fenced(secondHunk)) {
		t.Errorf("comment did not quote its own patch's hunk:\n%s", got)
	}
	if strings.Contains(got, firstHunk) {
		t.Errorf("quoted the other patch's hunk:\n%s", got)
	}
}

// TestExportDiffWidensFenceAroundEmbeddedFence stops diff content that itself
// contains a code fence from breaking out of the block.
func TestExportDiffWidensFenceAroundEmbeddedFence(t *testing.T) {
	fixture := "diff --git a/README.md b/README.md\n" +
		"--- a/README.md\n" +
		"+++ b/README.md\n" +
		"@@ -1,1 +1,2 @@\n" +
		" intro\n" +
		"+```\n"
	files := parseFixture(t, fixture)
	s := diffSession(t, "README.md")
	addLine(t, s, "README.md", 2, model.LineSideNew, "Stray fence")

	got := renderDiffExport(t, s, files)

	if !strings.Contains(got, "````diff\n") || !strings.Contains(got, "\n````\n") {
		t.Errorf("embedded fence should widen the block to four backticks:\n%s", got)
	}
}
