// commits.go ports the commit-selection state machine from tuicr's
// src/app/commits.rs: the commit-list cursor/scroll, the sweep selection
// semantics of toggling, the paged load-more, `(`/`)` cycling, and the
// inline-selector helpers. Pure state plus RecentCommits paging — the diff
// loading a selection triggers lives in the UI/run layer (see
// commitselect.go and diffload.go).
package app

import (
	"fmt"

	"github.com/infrashift/mrman/internal/model"
)

// DefaultCommitPageSize is the commit selector's initial load and load-more
// page size (tuicr's VISIBLE_COMMIT_COUNT / commit_page_size).
const DefaultCommitPageSize = 10

// CommitOrder is the inline commit selector's display order. The stored
// ReviewCommits list is always newest-first; this only flips presentation
// (render + input mapping), never the underlying data model.
type CommitOrder int

// Commit display orders.
const (
	// CommitDescending shows the newest commit at the top (the default).
	CommitDescending CommitOrder = iota
	// CommitAscending shows the oldest commit at the top.
	CommitAscending
)

// CommitSelectionStart says which commits a multi-commit review first opens
// with.
type CommitSelectionStart int

// Commit selection starts.
const (
	// CommitSelectionAll selects the whole range (the default).
	CommitSelectionAll CommitSelectionStart = iota
	// CommitSelectionOldest selects only the oldest commit, for a
	// walk-forward per-commit review.
	CommitSelectionOldest
)

// InitialCommitRange is the commit-selection range a fresh multi-commit
// review opens with, honoring the CommitSelectionStart config. Commits are
// stored newest-first, so the oldest commit is the last index — that stays
// true regardless of the CommitOrder display setting (presentation only).
// Returns nil for an empty list.
func InitialCommitRange(start CommitSelectionStart, n int) *model.IndexRange {
	switch {
	case n == 0:
		return nil
	case start == CommitSelectionOldest:
		return &model.IndexRange{n - 1, n - 1}
	default:
		return &model.IndexRange{0, n - 1}
	}
}

// IsStrictCommitSelection reports whether rng selects a valid, strict
// (non-full) subrange of total commits.
func IsStrictCommitSelection(rng *model.IndexRange, total int) bool {
	if rng == nil {
		return false
	}
	start, end := rng[0], rng[1]
	return total > 0 && start >= 0 && start <= end && end < total &&
		(start > 0 || end+1 < total)
}

// CommitsAscending reports whether the inline commit selector renders
// oldest-first. Presentation only — ReviewCommits is always newest-first.
func (a *App) CommitsAscending() bool {
	return a.CommitOrder == CommitAscending
}

// CommitDataIndex converts between a data index into ReviewCommits and its
// on-screen display row (and back — the mapping is its own inverse).
// Identity in descending order; mirrored (n-1-i) in ascending order.
func (a *App) CommitDataIndex(index int) int {
	n := len(a.ReviewCommits)
	if a.CommitsAscending() && n > 0 {
		return n - 1 - min(index, n-1)
	}
	return index
}

// CommitSelectionSummary is the status-bar description of the current
// inline commit selection, or "" when the whole range is selected (the
// caller shows the plain total). A single selected commit reports its
// 1-based display position — so the value changes as `(` / `)` cycle —
// while a multi-commit subrange reports the selected count.
func (a *App) CommitSelectionSummary() string {
	if a.CommitSelectionRange == nil {
		return ""
	}
	total := len(a.ReviewCommits)
	start, end := a.CommitSelectionRange[0], a.CommitSelectionRange[1]
	selected := satSub(end, start) + 1
	if total <= 1 || selected >= total {
		return ""
	}
	if start == end {
		return fmt.Sprintf("commit %d/%d", a.CommitDataIndex(start)+1, total)
	}
	return fmt.Sprintf("%d of %d commits", selected, total)
}

// HasReviewCommits reports whether the current review has a multi-commit
// selection that `(` / `)` can cycle through. Unlike
// HasInlineCommitSelector, this ignores pane visibility so cycling still
// works while the pane is hidden (the status bar shows the count as
// feedback).
func (a *App) HasReviewCommits() bool {
	return len(a.ReviewCommits) > 1 && a.DiffSource.Kind != DiffSourceWorkingTree
}

// HasInlineCommitSelector reports whether the inline commit selector panel
// should be displayed.
func (a *App) HasInlineCommitSelector() bool {
	return a.ShowCommitSelector && a.HasReviewCommits()
}

// ToggleCommitSelector flips the inline commit selector's visibility. When
// hiding it while it is focused, focus moves back to the diff so input
// keeps flowing.
func (a *App) ToggleCommitSelector() {
	visible := !a.ShowCommitSelector
	a.ShowCommitSelector = visible
	if !visible && a.FocusedPanel == PanelCommitSelector {
		a.FocusedPanel = PanelDiff
	}
	status := "hidden"
	if visible {
		status = "visible"
	}
	a.SetMessage("Commit selector: " + status)
}

// CommitSelectUp moves the selector cursor up one row, scrolling when the
// cursor leaves the visible area.
func (a *App) CommitSelectUp() {
	if a.CommitListCursor > 0 {
		a.CommitListCursor--
		if a.CommitListCursor < a.CommitListScrollOffset {
			a.CommitListScrollOffset = a.CommitListCursor
		}
	}
}

// CommitSelectDown moves the selector cursor down one row — onto the "show
// more" row when one is present — scrolling when the cursor leaves the
// visible area.
func (a *App) CommitSelectDown() {
	maxCursor := satSub(a.VisibleCommitCount, 1)
	if a.CanShowMoreCommits() {
		maxCursor = a.VisibleCommitCount
	}
	if a.CommitListCursor < maxCursor {
		a.CommitListCursor++
		if a.CommitListViewportHeight > 0 &&
			a.CommitListCursor >= a.CommitListScrollOffset+a.CommitListViewportHeight {
			a.CommitListScrollOffset = a.CommitListCursor - a.CommitListViewportHeight + 1
		}
	}
}

// ToggleCommitSelectionAndAdvance toggles the cursor commit's membership in
// the selection range, then (only if the cursor commit was newly added to
// the selection) moves the cursor past the end of the range. Lets the user
// press Enter/Space repeatedly to sweep a contiguous run of commits.
//
// Other toggle outcomes leave the cursor in place: edge presses (deselect
// the cursor commit), middle presses (truncate the range without
// unselecting the cursor commit), and clearing the last selection. Those
// aren't "sweep" actions, so advancing would surprise.
func (a *App) ToggleCommitSelectionAndAdvance() {
	cursor := a.CommitListCursor
	wasSelected := a.IsCommitSelected(cursor)
	a.ToggleCommitSelection()
	nowSelected := a.IsCommitSelected(cursor)
	if wasSelected || !nowSelected {
		return
	}
	if a.CommitSelectionRange == nil {
		return
	}
	end := a.CommitSelectionRange[1]
	for a.CommitListCursor <= end {
		before := a.CommitListCursor
		a.CommitSelectDown()
		if a.CommitListCursor == before {
			return
		}
	}
}

// IsOnExpandRow reports whether the cursor is on the "show more" row.
func (a *App) IsOnExpandRow() bool {
	return a.CanShowMoreCommits() && a.CommitListCursor == a.VisibleCommitCount
}

// CanShowMoreCommits reports whether a "show more" row should render:
// either loaded-but-hidden commits exist, or the backend may have older
// history.
func (a *App) CanShowMoreCommits() bool {
	return a.VisibleCommitCount < len(a.CommitList) || a.HasMoreCommits
}

// CommitSelectRowCount is the number of selectable rows the target selector
// renders: the visible commits plus the "show more" row when present.
func (a *App) CommitSelectRowCount() int {
	if a.CanShowMoreCommits() {
		return a.VisibleCommitCount + 1
	}
	return a.VisibleCommitCount
}

// ExpandCommit reveals one more page of commits: first from the
// already-loaded list, then by fetching older history from the backend.
func (a *App) ExpandCommit() error {
	if a.VisibleCommitCount < len(a.CommitList) {
		a.VisibleCommitCount = min(a.VisibleCommitCount+a.CommitPageSize, len(a.CommitList))
		return nil
	}

	if !a.HasMoreCommits {
		a.SetMessage("No more commits")
		return nil
	}

	offset := a.loadedHistoryCommitCount()
	limit := a.CommitPageSize

	newCommits, err := a.VCS.RecentCommits(offset, limit)
	if err != nil {
		return err
	}

	if len(newCommits) == 0 {
		a.HasMoreCommits = false
		a.SetMessage("No more commits")
		return nil
	}

	if len(newCommits) < limit {
		a.HasMoreCommits = false
		a.SetMessage("No more commits")
	}

	a.CommitList = append(a.CommitList, newCommits...)
	a.VisibleCommitCount = len(a.CommitList)
	return nil
}

// ToggleCommitSelection toggles the cursor commit's membership in the
// selection range:
//
//   - no selection: select just the cursor commit
//   - everything selected: collapse to just the cursor commit
//   - press on the single selected commit: deselect all
//   - press at a range edge: shrink from that edge
//   - press in the middle: truncate to (start, cursor-1)
//   - press outside the range: extend to include the cursor
func (a *App) ToggleCommitSelection() {
	cursor := a.CommitListCursor
	if cursor >= len(a.CommitList) {
		return
	}

	if a.CommitSelectionRange == nil {
		a.CommitSelectionRange = &model.IndexRange{cursor, cursor}
		return
	}

	start, end := a.CommitSelectionRange[0], a.CommitSelectionRange[1]
	allSelected := len(a.CommitList) > 1 && start == 0 && end == len(a.CommitList)-1
	if allSelected {
		a.CommitSelectionRange = &model.IndexRange{cursor, cursor}
		return
	}

	switch {
	case cursor >= start && cursor <= end:
		// Cursor is within the range — shrink or deselect.
		switch {
		case start == end:
			// Only one commit selected: deselect all.
			a.CommitSelectionRange = nil
		case cursor == start:
			// At the start edge: shrink from the start.
			a.CommitSelectionRange = &model.IndexRange{start + 1, end}
		case cursor == end:
			// At the end edge: shrink from the end.
			a.CommitSelectionRange = &model.IndexRange{start, end - 1}
		default:
			// In the middle: deselect the cursor and everything after it.
			a.CommitSelectionRange = &model.IndexRange{start, cursor - 1}
		}
	default:
		// Cursor is outside the range — extend to include it.
		a.CommitSelectionRange = &model.IndexRange{min(start, cursor), max(end, cursor)}
	}
}

// IsCommitSelected reports whether the commit at index is inside the
// selection range.
func (a *App) IsCommitSelected(index int) bool {
	if a.CommitSelectionRange == nil {
		return false
	}
	return index >= a.CommitSelectionRange[0] && index <= a.CommitSelectionRange[1]
}

// SelectedCommitSet is the set of commit ids currently selected in the
// commit selector, including the synthetic staged/unstaged ids. hasSet is
// false when there is no selection. Comment visibility uses the private
// selectedCommitSet (comments.go), which excludes the synthetic ids — they
// never scope comments.
func (a *App) SelectedCommitSet() (set map[string]bool, hasSet bool) {
	if a.CommitSelectionRange == nil {
		return nil, false
	}
	start, end := a.CommitSelectionRange[0], a.CommitSelectionRange[1]
	set = map[string]bool{}
	if start > end || len(a.ReviewCommits) == 0 {
		return set, true
	}
	end = min(end, len(a.ReviewCommits)-1)
	for i := max(start, 0); i <= end; i++ {
		set[a.ReviewCommits[i].ID] = true
	}
	return set, true
}

// CommitIDForNewComment is the single commit SHA to stamp on a new comment
// when the inline selector shows exactly one commit. Nil otherwise (full
// range, multi-commit subset, or no selector) — those comments get
// CommitID nil so they stay visible across selections.
func (a *App) CommitIDForNewComment() *string {
	if a.CommitSelectionRange == nil {
		return nil
	}
	start, end := a.CommitSelectionRange[0], a.CommitSelectionRange[1]
	if start != end || start < 0 || start >= len(a.ReviewCommits) {
		return nil
	}
	c := &a.ReviewCommits[start]
	if isSpecialCommit(c) {
		return nil
	}
	id := c.ID
	return &id
}

// CycleCommitNext cycles the inline commit selector to the next individual
// commit (the `)` key): all → last, i → i+1, last → all.
func (a *App) CycleCommitNext() {
	n := len(a.ReviewCommits)
	if n == 0 {
		return
	}
	allSelected := model.IndexRange{0, n - 1}

	switch {
	case a.CommitSelectionRange == nil:
		// None selected: select all.
		a.CommitSelectionRange = &allSelected
	case *a.CommitSelectionRange == allSelected:
		// all → last.
		a.CommitSelectionRange = &model.IndexRange{n - 1, n - 1}
		a.CommitListCursor = n - 1
	default:
		i, j := a.CommitSelectionRange[0], a.CommitSelectionRange[1]
		switch {
		case i != j:
			// Multi-commit subrange: select the last of that range.
			a.CommitSelectionRange = &model.IndexRange{j, j}
			a.CommitListCursor = j
		case i == n-1:
			// last → all.
			a.CommitSelectionRange = &allSelected
		default:
			// i → i+1.
			a.CommitSelectionRange = &model.IndexRange{i + 1, i + 1}
			a.CommitListCursor = i + 1
		}
	}
}

// CycleCommitPrev cycles the inline commit selector to the previous
// individual commit (the `(` key): all → first, i → i-1, first → all.
func (a *App) CycleCommitPrev() {
	n := len(a.ReviewCommits)
	if n == 0 {
		return
	}
	allSelected := model.IndexRange{0, n - 1}

	switch {
	case a.CommitSelectionRange == nil:
		// None selected: select all.
		a.CommitSelectionRange = &allSelected
	case *a.CommitSelectionRange == allSelected:
		// all → first.
		a.CommitSelectionRange = &model.IndexRange{0, 0}
		a.CommitListCursor = 0
	default:
		i, j := a.CommitSelectionRange[0], a.CommitSelectionRange[1]
		switch {
		case i != j:
			// Multi-commit subrange: select the first of that range.
			a.CommitSelectionRange = &model.IndexRange{i, i}
			a.CommitListCursor = i
		case i == 0:
			// first → all.
			a.CommitSelectionRange = &allSelected
		default:
			// i → i-1.
			a.CommitSelectionRange = &model.IndexRange{i - 1, i - 1}
			a.CommitListCursor = i - 1
		}
	}
}

// specialCommitCount counts the synthetic staged/unstaged rows prepended to
// CommitList (they are always leading).
func (a *App) specialCommitCount() int {
	n := 0
	for i := range a.CommitList {
		if !isSpecialCommit(&a.CommitList[i]) {
			break
		}
		n++
	}
	return n
}

// loadedHistoryCommitCount is the number of real history commits loaded,
// i.e. the RecentCommits offset for the next page.
func (a *App) loadedHistoryCommitCount() int {
	return satSub(len(a.CommitList), a.specialCommitCount())
}

// copyIndexRange returns an independent copy of rng (nil-safe).
func copyIndexRange(rng *model.IndexRange) *model.IndexRange {
	if rng == nil {
		return nil
	}
	r := *rng
	return &r
}
