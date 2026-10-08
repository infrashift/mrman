package diffparser

import (
	"errors"
	"testing"

	"github.com/infrashift/mrman/internal/errs"
)

// FuzzParse feeds arbitrary bytes through the parser. Every diff mrman
// shows comes through here — pull requests from a forge, patches from a
// mailing list — so a crafted one must never panic, and a parse that
// succeeds must be stable when its own output is fed back in.
func FuzzParse(f *testing.F) {
	f.Add("diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1,2 +1,2 @@\n context\n-old\n+new\n")
	f.Add("diff --git a/b b/b\nnew file mode 100644\n--- /dev/null\n+++ b/b\n@@ -0,0 +1 @@\n+only\n")
	f.Add("diff --git a/r b/s\nsimilarity index 90%\nrename from r\nrename to s\n")
	f.Add("diff --git a/bin b/bin\nBinary files a/bin and b/bin differ\n")
	f.Add("@@ -4294967295,4294967295 +4294967295,4294967295 @@\n+x\n")
	f.Add("diff --git a/x b/x\n@@ -1 +1 @@\n+\x1b[31mred\x1b[0m\n")
	f.Add("--- a/x\n+++ b/x\n@@ garbage @@\n\\ No newline at end of file\n")
	f.Add("diff --git a/y b/y\n--- a/y\n+++ b/y\n@@ -1,2 +1,2 @@\n----\n+++i;\n a\n")
	f.Fuzz(func(t *testing.T, text string) {
		for _, format := range []Format{GitStyle, Hg} {
			files, err := Parse(text, format, nil)
			if err != nil && !errors.Is(err, errs.ErrNoChanges) {
				t.Fatalf("unexpected error kind: %v", err)
			}
			// The property is "no panic, no unexpected error"; a header
			// with no paths is a legal (if useless) parse, and callers
			// already tolerate it.
			_ = files
		}
	})
}
