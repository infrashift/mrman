package ui

import (
	"fmt"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/render"
	"github.com/infrashift/mrman/internal/theme"
)

// CommitStripPane renders the inline commit selector shown above the diff
// during a multi-commit review: which commits the diff currently covers,
// and which the reviewer has already signed off on.
type CommitStripPane struct {
	Theme *theme.Theme
}

// Title returns the pane's border title, carrying the selection summary.
func (p *CommitStripPane) Title(a *app.App) string {
	if summary := a.CommitSelectionSummary(); summary != "" {
		return fmt.Sprintf(" Commits · %s ", summary)
	}
	return fmt.Sprintf(" Commits · %d ", len(a.ReviewCommits))
}

// BuildLines renders the visible commit rows.
func (p *CommitStripPane) BuildLines(a *app.App, width, height int) []string {
	t := p.Theme
	emitter := &render.Emitter{}
	lines := make([]string, 0, height)

	count := a.CommitSelectRowCount()
	offset := min(a.CommitListScrollOffset, max(count-height, 0))
	for display := offset; display < count && len(lines) < height; display++ {
		dataIdx := a.CommitDataIndex(display)
		if dataIdx < 0 || dataIdx >= len(a.CommitList) {
			continue
		}
		commit := a.CommitList[dataIdx]
		isCursor := display == a.CommitListCursor && a.FocusedPanel == app.PanelCommitSelector
		isSelected := a.IsCommitSelected(dataIdx)

		box, boxStyle := "▢ ", render.Style{Fg: t.Pending}
		if isSelected {
			box, boxStyle = "▣ ", render.Style{Fg: t.Reviewed}
		}
		// A commit already covered by a submitted review of your own is
		// marked so a re-review can skip straight to what is new.
		covered := " "
		if a.IsCommitReviewed(commit.ID) {
			covered = "✓"
		}

		spans := []render.Span{
			{Text: cursorIndicator(isCursor), Style: render.Style{Fg: t.BorderFocused}},
			{Text: box, Style: boxStyle},
			{Text: covered + " ", Style: render.Style{Fg: t.Reviewed}},
			{Text: commit.ShortID + " ", Style: render.Style{Fg: t.CursorColor}},
			{Text: commit.Summary, Style: render.Style{Fg: t.FgPrimary}},
		}
		if isCursor {
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

// commitStripHeight is the pane's total height including borders, or 0 when
// it is not shown.
func commitStripHeight(a *app.App) int {
	if !a.HasInlineCommitSelector() {
		return 0
	}
	return min(len(a.ReviewCommits)+2, app.CommitStripMaxHeight)
}
