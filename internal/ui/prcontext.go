package ui

import (
	"errors"
	"math"

	tea "charm.land/bubbletea/v2"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/model"
)

// prContextResultMsg carries a fetched PR file snapshot back to the model.
type prContextResultMsg struct {
	Gen     uint64
	Key     app.PrKey
	Request app.PrContextRequest
	Lines   []model.DiffLine
	Err     error
}

// fetchPrContext loads one file's whole snapshot from the forge so gap
// expansion can serve from it synchronously afterwards.
//
// The whole file is fetched rather than just the requested range because a
// reviewer expanding context almost always expands again in the same file,
// and a second round trip per keypress is far more expensive than one blob
// read.
func (m *Model) fetchPrContext(req app.PrContextRequest) tea.Cmd {
	a := m.App
	if !a.InPrMode() {
		return nil
	}
	key, ok := a.Pr.CurrentPrKey()
	if !ok {
		return nil
	}
	backend := a.Pr.Backend
	details := a.Pr.Details
	gen := a.Pr.Gens.PrReload

	lineReq := forge.FileLinesRequest{
		Repository: details.Repository,
		BaseSHA:    details.BaseSHA,
		HeadSHA:    details.HeadSHA,
		Path:       req.Path(),
		Status:     req.Status,
		Side:       req.Side(),
	}
	// One request for the whole file: an open-ended range stops at its last
	// line. Asking FileLineCount first downloaded the file twice.
	lineReq.StartLine, lineReq.EndLine = 1, math.MaxUint32
	ctx := m.inflight.root()
	return func() tea.Msg {
		lines, err := backend.FetchFileLines(ctx, lineReq)
		return prContextResultMsg{Gen: gen, Key: key, Request: req, Lines: lines, Err: err}
	}
}

// handlePrContextResult installs a fetched snapshot and replays the
// expansion that asked for it, discarding results for a superseded PR.
func (m *Model) handlePrContextResult(msg prContextResultMsg) {
	a := m.App
	if !a.InPrMode() {
		return
	}
	key, ok := a.Pr.CurrentPrKey()
	if !ok || msg.Key != key || msg.Gen != a.Pr.Gens.PrReload {
		return // the PR was reloaded or switched underneath the fetch
	}
	if msg.Err != nil {
		a.FailPrContextSnapshot(msg.Request, forge.Describe(msg.Err))
		return
	}
	a.ApplyPrContextSnapshot(msg.Request, msg.Lines)
	m.syncViewport()
}

// expandGapAtCursor expands or collapses the gap under the cursor. In PR
// mode the first expansion in a file needs a forge round trip, which it
// returns as a command.
func (m *Model) expandGapAtCursor() tea.Cmd {
	a := m.App
	hit, ok := a.GapAtCursor()
	if !ok {
		return nil
	}
	switch hit.Kind {
	case app.GapHitExpander:
		limit := app.GapExpandBatch
		err := a.ExpandGap(hit.GapID, hit.Direction, &limit)
		if err == nil {
			return nil
		}
		if errors.Is(err, app.ErrContextNotLoaded) {
			return m.requestPrContext(hit.GapID.FileIdx, &app.PrContextReplay{
				Gap: hit.GapID, Direction: hit.Direction, Limit: &limit,
			})
		}
		a.SetError(err.Error())
	case app.GapHitExpandedContent:
		a.CollapseGap(hit.GapID)
	}
	return nil
}

// drainPrContextRequest issues a snapshot fetch armed from inside the app
// state machine — the {N}G / :{N} jump planner running into unfetched PR
// context. The model calls it after every dispatch.
func (m *Model) drainPrContextRequest() tea.Cmd {
	// Opportunistically load the file the cursor is in, so its end-of-file
	// gap becomes expandable at all.
	m.App.PrefetchContextForCurrentFile()

	req, ok := m.App.TakePrContextRequest()
	if !ok {
		return nil
	}
	return m.fetchPrContext(req)
}

// requestPrContext issues the snapshot fetch for a file, announcing it so a
// slow forge does not look like a dead keypress.
func (m *Model) requestPrContext(fileIdx int, replay *app.PrContextReplay) tea.Cmd {
	req, ok := m.App.PrContextRequestFor(fileIdx, replay)
	if !ok {
		return nil
	}
	m.App.SetMessage("Fetching context for " + req.Path() + "…")
	return m.fetchPrContext(req)
}
