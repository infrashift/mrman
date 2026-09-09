// patchtab.go is the state machine behind the selector's Patches tab: the
// inbox a mailing-list reviewer works from, listing .patch/.diff/mbox files
// in a directory the way the Pull Requests tab lists a forge's open PRs.
//
// It follows the same pull-based shape as prtab.go. The app records *what*
// should be loaded and the UI layer performs the scan off the render loop,
// with a generation counter so a result for a directory the user has since
// navigated away from is discarded rather than shown.

package app

import (
	"sort"
	"strings"
	"time"
)

// PatchRow is one artifact in the inbox listing.
type PatchRow struct {
	// Path is the artifact's absolute path.
	Path string
	// Name is its file name, which is what the row leads with.
	Name string
	// Subject is the first patch's mail subject, empty for a bare diff.
	Subject string
	// Author is the first patch's From: line, empty for a bare diff.
	Author string
	// Series describes the artifact's shape: "3 patches" for a series, empty
	// for a lone patch.
	Series string
	// Modified is the file's mtime, so the newest arrivals sort first.
	Modified time.Time
	// Err is set when the file could not be read as a patch; the row is still
	// listed, because a file the reviewer expected to see and cannot open is
	// worth showing rather than silently omitting.
	Err string
}

// Reviewable reports whether the row can be opened.
func (r PatchRow) Reviewable() bool { return r.Err == "" }

// PatchTabState is the Patches tab's list, cursor and load status.
type PatchTabState struct {
	// Dir is the directory being listed.
	Dir string
	// Rows are the artifacts found there, newest first.
	Rows []PatchRow

	Cursor         int
	ScrollOffset   int
	ViewportHeight int

	// Loading is true while a scan is in flight; Loaded records that one has
	// been attempted, so revisiting the tab does not rescan.
	Loading bool
	Loaded  bool
	// Err is a scan failure, shown in the tab body rather than the status bar
	// so it sits where the missing rows would have been.
	Err string

	// Filter narrows the loaded rows; FilterEditing is true while the "/"
	// prompt is open.
	Filter        string
	FilterEditing bool

	// Pending and PendingLoad carry a scan request for the UI layer to drain.
	Pending     bool
	PendingLoad PatchTabLoadRequest
	// Gen guards against applying a result for a directory that is no longer
	// the one on screen.
	Gen uint64
}

// PatchTabLoadRequest asks the UI layer to scan a directory for artifacts.
type PatchTabLoadRequest struct {
	Gen uint64
	Dir string
}

// ensurePatchTab lazily creates the tab's state, defaulting the directory to
// the review root — which for a patch review is where the artifact lives, so
// reopening the selector lists its siblings.
func (a *App) ensurePatchTab() *PatchTabState {
	if a.PatchTab == nil {
		dir := ""
		if a.VcsInfo != nil {
			dir = a.VcsInfo.RootPath
		}
		a.PatchTab = &PatchTabState{Dir: dir}
	}
	return a.PatchTab
}

// PatchTabDir is the directory currently listed.
func (a *App) PatchTabDir() string { return a.ensurePatchTab().Dir }

// requestPatchTabLoad arms a scan of the tab's directory.
func (a *App) requestPatchTabLoad() {
	pt := a.ensurePatchTab()
	pt.Gen++
	pt.Loading = true
	pt.Loaded = true
	pt.Err = ""
	pt.Pending = true
	pt.PendingLoad = PatchTabLoadRequest{Gen: pt.Gen, Dir: pt.Dir}
}

// TakePatchTabLoad hands the UI layer a pending scan request, if any.
func (a *App) TakePatchTabLoad() (PatchTabLoadRequest, bool) {
	pt := a.ensurePatchTab()
	if !pt.Pending {
		return PatchTabLoadRequest{}, false
	}
	pt.Pending = false
	return pt.PendingLoad, true
}

// ReloadPatchTab rescans the directory, discarding what is on screen.
func (a *App) ReloadPatchTab() {
	pt := a.ensurePatchTab()
	pt.Rows = nil
	pt.Cursor = 0
	pt.ScrollOffset = 0
	a.requestPatchTabLoad()
}

// SetPatchTabDir points the tab at another directory and rescans.
func (a *App) SetPatchTabDir(dir string) {
	pt := a.ensurePatchTab()
	pt.Dir = dir
	a.ReloadPatchTab()
}

// ApplyPatchTabRows installs a completed scan, newest first. Results from a
// superseded generation are dropped.
func (a *App) ApplyPatchTabRows(gen uint64, rows []PatchRow) {
	pt := a.ensurePatchTab()
	if gen != pt.Gen {
		return
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].Modified.After(rows[j].Modified)
	})
	pt.Rows = rows
	pt.Loading = false
	pt.Cursor = 0
	pt.ScrollOffset = 0
	a.clampPatchTabCursor()
}

// SetPatchTabError records a scan failure, dropping superseded results.
func (a *App) SetPatchTabError(gen uint64, message string) {
	pt := a.ensurePatchTab()
	if gen != pt.Gen {
		return
	}
	pt.Loading = false
	pt.Err = message
}

// PatchTabFilteredRows are the rows matching the current filter.
func (a *App) PatchTabFilteredRows() []PatchRow {
	pt := a.ensurePatchTab()
	if pt.Filter == "" {
		return pt.Rows
	}
	needle := strings.ToLower(pt.Filter)
	var out []PatchRow
	for _, r := range pt.Rows {
		if patchRowMatches(r, needle) {
			out = append(out, r)
		}
	}
	return out
}

// patchRowMatches reports whether a row matches a lowercased needle, across
// every field the reviewer can see.
func patchRowMatches(r PatchRow, needle string) bool {
	for _, field := range []string{r.Name, r.Subject, r.Author, r.Series} {
		if strings.Contains(strings.ToLower(field), needle) {
			return true
		}
	}
	return false
}

// PatchTabRowCount is the number of selectable rows.
func (a *App) PatchTabRowCount() int { return len(a.PatchTabFilteredRows()) }

// PatchTabDown moves the cursor down one row.
func (a *App) PatchTabDown() {
	pt := a.ensurePatchTab()
	pt.Cursor++
	a.clampPatchTabCursor()
}

// PatchTabUp moves the cursor up one row.
func (a *App) PatchTabUp() {
	pt := a.ensurePatchTab()
	pt.Cursor--
	a.clampPatchTabCursor()
}

// clampPatchTabCursor keeps the cursor inside the list and the scroll window
// around the cursor.
func (a *App) clampPatchTabCursor() {
	pt := a.ensurePatchTab()
	count := a.PatchTabRowCount()
	switch {
	case count == 0:
		pt.Cursor, pt.ScrollOffset = 0, 0
		return
	case pt.Cursor < 0:
		pt.Cursor = 0
	case pt.Cursor >= count:
		pt.Cursor = count - 1
	}

	height := pt.ViewportHeight
	if height <= 0 {
		return
	}
	if pt.Cursor < pt.ScrollOffset {
		pt.ScrollOffset = pt.Cursor
	}
	if pt.Cursor >= pt.ScrollOffset+height {
		pt.ScrollOffset = pt.Cursor - height + 1
	}
	if maxOffset := count - height; pt.ScrollOffset > maxOffset {
		pt.ScrollOffset = max(maxOffset, 0)
	}
}

// SelectedPatchRow is the row under the cursor.
func (a *App) SelectedPatchRow() (PatchRow, bool) {
	rows := a.PatchTabFilteredRows()
	pt := a.ensurePatchTab()
	if pt.Cursor < 0 || pt.Cursor >= len(rows) {
		return PatchRow{}, false
	}
	return rows[pt.Cursor], true
}

// ---- The "/" filter prompt ----

// PatchTabFilterEditing reports whether the filter prompt is open.
func (a *App) PatchTabFilterEditing() bool { return a.ensurePatchTab().FilterEditing }

// BeginPatchTabFilter opens the filter prompt, if the tab is showing.
func (a *App) BeginPatchTabFilter() {
	if a.TargetTab != TargetTabPatches {
		return
	}
	a.ensurePatchTab().FilterEditing = true
}

// CommitPatchTabFilter closes the prompt, keeping what was typed.
func (a *App) CommitPatchTabFilter() {
	a.ensurePatchTab().FilterEditing = false
	a.clampPatchTabCursor()
}

// CancelPatchTabFilter closes the prompt and clears the filter.
func (a *App) CancelPatchTabFilter() {
	pt := a.ensurePatchTab()
	pt.FilterEditing = false
	pt.Filter = ""
	a.clampPatchTabCursor()
}

// InsertPatchTabFilterChar adds a character to the filter.
func (a *App) InsertPatchTabFilterChar(r rune) {
	pt := a.ensurePatchTab()
	pt.Filter += string(r)
	pt.Cursor = 0
	a.clampPatchTabCursor()
}

// DeletePatchTabFilterChar removes the last rune of the filter, respecting
// multi-byte boundaries so a filter typed in any language deletes cleanly.
func (a *App) DeletePatchTabFilterChar() {
	pt := a.ensurePatchTab()
	if pt.Filter == "" {
		return
	}
	pt.Filter = pt.Filter[:PrevCharBoundary(pt.Filter, len(pt.Filter))]
	pt.Cursor = 0
	a.clampPatchTabCursor()
}

// DeletePatchTabFilterWord removes the word before the cursor.
func (a *App) DeletePatchTabFilterWord() {
	pt := a.ensurePatchTab()
	pt.Filter, _ = DeleteWordBefore(pt.Filter, len(pt.Filter))
	pt.Cursor = 0
	a.clampPatchTabCursor()
}

// ClearPatchTabFilter empties the filter, leaving the prompt open.
func (a *App) ClearPatchTabFilter() {
	pt := a.ensurePatchTab()
	pt.Filter = ""
	pt.Cursor = 0
	a.clampPatchTabCursor()
}

// SetPatchTabViewportHeight records how many rows the renderer can show, so
// the scroll window can follow the cursor.
func (a *App) SetPatchTabViewportHeight(height int) {
	a.ensurePatchTab().ViewportHeight = height
	a.clampPatchTabCursor()
}

// FilePatchLabel is the short identifier of the patch a diff entry came from
// — "2/5" for a series member — or "" when there is nothing to disambiguate.
//
// It is deliberately quiet. The label earns its place only when the diff holds
// files from more than one patch, which is exactly when two entries can share
// a display path and the reviewer needs to know which is which. Narrowed to a
// single patch every row would carry the same tag, which is noise.
func (a *App) FilePatchLabel(fileIdx int) string {
	if fileIdx < 0 || fileIdx >= len(a.DiffFiles) {
		return ""
	}
	id := a.DiffFiles[fileIdx].CommitID
	if id == "" || !a.diffSpansSeveralPatches() {
		return ""
	}
	for i := range a.ReviewCommits {
		if a.ReviewCommits[i].ID == id {
			return a.ReviewCommits[i].ShortID
		}
	}
	return ""
}

// diffSpansSeveralPatches reports whether the loaded diff came from more than
// one patch.
func (a *App) diffSpansSeveralPatches() bool {
	first := ""
	for i := range a.DiffFiles {
		id := a.DiffFiles[i].CommitID
		if id == "" {
			continue
		}
		if first == "" {
			first = id
			continue
		}
		if id != first {
			return true
		}
	}
	return false
}

// FilePathIsAmbiguous reports whether another entry in the diff shares this
// one's display path — the case the label exists for.
func (a *App) FilePathIsAmbiguous(fileIdx int) bool {
	if fileIdx < 0 || fileIdx >= len(a.DiffFiles) {
		return false
	}
	path := a.DiffFiles[fileIdx].DisplayPath()
	seen := false
	for i := range a.DiffFiles {
		if a.DiffFiles[i].DisplayPath() != path {
			continue
		}
		if seen {
			return true
		}
		seen = true
	}
	return false
}
