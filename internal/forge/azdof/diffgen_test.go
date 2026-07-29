package azdof

import (
	"fmt"
	"strings"
	"testing"
)

func TestSplitLinesKeepEnds(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{"empty", "", nil},
		{"single with newline", "a\n", []string{"a\n"}},
		{"single without newline", "a", []string{"a"}},
		{"two lines", "a\nb\n", []string{"a\n", "b\n"}},
		{"missing final newline", "a\nb", []string{"a\n", "b"}},
		{"blank lines", "\n\n", []string{"\n", "\n"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitLinesKeepEnds(tt.content)
			if len(got) != len(tt.want) {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("line %d = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// script renders an edit script compactly for assertions: "=a -b +c".
func script(edits []edit) string {
	parts := make([]string, 0, len(edits))
	for _, e := range edits {
		marker := map[editKind]string{editEqual: "=", editDelete: "-", editInsert: "+"}[e.kind]
		parts = append(parts, marker+strings.TrimSuffix(e.text, "\n"))
	}
	return strings.Join(parts, " ")
}

func lines(names ...string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = n + "\n"
	}
	return out
}

func TestDiffLinesScripts(t *testing.T) {
	tests := []struct {
		name string
		a, b []string
		want string
	}{
		{"both empty", nil, nil, ""},
		{"equal", lines("a", "b"), lines("a", "b"), "=a =b"},
		{"replace middle", lines("a", "b", "c"), lines("a", "x", "c"), "=a -b +x =c"},
		{"insert middle", lines("a", "b"), lines("a", "x", "b"), "=a +x =b"},
		{"delete middle", lines("a", "x", "b"), lines("a", "b"), "=a -x =b"},
		{"append", lines("a"), lines("a", "b"), "=a +b"},
		{"prepend", lines("b"), lines("a", "b"), "+a =b"},
		{"all new", nil, lines("a", "b"), "+a +b"},
		{"all gone", lines("a", "b"), nil, "-a -b"},
		{"total rewrite", lines("a", "b"), lines("x", "y"), "-a -b +x +y"},
		{
			"myers prefers early deletes",
			lines("a", "b", "c", "a", "b", "b", "a"),
			lines("c", "b", "a", "b", "a", "c"),
			// The classic Myers example: an 8-edit-distance-optimal script.
			"-a -b =c +b =a =b -b =a +c",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := script(diffLines(tt.a, tt.b)); got != tt.want {
				t.Fatalf("script = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDiffLinesOptimality checks the edit script is minimal for a few pairs
// with known edit distances.
func TestDiffLinesOptimality(t *testing.T) {
	tests := []struct {
		a, b []string
		want int // minimal number of non-equal edits
	}{
		{lines("a", "b", "c"), lines("a", "b", "c"), 0},
		{lines("a", "b", "c"), lines("a", "x", "c"), 2},
		{lines("a", "b", "c", "a", "b", "b", "a"), lines("c", "b", "a", "b", "a", "c"), 5},
	}
	for _, tt := range tests {
		changes := 0
		for _, e := range diffLines(tt.a, tt.b) {
			if e.kind != editEqual {
				changes++
			}
		}
		if changes != tt.want {
			t.Errorf("edit distance for %v -> %v = %d, want %d", tt.a, tt.b, changes, tt.want)
		}
	}
}

// TestDiffLinesRoundTrip verifies the script replays a into b.
func TestDiffLinesRoundTrip(t *testing.T) {
	a := lines("one", "two", "three", "four", "five", "six")
	b := lines("one", "2", "three", "3.5", "five", "seven", "six")
	var gotA, gotB []string
	for _, e := range diffLines(a, b) {
		switch e.kind {
		case editEqual:
			gotA = append(gotA, e.text)
			gotB = append(gotB, e.text)
		case editDelete:
			gotA = append(gotA, e.text)
		case editInsert:
			gotB = append(gotB, e.text)
		}
	}
	if strings.Join(gotA, "") != strings.Join(a, "") {
		t.Errorf("script does not replay old side")
	}
	if strings.Join(gotB, "") != strings.Join(b, "") {
		t.Errorf("script does not replay new side")
	}
}

// TestMyersFallback drives the edit distance over maxEditDistance and
// checks the degraded whole-replace script still round-trips.
func TestMyersFallback(t *testing.T) {
	var a, b []string
	for i := range 1100 {
		a = append(a, fmt.Sprintf("old-%d\n", i))
		b = append(b, fmt.Sprintf("new-%d\n", i))
	}
	edits := diffLines(a, b)
	if len(edits) != 2200 {
		t.Fatalf("edit count = %d, want 2200", len(edits))
	}
	for i, e := range edits {
		want := editDelete
		if i >= 1100 {
			want = editInsert
		}
		if e.kind != want {
			t.Fatalf("edit %d kind = %v, want %v", i, e.kind, want)
		}
	}
}

func TestFormatHunksContextAndGrouping(t *testing.T) {
	build := func(spec string) []edit {
		var edits []edit
		for i, c := range strings.Split(spec, " ") {
			text := fmt.Sprintf("l%d\n", i)
			switch c {
			case "=":
				edits = append(edits, edit{editEqual, text})
			case "-":
				edits = append(edits, edit{editDelete, text})
			case "+":
				edits = append(edits, edit{editInsert, text})
			}
		}
		return edits
	}

	t.Run("no changes yields empty", func(t *testing.T) {
		if got := formatHunks(build("= = ="), 3); got != "" {
			t.Fatalf("got %q, want empty", got)
		}
	})

	t.Run("context clamps at file edges", func(t *testing.T) {
		got := formatHunks(build("= - ="), 3)
		want := "@@ -1,3 +1,2 @@\n l0\n-l1\n l2\n"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("changes six apart merge into one hunk", func(t *testing.T) {
		// gap of exactly 2*context context lines merges (git's rule).
		got := formatHunks(build("- = = = = = = -"), 3)
		if strings.Count(got, "@@") != 2 { // one header, two @@ tokens
			t.Fatalf("want a single hunk, got %q", got)
		}
	})

	t.Run("changes seven apart split into two hunks", func(t *testing.T) {
		got := formatHunks(build("- = = = = = = = -"), 3)
		if strings.Count(got, "@@") != 4 {
			t.Fatalf("want two hunks, got %q", got)
		}
		wantFirst := "@@ -1,4 +1,3 @@\n"
		if !strings.HasPrefix(got, wantFirst) {
			t.Fatalf("first hunk header wrong: %q", got)
		}
	})

	t.Run("insert-only hunk header uses zero old count", func(t *testing.T) {
		got := formatHunks(build("+"), 3)
		want := "@@ -0,0 +1 @@\n+l0\n"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("delete-only hunk header uses zero new count", func(t *testing.T) {
		got := formatHunks(build("-"), 3)
		want := "@@ -1 +0,0 @@\n-l0\n"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("counts of one omit the comma form", func(t *testing.T) {
		got := formatHunks(build("- +"), 0)
		want := "@@ -1 +1 @@\n-l0\n+l1\n"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}

func TestFormatHunksNoNewlineMarkers(t *testing.T) {
	old := "a\nb"
	updated := "a\nb\n"
	got := formatHunks(diffLines(splitLinesKeepEnds(old), splitLinesKeepEnds(updated)), 3)
	want := "@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+b\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestUnifiedFileDiffModify(t *testing.T) {
	oldContent := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n"
	newContent := strings.Replace(oldContent, "line5", "changed5", 1)
	got := unifiedFileDiff("f.txt", "f.txt", oldContent, newContent)
	want := strings.Join([]string{
		"diff --git a/f.txt b/f.txt",
		"--- a/f.txt",
		"+++ b/f.txt",
		"@@ -2,7 +2,7 @@",
		" line2",
		" line3",
		" line4",
		"-line5",
		"+changed5",
		" line6",
		" line7",
		" line8",
		"",
	}, "\n")
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUnifiedFileDiffAdd(t *testing.T) {
	got := unifiedFileDiff("", "new.txt", "", "hello\nworld\n")
	want := strings.Join([]string{
		"diff --git a/new.txt b/new.txt",
		"new file mode 100644",
		"--- /dev/null",
		"+++ b/new.txt",
		"@@ -0,0 +1,2 @@",
		"+hello",
		"+world",
		"",
	}, "\n")
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUnifiedFileDiffDelete(t *testing.T) {
	got := unifiedFileDiff("gone.txt", "", "only\n", "")
	want := strings.Join([]string{
		"diff --git a/gone.txt b/gone.txt",
		"deleted file mode 100644",
		"--- a/gone.txt",
		"+++ /dev/null",
		"@@ -1 +0,0 @@",
		"-only",
		"",
	}, "\n")
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUnifiedFileDiffEmptyAdd(t *testing.T) {
	got := unifiedFileDiff("", "empty.txt", "", "")
	want := "diff --git a/empty.txt b/empty.txt\nnew file mode 100644\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestUnifiedFileDiffPureRename(t *testing.T) {
	got := unifiedFileDiff("old/name.txt", "new/name.txt", "same\n", "same\n")
	want := strings.Join([]string{
		"diff --git a/old/name.txt b/new/name.txt",
		"similarity index 100%",
		"rename from old/name.txt",
		"rename to new/name.txt",
		"",
	}, "\n")
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUnifiedFileDiffRenameWithEdit(t *testing.T) {
	got := unifiedFileDiff("a.txt", "b.txt", "one\ntwo\n", "one\n2\n")
	want := strings.Join([]string{
		"diff --git a/a.txt b/b.txt",
		"rename from a.txt",
		"rename to b.txt",
		"--- a/a.txt",
		"+++ b/b.txt",
		"@@ -1,2 +1,2 @@",
		" one",
		"-two",
		"+2",
		"",
	}, "\n")
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUnifiedFileDiffIdenticalEdit(t *testing.T) {
	if got := unifiedFileDiff("same.txt", "same.txt", "x\n", "x\n"); got != "" {
		t.Fatalf("identical blobs must produce no diff, got %q", got)
	}
}

func TestUnifiedFileDiffBinary(t *testing.T) {
	binary := "PNG\x00\x01\x02"
	t.Run("modify", func(t *testing.T) {
		got := unifiedFileDiff("img.png", "img.png", binary, binary+"more")
		want := "diff --git a/img.png b/img.png\nBinary files a/img.png and b/img.png differ\n"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
	t.Run("add", func(t *testing.T) {
		got := unifiedFileDiff("", "img.png", "", binary)
		want := "diff --git a/img.png b/img.png\nnew file mode 100644\nBinary files /dev/null and b/img.png differ\n"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
	t.Run("delete", func(t *testing.T) {
		got := unifiedFileDiff("img.png", "", binary, "")
		want := "diff --git a/img.png b/img.png\ndeleted file mode 100644\nBinary files a/img.png and /dev/null differ\n"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
	t.Run("nul beyond sniff window is text", func(t *testing.T) {
		content := strings.Repeat("a", binarySniffLen) + "\x00"
		if isBinaryContent(content) {
			t.Fatal("NUL beyond the sniff window must not classify as binary")
		}
	})
}
