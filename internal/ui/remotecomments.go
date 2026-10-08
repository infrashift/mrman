package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

// remoteCommentsResultMsg carries fetched remote discussions back to the
// model.
type remoteCommentsResultMsg struct {
	Gen       uint64
	Key       app.PrKey
	Threads   []forge.RemoteReviewThread
	Summaries []forge.RemoteReviewSummary
	// Meta drives commit-scope inference ("what landed since I last
	// reviewed"); nil on forges that do not scope reviews to commits.
	Meta *forge.ReviewMetadata
	Err  error
}

// drainRemoteCommentsLoad issues any remote-comments fetch the app armed.
func (m *Model) drainRemoteCommentsLoad() tea.Cmd {
	req, ok := m.App.TakeRemoteCommentsLoad()
	if !ok {
		return nil
	}
	backend := m.App.Pr.Backend
	details := m.App.Pr.Details
	caps := backend.Capabilities()

	ctx := m.inflight.replace(&m.inflight.threads)
	return func() tea.Msg {
		threads, err := backend.ListReviewThreads(ctx, details)
		if err != nil {
			return remoteCommentsResultMsg{Gen: req.Gen, Key: req.Key, Err: err}
		}
		// Summaries are asked of every forge: one without a review object
		// (GitLab, Azure DevOps) returns its general, file-less discussions
		// here, and review bodies are among them. Gating this on the
		// ReviewSummaries capability hid them entirely, because path-less
		// threads are not drawn. Summaries and metadata are best-effort, so
		// a forge without either still gets its inline threads.
		summaries, _ := backend.ListReviewSummaries(ctx, details)
		var meta *forge.ReviewMetadata
		if caps.CommitScopedReviews {
			meta, _ = backend.ReviewMetadata(ctx, details)
		}
		return remoteCommentsResultMsg{
			Gen: req.Gen, Key: req.Key, Threads: threads, Summaries: summaries, Meta: meta,
		}
	}
}

// handleRemoteCommentsResult applies fetched discussions.
func (m *Model) handleRemoteCommentsResult(msg remoteCommentsResultMsg) {
	if msg.Err != nil {
		m.App.FailRemoteComments(msg.Gen, msg.Key, msg.Err.Error())
		return
	}
	m.App.ApplyRemoteComments(msg.Gen, msg.Key, msg.Threads, msg.Summaries)
	// Preselecting "commits since my last review" changes what the diff
	// covers, so it has to happen before the viewport is sized.
	//
	// Reload only when the selection actually moved: a reload refetches
	// this very metadata, so reloading unconditionally never terminates.
	if msg.Meta != nil && m.App.ApplyPrReviewMetadata(msg.Meta) {
		m.queue(m.reloadInlineSelection())
	}
	m.syncViewport()
}

// setRemoteCommentsVisibility applies `:comments unresolved|all|hide`,
// fetching on demand the first time discussions are asked for.
func (m *Model) setRemoteCommentsVisibility(v forgetypes.PrCommentsVisibility) {
	if m.App.SetRemoteCommentsVisibility(v) && m.App.RequestRemoteComments() {
		m.queue(m.drainRemoteCommentsLoad())
	}
	m.syncViewport()
}

// loadRemoteCommentsOnOpen fetches discussions right after a PR opens, so
// the existing conversation is on screen before the reviewer starts reading.
func (m *Model) loadRemoteCommentsOnOpen() tea.Cmd {
	if !m.App.RequestRemoteComments() {
		return nil
	}
	return m.drainRemoteCommentsLoad()
}
