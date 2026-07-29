package app

import (
	"fmt"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/model"
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

	// Tab state for the selector's Pull Requests tab.
	TabRows    []forge.PullRequestSummary
	TabCursor  int
	TabLoading bool
	TabError   string
	NextPage   string
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
