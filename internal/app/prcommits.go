// prcommits.go drives the inline commit selector during a pull-request
// review: turning the PR's commits into selector rows, resolving a narrowed
// selection to the SHA range the forge should diff, and inferring which
// commits a previous review of your own already covered.
package app

import (
	"fmt"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs"
)

// prCommitsToInfo converts forge commits (oldest first) into selector rows
// (newest first, the order every selector path expects).
func prCommitsToInfo(commits []forge.Commit) []vcs.CommitInfo {
	out := make([]vcs.CommitInfo, 0, len(commits))
	for i := len(commits) - 1; i >= 0; i-- {
		c := commits[i]
		info := vcs.CommitInfo{
			ID:      c.OID,
			ShortID: c.ShortOID,
			Summary: c.Summary,
			Author:  c.Author,
		}
		if info.ShortID == "" && len(c.OID) >= 7 {
			info.ShortID = c.OID[:7]
		}
		if c.Timestamp != nil {
			info.Time = *c.Timestamp
		}
		out = append(out, info)
	}
	return out
}

// SetupPrCommitSelector installs the PR's commits as the inline selector's
// rows. The selector only earns its screen space on a multi-commit review,
// so a single-commit PR leaves it hidden.
func (a *App) SetupPrCommitSelector(commits []forge.Commit) {
	rows := prCommitsToInfo(commits)
	a.ReviewCommits = rows
	a.CommitList = append([]vcs.CommitInfo(nil), rows...)
	a.VisibleCommitCount = len(rows)
	a.HasMoreCommits = false
	a.CommitListCursor = 0
	a.CommitListScrollOffset = 0
	a.CommitSelectionRange = InitialCommitRange(a.CommitSelectionStart, len(rows))
	a.ShowCommitSelector = a.ShowCommitSelector && len(rows) > 1
}

// IsCommitReviewed reports whether a commit was already covered by a review
// of your own, per the forge's review metadata.
func (a *App) IsCommitReviewed(oid string) bool {
	return a.Pr != nil && a.Pr.ReviewedCommits[oid]
}

// ApplyPrReviewMetadata infers review scope from the forge's review records:
// which commits your last review already covered, and — when some commits
// landed after it — preselecting only those.
//
// It reports whether the selection actually moved, which the caller needs:
// reloading the diff refetches review metadata, so a caller that reloads
// unconditionally reloads forever.
//
// This is the "what changed since I last looked" shortcut. It only fires
// when the forge reports a commit-scoped review of yours (the
// CommitScopedReviews capability); otherwise the selection is left alone.
func (a *App) ApplyPrReviewMetadata(meta *forge.ReviewMetadata) (changed bool) {
	if meta == nil || !a.InPrMode() || len(a.ReviewCommits) == 0 {
		return false
	}
	lastOID := lastViewerReviewCommit(meta)
	if lastOID == "" {
		return false
	}

	// ReviewCommits is newest first, so everything at or after the index of
	// the reviewed commit is older than it, and therefore covered.
	reviewedIdx := -1
	for i := range a.ReviewCommits {
		if a.ReviewCommits[i].ID == lastOID {
			reviewedIdx = i
			break
		}
	}
	if reviewedIdx < 0 {
		// The reviewed commit is no longer in the PR (force-push, rebase);
		// nothing reliable to infer.
		return false
	}

	covered := make(map[string]bool, len(a.ReviewCommits)-reviewedIdx)
	for i := reviewedIdx; i < len(a.ReviewCommits); i++ {
		covered[a.ReviewCommits[i].ID] = true
	}
	a.Pr.ReviewedCommits = covered

	if reviewedIdx == 0 {
		a.SetMessage("No new commits since your last review")
		return false
	}
	// Preselect exactly the commits newer than the review: indices 0..
	// reviewedIdx-1 in newest-first order.
	want := model.IndexRange{0, reviewedIdx - 1}
	if a.CommitSelectionRange != nil && *a.CommitSelectionRange == want {
		return false // already scoped there; reloading would just loop
	}
	a.CommitSelectionRange = &want
	a.CommitListCursor = 0
	a.SetMessage(pluralCommits(reviewedIdx) + " since your last review")
	return true
}

func pluralCommits(n int) string {
	if n == 1 {
		return "1 commit"
	}
	return fmt.Sprintf("%d commits", n)
}

// lastViewerReviewCommit returns the head commit of the viewer's most recent
// review, "" when they have not reviewed or the forge does not scope reviews
// to commits.
func lastViewerReviewCommit(meta *forge.ReviewMetadata) string {
	if meta.ViewerLogin == "" {
		return ""
	}
	var (
		bestOID  string
		bestTime int64 = -1
	)
	for i := range meta.Reviews {
		record := &meta.Reviews[i]
		if record.Author != meta.ViewerLogin || record.CommitOID == "" {
			continue
		}
		when := int64(0)
		if record.SubmittedAt != nil {
			when = record.SubmittedAt.UnixNano()
		}
		if when >= bestTime {
			bestTime, bestOID = when, record.CommitOID
		}
	}
	return bestOID
}

// PrCommitRange resolves the current inline selection to the SHA pair the
// forge should diff.
//
// startSHA is the parent of the oldest selected commit: in a linear PR
// commit list that is simply the next entry in newest-first order, or the
// PR's base when the selection reaches the first commit. full reports that
// the selection spans every commit, so the caller can reuse the PR's
// cumulative diff instead of asking the forge for a range it already has.
func (a *App) PrCommitRange() (startSHA, endSHA string, full, ok bool) {
	if !a.InPrMode() || len(a.ReviewCommits) == 0 {
		return "", "", false, false
	}
	newest, oldest := 0, len(a.ReviewCommits)-1
	if r := a.CommitSelectionRange; r != nil {
		newest, oldest = r[0], r[1]
	}
	if newest < 0 || oldest >= len(a.ReviewCommits) || newest > oldest {
		return "", "", false, false
	}
	if newest == 0 && oldest == len(a.ReviewCommits)-1 {
		return "", "", true, true
	}

	startSHA = a.Pr.Details.BaseSHA
	if oldest+1 < len(a.ReviewCommits) {
		startSHA = a.ReviewCommits[oldest+1].ID
	}
	return startSHA, a.ReviewCommits[newest].ID, false, true
}

// SelectedCommitIDs returns the ids of the commits the inline selector
// currently covers, newest first.
func (a *App) SelectedCommitIDs() []string {
	if len(a.ReviewCommits) == 0 {
		return nil
	}
	start, end := 0, len(a.ReviewCommits)-1
	if r := a.CommitSelectionRange; r != nil {
		start, end = r[0], r[1]
	}
	if start < 0 || end >= len(a.ReviewCommits) || start > end {
		return nil
	}
	ids := make([]string, 0, end-start+1)
	for i := start; i <= end; i++ {
		ids = append(ids, a.ReviewCommits[i].ID)
	}
	return ids
}

// ApplyInlineSelectionDiff swaps in the diff for a narrowed commit
// selection, keeping the session and the selector rows.
//
// Unlike ApplyLoadedSelection this is a re-scoping of the same review, not a
// new one: comments and reviewed marks stay, and so does the commit list the
// user is steering with. Only the diff and the navigation state reset.
func (a *App) ApplyInlineSelectionDiff(files []model.DiffFile) {
	RegisterDiffFiles(a.Session, files)
	a.DiffFiles = files

	wrap := a.DiffState.WrapLines
	a.DiffState = NewDiffState()
	a.DiffState.WrapLines = wrap
	a.FileListState = FileListState{}
	a.ClearExpandedGaps()
	a.PrContext = nil

	a.insertCommitMessageIfSingle()
	a.SortFilesByDirectory(true)
	a.ExpandAllDirs()
	a.populateFileLineCountCache()
	a.RebuildAnnotations()
}
