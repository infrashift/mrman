package diffparser

import (
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

// Inside a counted hunk a body line is content whatever follows its prefix.
// Deleting a YAML document marker ("---" becomes "----"), deleting an SQL or
// Lua comment ("-- x" becomes "--- x") and adding "++i;" ("+++i;") all start
// with the same three characters as a file header. Skipping them dropped the
// row and shifted every later line number on that side of the hunk, which is
// what comments, HunkReviewKey and forge positions are keyed on.

func TestCountedHunkKeepsHeaderLookalikeRows(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		header  string
		origins []model.LineOrigin
		content []string
	}{
		{
			name:    "deleted YAML document marker",
			header:  "@@ -1,3 +1,2 @@",
			body:    "----\n a: 1\n b: 2\n",
			origins: []model.LineOrigin{model.OriginDeletion, model.OriginContext, model.OriginContext},
			content: []string{"---", "a: 1", "b: 2"},
		},
		{
			name:    "deleted SQL comment",
			header:  "@@ -1,2 +1,1 @@",
			body:    "--- note\n select 1;\n",
			origins: []model.LineOrigin{model.OriginDeletion, model.OriginContext},
			content: []string{"-- note", "select 1;"},
		},
		{
			name:    "added pre-increment",
			header:  "@@ -1,1 +1,2 @@",
			body:    "+++i;\n tail\n",
			origins: []model.LineOrigin{model.OriginAddition, model.OriginContext},
			content: []string{"++i;", "tail"},
		},
		{
			name:    "added and deleted lookalikes in one hunk",
			header:  "@@ -1,3 +1,3 @@",
			body:    " head\n----\n++++\n tail\n",
			origins: []model.LineOrigin{model.OriginContext, model.OriginDeletion, model.OriginAddition, model.OriginContext},
			content: []string{"head", "---", "+++", "tail"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text := "diff --git a/f b/f\n--- a/f\n+++ b/f\n" + tc.header + "\n" + tc.body
			h := parseOneHunk(t, text)
			if len(h.Lines) != len(tc.origins) {
				t.Fatalf("got %d rows, want %d: %+v", len(h.Lines), len(tc.origins), h.Lines)
			}
			for i, l := range h.Lines {
				if l.Origin != tc.origins[i] || l.Content != tc.content[i] {
					t.Errorf("row %d = %v %q, want %v %q", i, l.Origin, l.Content, tc.origins[i], tc.content[i])
				}
			}
		})
	}
}

// TestHeaderLookalikeKeepsLaterLineNumbers pins the consequence that reaches
// disk: the row after a deleted "---" keeps its real old-side number.
func TestHeaderLookalikeKeepsLaterLineNumbers(t *testing.T) {
	h := parseOneHunk(t, "diff --git a/f.yaml b/f.yaml\n--- a/f.yaml\n+++ b/f.yaml\n"+
		"@@ -10,3 +10,2 @@\n----\n a: 1\n b: 2\n")
	last := h.Lines[len(h.Lines)-1]
	if last.OldLineno == nil || *last.OldLineno != 12 {
		t.Errorf("old line of %q = %v, want 12", last.Content, last.OldLineno)
	}
	if last.NewLineno == nil || *last.NewLineno != 11 {
		t.Errorf("new line of %q = %v, want 11", last.Content, last.NewLineno)
	}
}

// TestUncountedHunkStillSkipsHeaderLines keeps the guard where it is still
// needed: a hunk whose header could not be measured has no budget to end it,
// so it reads to the next structural marker, and stray "---"/"+++" header
// lines on the way are not diff rows.
func TestUncountedHunkStillSkipsHeaderLines(t *testing.T) {
	h := parseOneHunk(t, "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1,bad +1,bad @@\n-old\n+new\n--- a/y\n+++ b/y\n")
	if len(h.Lines) != 2 {
		t.Fatalf("got %d rows, want 2 (old, new): %+v", len(h.Lines), h.Lines)
	}
}
