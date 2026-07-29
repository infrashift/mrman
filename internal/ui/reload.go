package ui

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/ignore"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs"
)

// prReloadResultMsg carries a refetched pull request back to the model.
type prReloadResultMsg struct {
	Request app.PrReloadRequest
	Load    app.PullRequestLoad
	Err     error
}

// reloadDiff implements `:e` / `:reload`. Local reviews re-read the working
// tree or commit range; a PR review refetches from the forge.
func (m *Model) reloadDiff() tea.Cmd {
	if m.App.InPrMode() {
		return m.reloadPullRequest()
	}
	m.reloadLocalDiff()
	return nil
}

// reloadLocalDiff re-reads the current local review target in place.
func (m *Model) reloadLocalDiff() {
	a := m.App
	files, err := m.readCurrentSource()
	if err != nil {
		if errors.Is(err, errs.ErrNoChanges) {
			a.SetWarning("Nothing left to review — the changes are gone")
			return
		}
		a.SetError("Reload failed: " + err.Error())
		return
	}
	a.SetMessage(a.ApplyReloadedDiff(files).Message())
	m.syncViewport()
}

// readCurrentSource re-runs whichever VCS read produced the current diff.
func (m *Model) readCurrentSource() ([]model.DiffFile, error) {
	a := m.App
	h := m.Theme.Highlighter()
	var (
		files []model.DiffFile
		err   error
	)
	switch a.DiffSource.Kind {
	case app.DiffSourceStaged:
		files, err = a.VCS.StagedDiff(h)
	case app.DiffSourceUnstaged:
		files, err = a.VCS.UnstagedDiff(h)
	case app.DiffSourceCommitRange:
		files, err = a.VCS.CommitRangeDiff(
			vcs.ResolvedRevisionRange{CommitIDs: reversed(a.DiffSource.Commits)}, h)
	case app.DiffSourceStagedUnstagedAndCommits:
		files, err = a.VCS.WorkingTreeWithCommitsDiff(reversed(a.DiffSource.Commits), h)
	default:
		files, err = a.VCS.WorkingTreeDiff(h)
	}
	if err != nil {
		return nil, err
	}
	if a.VcsInfo != nil {
		files = ignore.Load(a.VcsInfo.RootPath).FilterDiffFiles(files)
	}
	return files, nil
}

// reloadPullRequest refetches the open pull request from the forge.
func (m *Model) reloadPullRequest() tea.Cmd {
	a := m.App
	req, ok := a.StartPrReload()
	if !ok {
		return nil
	}
	backend := a.Pr.Backend
	repo := a.Pr.Details.Repository
	target := forge.Target{Repository: &repo, Number: a.Pr.Details.Number}
	highlighter := m.Theme.Highlighter()
	localCheckout := m.localCheckout
	a.SetMessage("Reloading pull request…")

	return func() tea.Msg {
		load, err := fetchPullRequest(context.Background(), backend, &repo,
			target, highlighter, localCheckout)
		return prReloadResultMsg{Request: req, Load: load, Err: err}
	}
}

// handlePrReloadResult installs a refetched pull request.
//
// When the PR advanced to a new head commit it is a different review: the
// session is keyed by head SHA, so the outgoing one is retired and the
// session for the new head is opened (or resumed). Comments already written
// against the old head stay with the old session rather than silently
// re-anchoring to lines that may have moved.
func (m *Model) handlePrReloadResult(msg prReloadResultMsg) tea.Cmd {
	a := m.App
	if a.PrReloadIsStale(msg.Request) {
		return nil
	}
	a.Pr.Reloading = false
	if msg.Err != nil {
		a.FailPrReload(msg.Err.Error())
		return nil
	}

	if a.PrHeadMoved(&msg.Load) {
		fresh := app.NewPrSession(msg.Load.Details)
		if m.session != nil {
			m.session.finish(a)
		}
		lifecycle, session := openPrSession(m.store, fresh)
		m.session = lifecycle
		a.ApplyPullRequest(msg.Load, session)
		a.SetMessage("Pull request advanced to " + shortSHA(msg.Load.Details.HeadSHA) +
			" — opened a review for the new head")
		m.syncViewport()
		return m.loadRemoteCommentsOnOpen()
	}

	// Same head: keep the session and its comments, refresh the content.
	a.Pr.Details = msg.Load.Details
	a.Pr.Commits = msg.Load.Commits
	stats := a.ApplyReloadedDiff(msg.Load.Files)
	a.SetMessage(stats.Message())
	m.syncViewport()
	return m.loadRemoteCommentsOnOpen()
}
