package ui

import (
	render "github.com/infrashift/mrman/charmkit/cellrender"
	"github.com/infrashift/mrman/internal/syntax"
)

// fromSyntaxSpans converts highlighter spans (hex-string styles) into cell
// spans (color.Color styles).
//
// This bridge lives in mrman rather than in cellrender: cellrender must not
// know that mrman highlights with chroma, and internal/syntax must stay free
// of UI types so the model layer can carry spans without a toolkit.
func fromSyntaxSpans(spans []syntax.Span) []render.Span {
	out := make([]render.Span, len(spans))
	for i, sp := range spans {
		out[i] = render.Span{
			Text: sp.Text,
			Style: render.Style{
				Fg:        render.ParseHexColor(sp.Style.FG),
				Bg:        render.ParseHexColor(sp.Style.BG),
				Bold:      sp.Style.Bold,
				Italic:    sp.Style.Italic,
				Underline: sp.Style.Underline,
			},
		}
	}
	return out
}
