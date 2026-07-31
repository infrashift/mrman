// Package ui renders mrman's TUI from the app state, porting tuicr's ui
// modules onto the render-package compositor model: every visual effect is
// resolved at line-build time and serialized once per frame.
package ui

import (
	"fmt"
	"image/color"
	"strings"

	render "github.com/infrashift/mrman/charmkit/cellrender"
	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/theme"
)

// headerRule is the 40-char double rule after section headers.
const headerRule = "════════════════════════════════════════"

// DiffPane renders the diff panel's inner content rows.
type DiffPane struct {
	Theme *theme.Theme
}

// cursorIndicator returns the two-column cursor gutter for a row.
func cursorIndicator(current bool) string {
	if current {
		return "▶ "
	}
	return "  "
}

// BuildLines produces one string per visible inner row of the diff pane.
// width/height are the inner dimensions (borders excluded).
func (p *DiffPane) BuildLines(a *app.App, width, height int) []string {
	t := p.Theme
	emitter := &render.Emitter{}
	lines := make([]string, 0, height)

	start := a.DiffState.ScrollOffset
	end := start + height
	total := a.TotalLines()
	if end > total {
		end = total
	}

	lw := a.LinenoWidth()
	for i := start; i < end; i++ {
		if i >= len(a.LineAnnotations) {
			break
		}
		ann := &a.LineAnnotations[i]
		row := p.buildRow(a, ann, i, lw, width)

		// Overlay order ports tuicr's painter sequence: section tint →
		// row bg pad → cursor line → horizontal scroll → serialize.
		switch ann.Kind {
		case app.AnnHunkHeader, app.AnnExpander, app.AnnHiddenLines:
			row.Spans = render.FillBg(row.Spans, t.SectionHighlightBg())
			row.Spans = render.PadToWidth(row.Spans, width, render.Style{Bg: t.SectionHighlightBg()})
		}
		if row.BaseBg != nil {
			row.Spans = render.FillBg(row.Spans, row.BaseBg)
			row.Spans = render.PadToWidth(row.Spans, width, render.Style{Bg: row.BaseBg})
		}
		if a.CursorLineHighlight && i == a.DiffState.CursorLine && !ann.IsDecoration() {
			row.Spans = overrideRowBg(row.Spans, width, t.CursorLineBg)
		}
		if a.DiffState.ScrollX > 0 && !a.DiffState.WrapLines {
			row.Spans = render.ApplyHorizontalScroll(row.Spans, a.DiffState.ScrollX)
		}
		lines = append(lines, emitter.Line(row.Spans))
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

// overrideRowBg replaces every span's background and pads to full width.
func overrideRowBg(spans []render.Span, width int, bg color.Color) []render.Span {
	if bg == nil {
		return spans
	}
	out := make([]render.Span, len(spans))
	for i, s := range spans {
		s.Style.Bg = bg
		out[i] = s
	}
	return render.PadToWidth(out, width, render.Style{Bg: bg})
}

func (p *DiffPane) buildRow(a *app.App, ann *app.AnnotatedLine, idx, lw, width int) render.LogicalLine {
	t := p.Theme
	cur := idx == a.DiffState.CursorLine
	ind := render.Span{Text: cursorIndicator(cur), Style: render.Style{Fg: t.BorderFocused}}
	if !cur {
		ind.Style = render.Style{}
	}

	switch ann.Kind {
	case app.AnnFileHeader:
		return p.fileHeaderRow(a, ann, ind, width)
	case app.AnnHunkHeader:
		return p.hunkHeaderRow(a, ann, ind)
	case app.AnnDiffLine:
		return p.diffLineRow(a, ann, ind, lw)
	case app.AnnSideBySideLine:
		return p.sbsLineRow(a, ann, ind, lw, width)
	case app.AnnExpandedContext:
		return p.expandedContextRow(a, ann, ind, lw)
	case app.AnnExpander:
		return p.expanderRow(a, ann, ind)
	case app.AnnHiddenLines:
		return render.LogicalLine{Kind: render.RowHiddenStub, Ann: idx, Spans: []render.Span{
			ind,
			{Text: fmt.Sprintf("       ... %d lines hidden ...", ann.Count), Style: render.Style{Fg: t.FgDim}},
		}}
	case app.AnnBinaryOrEmpty:
		file := &a.DiffFiles[ann.FileIdx]
		label := "(no changes)"
		if file.IsBinary {
			label = "(binary file)"
		} else if file.IsTooLarge {
			label = "(file too large to display)"
		}
		return render.LogicalLine{Kind: render.RowDiffLine, Ann: idx, Spans: []render.Span{
			ind, {Text: label, Style: render.Style{Fg: t.FgDim}},
		}}
	case app.AnnReviewComment, app.AnnFileComment, app.AnnLineComment:
		return p.commentBoxRow(a, ann, idx, ind, width)
	case app.AnnRemoteThreadLine, app.AnnRemoteReviewSummaryLine:
		return p.remoteBoxRow(a, ann, idx, ind, width)
	case app.AnnReviewCommentsHeader:
		text := "═══ Review Comments "
		fillWidth := width - render.StringWidth(text) - render.StringWidth(ind.Text)
		if fillWidth < 0 {
			fillWidth = 0
		}
		return render.LogicalLine{Kind: render.RowFileHeader, Ann: idx, Spans: []render.Span{
			ind,
			{Text: text + strings.Repeat("═", fillWidth), Style: render.Style{Fg: t.FgPrimary, Bold: true}},
		}}
	case app.AnnSpacing:
		return render.LogicalLine{Kind: render.RowBlank, Ann: idx, Spans: []render.Span{ind}}
	}
	return render.LogicalLine{Kind: render.RowBlank, Ann: idx, Spans: []render.Span{ind}}
}

func (p *DiffPane) fileHeaderRow(a *app.App, ann *app.AnnotatedLine, ind render.Span, width int) render.LogicalLine {
	t := p.Theme
	file := &a.DiffFiles[ann.FileIdx]
	var b strings.Builder
	b.WriteString("═══ ")
	if a.Session.IsFileReviewed(file.DisplayPath()) {
		b.WriteString("✓ ")
	}
	b.WriteString(file.DisplayPath())
	if !file.IsCommitMessage && !a.IsPristineMode {
		fmt.Fprintf(&b, " [%c]", file.Status.Char())
	}
	// Which patch of the series this entry came from, when the diff spans
	// more than one and the path alone would not say.
	if label := a.FilePatchLabel(ann.FileIdx); label != "" {
		fmt.Fprintf(&b, " · patch %s", label)
	}
	b.WriteString(" ")
	text := b.String()
	// Fill the rest of the row with ═ to the right edge.
	fillWidth := width - render.StringWidth(text) - render.StringWidth(ind.Text)
	fill := ""
	if fillWidth > 0 {
		fill = strings.Repeat("═", fillWidth)
	} else {
		fill = headerRule
	}
	return render.LogicalLine{Kind: render.RowFileHeader, Ann: 0, Spans: []render.Span{
		ind,
		{Text: text, Style: render.Style{Fg: t.FgPrimary, Bold: true}},
		{Text: fill, Style: render.Style{Fg: t.FgPrimary, Bold: true}},
	}}
}

func (p *DiffPane) hunkHeaderRow(a *app.App, ann *app.AnnotatedLine, ind render.Span) render.LogicalLine {
	t := p.Theme
	file := &a.DiffFiles[ann.FileIdx]
	header := file.Hunks[ann.HunkIdx].Header
	style := render.Style{Fg: t.FgDim}
	if key, ok := file.HunkReviewKey(ann.HunkIdx); ok && a.Session.IsHunkReviewed(file.DisplayPath(), key) {
		header = "✓ " + header
		style = render.Style{Fg: t.Reviewed}
	}
	return render.LogicalLine{Kind: render.RowHunkHeader, Spans: []render.Span{
		ind, {Text: header, Style: style},
	}}
}

func (p *DiffPane) diffLineRow(a *app.App, ann *app.AnnotatedLine, ind render.Span, lw int) render.LogicalLine {
	t := p.Theme
	file := &a.DiffFiles[ann.FileIdx]
	line := &file.Hunks[ann.HunkIdx].Lines[ann.LineIdx]

	lineno := ""
	switch line.Origin {
	case model.OriginAddition:
		if line.NewLineno != nil {
			lineno = fmt.Sprintf("%d", *line.NewLineno)
		}
	case model.OriginDeletion:
		if line.OldLineno != nil {
			lineno = fmt.Sprintf("%d", *line.OldLineno)
		}
	default:
		if line.NewLineno != nil {
			lineno = fmt.Sprintf("%d", *line.NewLineno)
		} else if line.OldLineno != nil {
			lineno = fmt.Sprintf("%d", *line.OldLineno)
		}
	}
	if file.IsCommitMessage {
		lineno = ""
	}

	var baseBg color.Color
	prefix, contentStyle := "  ", render.Style{Fg: t.DiffContext}
	switch line.Origin {
	case model.OriginAddition:
		prefix, contentStyle = "▌ ", render.Style{Fg: t.DiffAdd}
		baseBg = t.DiffAddBg
		if line.HighlightedSpans != nil {
			baseBg = t.SyntaxAddBg
		}
	case model.OriginDeletion:
		prefix, contentStyle = "▌ ", render.Style{Fg: t.DiffDel}
		baseBg = t.DiffDelBg
		if line.HighlightedSpans != nil {
			baseBg = t.SyntaxDelBg
		}
	}

	spans := []render.Span{
		ind,
		{Text: fmt.Sprintf("%*s ", lw, lineno), Style: render.Style{Fg: t.FgDim}},
		{Text: prefix, Style: contentStyle},
	}
	if line.HighlightedSpans != nil {
		spans = append(spans, fromSyntaxSpans(line.HighlightedSpans)...)
	} else {
		spans = append(spans, render.Span{Text: line.Content, Style: contentStyle})
	}
	return render.LogicalLine{Kind: render.RowDiffLine, BaseBg: baseBg, Spans: spans}
}

// sbsLineRow renders one paired side-by-side row:
// [ind][old lw][ ][marker][left content pad] │ [new lw][ ][marker][right content pad]
func (p *DiffPane) sbsLineRow(a *app.App, ann *app.AnnotatedLine, ind render.Span, lw, width int) render.LogicalLine {
	t := p.Theme
	file := &a.DiffFiles[ann.FileIdx]
	hunk := &file.Hunks[ann.HunkIdx]
	contentWidth := (width - app.SbsOverhead(lw)) / 2
	if contentWidth < 1 {
		contentWidth = 1
	}

	sideSpans := func(lineIdx *int, wantOrigin model.LineOrigin, lineno *uint32) []render.Span {
		numText := ""
		if lineno != nil {
			numText = fmt.Sprintf("%d", *lineno)
		}
		if lineIdx == nil {
			pad := strings.Repeat(" ", lw+2+contentWidth)
			return []render.Span{{Text: pad}}
		}
		line := &hunk.Lines[*lineIdx]
		style := render.Style{Fg: t.DiffContext}
		marker := " "
		var bg color.Color
		switch wantOrigin {
		case model.OriginDeletion:
			style, marker, bg = render.Style{Fg: t.DiffDel}, "▌", t.DiffDelBg
		case model.OriginAddition:
			style, marker, bg = render.Style{Fg: t.DiffAdd}, "▌", t.DiffAddBg
		}
		if line.Origin == model.OriginContext {
			style, marker, bg = render.Style{Fg: t.DiffContext}, " ", nil
		}
		var content []render.Span
		if line.HighlightedSpans != nil {
			content = fromSyntaxSpans(line.HighlightedSpans)
			switch line.Origin {
			case model.OriginDeletion:
				bg = t.SyntaxDelBg
			case model.OriginAddition:
				bg = t.SyntaxAddBg
			case model.OriginContext:
			}
		} else {
			content = []render.Span{{Text: line.Content, Style: style}}
		}
		padStyle := render.Style{Bg: bg}
		if bg != nil {
			for i := range content {
				if content[i].Style.Bg == nil {
					content[i].Style.Bg = bg
				}
			}
		}
		row := []render.Span{
			{Text: fmt.Sprintf("%*s ", lw, numText), Style: render.Style{Fg: t.FgDim}},
			{Text: marker, Style: render.Style{Fg: style.Fg, Bg: bg}},
		}
		row = append(row, render.TruncateOrPadSpans(content, contentWidth, padStyle)...)
		return row
	}

	spans := []render.Span{ind}
	spans = append(spans, sideSpans(ann.DelLineIdx, model.OriginDeletion, ann.OldLineno)...)
	spans = append(spans, render.Span{Text: " │ ", Style: render.Style{Fg: t.FgDim}})
	spans = append(spans, sideSpans(ann.AddLineIdx, model.OriginAddition, ann.NewLineno)...)
	return render.LogicalLine{Kind: render.RowDiffLine, Spans: spans}
}

func (p *DiffPane) expandedContextRow(a *app.App, ann *app.AnnotatedLine, ind render.Span, lw int) render.LogicalLine {
	t := p.Theme
	line := a.GetExpandedLine(ann.GapID, ann.LineIdx)
	if line == nil {
		return render.LogicalLine{Kind: render.RowBlank, Spans: []render.Span{ind}}
	}
	lineno := ""
	if line.NewLineno != nil {
		lineno = fmt.Sprintf("%d", *line.NewLineno)
	}
	return render.LogicalLine{Kind: render.RowDiffLine, Spans: []render.Span{
		ind,
		{Text: fmt.Sprintf("%*s ", lw, lineno), Style: render.Style{Fg: t.ExpandedContextFg}},
		{Text: "  ", Style: render.Style{Fg: t.ExpandedContextFg}},
		{Text: line.Content, Style: render.Style{Fg: t.ExpandedContextFg}},
	}}
}

func (p *DiffPane) expanderRow(a *app.App, ann *app.AnnotatedLine, ind render.Span) render.LogicalLine {
	t := p.Theme
	arrow := "↕"
	switch ann.Direction {
	case app.ExpandDown:
		arrow = "↓"
	case app.ExpandUp:
		arrow = "↑"
	}
	remaining, _ := a.GapSize(ann.GapID)
	count := int(remaining)
	if count > app.GapExpandBatch {
		count = app.GapExpandBatch
	}
	return render.LogicalLine{Kind: render.RowExpander, Spans: []render.Span{
		ind,
		{Text: fmt.Sprintf("       ... %s expand (%d lines) ...", arrow, count), Style: render.Style{Fg: t.FgDim}},
	}}
}
