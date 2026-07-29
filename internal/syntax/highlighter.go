package syntax

import (
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// MaxHighlightFileBytes caps full-file highlighting; larger files fall back
// to per-hunk defaults so runaway generated artifacts stay cheap.
const MaxHighlightFileBytes = 1 << 20

// Highlighter tokenizes source through chroma with a fixed style. The
// diff-row backgrounds for added/deleted lines are theme-owned and injected
// by ApplyDiffBackground; chroma's own backgrounds are ignored.
type Highlighter struct {
	style *chroma.Style
	addBg string
	delBg string
}

// NewHighlighter builds a highlighter from a chroma style name (falling back
// to chroma's default style for unknown names) and the theme's add/del row
// backgrounds as "#rrggbb" strings (empty = none).
func NewHighlighter(styleName, addBg, delBg string) *Highlighter {
	style := styles.Get(styleName)
	if style == nil {
		style = styles.Fallback
	}
	return &Highlighter{style: style, addBg: addBg, delBg: delBg}
}

// NewHighlighterWithStyle builds a highlighter from an explicit style, used
// for user-supplied chroma XML style files.
func NewHighlighterWithStyle(style *chroma.Style, addBg, delBg string) *Highlighter {
	if style == nil {
		style = styles.Fallback
	}
	return &Highlighter{style: style, addBg: addBg, delBg: delBg}
}

// StyleName returns the active chroma style's name.
func (h *Highlighter) StyleName() string { return h.style.Name }

// fallbackExtensions maps extensions chroma has no lexer alias for onto
// close-enough grammars (port of tuicr's fallback table, adjusted to
// chroma's registry: jsx/tsx/vue/svelte exist natively).
var fallbackExtensions = map[string]string{
	"hbs": "html", "handlebars": "html", "mustache": "html",
	"ejs": "html", "pug": "html", "jade": "html", "njk": "html",
	"jsonc": "json", "json5": "json", "prisma": "json",
	"heex": "elixir", "eex": "elixir",
	"mjs": "js", "cjs": "js",
}

// fallbackFilenames maps exact basenames onto grammars.
var fallbackFilenames = map[string]string{
	"Containerfile": "docker",
	"Justfile":      "make",
	"justfile":      "make",
}

// containerExtensions need full-file context before nested grammars
// activate; html/md deliberately excluded for performance (tuicr parity).
var containerExtensions = map[string]bool{
	"vue": true, "svelte": true, "astro": true, "mdx": true,
	"php": true, "erb": true, "eex": true, "heex": true,
}

// NeedsFullFileHighlight reports whether the file's grammar requires
// whole-file context to highlight correctly.
func NeedsFullFileHighlight(path string) bool {
	return containerExtensions[strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))]
}

func (h *Highlighter) lexerFor(path, firstLine string) chroma.Lexer {
	if lexer := lexers.Match(filepath.Base(path)); lexer != nil {
		return lexer
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	if lexer := lexers.Match("x." + ext); lexer != nil {
		return lexer
	}
	if alias, ok := fallbackExtensions[ext]; ok {
		if lexer := lexers.Get(alias); lexer != nil {
			return lexer
		}
	}
	if alias, ok := fallbackFilenames[filepath.Base(path)]; ok {
		if lexer := lexers.Get(alias); lexer != nil {
			return lexer
		}
	}
	if firstLine != "" {
		if lexer := lexers.Analyse(firstLine); lexer != nil {
			return lexer
		}
	}
	return nil
}

// HighlightFileLines tokenizes the whole line sequence with full-file
// context and returns per-line spans, ok=false when no lexer matches or the
// content trips the size/NUL guards. Safe on a nil receiver (no
// highlighting configured).
func (h *Highlighter) HighlightFileLines(path string, lines []string) ([][]Span, bool) {
	if h == nil {
		return nil, false
	}
	content := strings.Join(lines, "\n")
	if len(content) > MaxHighlightFileBytes || strings.ContainsRune(content, 0) {
		return nil, false
	}
	firstLine := ""
	if len(lines) > 0 {
		firstLine = lines[0]
	}
	lexer := h.lexerFor(path, firstLine)
	if lexer == nil {
		return nil, false
	}
	lexer = chroma.Coalesce(lexer)

	iterator, err := lexer.Tokenise(nil, content)
	if err != nil {
		return nil, false
	}
	return h.splitTokensToLines(iterator, len(lines)), true
}

// HighlightMarkdownLines highlights comment bodies as markdown. Safe on a
// nil receiver.
func (h *Highlighter) HighlightMarkdownLines(lines []string) [][]Span {
	if h == nil {
		return nil
	}
	lexer := lexers.Get("markdown")
	if lexer == nil {
		return nil
	}
	iterator, err := chroma.Coalesce(lexer).Tokenise(nil, strings.Join(lines, "\n"))
	if err != nil {
		return nil
	}
	return h.splitTokensToLines(iterator, len(lines))
}

func (h *Highlighter) splitTokensToLines(iterator chroma.Iterator, lineCount int) [][]Span {
	result := make([][]Span, lineCount)
	line := 0
	for token := iterator(); token != chroma.EOF; token = iterator() {
		style := h.spanStyle(token.Type)
		parts := strings.Split(token.Value, "\n")
		for i, part := range parts {
			if i > 0 {
				line++
			}
			if line >= lineCount {
				return result
			}
			if part == "" {
				continue
			}
			result[line] = append(result[line], Span{Style: style, Text: part})
		}
	}
	return result
}

// spanStyle resolves a token type to a plain span style, ignoring chroma
// backgrounds (theme-owned).
func (h *Highlighter) spanStyle(tokenType chroma.TokenType) Style {
	entry := h.style.Get(tokenType)
	s := Style{
		Bold:      entry.Bold == chroma.Yes,
		Italic:    entry.Italic == chroma.Yes,
		Underline: entry.Underline == chroma.Yes,
	}
	if entry.Colour.IsSet() {
		s.FG = entry.Colour.String()
	}
	return s
}

// ApplyDiffBackground stamps the theme's add/del row background onto every
// span of a highlighted add/del line (context lines pass through). Safe on
// a nil receiver.
func (h *Highlighter) ApplyDiffBackground(spans []Span, origin DiffOrigin) []Span {
	if h == nil {
		return spans
	}
	bg := ""
	switch origin {
	case DiffOriginAddition:
		bg = h.addBg
	case DiffOriginDeletion:
		bg = h.delBg
	}
	if bg == "" {
		return spans
	}
	out := make([]Span, len(spans))
	for i, span := range spans {
		span.Style.BG = bg
		out[i] = span
	}
	return out
}

// DiffOrigin mirrors model.LineOrigin without importing model (which would
// create an import cycle: model already imports syntax for Span).
type DiffOrigin int

// Diff origins.
const (
	DiffOriginContext DiffOrigin = iota
	DiffOriginAddition
	DiffOriginDeletion
)
