package ui

import (
	"fmt"
	"strings"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/render"
	"github.com/infrashift/mrman/internal/theme"
)

// FileListPane renders the file tree panel's inner rows.
type FileListPane struct {
	Theme *theme.Theme
}

// Title returns the pane's border title.
func (p *FileListPane) Title(a *app.App) string {
	return fmt.Sprintf(" Files · %d/%d ", a.ReviewedCount(), a.FileCount())
}

// BuildLines renders the visible tree rows.
func (p *FileListPane) BuildLines(a *app.App, width, height int) []string {
	t := p.Theme
	emitter := &render.Emitter{}
	items := a.BuildVisibleItems()
	lines := make([]string, 0, height)

	offset := a.FileListState.Offset()
	for i := offset; i < len(items) && len(lines) < height; i++ {
		item := items[i]
		selected := i == a.FileListState.Selected()
		var spans []render.Span
		indent := strings.Repeat("  ", item.Depth)

		if item.IsDir {
			glyph := "▶ "
			if item.Expanded {
				glyph = "▼ "
			}
			name := item.Path
			if idx := strings.LastIndex(name, "/"); idx >= 0 {
				name = name[idx+1:]
			}
			spans = []render.Span{
				{Text: indent},
				{Text: glyph, Style: render.Style{Fg: t.DiffHunkHeader}},
				{Text: name + "/", Style: render.Style{Fg: t.FgPrimary}},
			}
		} else {
			file := &a.DiffFiles[item.FileIdx]
			path := file.DisplayPath()
			name := path
			if idx := strings.LastIndex(path, "/"); idx >= 0 {
				name = path[idx+1:]
			}
			box, boxStyle := "▢ ", render.Style{Fg: t.Pending}
			if a.Session.IsFileReviewed(path) {
				box, boxStyle = "▣ ", render.Style{Fg: t.Reviewed}
			}
			spans = []render.Span{
				{Text: indent},
				{Text: box, Style: boxStyle},
			}
			if !file.IsCommitMessage && !a.IsPristineMode {
				spans = append(spans, render.Span{
					Text:  fmt.Sprintf("%c ", file.Status.Char()),
					Style: render.Style{Fg: theme.FileStatusColor(t, file.Status.Char())},
				})
			}
			spans = append(spans, render.Span{Text: name, Style: render.Style{Fg: t.FgPrimary}})
		}

		if selected {
			for j := range spans {
				spans[j].Style.Bg = t.BgHighlight
			}
			spans = render.PadToWidth(spans, width, render.Style{Bg: t.BgHighlight})
		}
		if a.FileListState.ScrollX > 0 {
			spans = render.ApplyHorizontalScroll(spans, a.FileListState.ScrollX)
		}
		spans = render.TruncateOrPadSpans(spans, width, render.Style{})
		lines = append(lines, emitter.Line(spans))
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}
