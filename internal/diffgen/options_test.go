package diffgen

import (
	"strings"
	"testing"
)

// stripSpace is the LineKey a whitespace-insensitive comparison uses: it
// removes every space and tab so indentation and alignment changes stop
// registering as edits.
func stripSpace(line string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, line)
}

func TestOptionsDefaults(t *testing.T) {
	tests := []struct {
		name             string
		opts             Options
		wantContext      int
		wantEditDistance int
	}{
		{"zero value takes git defaults", Options{}, diffContextLines, maxEditDistance},
		{"negative is treated as unset", Options{Context: -1, MaxEditDistance: -1}, diffContextLines, maxEditDistance},
		{"explicit values win", Options{Context: 1, MaxEditDistance: 8}, 1, 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.opts.context(); got != tt.wantContext {
				t.Errorf("context() = %d, want %d", got, tt.wantContext)
			}
			if got := tt.opts.editBound(); got != tt.wantEditDistance {
				t.Errorf("editBound() = %d, want %d", got, tt.wantEditDistance)
			}
		})
	}
}

// TestLineKeyDecidesEqualityNotOutput is the whole contract of LineKey: it
// changes what counts as a difference without changing a byte of what the
// reviewer reads.
func TestLineKeyDecidesEqualityNotOutput(t *testing.T) {
	const (
		oldContent = "func f() {\n    return 1\n}\n"
		newContent = "func f() {\n\treturn 1\n}\n"
	)

	t.Run("verbatim comparison sees the reindent", func(t *testing.T) {
		got := UnifiedFileDiff("f.go", "f.go", oldContent, newContent, Options{})
		if !strings.Contains(got, "-    return 1") || !strings.Contains(got, "+\treturn 1") {
			t.Fatalf("want the reindent reported, got %q", got)
		}
	})

	t.Run("whitespace key hides it entirely", func(t *testing.T) {
		got := UnifiedFileDiff("f.go", "f.go", oldContent, newContent, Options{LineKey: stripSpace})
		if got != "" {
			t.Fatalf("want no difference, got %q", got)
		}
	})

	t.Run("real edits still surface, in their original text", func(t *testing.T) {
		// Same reindent, plus a genuine change on the return value.
		updated := "func f() {\n\treturn 2\n}\n"
		got := UnifiedFileDiff("f.go", "f.go", oldContent, updated, Options{LineKey: stripSpace})
		// The emitted lines must be the file's own text, not the key's
		// whitespace-stripped projection.
		if !strings.Contains(got, "-    return 1") {
			t.Errorf("deleted line lost its original indentation: %q", got)
		}
		if !strings.Contains(got, "+\treturn 2") {
			t.Errorf("inserted line lost its original indentation: %q", got)
		}
		if strings.Contains(got, "-return1") || strings.Contains(got, "+return2") {
			t.Errorf("emitted the key projection instead of the text: %q", got)
		}
	})
}

// TestNoRenameHeaders guards the reason the option exists: two differently
// named files being compared are not a rename, and diffparser turns a
// rename header into StatusRenamed, which paints a meaningless R badge.
func TestNoRenameHeaders(t *testing.T) {
	const oldContent, newContent = "one\ntwo\n", "one\n2\n"

	t.Run("default reports a rename", func(t *testing.T) {
		got := UnifiedFileDiff("a.txt", "b.txt", oldContent, newContent, Options{})
		if !strings.Contains(got, "rename from a.txt") || !strings.Contains(got, "rename to b.txt") {
			t.Fatalf("want rename headers, got %q", got)
		}
	})

	t.Run("suppressed leaves an ordinary modification", func(t *testing.T) {
		got := UnifiedFileDiff("a.txt", "b.txt", oldContent, newContent, Options{NoRenameHeaders: true})
		if strings.Contains(got, "rename") || strings.Contains(got, "similarity index") {
			t.Fatalf("want no rename metadata, got %q", got)
		}
		for _, want := range []string{"diff --git a/a.txt b/b.txt", "--- a/a.txt", "+++ b/b.txt", "-two", "+2"} {
			if !strings.Contains(got, want) {
				t.Errorf("missing %q in %q", want, got)
			}
		}
	})

	t.Run("suppressed and identical yields nothing to review", func(t *testing.T) {
		// With rename headers this is a pure rename and the headers alone
		// carry it. Without them there is genuinely no difference.
		got := UnifiedFileDiff("a.txt", "b.txt", "same\n", "same\n", Options{NoRenameHeaders: true})
		if got != "" {
			t.Fatalf("want empty, got %q", got)
		}
	})

	t.Run("adds and deletes are unaffected", func(t *testing.T) {
		added := UnifiedFileDiff("", "new.txt", "", "hi\n", Options{NoRenameHeaders: true})
		if !strings.Contains(added, "new file mode") || !strings.Contains(added, "--- /dev/null") {
			t.Errorf("add lost its headers: %q", added)
		}
		deleted := UnifiedFileDiff("gone.txt", "", "hi\n", "", Options{NoRenameHeaders: true})
		if !strings.Contains(deleted, "deleted file mode") || !strings.Contains(deleted, "+++ /dev/null") {
			t.Errorf("delete lost its headers: %q", deleted)
		}
	})
}

// TestMaxEditDistanceDegrades pins the bounded-search contract with a tiny
// bound, so the fallback is exercised without building an input large
// enough to trip the real 2048 ceiling.
func TestMaxEditDistanceDegrades(t *testing.T) {
	// Ten lines, every one different: edit distance 20, well past a bound
	// of 4 but trivially within the default.
	var oldLines, newLines strings.Builder
	for i := range 10 {
		oldLines.WriteString(string(rune('a'+i)) + "\n")
		newLines.WriteString(string(rune('A'+i)) + "\n")
	}
	old, updated := oldLines.String(), newLines.String()

	tight := diffLines(splitLinesKeepEnds(old), splitLinesKeepEnds(updated), Options{MaxEditDistance: 4})
	for i, e := range tight {
		if e.kind == editEqual {
			t.Fatalf("edit %d: bounded search should degrade to delete-all/insert-all, got an equal line", i)
		}
	}

	// The same input under the default bound is diffed properly, so the
	// degradation above is the bound talking and not the input.
	loose := diffLines(splitLinesKeepEnds(old), splitLinesKeepEnds(updated), Options{})
	if len(loose) != len(tight) {
		t.Fatalf("default bound changed the script length: %d vs %d", len(loose), len(tight))
	}
}

func TestContextOption(t *testing.T) {
	const old = "1\n2\n3\n4\n5\n6\n7\n"
	const updated = "1\n2\n3\nX\n5\n6\n7\n"

	tests := []struct {
		name       string
		context    int
		wantHeader string
	}{
		{"zero takes the default of three", 0, "@@ -1,7 +1,7 @@"},
		{"one narrows to a single line either side", 1, "@@ -3,3 +3,3 @@"},
		{"two widens it", 2, "@@ -2,5 +2,5 @@"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UnifiedFileDiff("f.txt", "f.txt", old, updated, Options{Context: tt.context})
			if !strings.Contains(got, tt.wantHeader) {
				t.Fatalf("want header %q in %q", tt.wantHeader, got)
			}
		})
	}
}
