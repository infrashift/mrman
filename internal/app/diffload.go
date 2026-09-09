// diffload.go ports the diff-load plumbing of tuicr's src/app/diff_load.rs
// that belongs to the app state machine: the synthetic STAGED/UNSTAGED
// selector rows, the change-status probe with its unsupported-backend
// fallback, the synthetic commit-message pseudo-file, and the shared reset
// sequence (ApplyLoadedSelection) every load_*_selection path runs after
// the UI layer has produced the new diff and session.

package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs"
)

// stagedCommitEntry is the synthetic "Staged changes" selector row.
func stagedCommitEntry() vcs.CommitInfo {
	return vcs.CommitInfo{
		ID:      StagedSelectionID,
		ShortID: "STAGED",
		Summary: "Staged changes",
		Time:    now(),
	}
}

// unstagedCommitEntry is the synthetic "Unstaged changes" selector row.
func unstagedCommitEntry() vcs.CommitInfo {
	return vcs.CommitInfo{
		ID:      UnstagedSelectionID,
		ShortID: "UNSTAGED",
		Summary: "Unstaged changes",
		Time:    now(),
	}
}

// isStagedCommit reports whether the row is the synthetic staged entry.
func isStagedCommit(c *vcs.CommitInfo) bool { return c.ID == StagedSelectionID }

// isUnstagedCommit reports whether the row is the synthetic unstaged entry.
func isUnstagedCommit(c *vcs.CommitInfo) bool { return c.ID == UnstagedSelectionID }

// resolveChangeStatus resolves the staged/unstaged status the commit
// selector renders. Backends without a cheap status probe (ErrUnsupported)
// are assumed to have both, then each side is verified by actually diffing
// — tuicr's get_change_status_with_ignore fallback.
func (a *App) resolveChangeStatus() (vcs.ChangeStatus, error) {
	status, err := a.VCS.ChangeStatus()
	if err == nil {
		return status, nil
	}
	if !errors.Is(err, errs.ErrUnsupported) {
		return vcs.ChangeStatus{}, err
	}
	staged, err := a.sideHasChanges(true)
	if err != nil {
		return vcs.ChangeStatus{}, err
	}
	unstaged, err := a.sideHasChanges(false)
	if err != nil {
		return vcs.ChangeStatus{}, err
	}
	return vcs.ChangeStatus{Staged: staged, Unstaged: unstaged}, nil
}

// sideHasChanges verifies one side of the working tree by diffing it (no
// highlighter — only existence matters). Unstaged falls back to the whole
// working tree for backends without a separate unstaged diff, mirroring
// tuicr's get_unstaged_diff_with_ignore. ErrNoChanges and ErrUnsupported
// both mean "nothing reviewable on this side".
func (a *App) sideHasChanges(staged bool) (bool, error) {
	var files []model.DiffFile
	var err error
	if staged {
		files, err = a.VCS.StagedDiff(nil)
	} else {
		files, err = a.VCS.UnstagedDiff(nil)
		if err != nil && errors.Is(err, errs.ErrUnsupported) {
			files, err = a.VCS.WorkingTreeDiff(nil)
		}
	}
	if err != nil {
		if errors.Is(err, errs.ErrNoChanges) || errors.Is(err, errs.ErrUnsupported) {
			return false, nil
		}
		return false, err
	}
	return len(files) > 0, nil
}

// InsertCommitMessageIfSingle returns files with a synthetic "Commit
// Message (<short>)" pseudo-file prepended when commits holds exactly one
// real commit; any previous commit-message pseudo-file is stripped first.
//
// The synthetic path embeds the commit's short id so that comments on
// different commits' messages get distinct session keys (the session
// indexes comments by path) and the exported review records which commit
// each commit-message comment belongs to.
func InsertCommitMessageIfSingle(files []model.DiffFile, commits []vcs.CommitInfo) []model.DiffFile {
	kept := make([]model.DiffFile, 0, len(files)+1)
	for i := range files {
		if !files[i].IsCommitMessage {
			kept = append(kept, files[i])
		}
	}

	if len(commits) != 1 || isSpecialCommit(&commits[0]) {
		return kept
	}
	commit := &commits[0]

	fullMessage := commit.Summary
	if commit.Body != nil {
		fullMessage += "\n\n" + *commit.Body
	}

	// Rust's str::lines drops a trailing newline's empty tail.
	var diffLines []model.DiffLine
	for i, line := range strings.Split(strings.TrimSuffix(fullMessage, "\n"), "\n") {
		lineno := uint32(i) + 1
		diffLines = append(diffLines, model.DiffLine{
			Origin:    model.OriginContext,
			Content:   line,
			NewLineno: &lineno,
		})
	}
	lineCount := uint32(len(diffLines)) //nolint:gosec // G115: line numbers fit uint32
	hunks := []model.DiffHunk{{
		Lines:    diffLines,
		OldStart: 0,
		OldCount: 0,
		NewStart: 1,
		NewCount: lineCount,
	}}
	// The file tree splits a display path on "/" to build its directories, so
	// the pseudo-file's name must not contain one. A commit's ShortID is a
	// SHA and never does; a patch's is its series position ("3/3"), which
	// would otherwise render as a directory "Commit Message (3" holding a
	// file "3)".
	path := fmt.Sprintf("Commit Message (%s)", strings.ReplaceAll(commit.ShortID, "/", "-"))
	commitMsgFile := model.DiffFile{
		SourceIndex:     -1,
		NewPath:         &path,
		Status:          model.StatusAdded,
		Hunks:           hunks,
		IsCommitMessage: true,
		ContentHash:     model.ComputeContentHash(hunks),
	}
	return append([]model.DiffFile{commitMsgFile}, kept...)
}

// singleSelectedCommit resolves the one commit the commit-message
// pseudo-file describes: the single selected commit, or the only review
// commit when there is no selection. Nil for multi-commit selections.
func (a *App) singleSelectedCommit() *vcs.CommitInfo {
	if a.CommitSelectionRange != nil {
		start, end := a.CommitSelectionRange[0], a.CommitSelectionRange[1]
		if start == end && start >= 0 && start < len(a.ReviewCommits) {
			return &a.ReviewCommits[start]
		}
		return nil
	}
	if len(a.ReviewCommits) == 1 {
		return &a.ReviewCommits[0]
	}
	return nil
}

// insertCommitMessageIfSingle applies InsertCommitMessageIfSingle to the
// live diff, registering the pseudo-file in the session like any diff file.
func (a *App) insertCommitMessageIfSingle() {
	var commits []vcs.CommitInfo
	if c := a.singleSelectedCommit(); c != nil {
		commits = []vcs.CommitInfo{*c}
	}
	a.DiffFiles = InsertCommitMessageIfSingle(a.DiffFiles, commits)
	if len(a.DiffFiles) > 0 && a.DiffFiles[0].IsCommitMessage {
		a.Session.AddDiffFile(&a.DiffFiles[0])
	}
}

// ApplyLoadedSelection installs a freshly loaded review target: it swaps
// the diff, session, and source, then runs the reset sequence every tuicr
// load_*_selection path shares — fresh navigation state, cleared expanded
// gaps, re-sort, re-expand, rebuilt annotations, and no cursor restoration.
//
// Commit-bearing sources additionally set up the inline commit selector
// from the rows stashed by ConfirmCommitSelection (tuicr's
// confirm_commit_selection_inner / load_staged_unstaged_and_commits
// epilogue), including the CommitSelectionStart-configured initial range.
// When that range is a strict subrange (IsStrictCommitSelection) the caller
// must narrow the diff via the inline-selection reload, exactly like tuicr.
func (a *App) ApplyLoadedSelection(files []model.DiffFile, session *model.ReviewSession, source DiffSource) {
	RegisterDiffFiles(session, files)
	a.Session = session
	a.DiffFiles = files
	a.DiffSource = source
	a.InputMode = input.ModeNormal

	// Reset navigation state.
	wrap := a.DiffState.WrapLines
	a.DiffState = NewDiffState()
	a.DiffState.WrapLines = wrap
	a.FileListState = FileListState{}
	a.ClearExpandedGaps()

	// A selection load can narrow the diff to a subrange of commits, which
	// hides hunks the comments were written against. Anchor verdicts are not
	// recomputed here and the old ones are dropped: a comment must never read
	// "outdated" because the reviewer narrowed the view past it. The next
	// full-target open or reload re-establishes them.
	a.ClearAnchorVerdicts()

	// Set up the inline commit selector for commit-bearing sources
	// (newest-first display order); staged/unstaged-only loads leave the
	// selector state untouched, mirroring tuicr's load_*_selection.
	switch source.Kind {
	case DiffSourceCommitRange, DiffSourceStagedUnstagedAndCommits:
		selected := a.pendingSelectedCommits
		a.pendingSelectedCommits = nil
		a.InstallReviewCommits(selected)
	default:
		a.pendingSelectedCommits = nil
	}

	a.SortFilesByDirectory(true)
	a.ExpandAllDirs()
	a.populateFileLineCountCache()
	a.RebuildAnnotations()
}
