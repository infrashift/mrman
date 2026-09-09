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
	total := a.TotalLines()

	lw := a.LinenoWidth()
	// Logical lines fully on screen, reported back through VisibleLineCount
	// so scrolling and cursor-visibility stay right when one line wraps to
	// several rows. Counted only when every row of the line fits: a line
	// half off the bottom is not one the cursor may sit on unseen.
	fitted := 0
	// rowAnns records which annotation each drawn row came from, so mouse
	// hit-testing does not have to guess how many rows a line took.
	rowAnns := make([]int, 0, height)
	for i := start; i < total && len(lines) < height; i++ {
		if i >= len(a.LineAnnotations) {
			break
		}
		ann := &a.LineAnnotations[i]
		row := p.buildRow(a, ann, i, lw, width)

		// The selected range is measured once against the unwrapped row, then
		// shifted per visual row below by the columns already consumed.
		selLo, selHi, hasSel := selectionCols(a, ann, i, lw, width)

		// One logical line is one row unless wrapping splits it. The row
		// treatment below runs per visual row so a wrapped continuation
		// carries the same tint as the row it came from.
		rows := [][]render.Span{row.Spans}
		if a.DiffState.WrapLines {
			rows = render.WrapSpans(row.Spans, width)
		}
		wholeLineFits := true
		consumed := 0
		for _, spans := range rows {
			if len(lines) >= height {
				wholeLineFits = false
				break
			}
			// Captured before the overlays below pad the row, because the
			// column math has to follow the wrapped text, not the padding.
			rowWidth := render.SpanWidth(spans)
			// Overlay order ports tuicr's painter sequence: section tint →
			// row bg pad → cursor line → horizontal scroll → serialize.
			switch ann.Kind {
			case app.AnnHunkHeader, app.AnnExpander, app.AnnHiddenLines:
				spans = render.FillBg(spans, t.SectionHighlightBg())
				spans = render.PadToWidth(spans, width, render.Style{Bg: t.SectionHighlightBg()})
			}
			if row.BaseBg != nil {
				spans = render.FillBg(spans, row.BaseBg)
				spans = render.PadToWidth(spans, width, render.Style{Bg: row.BaseBg})
			}
			if a.CursorLineHighlight && i == a.DiffState.CursorLine && !ann.IsDecoration() {
				spans = overrideRowBg(spans, width, t.CursorLineBg)
			}
			// After the cursor line so a selection stays visible on the row
			// the cursor sits on, and before horizontal scroll because the
			// columns above are measured on the unscrolled row.
			if hasSel {
				spans = render.OverrideBg(spans, selLo-consumed, selHi-consumed,
					t.VisualSelectionBg())
			}
			consumed += rowWidth
			if a.DiffState.ScrollX > 0 && !a.DiffState.WrapLines {
				spans = render.ApplyHorizontalScroll(spans, a.DiffState.ScrollX)
			}
			// Clamp to the pane. Nothing above this guarantees it: PadToWidth
			// only ever pads, and the unified row builders take no width at
			// all. An over-wide row escapes the panel, the terminal wraps it,
			// and the frame grows past the screen — which is what pushes the
			// status bar and the comment box out of sight.
			//
			// Only ever narrows. Padding every short row here would repaint
			// the gap between the text and the panel edge in the default
			// background rather than the panel's own.
			if render.SpanWidth(spans) > width {
				spans = render.TruncateOrPadSpans(spans, width, render.Style{})
			}
			lines = append(lines, emitter.Line(spans))
			rowAnns = append(rowAnns, i)
		}
		if wholeLineFits {
			fitted++
		}
	}
	a.DiffState.VisibleLineCount = fitted
	a.DiffState.RowAnnotations = rowAnns
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

// selectionCols is the display-column range [lo, hi) of row idx that the
// active selection covers, in unscrolled row coordinates. Reports false when
// the row carries no selection.
func selectionCols(a *app.App, ann *app.AnnotatedLine, idx, lw, width int) (int, int, bool) {
	sel, ok := a.SelectionForRow(idx)
	if !ok {
		return 0, 0, false
	}
	if sel.WholeRow {
		return 0, width, true
	}
	content, ok := a.ContentForSide(idx, sel.Side)
	if !ok {
		return 0, 0, false
	}
	gutter := app.UnifiedGutter(lw)
	if ann.Kind == app.AnnSideBySideLine {
		// ContentForSide falls back to the other pane when the requested one
		// is empty, so the highlight has to follow the pane the text actually
		// came from rather than the side that was asked for.
		onNew := sel.Side == model.LineSideNew
		if onNew && ann.AddLineIdx == nil {
			onNew = ann.DelLineIdx == nil
		} else if !onNew && ann.DelLineIdx == nil {
			onNew = true
		}
		if onNew {
			gutter = app.SbsRightGutter(lw, width)
		} else {
			gutter = app.SbsLeftGutter(lw)
		}
	}
	lo := gutter + runeOffsetToCol(content, sel.Lo)
	hi := gutter + runeOffsetToCol(content, sel.Hi)
	if ann.Kind == app.AnnSideBySideLine {
		// Content is clipped to its own column, so the highlight must stop at
		// the divider rather than bleeding into the other side.
		limit := gutter + app.SbsContentWidth(lw, width)
		lo, hi = min(lo, limit), min(hi, limit)
	}
	return lo, hi, hi > lo
}

// runeOffsetToCol converts a rune offset into s to a display column. The
// selection model counts runes, the renderer counts columns, and a tab or a
// wide rune is more than one column wide.
func runeOffsetToCol(s string, off int) int {
	col, n := 0, 0
	for _, r := range s {
		if n >= off {
			break
		}
		col += render.StringWidth(string(r))
		n++
	}
	return col
}

// colToRuneOffset is runeOffsetToCol's inverse: the rune offset into s at
// display column col, clamped to the end of s. A column landing inside a wide
// rune resolves to the offset before it.
func colToRuneOffset(s string, col int) int {
	cur, n := 0, 0
	for _, r := range s {
		w := render.StringWidth(string(r))
		if cur+w > col {
			break
		}
		cur += w
		n++
	}
	return n
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
		fillWidth := max(width-render.StringWidth(text)-render.StringWidth(ind.Text), 0)
		return render.LogicalLine{Kind: render.RowFileHeader, Ann: idx, Spans: []render.Span{
			ind,
			{Text: text + strings.Repeat("═", fillWidth), Style: render.Style{Fg: t.FgPrimary, Bold: true}},
		}}
	case app.AnnSpacing:
		return render.LogicalLine{Kind: render.RowBlank, Ann: idx, Spans: []render.Span{ind}}
	}
	return render.LogicalLine{Kind: render.RowBlank, Ann: idx, Spans: []render.Span{ind}}
}

// renamedFrom returns the path a file moved from, or "" when it did not
// move. It reads the paths rather than the status so a copy — which carries
// both paths but a different badge — is described too.
func renamedFrom(file *model.DiffFile) string {
	if file.OldPath == nil || file.NewPath == nil || *file.OldPath == *file.NewPath {
		return ""
	}
	return *file.OldPath
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
	// Where a renamed file came from. Without this the move is invisible:
	// DisplayPath is the new path, OldPath is rendered nowhere else, and an
	// R badge on its own says a file moved without saying from where.
	if from := renamedFrom(file); from != "" {
		fmt.Fprintf(&b, " · renamed from %s", from)
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
	fill := headerRule
	if fillWidth > 0 {
		fill = strings.Repeat("═", fillWidth)
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
	contentWidth := app.SbsContentWidth(lw, width)

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
	count := min(int(remaining), app.GapExpandBatch)
	return render.LogicalLine{Kind: render.RowExpander, Spans: []render.Span{
		ind,
		{Text: fmt.Sprintf("       ... %s expand (%d lines) ...", arrow, count), Style: render.Style{Fg: t.FgDim}},
	}}
}
