package app

import (
	"fmt"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs"
)

// PrKey is the staleness identity for every async PR operation: results
// whose key no longer matches the open PR are discarded.
type PrKey struct {
	Repository forgetypes.Repository
	Number     uint64
	HeadSHA    string
}

// Gens holds one generation counter per async family; bumping a counter
// invalidates in-flight results of that family.
type Gens struct {
	PrList, PrOpen, PrReload, PrThreads, PrSubmit uint64
}

// PrState is the PR-mode state attached to App.
type PrState struct {
	Backend    forge.Forge
	Repository *forgetypes.Repository
	Details    *forge.PullRequestDetails
	Commits    []forge.Commit
	Threads    []forge.RemoteReviewThread
	Summaries  []forge.RemoteReviewSummary
	Gens       Gens

	// Pending flags drive spinners.
	Opening, Reloading, ThreadsLoading, Submitting bool
	// ThreadsPending arms the remote-comments fetch drained by
	// TakeRemoteCommentsLoad; PendingThreads is the request it yields.
	ThreadsPending bool
	PendingThreads RemoteCommentsLoadRequest

	// Tab state for the selector's Pull Requests tab (see prtab.go).
	TabRows    []forge.PullRequestSummary
	TabCursor  int
	TabLoading bool
	TabError   string
	NextPage   string
	// TabScope selects all-open vs review-requested listings ("r").
	TabScope forge.ListScope
	// TabFilter is the local substring filter ("/") applied to loaded rows
	// only; it never re-queries the forge.
	TabFilter string
	// TabFilterEditing is the "/" prompt sub-state of the tab.
	TabFilterEditing bool
	// TabScrollOffset and TabViewportHeight are the tab's scroll window;
	// the height is set by the renderer.
	TabScrollOffset   int
	TabViewportHeight int
	// TabLoaded records that a first page was already requested, so
	// revisiting the tab does not refetch.
	TabLoaded bool
	// TabPending arms the pull-based load hook drained by TakePrTabLoad;
	// PendingLoad is the request it yields.
	TabPending  bool
	PendingLoad PrTabLoadRequest
}

// CurrentPrKey returns the staleness key of the open PR.
func (p *PrState) CurrentPrKey() (PrKey, bool) {
	if p == nil || p.Details == nil {
		return PrKey{}, false
	}
	return PrKey{
		Repository: p.Details.Repository,
		Number:     p.Details.Number,
		HeadSHA:    p.Details.HeadSHA,
	}, true
}

// InPrMode reports whether a PR review is open.
func (a *App) InPrMode() bool {
	return a.Pr != nil && a.Pr.Details != nil
}

// PrSessionRepoPath is the synthetic repo path for PR sessions so they never
// collide with local checkouts.
func PrSessionRepoPath(repo forgetypes.Repository) string {
	return fmt.Sprintf("forge:%s/%s", repo.Host, repo.Slug())
}

// PullRequestLoad is a fully fetched pull request ready to review. The UI
// layer builds it (it owns the forge driver, the diff parser and the
// highlighter) and hands it to ApplyPullRequest.
type PullRequestLoad struct {
	Backend    forge.Forge
	Repository *forgetypes.Repository
	Details    *forge.PullRequestDetails
	Commits    []forge.Commit
	Files      []model.DiffFile
	// VCS is the backend to run the review against — prnoop in PR mode,
	// constructed by the UI layer. Nil leaves the current backend in place.
	VCS vcs.Backend
}

// ApplyPullRequest swaps the app onto a freshly loaded pull request, running
// the same reset sequence as ApplyLoadedSelection: new diff, new session,
// fresh navigation state, cleared gaps, re-sort, re-expand, rebuilt
// annotations.
//
// The selector's Pull Requests tab state deliberately survives, so closing
// the PR and reopening the selector shows the list the user just came from
// instead of refetching it.
func (a *App) ApplyPullRequest(load PullRequestLoad, session *model.ReviewSession) {
	p := a.ensurePr()
	p.Backend = load.Backend
	p.Repository = load.Repository
	p.Details = load.Details
	p.Commits = load.Commits
	p.Threads = nil
	p.Summaries = nil
	p.Opening = false
	p.Reloading = false

	if load.VCS != nil {
		a.VCS = load.VCS
	}
	RegisterDiffFiles(session, load.Files)
	a.Session = session
	a.DiffFiles = load.Files
	a.DiffSource = DiffSource{Kind: DiffSourcePullRequest}
	a.InputMode = input.ModeNormal

	wrap := a.DiffState.WrapLines
	a.DiffState = NewDiffState()
	a.DiffState.WrapLines = wrap
	a.FileListState = FileListState{}
	a.ClearExpandedGaps()

	// A PR review has no local commit-range selection behind it; the PR's
	// own commits drive the inline selector separately.
	a.ReviewCommits = nil
	a.CommitList = nil
	a.CommitSelectionRange = nil
	a.SavedInlineSelection = nil
	a.ShowCommitSelector = false
	a.pendingSelectedCommits = nil
	a.CommitDiffCache = map[model.IndexRange][]model.DiffFile{}

	a.SortFilesByDirectory(true)
	a.ExpandAllDirs()
	a.populateFileLineCountCache()
	a.RebuildAnnotations()
}

// NewPrSession builds the session identity for an opened PR.
func NewPrSession(details *forge.PullRequestDetails) *model.ReviewSession {
	session := model.NewReviewSession(
		PrSessionRepoPath(details.Repository),
		details.HeadSHA,
		nil,
		model.SourcePullRequest,
	)
	session.PrSessionKey = &forgetypes.PrSessionKey{
		Repository: details.Repository,
		Number:     details.Number,
		HeadSHA:    details.HeadSHA,
	}
	return session
}
