// prtab.go implements the Pull Requests tab of the review target selector,
// porting tuicr's src/forge/selector.rs state machine: a paged PR listing,
// the local "/" filter over already-loaded rows, the "r" all-open vs
// review-requested scope toggle, and cursor/scroll bookkeeping.
//
// The app layer never calls a forge itself. It records *what* should be
// fetched in a pending PrTabLoadRequest, which the UI layer drains with
// TakePrTabLoad, performs off the render loop, and feeds back through
// ApplyPrTabPage or SetPrTabError. Every result carries the PrList
// generation it was issued under so a stale page — from a scope toggle or a
// reopened selector — is discarded instead of overwriting fresher rows.
package app

import (
	"strconv"
	"strings"

	"github.com/infrashift/mrman/internal/forge"
)

// DefaultPrPageSize is the page size requested from the forge. It matches
// tuicr's per_page=100 listings: PR lists are small enough that one page
// almost always suffices, so paging is the exception rather than the norm.
const DefaultPrPageSize = 100

// PrTabLoadRequest describes one PR-list page fetch the UI layer owes.
type PrTabLoadRequest struct {
	// Gen is the PrList generation this request was issued under; results
	// carrying a different generation are stale and dropped.
	Gen uint64
	// Scope selects all-open vs review-requested.
	Scope forge.ListScope
	// PageToken is the forge-owned cursor; "" requests the first page.
	PageToken string
	// Append is true for "load more" (keep existing rows), false for a
	// fresh listing (replace them).
	Append bool
}

// ensurePr returns the PR state, creating it when absent. The selector's
// Pull Requests tab is reachable from a purely local review, where nothing
// has allocated PrState yet; Details stays nil so InPrMode remains false.
func (a *App) ensurePr() *PrState {
	if a.Pr == nil {
		a.Pr = &PrState{}
	}
	return a.Pr
}

// EnsurePrState returns the PR state, creating it when absent. Exported for
// the UI layer, which needs somewhere to record generation counters for
// async PR operations started from a purely local review.
func (a *App) EnsurePrState() *PrState { return a.ensurePr() }

// requestPrTabLoad arms a page fetch for the UI layer to drain.
func (a *App) requestPrTabLoad(appendPage bool) {
	p := a.ensurePr()
	p.Gens.PrList++
	p.TabLoading = true
	p.TabError = ""
	p.TabLoaded = true

	token := ""
	if appendPage {
		token = p.NextPage
	}
	p.TabPending = true
	p.PendingLoad = PrTabLoadRequest{
		Gen:       p.Gens.PrList,
		Scope:     p.TabScope,
		PageToken: token,
		Append:    appendPage,
	}
}

// TakePrTabLoad drains the pending PR-list fetch, if any. The UI layer calls
// it after every selector dispatch and issues the returned request as a
// tea.Cmd.
func (a *App) TakePrTabLoad() (PrTabLoadRequest, bool) {
	if a.Pr == nil || !a.Pr.TabPending {
		return PrTabLoadRequest{}, false
	}
	a.Pr.TabPending = false
	return a.Pr.PendingLoad, true
}

// ApplyPrTabPage installs a fetched page, discarding stale generations.
func (a *App) ApplyPrTabPage(gen uint64, page *forge.PullRequestPage, appendPage bool) {
	p := a.ensurePr()
	if gen != p.Gens.PrList {
		return // superseded by a newer request
	}
	p.TabLoading = false
	p.TabError = ""
	if page == nil {
		p.NextPage = ""
		return
	}
	if appendPage {
		p.TabRows = append(p.TabRows, page.Items...)
	} else {
		p.TabRows = page.Items
		p.TabCursor = 0
		p.TabScrollOffset = 0
	}
	p.NextPage = page.NextPageToken
	a.clampPrTabCursor()
}

// SetPrTabError records a failed fetch, discarding stale generations.
func (a *App) SetPrTabError(gen uint64, message string) {
	p := a.ensurePr()
	if gen != p.Gens.PrList {
		return
	}
	p.TabLoading = false
	p.TabError = message
}

// PrTabFilteredRows returns the rows passing the local filter. Like tuicr,
// the filter is applied to already-loaded rows only and never re-queries the
// forge, so it stays instant while a listing is still paging in.
func (a *App) PrTabFilteredRows() []forge.PullRequestSummary {
	if a.Pr == nil {
		return nil
	}
	query := strings.ToLower(strings.TrimSpace(a.Pr.TabFilter))
	if query == "" {
		return a.Pr.TabRows
	}
	var kept []forge.PullRequestSummary
	for i := range a.Pr.TabRows {
		if prRowMatches(&a.Pr.TabRows[i], query) {
			kept = append(kept, a.Pr.TabRows[i])
		}
	}
	return kept
}

// prRowMatches reports whether a row matches the lowercased filter query,
// searching the fields a reviewer would type: number, title, author, branch.
func prRowMatches(row *forge.PullRequestSummary, query string) bool {
	if strings.Contains(strconv.FormatUint(row.Number, 10), query) {
		return true
	}
	for _, field := range []string{row.Title, row.Author, row.HeadRefName} {
		if strings.Contains(strings.ToLower(field), query) {
			return true
		}
	}
	return false
}

// CanLoadMorePrs reports whether a "load more" row should render.
func (a *App) CanLoadMorePrs() bool {
	return a.Pr != nil && a.Pr.NextPage != ""
}

// PrTabRowCount is the number of selectable rows: filtered PRs plus the
// "load more" row when another page exists.
func (a *App) PrTabRowCount() int {
	count := len(a.PrTabFilteredRows())
	if a.CanLoadMorePrs() {
		count++
	}
	return count
}

// IsOnPrLoadMoreRow reports whether the cursor rests on the "load more" row.
func (a *App) IsOnPrLoadMoreRow() bool {
	return a.CanLoadMorePrs() && a.Pr.TabCursor == len(a.PrTabFilteredRows())
}

// PrTabSelected returns the PR under the cursor, false on the "load more"
// row or an empty listing.
func (a *App) PrTabSelected() (forge.PullRequestSummary, bool) {
	rows := a.PrTabFilteredRows()
	if a.Pr == nil || a.Pr.TabCursor < 0 || a.Pr.TabCursor >= len(rows) {
		return forge.PullRequestSummary{}, false
	}
	return rows[a.Pr.TabCursor], true
}

// PrTabDown moves the cursor down one row, scrolling at the viewport edge.
func (a *App) PrTabDown() {
	p := a.ensurePr()
	if p.TabCursor < satSub(a.PrTabRowCount(), 1) {
		p.TabCursor++
		if p.TabViewportHeight > 0 && p.TabCursor >= p.TabScrollOffset+p.TabViewportHeight {
			p.TabScrollOffset = p.TabCursor - p.TabViewportHeight + 1
		}
	}
}

// PrTabUp moves the cursor up one row, scrolling at the viewport edge.
func (a *App) PrTabUp() {
	p := a.ensurePr()
	if p.TabCursor > 0 {
		p.TabCursor--
		if p.TabCursor < p.TabScrollOffset {
			p.TabScrollOffset = p.TabCursor
		}
	}
}

// clampPrTabCursor keeps the cursor and scroll window inside the row set
// after the rows change (a new page, or a filter edit shrinking the list).
func (a *App) clampPrTabCursor() {
	p := a.ensurePr()
	maxCursor := satSub(a.PrTabRowCount(), 1)
	if p.TabCursor > maxCursor {
		p.TabCursor = maxCursor
	}
	if p.TabCursor < 0 {
		p.TabCursor = 0
	}
	if p.TabScrollOffset > p.TabCursor {
		p.TabScrollOffset = p.TabCursor
	}
}

// LoadMorePrs requests the next page when the cursor is on the "load more"
// row. It reports whether a fetch was armed.
func (a *App) LoadMorePrs() bool {
	if !a.CanLoadMorePrs() || a.Pr.TabLoading {
		return false
	}
	a.requestPrTabLoad(true)
	return true
}

// TogglePrTabScope flips between all-open and review-requested listings and
// refetches from the first page — the scope is a server-side filter, so
// unlike the "/" filter it cannot be applied to loaded rows.
func (a *App) TogglePrTabScope() {
	p := a.ensurePr()
	p.TabScope = p.TabScope.Toggled()
	p.TabRows = nil
	p.NextPage = ""
	p.TabCursor = 0
	p.TabScrollOffset = 0
	a.requestPrTabLoad(false)
	a.SetMessage("Merge requests: " + p.TabScope.Label())
}

// ReloadPrTab refetches the listing from the first page, keeping scope and
// filter.
func (a *App) ReloadPrTab() {
	p := a.ensurePr()
	p.TabRows = nil
	p.NextPage = ""
	p.TabCursor = 0
	p.TabScrollOffset = 0
	a.requestPrTabLoad(false)
}

// --- "/" filter sub-state ---

// PrTabFilterEditing reports whether the "/" filter prompt is open. It is a
// sub-state of the selector rather than a top-level input mode, matching
// tuicr's pr_filter_editing().
func (a *App) PrTabFilterEditing() bool {
	return a.Pr != nil && a.Pr.TabFilterEditing
}

// BeginPrTabFilter opens the "/" filter prompt on the Pull Requests tab.
func (a *App) BeginPrTabFilter() {
	if a.TargetTab != TargetTabPullRequests {
		return
	}
	a.ensurePr().TabFilterEditing = true
}

// CommitPrTabFilter closes the filter prompt, keeping the typed query.
func (a *App) CommitPrTabFilter() {
	if a.Pr != nil {
		a.Pr.TabFilterEditing = false
	}
}

// CancelPrTabFilter closes the filter prompt and clears the query, restoring
// the unfiltered listing.
func (a *App) CancelPrTabFilter() {
	if a.Pr == nil {
		return
	}
	a.Pr.TabFilterEditing = false
	a.Pr.TabFilter = ""
	a.clampPrTabCursor()
}

// InsertPrTabFilterChar appends a rune to the filter query.
func (a *App) InsertPrTabFilterChar(r rune) {
	p := a.ensurePr()
	p.TabFilter += string(r)
	a.clampPrTabCursor()
}

// DeletePrTabFilterChar removes the last rune of the filter query.
func (a *App) DeletePrTabFilterChar() {
	p := a.ensurePr()
	if p.TabFilter == "" {
		return
	}
	p.TabFilter = p.TabFilter[:PrevCharBoundary(p.TabFilter, len(p.TabFilter))]
	a.clampPrTabCursor()
}

// DeletePrTabFilterWord removes the last whitespace-delimited word.
func (a *App) DeletePrTabFilterWord() {
	p := a.ensurePr()
	p.TabFilter, _ = DeleteWordBefore(p.TabFilter, len(p.TabFilter))
	a.clampPrTabCursor()
}

// ClearPrTabFilter empties the filter query, leaving the prompt open.
func (a *App) ClearPrTabFilter() {
	a.ensurePr().TabFilter = ""
	a.clampPrTabCursor()
}
