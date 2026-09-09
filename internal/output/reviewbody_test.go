package output

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewBodyDefaultShape(t *testing.T) {
	tmpl, warnings := LoadReviewBodyTemplate("")
	if len(warnings) != 0 {
		t.Fatalf("warnings: %v", warnings)
	}
	got, err := RenderReviewBody(tmpl, &ReviewBodyData{
		ReviewComments: []ReviewBodyComment{
			{Content: "Overall looks solid."},
			{Content: "Second thought."},
		},
		MovedToSummary: []ReviewBodyComment{
			{Type: "issue", Path: "src/a.go", Content: "off-by-one"},
			{Path: "src/b.go", Content: "untyped item"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "Overall looks solid.\n\nSecond thought.\n\n## Unplaced comments\n\n- [ISSUE] src/a.go: off-by-one\n- src/b.go: untyped item"
	if got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestReviewBodyEmptyRendersEmpty(t *testing.T) {
	tmpl, _ := LoadReviewBodyTemplate("")
	got, err := RenderReviewBody(tmpl, &ReviewBodyData{})
	if err != nil || got != "" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestReviewBodyOnlyUnplaced(t *testing.T) {
	tmpl, _ := LoadReviewBodyTemplate("")
	got, err := RenderReviewBody(tmpl, &ReviewBodyData{
		MovedToSummary: []ReviewBodyComment{{Type: "note", Path: "x.go", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(got, "\n") || !strings.HasPrefix(got, "## Unplaced comments") {
		t.Fatalf("got %q", got)
	}
}

func TestReviewBodyOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rb.tmpl")
	if err := os.WriteFile(path, []byte("CUSTOM: {{len .ReviewComments}}"), 0o644); err != nil {
		t.Fatal(err)
	}
	tmpl, warnings := LoadReviewBodyTemplate(path)
	if len(warnings) != 0 {
		t.Fatalf("warnings: %v", warnings)
	}
	got, _ := RenderReviewBody(tmpl, &ReviewBodyData{ReviewComments: []ReviewBodyComment{{Content: "x"}}})
	if got != "CUSTOM: 1" {
		t.Fatalf("got %q", got)
	}

	// Parse error falls back with a warning.
	bad := filepath.Join(dir, "bad.tmpl")
	if err := os.WriteFile(bad, []byte("{{.Broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, warnings = LoadReviewBodyTemplate(bad)
	if len(warnings) != 1 {
		t.Fatalf("warnings: %v", warnings)
	}
	// Missing file falls back with a warning.
	_, warnings = LoadReviewBodyTemplate(filepath.Join(dir, "missing.tmpl"))
	if len(warnings) != 1 {
		t.Fatalf("warnings: %v", warnings)
	}
}

// TestReviewBodyBadgesTheAuthor covers attribution in the body posted to the
// forge. A review submitted on behalf of several authors — a person plus an
// agent, typically — has to say which comment came from whom.
func TestReviewBodyBadgesTheAuthor(t *testing.T) {
	tmpl, _ := LoadReviewBodyTemplate("")
	got, err := RenderReviewBody(tmpl, &ReviewBodyData{
		ReviewComments: []ReviewBodyComment{
			{Content: "Theirs.", Author: "claude", ShowAuthor: true},
			{Content: "Mine.", Author: "ryan"},
		},
		MovedToSummary: []ReviewBodyComment{
			{Type: "issue", Path: "src/a.go", Content: "off-by-one", Author: "claude", ShowAuthor: true},
			{Path: "src/b.go", Content: "untyped", Author: "claude", ShowAuthor: true},
			{Type: "note", Path: "src/c.go", Content: "mine", Author: "ryan"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "**[@claude]** Theirs.\n\nMine.\n\n## Unplaced comments\n\n" +
		"- [ISSUE @claude] src/a.go: off-by-one\n" +
		"- [@claude] src/b.go: untyped\n" +
		"- [NOTE] src/c.go: mine"
	if got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

// TestReviewBodyLeavesUnauthoredCommentsAlone pins backward compatibility for
// sessions written before mrman stamped authors.
func TestReviewBodyLeavesUnauthoredCommentsAlone(t *testing.T) {
	tmpl, _ := LoadReviewBodyTemplate("")
	got, err := RenderReviewBody(tmpl, &ReviewBodyData{
		ReviewComments: []ReviewBodyComment{{Content: "old note", ShowAuthor: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "old note" {
		t.Fatalf("an unauthored comment must render unchanged, got %q", got)
	}
}
