package diffparser

import (
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

// Git C-quotes a path in its diff headers when the path holds a non-ASCII
// byte (with core.quotePath, the default), a control character, a double
// quote or a backslash, and appends a bare tab to a ---/+++ path containing a
// space. The parsed paths must be the real ones: they name the file on disk
// for gap expansion, line counts and highlighting, and on the forge for
// comment positions.

func TestQuotedPathsAreUnquoted(t *testing.T) {
	cases := []struct {
		name             string
		diff             string
		oldPath, newPath string
		status           model.FileStatus
	}{
		{
			name: "non-ASCII",
			diff: "diff --git \"a/caf\\303\\251.go\" \"b/caf\\303\\251.go\"\n" +
				"--- \"a/caf\\303\\251.go\"\n+++ \"b/caf\\303\\251.go\"\n@@ -1 +1 @@\n-a\n+b\n",
			oldPath: "café.go", newPath: "café.go", status: model.StatusModified,
		},
		{
			name: "embedded quote",
			diff: "diff --git \"a/quo\\\"te.txt\" \"b/quo\\\"te.txt\"\n" +
				"--- \"a/quo\\\"te.txt\"\n+++ \"b/quo\\\"te.txt\"\n@@ -1 +1 @@\n-a\n+b\n",
			oldPath: `quo"te.txt`, newPath: `quo"te.txt`, status: model.StatusModified,
		},
		{
			name: "space with git's trailing tab",
			diff: "diff --git a/sp ace.txt b/sp ace.txt\n" +
				"--- a/sp ace.txt\t\n+++ b/sp ace.txt\t\n@@ -1 +1 @@\n-a\n+b\n",
			oldPath: "sp ace.txt", newPath: "sp ace.txt", status: model.StatusModified,
		},
		{
			name: "pure rename quoting only the new side",
			diff: "diff --git a/plain.txt \"b/pla\\303\\256n.txt\"\nsimilarity index 100%\n" +
				"rename from plain.txt\nrename to \"pla\\303\\256n.txt\"\n",
			oldPath: "plain.txt", newPath: "plaîn.txt", status: model.StatusRenamed,
		},
		{
			name:    "mode-only change falls back to the quoted diff --git line",
			diff:    "diff --git \"a/caf\\303\\251.sh\" \"b/caf\\303\\251.sh\"\nold mode 100644\nnew mode 100755\n",
			oldPath: "café.sh", newPath: "café.sh", status: model.StatusModified,
		},
		{
			name:    "binary",
			diff:    "diff --git \"a/\\303\\251.png\" \"b/\\303\\251.png\"\nBinary files \"a/\\303\\251.png\" and \"b/\\303\\251.png\" differ\n",
			oldPath: "é.png", newPath: "é.png", status: model.StatusModified,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files, err := Parse(tc.diff, GitStyle, nil)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if len(files) != 1 {
				t.Fatalf("got %d files, want 1", len(files))
			}
			f := files[0]
			if f.OldPath == nil || *f.OldPath != tc.oldPath {
				t.Errorf("old path = %v, want %q", deref(f.OldPath), tc.oldPath)
			}
			if f.NewPath == nil || *f.NewPath != tc.newPath {
				t.Errorf("new path = %v, want %q", deref(f.NewPath), tc.newPath)
			}
			if f.Status != tc.status {
				t.Errorf("status = %v, want %v", f.Status, tc.status)
			}
		})
	}
}

func TestSplitPathPair(t *testing.T) {
	cases := []struct {
		in, sep, anchor string
		left, right     string
		ok              bool
	}{
		{`a/x b/x`, " ", "b/", `a/x`, `b/x`, true},
		{`a/x y b/x y`, " ", "b/", `a/x y`, `b/x y`, true},
		{`"a/\303" "b/\303"`, " ", "b/", `"a/\303"`, `"b/\303"`, true},
		{`a/p "b/\303"`, " ", "b/", `a/p`, `"b/\303"`, true},
		{`"a/\"q" b/q`, " ", "b/", `"a/\"q"`, `b/q`, true},
		{`/dev/null and "b/\303"`, " and ", "", `/dev/null`, `"b/\303"`, true},
		{`"unterminated`, " ", "b/", "", "", false},
		{`no separator`, " ", "b/", "", "", false},
	}
	for _, tc := range cases {
		left, right, ok := splitPathPair(tc.in, tc.sep, tc.anchor)
		if ok != tc.ok || left != tc.left || right != tc.right {
			t.Errorf("splitPathPair(%q) = %q, %q, %v; want %q, %q, %v",
				tc.in, left, right, ok, tc.left, tc.right, tc.ok)
		}
	}
}

func deref(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}
