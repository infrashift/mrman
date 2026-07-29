// Package syntax provides syntax highlighting via chroma and the plain-data
// Span type carried on diff lines. Keeping Span free of UI dependencies lets
// the model layer stay toolkit-agnostic; the render layer converts spans to
// terminal styles.
package syntax

// Style is a plain, comparable text style. Zero values mean "inherit".
type Style struct {
	FG        string // hex color "#rrggbb", or "" to inherit
	BG        string // hex color "#rrggbb", or "" to inherit
	Bold      bool
	Italic    bool
	Underline bool
}

// Span is a run of text with one style.
type Span struct {
	Style Style
	Text  string
}
