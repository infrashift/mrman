// tree.go ports tuicr's src/app/tree.rs: the file-list tree (directories
// expandable/collapsible, files ordered commit-message-first then by sorted
// directory), the list selection state, and the view toggles that live with
// the file list.
package app

import (
	"path"
	"sort"

	"github.com/infrashift/mrman/internal/model"
)

// FileTreeItem is one visible row of the file tree: a directory (IsDir with
// Path/Expanded) or a file (FileIdx).
type FileTreeItem struct {
	IsDir    bool
	Path     string // directory path when IsDir
	Depth    int
	Expanded bool // when IsDir
	FileIdx  int  // when !IsDir
}

// FileListState is the file list's selection and scroll state, mirroring
// tuicr's ratatui-backed FileListState (selected row + viewport offset).
type FileListState struct {
	selected int
	offset   int

	ScrollX         int
	ViewportWidth   int // set during render
	ViewportHeight  int // set during render
	MaxContentWidth int // set during render
}

// Selected is the selected row index.
func (s *FileListState) Selected() int { return s.selected }

// Select sets the selected row index.
func (s *FileListState) Select(index int) { s.selected = index }

// Offset is the viewport scroll offset.
func (s *FileListState) Offset() int { return s.offset }

// SetOffset sets the viewport scroll offset.
func (s *FileListState) SetOffset(offset int) { s.offset = offset }

// ScrollLeft scrolls the list content left.
func (s *FileListState) ScrollLeft(cols int) {
	s.ScrollX = satSub(s.ScrollX, cols)
}

// ScrollRight scrolls the list content right, clamped to the content width.
func (s *FileListState) ScrollRight(cols int) {
	maxScrollX := satSub(s.MaxContentWidth, s.ViewportWidth)
	s.ScrollX = min(s.ScrollX+cols, maxScrollX)
}

// pathAncestors returns the directories containing p, outermost first
// (e.g. "a/b/c.rs" yields ["a", "a/b"]).
func pathAncestors(p string) []string {
	var ancestors []string
	for dir := path.Dir(p); dir != "." && dir != "/" && dir != ""; dir = path.Dir(dir) {
		ancestors = append(ancestors, dir)
	}
	// Reverse to outermost-first order.
	for i, j := 0, len(ancestors)-1; i < j; i, j = i+1, j-1 {
		ancestors[i], ancestors[j] = ancestors[j], ancestors[i]
	}
	return ancestors
}

// FileListDown moves the file-list selection down by n rows.
func (a *App) FileListDown(n int) {
	visibleItems := a.BuildVisibleItems()
	maxIdx := satSub(len(visibleItems), 1)
	a.FileListState.Select(min(a.FileListState.Selected()+n, maxIdx))
	a.scrollFileListToSelection(len(visibleItems))
	a.followFileListInSingleFileView()
}

// scrollFileListToSelection scrolls the tree viewport the minimum amount
// that keeps the selection on screen.
//
// Without this the selection walked off the bottom of the pane and the tree
// simply stopped responding: the only thing that ever moved the offset was
// the mouse wheel, so a keyboard user could not reach past the first
// screenful of a large review. The other list panes each already do this —
// see clampCommentNavCursor.
func (a *App) scrollFileListToSelection(total int) {
	viewport := a.FileListState.ViewportHeight
	if viewport <= 0 {
		return
	}
	selected := a.FileListState.Selected()
	offset := a.FileListState.Offset()
	if selected < offset {
		offset = selected
	}
	if selected >= offset+viewport {
		offset = selected - viewport + 1
	}
	a.FileListState.SetOffset(max(min(offset, satSub(total, viewport)), 0))
}

// FileListUp moves the file-list selection up by n rows.
func (a *App) FileListUp(n int) {
	a.FileListState.Select(satSub(a.FileListState.Selected(), n))
	a.scrollFileListToSelection(len(a.BuildVisibleItems()))
	a.followFileListInSingleFileView()
}

// followFileListInSingleFileView reveals the highlighted file immediately
// in single-file view (the diff panel always shows one file at a time).
// Skips directories and no-ops outside single-file view to keep multi-file
// scrolling exactly as before.
func (a *App) followFileListInSingleFileView() {
	if !a.IsSingleFileView {
		return
	}
	if item, ok := a.SelectedTreeItem(); ok && !item.IsDir {
		a.JumpToFile(item.FileIdx)
	}
}

// FileListViewportScrollDown scrolls the file-list viewport down by lines
// without moving the selection unless it would fall off the top.
func (a *App) FileListViewportScrollDown(lines int) {
	total := len(a.BuildVisibleItems())
	viewport := max(a.FileListState.ViewportHeight, 1)
	maxOffset := satSub(total, viewport)
	newOffset := min(a.FileListState.Offset()+lines, maxOffset)
	a.FileListState.SetOffset(newOffset)
	if a.FileListState.Selected() < newOffset {
		a.FileListState.Select(newOffset)
	}
}

// FileListViewportScrollUp scrolls the file-list viewport up by lines
// without moving the selection unless it would fall off the bottom.
func (a *App) FileListViewportScrollUp(lines int) {
	viewport := max(a.FileListState.ViewportHeight, 1)
	newOffset := satSub(a.FileListState.Offset(), lines)
	a.FileListState.SetOffset(newOffset)
	maxVisible := satSub(newOffset+viewport, 1)
	if a.FileListState.Selected() > maxVisible {
		a.FileListState.Select(maxVisible)
	}
}

// ToggleDiffViewMode flips between unified and side-by-side rendering.
func (a *App) ToggleDiffViewMode() {
	if a.IsPristineMode {
		// Side-by-side has nothing to show in pristine mode: there is no
		// diff, so the two panes would render identical content.
		a.SetMessage("side-by-side not available in pristine mode")
		return
	}
	modeName := "unified"
	if a.DiffViewMode == ViewUnified {
		a.DiffViewMode = ViewSideBySide
		modeName = "side-by-side"
	} else {
		a.DiffViewMode = ViewUnified
	}
	a.SetMessage("Diff view mode: " + modeName)
	a.RebuildAnnotations()
}

// ToggleFileList shows/hides the file list panel, moving focus back to the
// diff when the hidden panel had it.
func (a *App) ToggleFileList() {
	a.ShowFileList = !a.ShowFileList
	if !a.ShowFileList && (a.FocusedPanel == PanelFileList || a.FocusedPanel == PanelComments) {
		a.FocusedPanel = PanelDiff
	}
	status := "hidden"
	if a.ShowFileList {
		status = "visible"
	}
	a.SetMessage("File list: " + status)
}

// ToggleSingleFileView toggles single-file view. When on, the diff panel
// renders only the currently focused file instead of the full
// continuous-scroll concatenation.
func (a *App) ToggleSingleFileView() {
	a.IsSingleFileView = !a.IsSingleFileView
	// calculateFileScrollOffset changes meaning across modes (single-file
	// stops at the review-comments header, all-files accumulates), so
	// re-snap the viewport to the current file.
	start := a.calculateFileScrollOffset(a.DiffState.CurrentFileIdx)
	a.DiffState.ScrollOffset = start
	a.DiffState.CursorLine = start
	status := "all files"
	if a.IsSingleFileView {
		status = "single file"
	}
	a.SetMessage("View: " + status)
	a.RebuildAnnotations()
}

// SortFilesByDirectory reorders DiffFiles: commit-message files first, then
// files grouped by directory in lexicographic directory order (insertion
// order within a directory), matching tuicr's BTreeMap walk. When
// resetPosition is false the previously focused file stays focused.
func (a *App) SortFilesByDirectory(resetPosition bool) {
	a.FileLineCountCache = map[int]uint32{}

	var currentPath string
	haveCurrent := false
	if !resetPosition {
		currentPath, haveCurrent = a.CurrentFilePath()
	}

	dirMap := map[string][]model.DiffFile{}
	var commitMsgFiles []model.DiffFile

	for i := range a.DiffFiles {
		file := a.DiffFiles[i]
		if file.IsCommitMessage {
			commitMsgFiles = append(commitMsgFiles, file)
			continue
		}
		dir := path.Dir(file.DisplayPath()) // "." for root files
		dirMap[dir] = append(dirMap[dir], file)
	}

	dirs := make([]string, 0, len(dirMap))
	for dir := range dirMap {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	sorted := make([]model.DiffFile, 0, len(a.DiffFiles))
	sorted = append(sorted, commitMsgFiles...)
	for _, dir := range dirs {
		sorted = append(sorted, dirMap[dir]...)
	}
	a.DiffFiles = sorted

	if haveCurrent {
		for idx := range a.DiffFiles {
			if a.DiffFiles[idx].DisplayPath() == currentPath {
				a.JumpToFile(idx)
				return
			}
		}
	}

	// Start at the overview position (review comments header) so the diff
	// title shows total stats on launch.
	a.DiffState.CursorLine = 0
	a.DiffState.ScrollOffset = 0
	a.DiffState.CurrentFileIdx = 0
}

// ExpandAllDirs expands every directory containing a diff file.
func (a *App) ExpandAllDirs() {
	a.ExpandedDirs = map[string]bool{}
	for i := range a.DiffFiles {
		for _, parent := range pathAncestors(a.DiffFiles[i].DisplayPath()) {
			a.ExpandedDirs[parent] = true
		}
	}
	a.ensureValidTreeSelection()
}

// CollapseAllDirs collapses every directory.
func (a *App) CollapseAllDirs() {
	a.ExpandedDirs = map[string]bool{}
	a.ensureValidTreeSelection()
}

// ToggleDirectory flips one directory's expanded state.
func (a *App) ToggleDirectory(dirPath string) {
	if a.ExpandedDirs[dirPath] {
		delete(a.ExpandedDirs, dirPath)
		a.ensureValidTreeSelection()
	} else {
		a.ExpandedDirs[dirPath] = true
	}
}

// ensureValidTreeSelection re-selects a sensible tree row after visibility
// changes: the current file if visible, else its nearest visible ancestor
// directory, else row 0.
func (a *App) ensureValidTreeSelection() {
	visibleItems := a.BuildVisibleItems()
	if len(visibleItems) == 0 {
		a.FileListState.Select(0)
		return
	}

	currentFileIdx := a.DiffState.CurrentFileIdx
	fileVisible := false
	for _, item := range visibleItems {
		if !item.IsDir && item.FileIdx == currentFileIdx {
			fileVisible = true
			break
		}
	}

	if fileVisible {
		if treeIdx, ok := a.fileIdxToTreeIdx(currentFileIdx); ok {
			a.FileListState.Select(treeIdx)
		}
		return
	}

	if currentFileIdx < len(a.DiffFiles) {
		ancestors := pathAncestors(a.DiffFiles[currentFileIdx].DisplayPath())
		// Walk innermost-first, matching tuicr's parent() loop.
		for i := len(ancestors) - 1; i >= 0; i-- {
			for treeIdx, item := range visibleItems {
				if item.IsDir && item.Path == ancestors[i] {
					a.FileListState.Select(treeIdx)
					return
				}
			}
		}
	}
	a.FileListState.Select(0)
}

// BuildVisibleItems builds the visible tree rows: each new directory is
// emitted once at first encounter; files under a collapsed directory are
// hidden along with any not-yet-seen deeper directories.
func (a *App) BuildVisibleItems() []FileTreeItem {
	var items []FileTreeItem
	seenDirs := map[string]bool{}

	for fileIdx := range a.DiffFiles {
		ancestors := pathAncestors(a.DiffFiles[fileIdx].DisplayPath())

		visible := true
		for depth, dir := range ancestors {
			if !seenDirs[dir] && visible {
				items = append(items, FileTreeItem{
					IsDir:    true,
					Path:     dir,
					Depth:    depth,
					Expanded: a.ExpandedDirs[dir],
				})
				seenDirs[dir] = true
			}
			if !a.ExpandedDirs[dir] {
				visible = false
			}
		}

		if visible {
			items = append(items, FileTreeItem{
				FileIdx: fileIdx,
				Depth:   len(ancestors),
			})
		}
	}

	return items
}

// SelectedTreeItem returns the tree row under the file-list selection.
func (a *App) SelectedTreeItem() (FileTreeItem, bool) {
	visibleItems := a.BuildVisibleItems()
	selectedIdx := a.FileListState.Selected()
	if selectedIdx < len(visibleItems) {
		return visibleItems[selectedIdx], true
	}
	return FileTreeItem{}, false
}
