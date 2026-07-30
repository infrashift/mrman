package diffparser

import (
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

// A hunk's @@ header declares how many lines it spans on each side. Anything
// after that is not part of the diff — but almost every trailer a real patch
// carries starts with "-", "+" or " " and would be read as a diff row without
// a length budget. These tests pin the budget.
//
// The damage was never only cosmetic: a phantom row shifts every line number
// after it, and flows into ComputeContentHash and HunkReviewKey, which key the
// reviewed state a session persists.

// parseOneHunk parses text and returns the single file's single hunk.
func parseOneHunk(t *testing.T, text string) model.DiffHunk {
	t.Helper()
	files, err := Parse(text, GitStyle, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	if len(files[0].Hunks) != 1 {
		t.Fatalf("got %d hunks, want 1", len(files[0].Hunks))
	}
	return files[0].Hunks[0]
}

// originCounts tallies a hunk's rows by origin, which is what a phantom row
// shows up in.
func originCounts(h model.DiffHunk) (add, del, ctx int) {
	for _, l := range h.Lines {
		switch l.Origin {
		case model.OriginAddition:
			add++
		case model.OriginDeletion:
			del++
		case model.OriginContext:
			ctx++
		}
	}
	return add, del, ctx
}

const budgetedHunk = `diff --git a/foo.c b/foo.c
--- a/foo.c
+++ b/foo.c
@@ -1,2 +1,3 @@
 context
+added
 tail
`

// TestHunkStopsAtFormatPatchSignature is the headline case: every file
// produced by `git format-patch` ends with "-- \n<version>\n\n". The "---"
// guard in the body loop does not catch "-- ", so it used to become a
// deletion row and consume an old-side line number.
func TestHunkStopsAtFormatPatchSignature(t *testing.T) {
	h := parseOneHunk(t, budgetedHunk+"-- \n2.43.0\n\n")

	add, del, ctx := originCounts(h)
	if del != 0 {
		t.Errorf("deletion rows = %d, want 0 — the signature is not a diff line", del)
	}
	if add != 1 || ctx != 2 {
		t.Errorf("got %d additions / %d context, want 1 / 2", add, ctx)
	}
	if got := len(h.Lines); got != 3 {
		t.Errorf("hunk has %d rows, want 3", got)
	}
}

// TestHunkStopsAtDiffstat covers the mbox case: between one patch's last hunk
// and the next patch's "diff --git", a series carries a diffstat whose lines
// begin with a space, i.e. read as context rows.
func TestHunkStopsAtDiffstat(t *testing.T) {
	h := parseOneHunk(t, budgetedHunk+" foo.c | 3 ++-\n 1 file changed, 2 insertions(+), 1 deletion(-)\n")

	if _, _, ctx := originCounts(h); ctx != 2 {
		t.Errorf("context rows = %d, want 2 — the diffstat is not diff content", ctx)
	}
}

// TestHunkStopsAtChangelogBullet covers the other half of the mbox case: a
// commit message bullet starting with "-" used to become a deletion.
func TestHunkStopsAtChangelogBullet(t *testing.T) {
	h := parseOneHunk(t, budgetedHunk+"\nChanges since v1:\n- rebased on next\n- fixed the errno\n")

	if _, del, _ := originCounts(h); del != 0 {
		t.Errorf("deletion rows = %d, want 0 — changelog bullets are not deletions", del)
	}
}

// TestHunkLineNumbersSurviveTrailingJunk is why the budget matters beyond row
// counts: a phantom row shifts the numbering that comments anchor to.
func TestHunkLineNumbersSurviveTrailingJunk(t *testing.T) {
	clean := parseOneHunk(t, budgetedHunk)
	trailing := parseOneHunk(t, budgetedHunk+"-- \n2.43.0\n\n")

	if len(clean.Lines) != len(trailing.Lines) {
		t.Fatalf("row count differs: %d vs %d", len(clean.Lines), len(trailing.Lines))
	}
	for i := range clean.Lines {
		a, b := clean.Lines[i], trailing.Lines[i]
		if a.Content != b.Content || a.Origin != b.Origin {
			t.Errorf("row %d differs: %q/%v vs %q/%v", i, a.Content, a.Origin, b.Content, b.Origin)
		}
		if !linenoEq(a.OldLineno, b.OldLineno) || !linenoEq(a.NewLineno, b.NewLineno) {
			t.Errorf("row %d line numbers differ", i)
		}
	}
}

func linenoEq(a, b *uint32) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// TestTrailingJunkDoesNotChangeContentHash is the consequence that reaches
// disk. ContentHash decides whether a previously reviewed file counts as
// changed; a phantom row silently unreviews it.
func TestTrailingJunkDoesNotChangeContentHash(t *testing.T) {
	clean, err := Parse(budgetedHunk, GitStyle, nil)
	if err != nil {
		t.Fatal(err)
	}
	trailing, err := Parse(budgetedHunk+"-- \n2.43.0\n\n", GitStyle, nil)
	if err != nil {
		t.Fatal(err)
	}
	if clean[0].ContentHash != trailing[0].ContentHash {
		t.Errorf("ContentHash changed with trailing junk: %016x vs %016x",
			clean[0].ContentHash, trailing[0].ContentHash)
	}
}

// TestMalformedHunkHeaderIsNotBudgeted guards the escape hatch. parseU32Or1
// falls back to 1 for anything unparsable, so budgeting a header we did not
// understand would truncate a hunk that parses fine today. Such a hunk must
// still be read to the next structural marker.
func TestMalformedHunkHeaderIsNotBudgeted(t *testing.T) {
	h := parseOneHunk(t, `diff --git a/foo.c b/foo.c
--- a/foo.c
+++ b/foo.c
@@ -x,y +z,w @@
 context
+added
 more
+another
`)

	if got := len(h.Lines); got != 4 {
		t.Errorf("hunk has %d rows, want 4 — a header we cannot parse must not "+
			"budget the body down to one line", got)
	}
}

// TestHunkStopsAtNextHunkBeforeBudgetSpent keeps the structural terminators
// authoritative when a header over-declares its length.
func TestHunkStopsAtNextHunkBeforeBudgetSpent(t *testing.T) {
	files, err := Parse(`diff --git a/foo.c b/foo.c
--- a/foo.c
+++ b/foo.c
@@ -1,99 +1,99 @@
 only one
@@ -50,1 +50,1 @@
 second hunk
`, GitStyle, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(files[0].Hunks); got != 2 {
		t.Fatalf("got %d hunks, want 2", got)
	}
	if got := len(files[0].Hunks[0].Lines); got != 1 {
		t.Errorf("first hunk has %d rows, want 1", got)
	}
}

// TestNoNewlineMarkerDoesNotSpendBudget covers the one body line that is
// neither side's content: "\ No newline at end of file".
func TestNoNewlineMarkerDoesNotSpendBudget(t *testing.T) {
	h := parseOneHunk(t, `diff --git a/foo.c b/foo.c
--- a/foo.c
+++ b/foo.c
@@ -1,1 +1,1 @@
-old
\ No newline at end of file
+new
\ No newline at end of file
`)

	add, del, _ := originCounts(h)
	if add != 1 || del != 1 {
		t.Errorf("got %d additions / %d deletions, want 1 / 1 — the no-newline "+
			"marker must not count against either side's budget", add, del)
	}
}

// TestWholeFileAdditionAndDeletion covers the zero-count edges, where one
// side's budget is spent before the body starts.
func TestWholeFileAdditionAndDeletion(t *testing.T) {
	added := parseOneHunk(t, `diff --git a/new.c b/new.c
new file mode 100644
--- /dev/null
+++ b/new.c
@@ -0,0 +1,3 @@
+one
+two
+three
`)
	if a, d, c := originCounts(added); a != 3 || d != 0 || c != 0 {
		t.Errorf("new file: got %d/%d/%d additions/deletions/context, want 3/0/0", a, d, c)
	}

	deleted := parseOneHunk(t, `diff --git a/gone.c b/gone.c
deleted file mode 100644
--- a/gone.c
+++ /dev/null
@@ -1,2 +0,0 @@
-one
-two
`)
	if a, d, c := originCounts(deleted); a != 0 || d != 2 || c != 0 {
		t.Errorf("deleted file: got %d/%d/%d additions/deletions/context, want 0/2/0", a, d, c)
	}
}

// TestMultiFilePatchWithSignatures is the shape `git format-patch --stdout`
// actually produces for a series: several files, junk between them.
func TestMultiFilePatchWithSignatures(t *testing.T) {
	text := strings.Join([]string{
		budgetedHunk,
		"-- \n2.43.0\n\n",
		"From 0000000000000000000000000000000000000000 Mon Sep 17 00:00:00 2001\n",
		"Subject: [PATCH 2/2] second\n\n",
		" bar.c | 1 +\n 1 file changed\n\n",
		"diff --git a/bar.c b/bar.c\n--- a/bar.c\n+++ b/bar.c\n@@ -1,1 +1,2 @@\n keep\n+new line\n",
		"-- \n2.43.0\n\n",
	}, "")

	files, err := Parse(text, GitStyle, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2", len(files))
	}
	for _, f := range files {
		if len(f.Hunks) != 1 {
			t.Fatalf("%s: got %d hunks, want 1", *f.NewPath, len(f.Hunks))
		}
		add, del, _ := originCounts(f.Hunks[0])
		if del != 0 {
			t.Errorf("%s: %d deletion rows, want 0", *f.NewPath, del)
		}
		if add != 1 {
			t.Errorf("%s: %d addition rows, want 1", *f.NewPath, add)
		}
	}
}
