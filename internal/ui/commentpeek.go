package ui

import (
	"fmt"
	"strings"

	render "github.com/infrashift/mrman/charmkit/cellrender"
	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/theme"
)

// peekMaxHeight caps the panel so it never swallows the whole diff pane; it
// is a peek, not a view.
const peekMaxHeight = 16

// commentPeekOverlay renders the read-only comment panel as rows to be copied
// over the bottom of the diff pane, the same way the submit modal and the
// comment input box are placed. Returns nil when the panel is closed.
//
// The panel exists because marking a file reviewed folds it: without this the
// only way to re-read a comment on a finished file was toggling r off, which
// unfolds the file and loses your position.
func (p *DiffPane) commentPeekOverlay(a *app.App, width, maxHeight int) []string {
	peek := a.CommentPeek
	if peek == nil {
		return nil
	}
	t := p.Theme
	emitter := &render.Emitter{}
	borderStyle := render.Style{Fg: t.BorderFocused, Bold: true}

	height := min(len(peek.Lines)+2, min(peekMaxHeight, maxHeight))
	bodyHeight := max(height-2, 1)
	peek.ViewportHeight = bodyHeight
	if peek.ScrollOffset > max(len(peek.Lines)-bodyHeight, 0) {
		peek.ScrollOffset = max(len(peek.Lines)-bodyHeight, 0)
	}

	title := a.PeekTitle()
	hint := " (j/k scroll · Esc close) "
	rows := make([]string, 0, height)
	rows = append(rows, emitter.Line(peekRule(t, "╭──", title, hint, width)))

	for i := 0; i < bodyHeight; i++ {
		idx := peek.ScrollOffset + i
		if idx >= len(peek.Lines) {
			rows = append(rows, emitter.Line([]render.Span{
				{Text: "  │", Style: borderStyle},
			}))
			continue
		}
		rows = append(rows, emitter.Line(peekRow(t, &peek.Lines[idx], width)))
	}
	rows = append(rows, emitter.Line([]render.Span{
		{Text: "  ╰" + strings.Repeat("─", max(width-3, 0)), Style: borderStyle},
	}))
	return rows
}

// peekRule builds the panel's top border with its title and key hint.
func peekRule(t *theme.Theme, lead, title, hint string, width int) []render.Span {
	spans := []render.Span{
		{Text: "  " + lead, Style: render.Style{Fg: t.BorderFocused, Bold: true}},
		{Text: title, Style: render.Style{Fg: t.FgPrimary, Bold: true}},
		{Text: hint, Style: render.Style{Fg: t.FgDim}},
	}
	used := 0
	for _, s := range spans {
		used += render.StringWidth(s.Text)
	}
	if fill := width - used; fill > 0 {
		spans = append(spans, render.Span{
			Text:  strings.Repeat("─", fill),
			Style: render.Style{Fg: t.BorderFocused, Bold: true},
		})
	}
	return render.TruncateOrPadSpans(spans, width, render.Style{})
}

// peekRow styles one panel row. Context lines keep their line numbers so the
// anchor is locatable in the real file; the anchor row is highlighted.
func peekRow(t *theme.Theme, line *app.PeekLine, width int) []render.Span {
	border := render.Span{Text: "  │ ", Style: render.Style{Fg: t.BorderFocused, Bold: true}}
	spans := []render.Span{border}

	switch line.Kind {
	case app.PeekAnchor, app.PeekContext:
		style := render.Style{Fg: t.DiffContext}
		gutter := render.Style{Fg: t.FgDim}
		if line.Kind == app.PeekAnchor {
			style = render.Style{Fg: t.FgPrimary, Bg: t.CursorLineBg}
			gutter = render.Style{Fg: t.CursorColor, Bg: t.CursorLineBg}
		}
		spans = append(spans,
			render.Span{Text: fmt.Sprintf("%5d ", line.Lineno), Style: gutter},
			render.Span{Text: strings.TrimRight(line.Text, "\n"), Style: style})
	case app.PeekCommentHeader:
		spans = append(spans, render.Span{
			Text:  line.Text,
			Style: render.Style{Fg: commentTypeColor(t, line.CommentType), Bold: true},
		})
	case app.PeekCommentBody:
		spans = append(spans, render.Span{
			Text: "  " + line.Text, Style: render.Style{Fg: t.FgPrimary},
		})
	case app.PeekNotice:
		spans = append(spans, render.Span{
			Text: line.Text, Style: render.Style{Fg: t.FgDim},
		})
	case app.PeekSeparator:
	}
	return render.TruncateOrPadSpans(spans, width, render.Style{})
}
