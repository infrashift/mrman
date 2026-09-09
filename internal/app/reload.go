// reload.go implements `:e` / `:reload` — re-reading the review target
// after it changed underneath the reviewer, porting tuicr's reload_diff.
//
// A reload never loses comments: they live in the session, keyed by path,
// so replacing the diff files re-attaches them. Reviewed marks are
// deliberately different — a file whose content moved loses its mark,
// because a review of the old content says nothing about the new. The
// reload reports both.

package app

import (
	"fmt"

	"github.com/infrashift/mrman/internal/model"
)

// ReloadStats describes what a reload changed, for the status message.
type ReloadStats struct {
	// Files is the file count after the reload.
	Files int
	// Changed is how many files' content differs from before, by content
	// hash — the number a reviewer actually cares about.
	Changed int
	// Added and Removed are files that appeared or disappeared entirely.
	Added, Removed int
	// Unreviewed is how many files lost their reviewed mark because their
	// content moved — the number that decides whether the reviewer has to
	// go back over anything.
	Unreviewed int
	// Anchors is what the reload did to the comments' line anchors.
	Anchors AnchorStats
}

// Message renders the stats the way tuicr phrases them.
func (s ReloadStats) Message() string {
	msg := fmt.Sprintf("Reloaded %d file(s)", s.Files)
	if s.Changed > 0 {
		msg += fmt.Sprintf(", %d changed", s.Changed)
	}
	if s.Added > 0 {
		msg += fmt.Sprintf(", %d new", s.Added)
	}
	if s.Removed > 0 {
		msg += fmt.Sprintf(", %d gone", s.Removed)
	}
	if s.Unreviewed > 0 {
		msg += fmt.Sprintf(", %d changed since last review", s.Unreviewed)
	}
	if anchors := s.Anchors.Message(); anchors != "" {
		msg += "; " + anchors
	}
	return msg
}

// ApplyReloadedDiff swaps in freshly read diff files, keeping the session
// (and therefore every comment) and reporting what moved.
//
// Keeping the comments is not the same as keeping them correct: the content
// they were written about may have moved or gone. Every anchor is re-checked
// against the new diff, and the stats say what that cost.
//
// Expanded gaps and the PR context cache are dropped: they were computed
// against the old content and would place stale lines in the middle of a
// fresh diff. The cursor returns to the top for the same reason — the line
// it was on may no longer exist.
func (a *App) ApplyReloadedDiff(files []model.DiffFile) ReloadStats {
	stats := reloadStats(a.DiffFiles, files)

	// AddDiffFile reports when a previously reviewed file's content moved,
	// which clears its reviewed mark: that is exactly the "you need to look
	// at this again" count worth telling the reviewer about.
	for i := range files {
		if a.Session.AddDiffFile(&files[i]) {
			stats.Unreviewed++
		}
	}
	a.DiffFiles = files

	wrap := a.DiffState.WrapLines
	a.DiffState = NewDiffState()
	a.DiffState.WrapLines = wrap
	a.FileListState = FileListState{}
	a.ClearExpandedGaps()
	a.PrContext = nil

	a.SortFilesByDirectory(true)
	a.ExpandAllDirs()
	a.populateFileLineCountCache()

	stats.Anchors = a.ValidateCommentAnchors()
	if stats.Anchors.Moved > 0 {
		a.Dirty = true
	}

	a.RebuildAnnotations()
	return stats
}

// reloadStats compares two diff-file sets by path and content hash.
func reloadStats(before, after []model.DiffFile) ReloadStats {
	oldHashes := make(map[string]uint64, len(before))
	for i := range before {
		oldHashes[before[i].DisplayPath()] = before[i].ContentHash
	}
	stats := ReloadStats{Files: len(after)}
	seen := make(map[string]bool, len(after))
	for i := range after {
		path := after[i].DisplayPath()
		seen[path] = true
		hash, existed := oldHashes[path]
		switch {
		case !existed:
			stats.Added++
		case hash != after[i].ContentHash:
			stats.Changed++
		}
	}
	for path := range oldHashes {
		if !seen[path] {
			stats.Removed++
		}
	}
	return stats
}

// PrReloadRequest asks the UI layer to refetch the open pull request.
type PrReloadRequest struct {
	// Gen is the PrReload generation guarding the result.
	Gen uint64
	// Key is the pull request as it looked when the reload started; a
	// result whose repository or number differs is for a PR the user has
	// since navigated away from.
	Key PrKey
}

// StartPrReload arms a refetch of the open pull request. It reports false
// outside PR mode.
func (a *App) StartPrReload() (PrReloadRequest, bool) {
	if !a.InPrMode() {
		return PrReloadRequest{}, false
	}
	key, ok := a.Pr.CurrentPrKey()
	if !ok {
		return PrReloadRequest{}, false
	}
	a.Pr.Gens.PrReload++
	a.Pr.Reloading = true
	return PrReloadRequest{Gen: a.Pr.Gens.PrReload, Key: key}, true
}

// PrReloadIsStale reports whether a reload result should be discarded.
func (a *App) PrReloadIsStale(req PrReloadRequest) bool {
	if !a.InPrMode() || req.Gen != a.Pr.Gens.PrReload {
		return true
	}
	key, ok := a.Pr.CurrentPrKey()
	if !ok {
		return true
	}
	// The head SHA is expected to differ — that is the whole point of a
	// reload — so only the repository and number are compared.
	return key.Repository != req.Key.Repository || key.Number != req.Key.Number
}

// PrHeadMoved reports whether a reloaded PR advanced to a new head commit,
// which means the review needs a different session.
func (a *App) PrHeadMoved(reloaded *PullRequestLoad) bool {
	return a.InPrMode() && reloaded.Details != nil &&
		reloaded.Details.HeadSHA != a.Pr.Details.HeadSHA
}

// FailPrReload clears the spinner and reports the failure.
func (a *App) FailPrReload(message string) {
	if a.InPrMode() {
		a.Pr.Reloading = false
	}
	a.SetError("Reload failed: " + message)
}
