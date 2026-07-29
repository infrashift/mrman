// reviewedmut.go ports the mutation half of tuicr's src/app/reviewed.rs:
// toggling file/hunk reviewed state, staging reviewed files, and queueing
// external-editor targets. The read-only reviewed helpers live in app.go.
package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/infrashift/mrman/internal/editor"
)

// ToggleReviewed toggles the reviewed flag of the file under the diff
// cursor, collapsing it and snapping the cursor to its header.
func (a *App) ToggleReviewed() {
	a.ToggleReviewedForFileIdx(a.DiffState.CurrentFileIdx, true)
}

// ToggleReviewedForFileIdx toggles the reviewed flag of file fileIdx (e.g.
// from the file-list selection). adjustCursor moves the diff cursor to the
// file's header line; the file list path passes false to keep the cursor
// where it is.
func (a *App) ToggleReviewedForFileIdx(fileIdx int, adjustCursor bool) {
	if fileIdx >= len(a.DiffFiles) {
		return
	}
	path := a.DiffFiles[fileIdx].DisplayPath()
	review := a.Session.File(path)
	if review == nil {
		return
	}
	review.Reviewed = !review.Reviewed
	a.Dirty = true

	// Update CurrentFileIdx before rebuilding annotations: single-file view
	// filters annotations against it.
	if adjustCursor {
		a.DiffState.CurrentFileIdx = fileIdx
	}
	a.RebuildAnnotations()

	if adjustCursor {
		a.DiffState.CursorLine = a.calculateFileScrollOffset(fileIdx)
		a.ensureCursorVisible()
	}

	if review.Reviewed {
		a.SetMessage("File marked reviewed")
	} else {
		a.SetMessage("File marked unreviewed")
	}
}

// ToggleHunkReviewed toggles the reviewed flag of the hunk under the
// cursor, collapsing its body and parking the cursor on its header.
func (a *App) ToggleHunkReviewed() {
	fileIdx, hunkIdx, ok := a.HunkAtCursor()
	if !ok {
		a.SetWarning("Move cursor to a hunk to toggle reviewed")
		return
	}

	path, key, ok := a.hunkReviewTarget(fileIdx, hunkIdx)
	if !ok {
		a.SetWarning("Move cursor to a hunk to toggle reviewed")
		return
	}

	review := a.Session.File(path)
	if review == nil {
		return
	}

	reviewed := review.ToggleHunkReviewed(key)
	a.Dirty = true
	a.RebuildAnnotations()
	a.DiffState.CurrentFileIdx = fileIdx
	if treeIdx, ok := a.fileIdxToTreeIdx(fileIdx); ok {
		a.FileListState.Select(treeIdx)
	}
	if headerLine, ok := a.HunkHeaderLine(fileIdx, hunkIdx); ok {
		a.DiffState.CursorLine = headerLine
	}
	a.ensureCursorVisible()

	if reviewed {
		a.SetMessage("Hunk marked reviewed")
	} else {
		a.SetMessage("Hunk marked unreviewed")
	}
}

// CanStage reports whether the active diff source supports staging reviewed
// files (git worktree-backed unstaged views only).
func (a *App) CanStage() bool {
	return a.DiffSource.Kind == DiffSourceUnstaged || a.DiffSource.Kind == DiffSourceStagedAndUnstaged
}

// StageReviewedFiles stages every session-reviewed file through the VCS
// backend and returns how many were staged. The UI layer reloads the diff
// afterwards (staged content leaves the unstaged view).
func (a *App) StageReviewedFiles() int {
	if !a.CanStage() {
		a.SetError("Staging only available when viewing unstaged diffs")
		return 0
	}
	var reviewedPaths []string
	for path, review := range a.Session.Files {
		if review.Reviewed {
			reviewedPaths = append(reviewedPaths, path)
		}
	}
	if len(reviewedPaths) == 0 {
		a.SetWarning("No reviewed files to stage")
		return 0
	}
	sort.Strings(reviewedPaths)
	staged := 0
	for _, path := range reviewedPaths {
		if err := a.VCS.StageFile(path); err != nil {
			a.SetError(fmt.Sprintf("Failed to stage %s: %v", path, err))
			return staged
		}
		staged++
	}
	a.SetMessage(fmt.Sprintf("Staged %d reviewed file(s)", staged))
	return staged
}

// TakePendingEditorTarget takes the queued editor target after action
// dispatch. The main event loop consumes this after leaving the TUI screen,
// because App does not own terminal state.
func (a *App) TakePendingEditorTarget() (editor.Target, bool) {
	if a.PendingEditorTarget == nil {
		return editor.Target{}, false
	}
	target := *a.PendingEditorTarget
	a.PendingEditorTarget = nil
	return target, true
}

// QueueEditorForFocusedItem resolves the currently focused UI item into an
// editor target queued on PendingEditorTarget for the main event loop to
// perform the terminal handoff. Invalid focus states are reported through
// the status bar instead.
func (a *App) QueueEditorForFocusedItem() {
	switch a.FocusedPanel {
	case PanelFileList:
		item, ok := a.SelectedTreeItem()
		switch {
		case !ok:
			a.SetWarning("No file selected")
		case item.IsDir:
			a.SetWarning("Select a file to open in editor")
		default:
			a.queueEditorForFileIdx(item.FileIdx, nil)
		}
	case PanelDiff:
		fileIdx := a.DiffState.CurrentFileIdx
		var line *uint32
		if a.DiffState.CursorLine < len(a.LineAnnotations) {
			ann := &a.LineAnnotations[a.DiffState.CursorLine]
			switch ann.Kind {
			case AnnExpander, AnnHiddenLines, AnnExpandedContext:
				fileIdx = ann.GapID.FileIdx
			default:
				if idx, ok := annotationFileIdx(ann); ok {
					fileIdx = idx
				}
			}
			if ann.Kind == AnnExpandedContext {
				if expanded := a.GetExpandedLine(ann.GapID, ann.LineIdx); expanded != nil {
					if expanded.NewLineno != nil {
						line = expanded.NewLineno
					} else {
						line = expanded.OldLineno
					}
				}
			} else if lineno, _, ok := a.LineAtCursor(); ok {
				line = &lineno
			}
		}
		a.queueEditorForFileIdx(fileIdx, line)
	case PanelComments, PanelCommitSelector:
		a.SetWarning("Focus a file or diff line to open in editor")
	}
}

// queueEditorForFileIdx resolves file fileIdx to an absolute worktree path
// and queues it, refusing commit-message pseudo-files and paths that do not
// exist on disk (deleted files, remote-only files).
func (a *App) queueEditorForFileIdx(fileIdx int, line *uint32) {
	if fileIdx >= len(a.DiffFiles) {
		a.SetWarning("No file selected")
		return
	}
	file := &a.DiffFiles[fileIdx]
	if file.IsCommitMessage {
		a.SetWarning("Commit message has no local file to open")
		return
	}

	displayPath := file.DisplayPath()
	// PR mode's root path is a synthetic forge identity, never a real
	// directory. M6 hook: fall back to the forge backend's local checkout.
	if !filepath.IsAbs(a.VcsInfo.RootPath) {
		a.SetWarning(fmt.Sprintf("Cannot open %s: no local checkout", displayPath))
		return
	}

	path := filepath.Join(a.VcsInfo.RootPath, displayPath)
	// Deleted files and remote-only PR files have diff rows, but no
	// worktree file the external editor can open.
	if _, err := os.Stat(path); err != nil {
		a.SetWarning(fmt.Sprintf("Cannot open %s: file does not exist", path))
		return
	}

	a.PendingEditorTarget = &editor.Target{Path: path, Line: line}
}
