package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	render "github.com/infrashift/mrman/charmkit/cellrender"
	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/ignore"
	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs"
)

// selectorView renders the full-screen review-target selector (Local /
// Pull Requests tabs), replacing the normal layout entirely.
func (m *Model) selectorView() []string {
	a := m.App
	t := m.Theme
	emitter := &render.Emitter{}
	var rows []string

	// Top bar: brand + one chip per tab.
	branchInfo := ""
	switch {
	case a.TargetTab == app.TargetTabPullRequests:
		branchInfo = prSelectorTitle(a)
	case a.TargetTab == app.TargetTabPatches:
		branchInfo = " " + shortenDir(a.PatchTabDir(), 44) + " "
	case a.VcsInfo != nil:
		branch := "detached"
		if a.VcsInfo.BranchName != nil {
			branch = *a.VcsInfo.BranchName
		}
		branchInfo = fmt.Sprintf(" %s:%s ", a.VcsInfo.Type, branch)
	}
	topSpans := []render.Span{
		{Text: " mrman  ", Style: render.Style{Fg: t.FgPrimary, Bg: t.StatusBarBg, Bold: true}},
	}
	for i, tab := range app.AllTargetTabs() {
		if i > 0 {
			topSpans = append(topSpans, render.Span{Text: " ", Style: render.Style{Bg: t.StatusBarBg}})
		}
		style := render.Style{Fg: t.FgDim, Bg: t.StatusBarBg}
		if tab == a.TargetTab {
			style = render.Style{Fg: t.FgPrimary, Bg: t.BgHighlight, Bold: true}
		}
		topSpans = append(topSpans, render.Span{Text: " " + tab.Label() + " ", Style: style})
	}
	pad := max(m.width-render.SpanWidth(topSpans)-render.StringWidth(branchInfo), 0)
	topSpans = append(topSpans,
		render.Span{Text: strings.Repeat(" ", pad), Style: render.Style{Bg: t.StatusBarBg}},
		render.Span{Text: branchInfo, Style: render.Style{Fg: t.FgSecondary, Bg: t.StatusBarBg}})
	rows = append(rows, emitter.Line(topSpans))

	// Body rows.
	bodyH := m.height - 2
	var body []string
	switch a.TargetTab {
	case app.TargetTabPullRequests:
		a.EnsurePrState().TabViewportHeight = bodyH
		body = m.selectorPrRows(bodyH)
	case app.TargetTabPatches:
		a.SetPatchTabViewportHeight(bodyH)
		body = m.selectorPatchRows(bodyH)
	default:
		a.CommitListViewportHeight = bodyH
		body = m.selectorLocalRows(bodyH)
	}
	for len(body) < bodyH {
		body = append(body, "")
	}
	rows = append(rows, body[:bodyH]...)

	// Footer.
	hint := m.selectorFooterHint()
	selected := ""
	switch a.TargetTab {
	case app.TargetTabPullRequests:
		if n := len(a.PrTabFilteredRows()); n > 0 {
			selected = fmt.Sprintf(" %d shown ", n)
		}
	case app.TargetTabPatches:
		if n := a.PatchTabRowCount(); n > 0 {
			selected = fmt.Sprintf(" %d shown ", n)
		}
	default:
		if r := a.CommitSelectionRange; r != nil {
			selected = fmt.Sprintf(" %d selected ", r[1]-r[0]+1)
		}
	}
	footer := []render.Span{
		{Text: " SELECT ", Style: render.Style{Fg: t.ModeFg, Bg: t.ModeBg, Bold: true}},
		{Text: hint, Style: render.Style{Fg: t.FgSecondary, Bg: t.StatusBarBg}},
	}
	fpad := max(m.width-render.SpanWidth(footer)-render.StringWidth(selected), 0)
	footer = append(footer,
		render.Span{Text: strings.Repeat(" ", fpad), Style: render.Style{Bg: t.StatusBarBg}},
		render.Span{Text: selected, Style: render.Style{Fg: t.FgDim, Bg: t.StatusBarBg}})
	rows = append(rows, emitter.Line(footer))
	return rows
}

// selectorLocalRows renders the commit rows of the Local tab.
func (m *Model) selectorLocalRows(height int) []string {
	a := m.App
	t := m.Theme
	emitter := &render.Emitter{}
	var rows []string

	count := a.CommitSelectRowCount()
	for display := range count {
		dataIdx := a.CommitDataIndex(display)
		if dataIdx < 0 || dataIdx >= len(a.CommitList) {
			continue
		}
		commit := a.CommitList[dataIdx]
		isCursor := display == a.CommitListCursor
		isSelected := a.IsCommitSelected(dataIdx)

		cursor := "  "
		if isCursor {
			cursor = "▸ "
		}
		bar := "  "
		if isSelected {
			bar = "▌ "
		}
		box := "▢ "
		if isSelected {
			box = "▣ "
		}

		rowStyle := render.Style{Fg: t.FgPrimary}
		if isCursor {
			rowStyle = render.Style{Fg: t.FgPrimary, Bg: t.BgHighlight}
		} else if isSelected {
			rowStyle = render.Style{Fg: t.FgSecondary}
		}

		var spans []render.Span
		if commit.ID == app.StagedSelectionID || commit.ID == app.UnstagedSelectionID {
			tag := " · staged ·   "
			if commit.ID == app.UnstagedSelectionID {
				tag = " · unstaged · "
			}
			spans = []render.Span{
				{Text: cursor, Style: render.Style{Fg: t.BorderFocused}},
				{Text: bar, Style: render.Style{Fg: t.BorderFocused}},
				{Text: box, Style: rowStyle},
				{Text: tag, Style: render.Style{Fg: t.FileModified}},
				{Text: commit.Summary, Style: rowStyle},
			}
		} else {
			branch := ""
			if commit.BranchName != nil {
				branch = "[" + *commit.BranchName + "]"
			}
			spans = []render.Span{
				{Text: cursor, Style: render.Style{Fg: t.BorderFocused}},
				{Text: bar, Style: render.Style{Fg: t.BorderFocused}},
				{Text: box, Style: rowStyle},
				{Text: commit.ShortID + " ", Style: render.Style{Fg: t.CursorColor}},
				{Text: render.TruncateOrPad(branch, 16), Style: render.Style{Fg: t.BranchName}},
				{Text: render.TruncateOrPad(commit.Summary, 50), Style: rowStyle},
				{Text: "  " + render.TruncateOrPad(commit.Author, 12) + " · " + relativeTime(commit.Time),
					Style: render.Style{Fg: t.FgSecondary}},
			}
		}
		if isCursor {
			for i := range spans {
				spans[i].Style.Bg = t.BgHighlight
			}
			spans = render.PadToWidth(spans, m.width, render.Style{Bg: t.BgHighlight})
		}
		rows = append(rows, emitter.Line(spans))
	}
	if a.CanShowMoreCommits() {
		rows = append(rows, emitter.Line([]render.Span{
			{Text: "      … show more commits", Style: render.Style{Fg: t.FgDim}},
		}))
	}

	// Scroll window.
	offset := min(a.CommitListScrollOffset, len(rows))
	end := min(offset+height, len(rows))
	return rows[offset:end]
}

// relativeTime formats commit age like tuicr's format_relative_short.
func relativeTime(when time.Time) string {
	d := time.Since(when)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	}
	return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
}

// confirmSelection loads the confirmed target and swaps the app onto it.
func (m *Model) confirmSelection() {
	a := m.App
	sel, ok := a.ConfirmCommitSelection()
	if !ok {
		return
	}
	files, source, err := m.loadSelection(sel)
	if err != nil {
		a.SetError("Load failed: " + err.Error())
		return
	}

	// New source → new session identity.
	fresh := model.NewReviewSession(a.VcsInfo.RootPath, a.VcsInfo.HeadCommit,
		a.VcsInfo.BranchName, sessionSource(source))
	fresh.CommitRange = source.Commits
	var session *model.ReviewSession
	if m.session != nil {
		m.session.finish(a)
	}
	m.session, session = openSession(m.store, fresh)
	a.ApplyLoadedSelection(files, session, source)
	// ApplyLoadedSelection replaces DiffState wholesale, which zeroes the
	// viewport dimensions, and no WindowSizeMsg follows a selector confirm.
	// Anything sizing itself from ViewportWidth then computes against zero:
	// comment bodies wrapped at one character per line until the next
	// resize. Every other diff-swapping path (inlinecommits.go, reload.go,
	// prcontext.go) re-syncs for the same reason.
	m.syncViewport()
	a.InputMode = input.ModeNormal
}

// loadSelection performs the diff load for a confirmed selector choice.
func (m *Model) loadSelection(sel app.ConfirmedSelection) ([]model.DiffFile, app.DiffSource, error) {
	a := m.App
	h := m.Theme.Highlighter()
	var (
		files  []model.DiffFile
		source app.DiffSource
		err    error
	)
	switch sel.Kind {
	case app.SelectionStaged:
		files, err = a.VCS.StagedDiff(h)
		source = app.DiffSource{Kind: app.DiffSourceStaged}
	case app.SelectionUnstaged:
		files, err = a.VCS.UnstagedDiff(h)
		source = app.DiffSource{Kind: app.DiffSourceUnstaged}
	case app.SelectionStagedAndUnstaged:
		files, err = a.VCS.WorkingTreeDiff(h)
		source = app.DiffSource{Kind: app.DiffSourceStagedAndUnstaged}
	case app.SelectionCommits:
		rng := vcs.ResolvedRevisionRange{CommitIDs: reversed(sel.CommitIDs)}
		files, err = a.VCS.CommitRangeDiff(rng, h)
		source = app.DiffSource{Kind: app.DiffSourceCommitRange, Commits: sel.CommitIDs}
	case app.SelectionStagedUnstagedAndCommits:
		files, err = a.VCS.WorkingTreeWithCommitsDiff(reversed(sel.CommitIDs), h)
		source = app.DiffSource{Kind: app.DiffSourceStagedUnstagedAndCommits, Commits: sel.CommitIDs}
	}
	if err != nil {
		return nil, source, err
	}
	if a.VcsInfo != nil {
		files = ignore.Load(a.VcsInfo.RootPath).FilterDiffFiles(files)
	}
	if len(files) == 0 {
		return nil, source, errs.ErrNoChanges
	}
	return files, source, nil
}

// shortenDir fits a directory into the header, keeping the tail — a deep
// inbox path is identified by its last components, not by the mount point it
// hangs off, and an untruncated one pushes the tab chips off screen.
func shortenDir(dir string, maxWidth int) string {
	if render.StringWidth(dir) <= maxWidth {
		return dir
	}
	parts := strings.Split(dir, string(filepath.Separator))
	for i := 1; i < len(parts); i++ {
		candidate := "…" + string(filepath.Separator) +
			strings.Join(parts[i:], string(filepath.Separator))
		if render.StringWidth(candidate) <= maxWidth {
			return candidate
		}
	}
	// Even the last component is too long; clip it rather than overflow.
	return render.TruncateOrPad(parts[len(parts)-1], maxWidth)
}
