package ui

import (
	"fmt"
	"strings"

	render "github.com/infrashift/mrman/charmkit/cellrender"
	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/theme"
)

// CommentNavPane renders the comment navigator: one row per comment in the
// review, in display order, so a reviewer can see everything they have
// written (and everything the forge already holds) without scrolling the
// diff to find it.
type CommentNavPane struct {
	Theme *theme.Theme
}

// Title returns the pane's border title.
func (p *CommentNavPane) Title(a *app.App) string {
	return fmt.Sprintf(" Comments · %d ", len(a.BuildCommentNavigatorItems()))
}

// BuildLines renders the visible navigator rows.
func (p *CommentNavPane) BuildLines(a *app.App, width, height int) []string {
	t := p.Theme
	emitter := &render.Emitter{}
	items := a.BuildCommentNavigatorItems()
	lines := make([]string, 0, height)

	for i := a.CommentNav.Offset; i < len(items) && len(lines) < height; i++ {
		item := &items[i]
		selected := i == a.CommentNav.Cursor && a.FocusedPanel == app.PanelComments

		marker, markerStyle := commentNavMarker(t, item)
		spans := []render.Span{
			{Text: marker, Style: markerStyle},
			{Text: commentNavLabel(item), Style: render.Style{Fg: t.FgPrimary}},
		}
		if item.Author != "" && item.Author != a.Username {
			style := render.Style{Fg: t.FgSecondary}
			if accent, ok := theme.AuthorAccent(a.Username, item.Author); ok {
				style.Fg = accent
			}
			spans = append(spans, render.Span{Text: " @" + item.Author, Style: style})
		}

		if selected {
			for j := range spans {
				spans[j].Style.Bg = t.BgHighlight
			}
			spans = render.PadToWidth(spans, width, render.Style{Bg: t.BgHighlight})
		}
		spans = render.TruncateOrPadSpans(spans, width, render.Style{})
		lines = append(lines, emitter.Line(spans))
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

// commentNavMarker is the scope glyph for a navigator row. Remote items get
// a distinct, muted marker so a reviewer can tell at a glance which rows are
// their own unsent drafts and which are the forge's existing conversation.
func commentNavMarker(t *theme.Theme, item *app.CommentNavigatorItem) (string, render.Style) {
	if item.IsRemote {
		return "◇ ", render.Style{Fg: t.FgDim}
	}
	switch item.Key.Scope {
	case app.NavScopeReview:
		return "★ ", render.Style{Fg: t.CommentNote}
	case app.NavScopeFile:
		return "▣ ", render.Style{Fg: t.CommentSuggestion}
	default:
		return "● ", render.Style{Fg: t.CommentIssue}
	}
}

// commentNavLabel is the location text for a navigator row: the tightest
// anchor the comment has.
func commentNavLabel(item *app.CommentNavigatorItem) string {
	if item.Path == nil {
		return "review"
	}
	name := *item.Path
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	if item.Line == nil {
		return name
	}
	return fmt.Sprintf("%s:%d", name, *item.Line)
}
