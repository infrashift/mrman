package ui

import (
	"fmt"
	"image/color"
	"strings"

	render "github.com/infrashift/mrman/charmkit/cellrender"
	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/theme"
)

// remoteRowIndex walks back to find which display line of a remote box this
// annotation is, returning (displayIdx, total). Remote boxes are contiguous
// runs of one kind with the same thread or summary index.
func remoteRowIndex(a *app.App, idx int) int {
	ann := &a.LineAnnotations[idx]
	displayIdx := 0
	for i := idx - 1; i >= 0; i-- {
		prev := &a.LineAnnotations[i]
		if prev.Kind != ann.Kind || prev.ThreadIdx != ann.ThreadIdx || prev.SummaryIdx != ann.SummaryIdx {
			break
		}
		displayIdx++
	}
	return displayIdx
}

// remoteBoxRow renders one display line of a read-only remote box: the top
// border carries the author and status badge, the body carries the wrapped
// conversation, and the bottom border closes it.
//
// The box is deliberately visually distinct from a local comment box — a
// doubled left rule — so a reviewer never mistakes the forge's existing
// discussion for one of their own unsent drafts.
func (p *DiffPane) remoteBoxRow(a *app.App, ann *app.AnnotatedLine, idx int, ind render.Span, width int) render.LogicalLine {
	t := p.Theme
	displayIdx := remoteRowIndex(a, idx)

	var (
		author, badge string
		segments      []string
		total         int
	)
	switch ann.Kind {
	case app.AnnRemoteThreadLine:
		threads := a.VisibleRemoteThreads()
		if ann.ThreadIdx >= len(threads) {
			return render.LogicalLine{Kind: render.RowBlank, Ann: idx, Spans: []render.Span{ind}}
		}
		thread := &threads[ann.ThreadIdx]
		if root := thread.Root(); root != nil {
			author = root.Author
		}
		badge = app.RemoteThreadBadge(thread)
		segments = app.RemoteThreadSegments(thread, a.DiffState.ViewportWidth)
		total = app.RemoteThreadDisplayLines(thread, a.DiffState.ViewportWidth)
	case app.AnnRemoteReviewSummaryLine:
		summaries := a.VisibleRemoteSummaries()
		if ann.SummaryIdx >= len(summaries) {
			return render.LogicalLine{Kind: render.RowBlank, Ann: idx, Spans: []render.Span{ind}}
		}
		summary := &summaries[ann.SummaryIdx]
		author = summary.Author
		badge = summary.State.BadgeLabel()
		segments = app.RemoteSummarySegments(summary, a.DiffState.ViewportWidth)
		total = app.RemoteSummaryDisplayLines(summary, a.DiffState.ViewportWidth)
	default:
		return render.LogicalLine{Kind: render.RowBlank, Ann: idx, Spans: []render.Span{ind}}
	}

	borderStyle := render.Style{Fg: t.FgDim}
	authorStyle := render.Style{Fg: t.FgSecondary, Bold: true}
	if accent, ok := theme.AuthorAccent(a.Username, author); ok {
		authorStyle.Fg = accent
	}

	switch displayIdx {
	case 0:
		head := "    ╒══ "
		label := ""
		if author != "" {
			label = "@" + author + " "
		}
		badgeText := ""
		if badge != "" {
			badgeText = "(" + badge + ") "
		}
		fillWidth := width - render.StringWidth(head) - render.StringWidth(label) -
			render.StringWidth(badgeText) - render.StringWidth(ind.Text)
		if fillWidth < 0 {
			fillWidth = 0
		}
		return render.LogicalLine{Kind: render.RowCommentTop, Ann: idx, Spans: []render.Span{
			ind,
			{Text: head, Style: borderStyle},
			{Text: label, Style: authorStyle},
			{Text: badgeText, Style: render.Style{Fg: remoteBadgeColor(t, badge)}},
			{Text: strings.Repeat("═", fillWidth), Style: borderStyle},
		}}
	case total - 1:
		fillWidth := width - 5 - render.StringWidth(ind.Text)
		if fillWidth < 0 {
			fillWidth = 0
		}
		return render.LogicalLine{Kind: render.RowCommentBottom, Ann: idx, Spans: []render.Span{
			ind,
			{Text: "    ╘" + strings.Repeat("═", fillWidth), Style: borderStyle},
		}}
	default:
		text := ""
		if segIdx := displayIdx - 1; segIdx >= 0 && segIdx < len(segments) {
			text = segments[segIdx]
		}
		// Reply/author headers inside the body read as metadata, not prose.
		bodyStyle := render.Style{Fg: t.FgSecondary}
		if strings.HasPrefix(text, "@") || strings.HasPrefix(text, "↳ @") {
			bodyStyle = authorStyle
			if accent, ok := theme.AuthorAccent(a.Username, strings.TrimPrefix(
				strings.TrimPrefix(text, "↳ "), "@")); ok {
				bodyStyle.Fg = accent
			}
		}
		return render.LogicalLine{Kind: render.RowCommentMiddle, Ann: idx, Spans: []render.Span{
			ind,
			{Text: "    ║  ", Style: borderStyle},
			{Text: text, Style: bodyStyle},
		}}
	}
}

// remoteBadgeColor maps a thread or review badge to its theme slot.
func remoteBadgeColor(t *theme.Theme, badge string) color.Color {
	switch badge {
	case "approved":
		return t.DiffAdd
	case "changes requested":
		return t.DiffDel
	}
	return t.FgDim
}

// remoteCommentsSummaryLine describes the loaded remote discussion for the
// status bar, "" when there is nothing to report.
func remoteCommentsSummaryLine(a *app.App) string {
	if !a.InPrMode() {
		return ""
	}
	if a.Pr.ThreadsLoading {
		return "loading comments…"
	}
	threads := a.VisibleRemoteThreads()
	if len(threads) == 0 && len(a.VisibleRemoteSummaries()) == 0 {
		return ""
	}
	unresolved := 0
	for i := range threads {
		if !threads[i].IsResolved {
			unresolved++
		}
	}
	noun := "threads"
	if len(threads) == 1 {
		noun = "thread"
	}
	if unresolved == len(threads) {
		return fmt.Sprintf("%d %s", len(threads), noun)
	}
	return fmt.Sprintf("%d %s (%d unresolved)", len(threads), noun, unresolved)
}
