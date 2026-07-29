package syntax

import (
	"strings"
	"testing"
)

func TestHighlightFileLinesGo(t *testing.T) {
	h := NewHighlighter("tokyonight-storm", "#1c2a34", "#2f1e26")
	lines := []string{"package main", "", `var greeting = "hello"`}
	spans, ok := h.HighlightFileLines("main.go", lines)
	if !ok {
		t.Fatal("go source must highlight")
	}
	if len(spans) != 3 {
		t.Fatalf("got %d lines", len(spans))
	}
	if len(spans[0]) == 0 {
		t.Fatal("line 1 must have spans")
	}
	if len(spans[1]) != 0 {
		t.Fatal("blank line must have no spans")
	}
	// Reassembling span text reproduces the source line.
	var rebuilt strings.Builder
	for _, s := range spans[2] {
		rebuilt.WriteString(s.Text)
	}
	if rebuilt.String() != lines[2] {
		t.Fatalf("rebuilt %q != %q", rebuilt.String(), lines[2])
	}
	// The keyword should carry a color from the style.
	if spans[0][0].Style.FG == "" {
		t.Error("keyword span should carry a foreground color")
	}
}

func TestHighlighterGuards(t *testing.T) {
	h := NewHighlighter("tokyonight-day", "", "")
	if _, ok := h.HighlightFileLines("data.bin", []string{"abc\x00def"}); ok {
		t.Fatal("NUL content must be rejected")
	}
	huge := strings.Repeat("x", MaxHighlightFileBytes+1)
	if _, ok := h.HighlightFileLines("big.go", []string{huge}); ok {
		t.Fatal("oversized content must be rejected")
	}
	if _, ok := h.HighlightFileLines("mystery.zzzz", []string{"no lexer matches this"}); ok {
		t.Fatal("unknown extension without shebang must be rejected")
	}
}

func TestFallbackTables(t *testing.T) {
	h := NewHighlighter("github-dark", "", "")
	for _, name := range []string{"tmpl.hbs", "conf.json5", "view.heex", "Justfile", "Containerfile"} {
		if _, ok := h.HighlightFileLines(name, []string{"content = 1"}); !ok {
			t.Errorf("%s must resolve via fallback tables", name)
		}
	}
}

func TestNeedsFullFileHighlight(t *testing.T) {
	for _, p := range []string{"app.vue", "App.Svelte", "page.astro", "doc.mdx", "index.php", "view.erb"} {
		if !NeedsFullFileHighlight(p) {
			t.Errorf("%s must need full-file highlight", p)
		}
	}
	for _, p := range []string{"main.go", "index.html", "README.md"} {
		if NeedsFullFileHighlight(p) {
			t.Errorf("%s must not need full-file highlight", p)
		}
	}
}

func TestApplyDiffBackground(t *testing.T) {
	h := NewHighlighter("tokyonight-storm", "#1c2a34", "#2f1e26")
	spans := []Span{{Style: Style{FG: "#ff0000"}, Text: "code"}}
	added := h.ApplyDiffBackground(spans, DiffOriginAddition)
	if added[0].Style.BG != "#1c2a34" || added[0].Style.FG != "#ff0000" {
		t.Fatalf("added = %+v", added[0])
	}
	deleted := h.ApplyDiffBackground(spans, DiffOriginDeletion)
	if deleted[0].Style.BG != "#2f1e26" {
		t.Fatalf("deleted = %+v", deleted[0])
	}
	context := h.ApplyDiffBackground(spans, DiffOriginContext)
	if context[0].Style.BG != "" {
		t.Fatal("context must keep no background")
	}
	if spans[0].Style.BG != "" {
		t.Fatal("input spans must not be mutated")
	}
}

func TestHighlightMarkdownLines(t *testing.T) {
	h := NewHighlighter("tokyonight-storm", "", "")
	spans := h.HighlightMarkdownLines([]string{"# Title", "", "some **bold** text"})
	if len(spans) != 3 || len(spans[0]) == 0 {
		t.Fatalf("markdown spans = %v", spans)
	}
}

func TestTokyoNightStylesExist(t *testing.T) {
	for _, name := range []string{"tokyonight-storm", "tokyonight-day"} {
		h := NewHighlighter(name, "", "")
		if h.StyleName() != name {
			t.Errorf("style %q resolved to %q — chroma must ship it", name, h.StyleName())
		}
	}
	// Unknown style names fall back rather than crash.
	if NewHighlighter("no-such-style", "", "").StyleName() == "no-such-style" {
		t.Error("unknown style must fall back")
	}
}
