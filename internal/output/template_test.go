package output

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadNotesTemplateDefault(t *testing.T) {
	tmpl, warnings := LoadNotesTemplate("")
	if tmpl == nil {
		t.Fatal("nil template")
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
}

func TestLoadNotesTemplateOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.md.tmpl")
	if err := os.WriteFile(path, []byte("custom for {{.Slug}}: {{upper \"hi\"}}"), 0o600); err != nil {
		t.Fatal(err)
	}

	tmpl, warnings := LoadNotesTemplate(path)
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	got, err := RenderNotes(tmpl, &TemplateData{Slug: "my/slug"})
	if err != nil {
		t.Fatalf("RenderNotes: %v", err)
	}
	if got != "custom for my/slug: HI" {
		t.Errorf("override render = %q", got)
	}
}

func TestLoadNotesTemplateOverrideParseErrorFallsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.md.tmpl")
	if err := os.WriteFile(path, []byte("{{.Slug"), 0o600); err != nil {
		t.Fatal(err)
	}

	tmpl, warnings := LoadNotesTemplate(path)
	if len(warnings) != 1 || !strings.Contains(warnings[0], path) {
		t.Fatalf("expected one warning naming the override, got %v", warnings)
	}
	// The fallback must render identically to the embedded default.
	def, _ := LoadNotesTemplate("")
	data := &TemplateData{Slug: "s", ReviewComments: []TemplateComment{
		{Type: "ISSUE", Location: "a.go:1", Content: "x", Number: 1},
	}}
	got, err := RenderNotes(tmpl, data)
	if err != nil {
		t.Fatalf("RenderNotes: %v", err)
	}
	want, err := RenderNotes(def, data)
	if err != nil {
		t.Fatalf("RenderNotes default: %v", err)
	}
	if got != want {
		t.Errorf("fallback render diverges from default:\n%q\nvs\n%q", got, want)
	}
}

func TestLoadNotesTemplateOverrideMissingFileFallsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.tmpl")
	tmpl, warnings := LoadNotesTemplate(path)
	if tmpl == nil {
		t.Fatal("nil template")
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "using embedded default") {
		t.Fatalf("expected fallback warning, got %v", warnings)
	}
}

func TestRenderNotesSurfacesExecutionErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.md.tmpl")
	if err := os.WriteFile(path, []byte("{{.NoSuchField}}"), 0o600); err != nil {
		t.Fatal(err)
	}
	tmpl, warnings := LoadNotesTemplate(path)
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if _, err := RenderNotes(tmpl, &TemplateData{}); err == nil {
		t.Fatal("expected execution error")
	}
}

func TestTruncFn(t *testing.T) {
	cases := []struct {
		n    int
		s    string
		want string
	}{
		{3, "hello", "hel"},
		{10, "hello", "hello"},
		{0, "hello", ""},
		{-1, "hello", ""},
		{2, "héllo", "hé"}, // rune-safe
	}
	for _, tc := range cases {
		if got := truncFn(tc.n, tc.s); got != tc.want {
			t.Errorf("trunc(%d, %q) = %q, want %q", tc.n, tc.s, got, tc.want)
		}
	}
}

func TestIndentFn(t *testing.T) {
	if got := indentFn(2, "a\nb"); got != "  a\n  b" {
		t.Errorf("indent = %q", got)
	}
	if got := indentFn(0, "a\nb"); got != "a\nb" {
		t.Errorf("indent 0 = %q", got)
	}
}

func TestJoinFn(t *testing.T) {
	if got := joinFn(", ", []string{"a", "b"}); got != "a, b" {
		t.Errorf("join = %q", got)
	}
}

func TestCodefenceFn(t *testing.T) {
	if got := codefenceFn("go", "x := 1"); got != "```go\nx := 1\n```" {
		t.Errorf("codefence = %q", got)
	}
	// Fences inside the content extend the outer fence.
	got := codefenceFn("", "```\ninner\n```")
	if !strings.HasPrefix(got, "````\n") || !strings.HasSuffix(got, "\n````") {
		t.Errorf("codefence must extend past inner fences, got %q", got)
	}
}

func TestTemplateCommentMarkerAndBody(t *testing.T) {
	c := TemplateComment{Content: "first\r\nsecond\nthird", Number: 12}
	if got := c.Marker(); got != "12." {
		t.Errorf("Marker = %q", got)
	}
	if got := c.Body(); got != "first\n    second\n    third" {
		t.Errorf("Body = %q", got)
	}
	empty := TemplateComment{Number: 1}
	if got := empty.Body(); got != "" {
		t.Errorf("empty Body = %q", got)
	}
}
