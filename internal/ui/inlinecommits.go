package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/ignore"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/syntax"
	"github.com/infrashift/mrman/internal/vcs"
	"github.com/infrashift/mrman/internal/vcs/diffparser"
)

// prRangeDiffResultMsg carries a narrowed pull-request diff back to the
// model.
type prRangeDiffResultMsg struct {
	Gen   uint64
	Key   app.PrKey
	Files []model.DiffFile
	Err   error
}

// reloadInlineSelection reloads the diff for the inline commit selector's
// current selection. Local reviews re-diff the selected commit range from
// the VCS; PR reviews ask the forge for the range between the selection's
// boundary SHAs.
func (m *Model) reloadInlineSelection() tea.Cmd {
	if m.App.InPrMode() {
		return m.reloadPrCommitRange()
	}
	m.reloadLocalCommitRange()
	return nil
}

// reloadLocalCommitRange re-diffs the selected commits from the VCS.
func (m *Model) reloadLocalCommitRange() {
	a := m.App
	if len(a.ReviewCommits) == 0 {
		return
	}
	ids := a.SelectedCommitIDs()
	if len(ids) == 0 {
		return
	}
	// A repeat of the same selection is common — sweeping with space walks
	// through ranges — so a cache keeps it from re-diffing every press.
	key := *a.CommitSelectionRange
	if cached, ok := a.CommitDiffCache[key]; ok {
		a.ApplyInlineSelectionDiff(cached)
		m.syncViewport()
		return
	}

	files, err := a.VCS.CommitRangeDiff(
		vcs.ResolvedRevisionRange{CommitIDs: reversed(ids)}, m.Theme.Highlighter())
	if err != nil {
		a.SetError("Load failed: " + err.Error())
		return
	}
	if a.VcsInfo != nil {
		files = ignore.Load(a.VcsInfo.RootPath).FilterDiffFiles(files)
	}
	a.CommitDiffCache[key] = files
	a.ApplyInlineSelectionDiff(files)
	m.syncViewport()
}

// reloadPrCommitRange asks the forge for the diff of the selected commit
// subrange, or restores the cumulative diff when the whole PR is selected.
func (m *Model) reloadPrCommitRange() tea.Cmd {
	a := m.App
	startSHA, endSHA, full, ok := a.PrCommitRange()
	if !ok {
		return nil
	}
	if full {
		// The forge already gave us this diff when the PR opened; asking
		// again for the same bytes would be a wasted round trip.
		return m.reloadPullRequest()
	}
	if !a.Pr.Backend.Capabilities().CommitRangeDiff {
		a.SetWarning("This forge cannot diff a commit range — showing the whole merge request")
		a.CommitSelectionRange = nil
		return nil
	}

	key, keyOK := a.Pr.CurrentPrKey()
	if !keyOK {
		return nil
	}
	a.Pr.Gens.PrReload++
	gen := a.Pr.Gens.PrReload
	a.Pr.Reloading = true

	backend := a.Pr.Backend
	details := a.Pr.Details
	highlighter := m.Theme.Highlighter()
	localCheckout := m.localCheckout

	ctx := m.inflight.replace(&m.inflight.rangeDiff)
	return func() tea.Msg {
		patch, err := backend.GetCommitRangeDiff(ctx, details, startSHA, endSHA)
		if err != nil {
			return prRangeDiffResultMsg{Gen: gen, Key: key, Err: err}
		}
		files, err := parsePrPatch(patch, highlighter, localCheckout)
		return prRangeDiffResultMsg{Gen: gen, Key: key, Files: files, Err: err}
	}
}

// handlePrRangeDiffResult installs a narrowed pull-request diff.
func (m *Model) handlePrRangeDiffResult(msg prRangeDiffResultMsg) {
	a := m.App
	if !a.InPrMode() {
		return
	}
	key, ok := a.Pr.CurrentPrKey()
	if !ok || msg.Gen != a.Pr.Gens.PrReload || msg.Key != key {
		return // superseded by a newer selection or a different PR
	}
	a.Pr.Reloading = false
	if msg.Err != nil {
		a.SetError("Load failed: " + msg.Err.Error())
		return
	}
	a.ApplyInlineSelectionDiff(msg.Files)
	m.syncViewport()
}

// cycleCommit walks the inline selector to the next or previous single
// commit and reloads the diff for it.
func (m *Model) cycleCommit(forward bool) tea.Cmd {
	a := m.App
	if !a.HasReviewCommits() {
		a.SetMessage("This review has only one commit")
		return nil
	}
	if forward {
		a.CycleCommitNext()
	} else {
		a.CycleCommitPrev()
	}
	return m.reloadInlineSelection()
}

// parsePrPatch parses a forge patch into diff files, applying the local
// ignore rules when a checkout is available.
func parsePrPatch(patch string, highlighter *syntax.Highlighter, localCheckout string) ([]model.DiffFile, error) {
	files, err := diffparser.Parse(patch, diffparser.GitStyle, highlighter)
	if err != nil {
		return nil, err
	}
	if localCheckout != "" {
		files = ignore.Load(localCheckout).FilterDiffFiles(files)
	}
	return files, nil
}

// setCommitSelectorVisible applies `:set commits` / `:set nocommits`.
func (m *Model) setCommitSelectorVisible(visible bool) {
	a := m.App
	if a.ShowCommitSelector == visible {
		return
	}
	a.ToggleCommitSelector()
}
