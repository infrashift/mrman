// app.go ports the read-only core of tuicr's App state (src/app/mod.rs):
// the struct, the diff-source discriminator, view/panel enums, DiffState,
// HelpState, and the read-only reviewed-state helpers from
// src/app/reviewed.rs. Pure state — no rendering or theme imports; the ui
// layer renders from this.
package app

import (
	"github.com/infrashift/mrman/internal/editor"
	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs"
)

// DiffSourceKind discriminates what the app is diffing, mirroring tuicr's
// DiffSource enum variants.
type DiffSourceKind int

// Diff source kinds.
const (
	DiffSourceWorkingTree DiffSourceKind = iota
	DiffSourceStaged
	DiffSourceUnstaged
	DiffSourceStagedAndUnstaged
	DiffSourceCommitRange
	DiffSourceStagedUnstagedAndCommits
	// DiffSourcePullRequest is a stub in M3: the PR identity payload lands
	// with the forge milestone.
	DiffSourcePullRequest
	// DiffSourcePatch reviews a standalone patch artifact — a .patch file, a
	// mail message, or an mbox series — with no repository behind it.
	//
	// New kinds must be appended here, never inserted: output.ScopeKind
	// mirrors this order and callers convert with a plain int cast, so an
	// insertion silently relabels every exported review.
	DiffSourcePatch
)

// DiffSource is the runtime diff target. Commits carries the commit ids for
// DiffSourceCommitRange and DiffSourceStagedUnstagedAndCommits.
type DiffSource struct {
	Kind    DiffSourceKind
	Commits []string
}

// IncludesWorktreeChanges reports whether the active review target includes
// live worktree changes, i.e. whether reloading after an external editor
// exits can surface newly written worktree edits.
func (d DiffSource) IncludesWorktreeChanges() bool {
	switch d.Kind {
	case DiffSourceWorkingTree, DiffSourceUnstaged, DiffSourceStagedAndUnstaged,
		DiffSourceStagedUnstagedAndCommits:
		return true
	case DiffSourceStaged, DiffSourceCommitRange, DiffSourcePullRequest, DiffSourcePatch:
		return false
	}
	return false
}

// FocusedPanel identifies which panel has input focus.
type FocusedPanel int

// Focused panels.
const (
	PanelFileList FocusedPanel = iota
	PanelComments
	PanelDiff
	PanelCommitSelector
)

// DiffViewMode selects unified or side-by-side rendering.
type DiffViewMode int

// Diff view modes.
const (
	ViewUnified DiffViewMode = iota
	ViewSideBySide
)

// DiffState is the diff panel's scroll/cursor state.
type DiffState struct {
	ScrollOffset    int
	ScrollX         int
	CursorLine      int
	CurrentFileIdx  int
	ViewportHeight  int
	ViewportWidth   int
	MaxContentWidth int
	WrapLines       bool
	// VisibleLineCount is the number of logical lines that fit in the
	// viewport (set during render). When wrapping is enabled this accounts
	// for lines expanding to multiple visual rows.
	VisibleLineCount int
}

// NewDiffState returns the default diff state (wrapping on, everything else
// zero), mirroring tuicr's DiffState::default.
func NewDiffState() DiffState {
	return DiffState{WrapLines: true}
}

// EffectiveVisibleLines is the number of logical lines that fit in the
// viewport. It uses the render-computed VisibleLineCount (which accounts for
// line wrapping), falling back to ViewportHeight before the first render.
func (d *DiffState) EffectiveVisibleLines() int {
	if d.VisibleLineCount > 0 {
		return d.VisibleLineCount
	}
	return max(d.ViewportHeight, 1)
}

// EffectiveScrollMargin is the minimum number of lines kept between the
// cursor and the viewport edge (vim's scrolloff). It is strictly less than
// half the viewport to guarantee a stable free zone after centering (zz).
func (d *DiffState) EffectiveScrollMargin(scrollOffset int) int {
	return min(scrollOffset, satSub(d.EffectiveVisibleLines()/2, 1))
}

// HelpState is the help overlay's scroll and search state.
type HelpState struct {
	ScrollOffset   int
	ViewportHeight int
	TotalLines     int // set during render
	// SearchableLines is the plain-text help content the / search scans.
	SearchableLines []string
	// LastSearchPattern is the pattern reused by n/N inside help.
	LastSearchPattern *string
	// CurrentMatchLine is the line index of the active match.
	CurrentMatchLine *int
}

// App is the read-only diff-viewer state machine, ported field-for-field
// from tuicr's App for the read-only scope. Comments, sessions-watch, PR and
// submit state land in later milestones; the struct is designed for that
// extension but only the read-only surface is implemented here.
type App struct {
	VCS     vcs.Backend
	VcsInfo *vcs.Info
	Session *model.ReviewSession

	DiffFiles  []model.DiffFile
	DiffSource DiffSource

	InputMode    input.Mode
	FocusedPanel FocusedPanel
	DiffViewMode DiffViewMode

	FileListState FileListState
	DiffState     DiffState
	HelpState     HelpState
	// CommentPeek is the open read-only comment panel, nil when closed.
	CommentPeek *CommentPeekState

	CommandBuffer     string
	SearchBuffer      string
	LastSearchPattern *string
	// SearchReturnMode is the mode restored when the search shell exits.
	SearchReturnMode input.Mode

	ShouldQuit bool
	Dirty      bool
	Message    *Message

	// SupportsKeyboardEnhancement is true on terminals emitting key-release
	// events (kitty REPORT_EVENT_TYPES); it gates the two-press file walk.
	SupportsKeyboardEnhancement bool
	ShowFileList                bool
	// IsPristineMode is true when the session was opened via --all-files.
	IsPristineMode bool
	// IsSingleFileView renders only the currently focused file in the diff
	// panel instead of the continuous-scroll concatenation.
	IsSingleFileView bool
	// PrimedWalkNext is set when j tries to overflow past the last line of
	// the current file in single-file view; a deliberate second press walks
	// to the next file.
	PrimedWalkNext bool
	// PrimedWalkPrev is the symmetric inverse for the previous-file walk.
	PrimedWalkPrev bool
	// DownReleasedSinceArm is set when the Down key is released after
	// PrimedWalkNext was armed, so held-key auto-repeat never walks.
	DownReleasedSinceArm bool
	// UpReleasedSinceArm is the symmetric release gate for the prev walk.
	UpReleasedSinceArm  bool
	CursorLineHighlight bool
	// ScrollOffset is the configured scrolloff margin (config, not state).
	ScrollOffset int

	// ExpandedDirs holds directory paths currently expanded in the tree.
	ExpandedDirs map[string]bool
	// ExpandedTop stores lines expanded downward from the upper boundary of
	// each gap.
	ExpandedTop map[GapID][]model.DiffLine
	// ExpandedBottom stores lines expanded upward from the lower boundary of
	// each gap, in ascending line order.
	ExpandedBottom map[GapID][]model.DiffLine
	// FileLineCountCache caches file line counts keyed by file index.
	FileLineCountCache map[int]uint32
	// LineAnnotations describes what each rendered line represents.
	LineAnnotations []AnnotatedLine

	// PendingCount accumulates digits for {N}G jump-to-line.
	PendingCount *int
	// PathFilter scopes the diff to a file or directory.
	PathFilter *string

	// --- Comment-mode state (M4, tuicr src/app/mod.rs) ---

	// CommentBuffer is the comment text being edited; CommentCursor is a byte
	// offset into it (always on a UTF-8 boundary).
	CommentBuffer string
	CommentCursor int
	// CommentType is the type stamped on the comment being edited.
	CommentType model.CommentType
	// CommentTypes is the resolved, ordered comment-type cycle (config; the
	// typeless None entry is always present).
	CommentTypes []CommentTypeDef
	// CommentIsReviewLevel / CommentIsFileLevel say what scope the comment
	// under edit attaches to; neither set means line scope.
	CommentIsReviewLevel bool
	CommentIsFileLevel   bool
	// CommentLine anchors a line comment; CommentLineRange anchors a visual
	// range comment (its end doubles as CommentLine).
	CommentLine      *CommentAnchor
	CommentLineRange *CommentRangeAnchor
	// EditingCommentID is set when editing an existing comment in place.
	EditingCommentID *string
	// Username is stamped as the author on new comments (config).
	Username string

	// VisualSelection is the active visual-mode selection, nil outside
	// visual mode.
	VisualSelection *VisualSelection

	// --- Commit-selector state (M5, tuicr src/app/mod.rs + commits.rs) ---

	// CommitSelectionRange is the inclusive (start, end) index range selected
	// in the commit selector; nil means no selection.
	CommitSelectionRange *model.IndexRange
	// ReviewCommits is the commit list backing the inline selector,
	// newest-first (presentation order is CommitOrder's concern).
	ReviewCommits []vcs.CommitInfo
	// CommitList is the list the selector currently renders: the recent
	// history (plus synthetic staged/unstaged rows) in target-select mode,
	// or a copy of ReviewCommits for the inline selector. Newest-first.
	CommitList []vcs.CommitInfo
	// CommitListCursor / CommitListScrollOffset are the selector's cursor
	// row and scroll position.
	CommitListCursor       int
	CommitListScrollOffset int
	// CommitListViewportHeight is the selector's visible row count, set by
	// the renderer; zero before the first render disables scroll clamping.
	CommitListViewportHeight int
	// VisibleCommitCount is how many CommitList entries are shown before
	// the "show more" row.
	VisibleCommitCount int
	// CommitPageSize is the load-more page size (config; default 10).
	CommitPageSize int
	// HasMoreCommits is true while the backend may have older commits
	// beyond the loaded history.
	HasMoreCommits bool
	// CommitOrder flips the inline selector's display order. Presentation
	// only — ReviewCommits stays newest-first.
	CommitOrder CommitOrder
	// CommitSelectionStart picks which commits a fresh multi-commit review
	// opens with (config).
	CommitSelectionStart CommitSelectionStart
	// ShowCommitSelector is the inline selector pane's visibility.
	ShowCommitSelector bool
	// CommentNav is the comment navigator pane's cursor and scroll window.
	CommentNav CommentNavState
	// SavedInlineSelection preserves the inline selection while the target
	// selector is open, restored by ExitCommitSelectMode.
	SavedInlineSelection *model.IndexRange
	// TargetTab is the active tab of the review target selector.
	TargetTab TargetTab
	// CommitDiffCache caches loaded subrange diffs keyed by selection range
	// so re-selecting a subrange skips the reload (the UI layer's inline
	// reload flow populates it; ApplyLoadedSelection resets it).
	CommitDiffCache map[model.IndexRange][]model.DiffFile
	// pendingSelectedCommits stashes the rows resolved by
	// ConfirmCommitSelection (newest-first, synthetic entries included)
	// until ApplyLoadedSelection consumes them to build ReviewCommits.
	pendingSelectedCommits []vcs.CommitInfo

	// AnchorStats summarizes the last anchor validation pass, so the open and
	// reload paths can tell the reviewer what moved.
	AnchorStats AnchorStats
	// anchorVerdicts records per-comment anchor verdicts from that pass,
	// keyed by comment id. Derived state, never persisted: it describes the
	// relationship between the session and one particular diff, and writing
	// it into the session file would freeze a judgement that is only true
	// until the next edit.
	anchorVerdicts map[string]AnchorVerdict

	// remoteThreadsByLine and remoteThreadsByFile index the visible remote
	// discussions for the current annotation stream. Refreshed by
	// RebuildAnnotations and read by the height math, so row emission and
	// TotalLines always agree (see refreshRemoteThreadIndex).
	remoteThreadsByLine map[remoteThreadAnchor][]int
	remoteThreadsByFile map[string][]int

	// Pr is the PR-mode state; nil outside PR reviews.
	Pr *PrState
	// PrContext caches whole-file snapshots fetched from the forge so gap
	// expansion stays synchronous in PR mode (see prcontext.go).
	PrContext *PrContextProvider
	// PendingContextRequest is a snapshot fetch armed from inside the
	// state machine (the {N}G jump planner), drained by the UI layer.
	PendingContextRequest *PrContextRequest
	// Submit is the in-progress submit flow; nil when idle.
	Submit *SubmitState
	// CommentTypePrefix controls the [TYPE] prefix on submitted bodies.
	CommentTypePrefix bool

	// PendingEditorTarget is the queued external-editor target; the UI loop
	// consumes it via TakePendingEditorTarget after leaving the TUI screen.
	PendingEditorTarget *editor.Target
}

// satSub is usize-style saturating subtraction.
func satSub(a, b int) int {
	if a > b {
		return a - b
	}
	return 0
}

// satSubU32 is saturating subtraction for line numbers.
func satSubU32(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return 0
}

// absDiffU32 is the absolute difference of two line numbers.
func absDiffU32(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

// CurrentFile returns the currently focused diff file, or nil when the diff
// is empty.
func (a *App) CurrentFile() *model.DiffFile {
	if a.DiffState.CurrentFileIdx < len(a.DiffFiles) {
		return &a.DiffFiles[a.DiffState.CurrentFileIdx]
	}
	return nil
}

// CurrentFilePath returns the display path of the focused file.
func (a *App) CurrentFilePath() (string, bool) {
	if f := a.CurrentFile(); f != nil {
		return f.DisplayPath(), true
	}
	return "", false
}

// HunkAtCursor resolves the cursor to (fileIdx, hunkIdx) when it rests on a
// hunk header or diff line.
func (a *App) HunkAtCursor() (fileIdx, hunkIdx int, ok bool) {
	if a.DiffState.CursorLine >= len(a.LineAnnotations) {
		return 0, 0, false
	}
	ann := &a.LineAnnotations[a.DiffState.CursorLine]
	switch ann.Kind {
	case AnnHunkHeader, AnnDiffLine, AnnSideBySideLine:
		return ann.FileIdx, ann.HunkIdx, true
	default:
		return 0, 0, false
	}
}

// hunkReviewTarget resolves (path, review key) for a hunk.
func (a *App) hunkReviewTarget(fileIdx, hunkIdx int) (string, string, bool) {
	if fileIdx >= len(a.DiffFiles) {
		return "", "", false
	}
	file := &a.DiffFiles[fileIdx]
	key, ok := file.HunkReviewKey(hunkIdx)
	if !ok {
		return "", "", false
	}
	return file.DisplayPath(), key, true
}

// HunkHeaderLine returns the annotation index of the given hunk's header.
func (a *App) HunkHeaderLine(fileIdx, hunkIdx int) (int, bool) {
	for i := range a.LineAnnotations {
		ann := &a.LineAnnotations[i]
		if ann.Kind == AnnHunkHeader && ann.FileIdx == fileIdx && ann.HunkIdx == hunkIdx {
			return i, true
		}
	}
	return 0, false
}

// IsHunkReviewed reports whether the hunk is marked reviewed. It skips key
// computation (which hashes every hunk in the file) when this file has no
// reviewed hunks — the common case.
func (a *App) IsHunkReviewed(fileIdx, hunkIdx int) bool {
	if fileIdx >= len(a.DiffFiles) {
		return false
	}
	review := a.Session.File(a.DiffFiles[fileIdx].DisplayPath())
	if review == nil || len(review.ReviewedHunks) == 0 {
		return false
	}
	path, key, ok := a.hunkReviewTarget(fileIdx, hunkIdx)
	if !ok {
		return false
	}
	return a.Session.IsHunkReviewed(path, key)
}

// ShouldRenderGapBeforeHunk reports whether the hidden-context controls
// before a hunk render. Reviewed hunks collapse as a complete review unit:
// their body and adjoining hidden-context controls disappear with the header.
func (a *App) ShouldRenderGapBeforeHunk(fileIdx, hunkIdx int) bool {
	return !a.IsHunkReviewed(fileIdx, hunkIdx) &&
		(hunkIdx == 0 || !a.IsHunkReviewed(fileIdx, hunkIdx-1))
}

// FileCount is the number of files in the diff.
func (a *App) FileCount() int {
	return len(a.DiffFiles)
}

// ReviewedCount is the number of files marked reviewed.
func (a *App) ReviewedCount() int {
	return a.Session.ReviewedCount()
}

// DiffStat returns (total files, total additions, total deletions) across
// all diff files.
func (a *App) DiffStat() (files, additions, deletions int) {
	for i := range a.DiffFiles {
		adds, dels := a.DiffFiles[i].Stat()
		additions += adds
		deletions += dels
	}
	return len(a.DiffFiles), additions, deletions
}

// IsCursorInOverview reports whether the cursor is in the review comments
// area above all files.
func (a *App) IsCursorInOverview() bool {
	return a.DiffState.CursorLine < a.reviewCommentsRenderHeight()
}
