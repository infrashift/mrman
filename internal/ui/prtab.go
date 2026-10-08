package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	render "github.com/infrashift/mrman/charmkit/cellrender"
	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/input"
)

// prListResultMsg carries an async ListPullRequests page back to the model.
type prListResultMsg struct {
	Gen    uint64
	Append bool
	// Repo is the repository the listing resolved to; it is how a local
	// review learns which forge repo its PR tab is showing.
	Repo forgetypes.Repository
	Page *forge.PullRequestPage
	Err  error
}

// prOpenResultMsg carries an async pull-request open back to the model.
type prOpenResultMsg struct {
	Gen  uint64
	Load app.PullRequestLoad
	Err  error
}

// drainPrTabLoad issues any PR-list fetch the app state machine armed. The
// model calls it after every selector dispatch.
func (m *Model) drainPrTabLoad() tea.Cmd {
	req, ok := m.App.TakePrTabLoad()
	if !ok {
		return nil
	}
	resolver := m.forge
	ctx := m.inflight.replace(&m.inflight.list)
	return func() tea.Msg {
		backend, repo, err := resolver.Get()
		if err != nil {
			return prListResultMsg{Gen: req.Gen, Append: req.Append, Err: err}
		}
		page, err := backend.ListPullRequests(ctx, forge.ListQuery{
			Repository: repo,
			Scope:      req.Scope,
			PageToken:  req.PageToken,
			PageSize:   app.DefaultPrPageSize,
		})
		return prListResultMsg{Gen: req.Gen, Append: req.Append, Repo: repo, Page: page, Err: err}
	}
}

// handlePrListResult applies a fetched page, or surfaces the failure in the
// tab body rather than the status bar — the tab is the only thing affected.
func (m *Model) handlePrListResult(msg prListResultMsg) {
	a := m.App
	// A local review has no PR open, so the resolved listing repository is
	// the only thing that can name the forge in the selector header.
	if p := a.EnsurePrState(); p.Repository == nil && msg.Repo.Name != "" {
		repo := msg.Repo
		p.Repository = &repo
	}
	if msg.Err != nil {
		a.SetPrTabError(msg.Gen, msg.Err.Error())
		return
	}
	a.ApplyPrTabPage(msg.Gen, msg.Page, msg.Append)
}

// openSelectedPr fetches the pull request under the cursor and swaps the app
// onto it. The fetch runs off the render loop; PrOpen generation guards
// discard a result the user has already navigated away from.
func (m *Model) openSelectedPr() tea.Cmd {
	a := m.App
	row, ok := a.PrTabSelected()
	if !ok {
		return nil
	}
	p := a.EnsurePrState()
	p.Gens.PrOpen++
	p.Opening = true
	gen := p.Gens.PrOpen

	resolver := m.forge
	highlighter := m.Theme.Highlighter()
	localCheckout := m.localCheckout
	target := forge.Target{
		Repository: &row.Repository,
		Number:     row.Number,
		Original:   fmt.Sprintf("%s#%d", row.Repository.Slug(), row.Number),
	}
	a.SetMessage(fmt.Sprintf("Opening %s#%d…", row.Repository.Slug(), row.Number))

	ctx := m.inflight.replace(&m.inflight.open)
	return func() tea.Msg {
		backend, _, err := resolver.Get()
		if err != nil {
			return prOpenResultMsg{Gen: gen, Err: err}
		}
		load, err := fetchPullRequest(ctx, backend, target.Repository,
			target, highlighter, localCheckout)
		return prOpenResultMsg{Gen: gen, Load: load, Err: err}
	}
}

// handlePrOpenResult installs an opened pull request, discarding stale ones,
// and returns the follow-up fetch of its existing discussions.
func (m *Model) handlePrOpenResult(msg prOpenResultMsg) tea.Cmd {
	a := m.App
	if a.Pr == nil || msg.Gen != a.Pr.Gens.PrOpen {
		return nil // superseded: the user opened a different PR
	}
	a.Pr.Opening = false
	if msg.Err != nil {
		a.SetError("Open failed: " + msg.Err.Error())
		return nil
	}

	// A PR review is its own session, keyed by repository + number + head
	// SHA. Retire the outgoing session before swapping.
	fresh := app.NewPrSession(msg.Load.Details)
	if m.session != nil {
		m.shutdown(a)
	}
	lifecycle, session := openPrSession(m.store, fresh, msg.Load.Files, m.grantedEvents)
	m.session = lifecycle
	a.ApplyPullRequest(msg.Load, session)
	a.InputMode = input.ModeNormal
	a.SetMessage(fmt.Sprintf("Reviewing %s#%d · %s",
		msg.Load.Details.Repository.Slug(), msg.Load.Details.Number, msg.Load.Details.Title))
	return m.loadRemoteCommentsOnOpen()
}

// selectorPrRows renders the Pull Requests tab body.
func (m *Model) selectorPrRows(height int) []string {
	a := m.App
	t := m.Theme
	emitter := &render.Emitter{}

	if a.Pr != nil && a.Pr.TabError != "" {
		return []string{"", emitter.Line([]render.Span{
			{Text: "  " + a.Pr.TabError, Style: render.Style{Fg: t.DiffDel}},
		}), "", emitter.Line([]render.Span{
			{Text: "  r: switch scope · Esc: back to Local", Style: render.Style{Fg: t.FgDim}},
		})}
	}
	rows := a.PrTabFilteredRows()
	if len(rows) == 0 {
		message := "  No open merge requests."
		switch {
		case a.Pr != nil && a.Pr.TabLoading:
			message = "  Loading merge requests…"
		case a.Pr != nil && a.Pr.TabFilter != "":
			message = fmt.Sprintf("  No merge requests match %q.", a.Pr.TabFilter)
		case a.Pr != nil && a.Pr.TabScope == forge.ScopeReviewRequested:
			message = "  No merge requests are awaiting your review."
		}
		return []string{"", emitter.Line([]render.Span{
			{Text: message, Style: render.Style{Fg: t.FgDim}},
		})}
	}

	var out []string
	for i := range rows {
		out = append(out, m.prRow(emitter, &rows[i], i == a.Pr.TabCursor))
	}
	if a.CanLoadMorePrs() {
		label := "      … load more merge requests"
		if a.Pr.TabLoading {
			label = "      … loading"
		}
		style := render.Style{Fg: t.FgDim}
		spans := []render.Span{{Text: label, Style: style}}
		if a.IsOnPrLoadMoreRow() {
			spans[0].Style.Bg = t.BgHighlight
			spans = render.PadToWidth(spans, m.width, render.Style{Bg: t.BgHighlight})
		}
		out = append(out, emitter.Line(spans))
	}

	// Scroll window.
	offset := min(a.Pr.TabScrollOffset, len(out))
	end := min(offset+height, len(out))
	return out[offset:end]
}

// prRow renders one pull-request row.
func (m *Model) prRow(emitter *render.Emitter, row *forge.PullRequestSummary, isCursor bool) string {
	t := m.Theme
	rowStyle := render.Style{Fg: t.FgPrimary}
	cursor := "  "
	if isCursor {
		cursor = "▸ "
		rowStyle = render.Style{Fg: t.FgPrimary, Bg: t.BgHighlight}
	}

	marker, markerStyle := "  ", render.Style{Fg: t.FgDim}
	if row.IsDraft {
		marker, markerStyle = "◌ ", render.Style{Fg: t.FgDim}
	}

	updated := ""
	if row.UpdatedAt != nil {
		updated = " · " + relativeTime(*row.UpdatedAt)
	}

	spans := []render.Span{
		{Text: cursor, Style: render.Style{Fg: t.BorderFocused}},
		{Text: marker, Style: markerStyle},
		{Text: render.TruncateOrPad(fmt.Sprintf("#%d", row.Number), 7), Style: render.Style{Fg: t.CursorColor}},
		{Text: render.TruncateOrPad(row.Title, 50) + " ", Style: rowStyle},
		{Text: render.TruncateOrPad("["+row.HeadRefName+"]", 20), Style: render.Style{Fg: t.BranchName}},
		{Text: "  " + render.TruncateOrPad(row.Author, 14) + updated, Style: render.Style{Fg: t.FgSecondary}},
	}
	if isCursor {
		for i := range spans {
			spans[i].Style.Bg = t.BgHighlight
		}
		spans = render.PadToWidth(spans, m.width, render.Style{Bg: t.BgHighlight})
	}
	return emitter.Line(spans)
}

// selectorFooterHint returns the per-tab key hint for the selector footer.
func (m *Model) selectorFooterHint() string {
	a := m.App
	switch a.TargetTab {
	case app.TargetTabLocal:
		return "   j/k navigate · space range · ↵ confirm · tab next · q quit"
	case app.TargetTabPatches:
		if a.PatchTabFilterEditing() {
			return "   /" + a.PatchTab.Filter + "▏ ↵ apply · esc clear"
		}
		hint := "   j/k navigate · ↵ open · / filter · r rescan · tab next · esc local · q quit"
		if a.PatchTab != nil && a.PatchTab.Filter != "" {
			hint += fmt.Sprintf(" · filter:%q", a.PatchTab.Filter)
		}
		return hint
	}
	if a.PrTabFilterEditing() {
		return "   /" + a.Pr.TabFilter + "▏ ↵ apply · esc clear"
	}
	scope := forge.ScopeOpen.Label()
	if a.Pr != nil {
		scope = a.Pr.TabScope.Label()
	}
	hint := fmt.Sprintf("   j/k navigate · ↵ open · / filter · r scope:%s · tab next · esc local · q quit", scope)
	if a.Pr != nil && a.Pr.TabFilter != "" {
		hint += fmt.Sprintf(" · filter:%q", a.Pr.TabFilter)
	}
	return hint
}

// dispatchPrTabFilter handles the "/" filter prompt, a sub-state of the
// selector rather than a top-level input mode (tuicr's pr_filter_editing).
func (m *Model) dispatchPrTabFilter(action input.Action) {
	a := m.App
	switch action.Kind {
	case input.ExitMode:
		a.CancelPrTabFilter()
	case input.SubmitInput:
		a.CommitPrTabFilter()
	case input.InsertChar:
		a.InsertPrTabFilterChar(action.Ch)
	case input.DeleteChar:
		a.DeletePrTabFilterChar()
	case input.DeleteWord:
		a.DeletePrTabFilterWord()
	case input.ClearLine:
		a.ClearPrTabFilter()
	}
}

// openTargetSelector opens the review target selector on a tab, arming the
// PR listing fetch when that tab needs one.
func (m *Model) openTargetSelector(tab app.TargetTab) {
	if err := m.App.EnterTargetSelector(tab); err != nil {
		m.App.SetError("Cannot open the target selector: " + err.Error())
		return
	}
	m.queue(m.drainPrTabLoad())
}

// prSelectorTitle is the header suffix shown while the PR tab is active.
func prSelectorTitle(a *app.App) string {
	if a.Pr == nil || a.Pr.Repository == nil {
		return ""
	}
	return " " + strings.TrimSpace(a.Pr.Repository.Slug()) + " "
}
