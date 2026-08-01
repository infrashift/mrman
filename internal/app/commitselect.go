// commitselect.go ports the review target selector from tuicr's
// src/app/commits.rs: enter/exit transitions for input.ModeCommitSelect,
// tab cycling, and confirming a selection. Confirming resolves *what* to
// load (ConfirmedSelection); the actual diff loading and session lookup
// stay in the UI/run layer (which owns the persistence store and syntax
// highlighter), which then calls ApplyLoadedSelection (diffload.go).
package app

import (
	"slices"

	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs"
)

// TargetTab is the active tab in the review target selector.
//
// The selector internally still goes through input.ModeCommitSelect, but it
// shows several tabs to the user.
type TargetTab int

// Target selector tabs, in display order.
const (
	TargetTabLocal TargetTab = iota
	// TargetTabPullRequests lists the forge's open pull requests; its
	// state machine lives in prtab.go.
	TargetTabPullRequests
	// TargetTabPatches lists .patch/.diff/mbox artifacts in a directory —
	// the inbox a mailing-list reviewer works from. Its state machine lives
	// in patchtab.go.
	TargetTabPatches
)

// targetTabCount is how many tabs the selector shows. Kept beside the
// constants so adding one cannot leave the cycling behind.
const targetTabCount = 3

// Label is the tab's chip text.
func (t TargetTab) Label() string {
	switch t {
	case TargetTabLocal:
		return "Local"
	case TargetTabPullRequests:
		return "Merge Requests"
	case TargetTabPatches:
		return "Patches"
	}
	return ""
}

// AllTargetTabs lists the tabs in display order.
func AllTargetTabs() []TargetTab {
	return []TargetTab{TargetTabLocal, TargetTabPullRequests, TargetTabPatches}
}

// ExitSelectorAction tells the UI/run layer what follow-up load exiting the
// target selector requires (the loads need the highlighter and persistence
// store, which the app state machine does not own).
type ExitSelectorAction int

// Exit-selector follow-up actions.
const (
	// ExitSelectorNone requires no follow-up.
	ExitSelectorNone ExitSelectorAction = iota
	// ExitSelectorReloadInline means the restored inline selection's diff
	// must be reloaded (tuicr's reload_inline_selection_for_source).
	ExitSelectorReloadInline
	// ExitSelectorLoadWorkingTree means the review was commit-based with no
	// inline selector; tuicr falls back to the staged+unstaged diff.
	ExitSelectorLoadWorkingTree
)

// SelectionKind discriminates ConfirmedSelection.
type SelectionKind int

// Confirmed selection kinds.
const (
	SelectionStaged SelectionKind = iota
	SelectionUnstaged
	SelectionStagedAndUnstaged
	SelectionCommits
	SelectionStagedUnstagedAndCommits
)

// ConfirmedSelection is the resolved review target the UI layer loads a
// diff for after ConfirmCommitSelection.
type ConfirmedSelection struct {
	Kind SelectionKind
	// CommitIDs are the selected real commit ids, NEWEST-FIRST (the
	// selector's storage order). Note DiffSource.Commits keeps the
	// opposite, oldest-first order — reverse when building the source.
	CommitIDs []string
}

// InstallReviewCommits makes selected the commits this review spans: the
// inline strip's rows, the range ( and ) walk, and the per-commit diff
// cache's keys.
//
// Both entry points need it. LoadDiff calls it when the user confirms a
// selection; startup calls it for `mrman -r <range>`, which builds its App
// directly and so never reaches LoadDiff — that path used to report "this
// review has only one commit" and show no strip for a range the header
// itself described as twelve commits.
func (a *App) InstallReviewCommits(selected []vcs.CommitInfo) {
	a.ReviewCommits = selected
	a.CommitList = append([]vcs.CommitInfo(nil), selected...)
	rng := InitialCommitRange(a.CommitSelectionStart, len(selected))
	a.CommitSelectionRange = rng
	a.CommitListCursor = 0
	if rng != nil {
		a.CommitListCursor = rng[0]
	}
	a.CommitListScrollOffset = 0
	a.VisibleCommitCount = len(selected)
	a.HasMoreCommits = false
	a.ShowCommitSelector = len(selected) > 1
	a.CommitDiffCache = map[model.IndexRange][]model.DiffFile{}
	a.SavedInlineSelection = nil
	a.insertCommitMessageIfSingle()
}

// SelectorTabForReview is the tab the current review came from, so that
// reopening the selector lands where the user chose this review rather than
// on whichever tab happens to be first.
func (a *App) SelectorTabForReview() TargetTab {
	switch {
	case a.InPrMode():
		return TargetTabPullRequests
	case a.DiffSource.Kind == DiffSourcePatch:
		return TargetTabPatches
	}
	return TargetTabLocal
}

// EnterTargetSelector opens the review target selector on a specific tab.
//
// TargetTabLocal loads the recent-commits list plus synthetic
// staged/unstaged rows. TargetTabPullRequests additionally arms the lazy
// PR-list fetch (onTargetTabEntered).
func (a *App) EnterTargetSelector(initialTab TargetTab) error {
	// Save the inline selection so ExitCommitSelectMode can restore it.
	if len(a.ReviewCommits) > 0 {
		a.SavedInlineSelection = copyIndexRange(a.CommitSelectionRange)
	}

	status, commits, err := a.loadLocalTargets()
	if err != nil {
		// Only the Local tab needs a usable local VCS. PR mode runs on the
		// no-op backend, and a patch review may have no repository at all —
		// which is the entire point of it — so neither may be blocked by a
		// checkout that cannot answer.
		if initialTab == TargetTabLocal {
			return err
		}
		status, commits = vcs.ChangeStatus{}, nil
	}
	noLocalTargets := len(commits) == 0 && !status.Staged && !status.Unstaged
	// Allow opening the selector on the Pull Requests tab even when there
	// are no local commits or changes — the PR tab is the user's reason for
	// being here.
	if noLocalTargets && initialTab == TargetTabLocal {
		a.SetMessage("No commits or staged/unstaged changes found")
		return nil
	}

	// Check if there might be more commits.
	a.HasMoreCommits = len(commits) >= DefaultCommitPageSize
	a.CommitList = commits
	if status.Staged {
		a.CommitList = append([]vcs.CommitInfo{stagedCommitEntry()}, a.CommitList...)
	}
	if status.Unstaged {
		a.CommitList = append([]vcs.CommitInfo{unstagedCommitEntry()}, a.CommitList...)
	}
	a.CommitListCursor = 0
	a.CommitListScrollOffset = 0
	a.CommitSelectionRange = nil
	a.VisibleCommitCount = len(a.CommitList)
	a.InputMode = input.ModeCommitSelect

	a.SetTargetTab(initialTab)
	return nil
}

// loadLocalTargets probes the working tree and recent history backing the
// selector's Local tab.
func (a *App) loadLocalTargets() (vcs.ChangeStatus, []vcs.CommitInfo, error) {
	status, err := a.resolveChangeStatus()
	if err != nil {
		return vcs.ChangeStatus{}, nil, err
	}
	commits, err := a.VCS.RecentCommits(0, DefaultCommitPageSize)
	if err != nil {
		return vcs.ChangeStatus{}, nil, err
	}
	return status, commits, nil
}

// ExitCommitSelectMode leaves the target selector. When an inline selector
// exists its state (and saved selection) is restored; otherwise a
// commit-based review falls back to the working tree. The returned action
// tells the UI layer which diff reload, if any, it must perform.
func (a *App) ExitCommitSelectMode() ExitSelectorAction {
	a.InputMode = input.ModeNormal

	// If we have review commits, restore the inline selector state.
	if len(a.ReviewCommits) > 0 {
		a.CommitList = slices.Clone(a.ReviewCommits)
		a.CommitSelectionRange = a.SavedInlineSelection
		a.CommitListCursor = 0
		a.CommitListScrollOffset = 0
		a.VisibleCommitCount = len(a.ReviewCommits)
		a.HasMoreCommits = false
		a.SavedInlineSelection = nil

		if a.CommitSelectionRange != nil {
			return ExitSelectorReloadInline
		}
		return ExitSelectorNone
	}

	// If we were viewing commits, go back to the working tree.
	switch a.DiffSource.Kind {
	case DiffSourceCommitRange, DiffSourceStagedUnstagedAndCommits:
		return ExitSelectorLoadWorkingTree
	default:
		return ExitSelectorNone
	}
}

// CycleTargetTab switches to the next tab, or the previous one when forward
// is false.
//
// The direction argument became meaningful when a third tab arrived: with two
// it was decorative, and Shift-Tab did the same thing as Tab.
func (a *App) CycleTargetTab(forward bool) {
	delta := 1
	if !forward {
		delta = targetTabCount - 1 // -1, without going negative under modulo
	}
	a.SetTargetTab(TargetTab((int(a.TargetTab) + delta) % targetTabCount))
}

// SetTargetTab switches to a specific tab, arming whatever it loads lazily.
func (a *App) SetTargetTab(tab TargetTab) {
	a.TargetTab = tab
	a.onTargetTabEntered()
}

// onTargetTabEntered arms the lazy first load of whichever tab just became
// visible. The UI layer drains the request and performs the fetch; revisiting
// a tab that already loaded (or is mid-flight) does nothing, because
// refetching is an explicit action.
func (a *App) onTargetTabEntered() {
	switch a.TargetTab {
	case TargetTabPullRequests:
		p := a.ensurePr()
		if p.TabLoaded || p.TabLoading {
			return
		}
		a.requestPrTabLoad(false)
	case TargetTabPatches:
		pt := a.ensurePatchTab()
		if pt.Loaded || pt.Loading {
			return
		}
		a.requestPatchTabLoad()
	case TargetTabLocal:
		// The local list is loaded eagerly by EnterTargetSelector.
	}
}

// ConfirmCommitSelection resolves the current selector selection (falling
// back to the cursor row when nothing is toggled) to a review target. It
// reports false — with a status message — when nothing is selected. On
// success the selected rows are stashed for ApplyLoadedSelection, which the
// UI layer calls once it has loaded the diff and session for the returned
// selection.
func (a *App) ConfirmCommitSelection() (ConfirmedSelection, bool) {
	start, end := a.CommitListCursor, a.CommitListCursor
	if a.CommitSelectionRange != nil {
		start, end = a.CommitSelectionRange[0], a.CommitSelectionRange[1]
	}

	// Collect the selected entries. CommitList is newest-first, so the
	// slice below is newest-first too.
	var selected []vcs.CommitInfo
	for i := max(start, 0); i <= end && i < len(a.CommitList); i++ {
		selected = append(selected, a.CommitList[i])
	}
	if len(selected) == 0 {
		a.SetMessage("Select at least one commit")
		return ConfirmedSelection{}, false
	}

	var selectedStaged, selectedUnstaged bool
	var ids []string
	for i := range selected {
		switch {
		case isStagedCommit(&selected[i]):
			selectedStaged = true
		case isUnstagedCommit(&selected[i]):
			selectedUnstaged = true
		default:
			ids = append(ids, selected[i].ID)
		}
	}

	var kind SelectionKind
	switch {
	case (selectedStaged || selectedUnstaged) && len(ids) > 0:
		kind = SelectionStagedUnstagedAndCommits
	case selectedStaged && selectedUnstaged:
		kind = SelectionStagedAndUnstaged
	case selectedStaged:
		kind = SelectionStaged
	case selectedUnstaged:
		kind = SelectionUnstaged
	default:
		kind = SelectionCommits
	}

	a.pendingSelectedCommits = selected
	return ConfirmedSelection{Kind: kind, CommitIDs: ids}, true
}
