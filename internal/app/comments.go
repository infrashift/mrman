// comments.go ports tuicr's src/app/comments.rs (comment navigator,
// next/prev comment, find/delete at cursor, comment-mode transitions,
// save_comment, comment-type cycling) plus the commit-scoped visibility
// helpers from src/app/commits.rs. The commit selector itself lives in
// commits.go/commitselect.go (M5); the visibility predicates here run
// against the shared CommitSelectionRange / ReviewCommits state.
package app

import (
	"fmt"
	"slices"
	"strings"

	render "github.com/infrashift/mrman/charmkit/cellrender"
	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/reviewcli"
	"github.com/infrashift/mrman/internal/vcs"
)

// CommentTypeDef is one resolved comment-type definition from config: the
// id stamped on comments, the human label for badges, and the optional
// definition text and raw color string (parsed by the UI layer).
type CommentTypeDef struct {
	ID         string
	Label      string
	Definition *string
	Color      *string
}

// CommentAnchor anchors a line comment: line number plus diff side.
type CommentAnchor struct {
	Line uint32
	Side model.LineSide
}

// CommentRangeAnchor anchors a visual-range comment.
type CommentRangeAnchor struct {
	Range model.LineRange
	Side  model.LineSide
}

// noneCommentTypeDef is the typeless default entry, always cyclable.
func noneCommentTypeDef() CommentTypeDef {
	return CommentTypeDef{ID: model.CommentTypeNoneID}
}

// BuiltinCommentTypes is the comment-type cycle used when the config
// declares none: NOTE, ISSUE, SUGGESTION, PRAISE, then the typeless entry.
//
// This is a deliberate divergence from tuicr, which ships only the typeless
// type so an unconfigured review cannot classify anything and Tab has nothing
// to cycle — the feature was invisible until you found it in the docs and
// wrote a config file.
//
// NOTE leads, so it is the default a new comment starts on: an unclassified
// remark should read as a remark, not as a blocker. ISSUE as the default would
// stamp [ISSUE] on every submitted comment from anyone who never configured
// types.
//
// No Color is set. internal/ui maps these four ids onto the theme's
// comment_note / comment_issue / comment_suggestion / comment_praise slots, so
// they follow the active theme instead of pinning literal colors that would
// fight it.
func BuiltinCommentTypes() []CommentTypeDef {
	def := func(s string) *string { return &s }
	return []CommentTypeDef{
		{ID: "note", Label: "NOTE", Definition: def("worth knowing; answer or acknowledge")},
		{ID: "issue", Label: "ISSUE", Definition: def("must fix before merge")},
		{ID: "suggestion", Label: "SUGGESTION", Definition: def("optional improvement; implement it or say why not")},
		{ID: "praise", Label: "PRAISE", Definition: def("positive feedback; nothing to do")},
	}
}

// ResolveCommentTypes resolves the effective, ordered comment-type list.
// With no configured types the cycle is BuiltinCommentTypes plus None.
// Configuring types replaces the built-ins entirely (the first entry becomes
// the default), but None stays available: it is appended so it can still be
// cycled to, unless the user declared a "none" entry themselves.
func ResolveCommentTypes(configs []CommentTypeDef) []CommentTypeDef {
	if len(configs) == 0 {
		configs = BuiltinCommentTypes()
	}
	resolved := make([]CommentTypeDef, 0, len(configs)+1)
	for _, config := range configs {
		if config.Label == "" {
			config.Label = config.ID
		}
		resolved = append(resolved, config)
	}
	hasNone := slices.ContainsFunc(resolved, func(d CommentTypeDef) bool {
		return d.ID == model.CommentTypeNoneID
	})
	if !hasNone {
		resolved = append(resolved, noneCommentTypeDef())
	}
	return resolved
}

// CanCycleCommentTypes reports whether the resolved cycle has more than one
// entry, i.e. whether Tab does anything in the comment box. The single-entry
// case is reachable through configuration — declaring only a "none" entry —
// and the comment box uses this to decide whether to advertise Tab at all.
func (a *App) CanCycleCommentTypes() bool {
	return len(a.CommentTypes) > 1
}

// DefaultCommentType is the first entry of the resolved comment-type cycle.
func (a *App) DefaultCommentType() model.CommentType {
	if len(a.CommentTypes) > 0 {
		return model.CommentTypeFromID(a.CommentTypes[0].ID)
	}
	return model.CommentTypeNoneID
}

// CommentTypeLabel is the human-facing label for a comment type, e.g.
// SUGGESTION. Returns an empty string for the typeless None so callers
// render no badge.
func (a *App) CommentTypeLabel(commentType model.CommentType) string {
	if commentType.IsNone() {
		return ""
	}
	for i := range a.CommentTypes {
		if a.CommentTypes[i].ID == commentType.ID() {
			return strings.ToUpper(a.CommentTypes[i].Label)
		}
	}
	return commentType.ID()
}

// WrapSegments splits text into segments whose display width each fits
// within contentArea. Returns a single-element slice when the text already
// fits (including the empty string).
func WrapSegments(text string, contentArea int) []string {
	if contentArea == 0 || render.StringWidth(text) <= contentArea {
		return []string{text}
	}
	var segments []string
	remaining := text
	for len(remaining) > 0 {
		takeBytes := 0
		takenWidth := 0
		for _, c := range remaining {
			cw := render.StringWidth(string(c))
			if takenWidth+cw > contentArea {
				break
			}
			takenWidth += cw
			takeBytes += len(string(c))
		}
		// A single character wider than contentArea — emit it anyway so we
		// don't loop forever.
		if takeBytes == 0 {
			for _, c := range remaining {
				takeBytes = len(string(c))
				break
			}
		}
		segments = append(segments, remaining[:takeBytes])
		remaining = remaining[takeBytes:]
	}
	return segments
}

// CommentDisplayLines is the number of display lines a comment renders
// (top border + wrapped content segments + bottom border). Uses
// viewportWidth to account for pre-wrapped visual segments so the
// annotation count stays in sync with what the comment box renders.
func CommentDisplayLines(comment *model.Comment, viewportWidth int) int {
	// Mirrors the comment box content-area calculation:
	// indicator(1) + border prefix(7) + safety margin(2) = 10.
	contentArea := satSub(viewportWidth, 10)
	visualLines := 0
	for line := range strings.SplitSeq(comment.Content, "\n") {
		visualLines += len(WrapSegments(line, contentArea))
	}
	return 2 + visualLines
}

// --- Commit-scoped comment visibility (selector itself lands in M5) ---

// StagedSelectionID and UnstagedSelectionID identify the synthetic
// staged/unstaged rows of the inline commit selector (M5); they never scope
// comments.
const (
	StagedSelectionID   = "__mrman_staged__"
	UnstagedSelectionID = "__mrman_unstaged__"
)

// isSpecialCommit reports whether the commit row is a synthetic
// staged/unstaged selector entry rather than a real commit.
func isSpecialCommit(c *vcs.CommitInfo) bool {
	return c.ID == StagedSelectionID || c.ID == UnstagedSelectionID
}

// selectedCommitSet is the set of commit SHAs currently selected in the
// inline commit selector. hasSet is false when there is no selector (every
// comment visible). When the full range is selected the set contains every
// commit SHA; when a strict subset is selected only comments whose CommitID
// is in the set (or nil) are visible.
func (a *App) selectedCommitSet() (set map[string]bool, hasSet bool) {
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
		c := &a.ReviewCommits[i]
		if isSpecialCommit(c) {
			continue
		}
		set[c.ID] = true
	}
	return set, true
}

// commentVisibleWith is the pure visibility check against a precomputed
// commit set. hasSet == false means "no selector", so every comment is
// visible. This is the shared predicate all filtering sites converge on so
// height math and rendering never drift.
func commentVisibleWith(c *model.Comment, set map[string]bool, hasSet bool) bool {
	if c.CommitID == nil || !hasSet {
		return true
	}
	return set[*c.CommitID]
}

// CommentVisible reports whether a comment is visible under the current
// commit selection. Allocates the commit set on every call; hot paths
// should compute selectedCommitSet once and use commentVisibleWith.
func (a *App) CommentVisible(c *model.Comment) bool {
	set, hasSet := a.selectedCommitSet()
	return commentVisibleWith(c, set, hasSet)
}

// --- Comment navigator ---

// CommentNavigatorScope discriminates CommentNavigatorKey.
type CommentNavigatorScope int

// Comment navigator scopes.
const (
	NavScopeReview CommentNavigatorScope = iota
	NavScopeFile
	NavScopeLine
	// NavScopeRemoteThread and NavScopeRemoteSummary are the forge's own
	// discussions. They are navigable so m/M walk the whole conversation,
	// but never editable — see CommentNavigatorItem.IsRemote.
	NavScopeRemoteThread
	NavScopeRemoteSummary
)

// CommentNavigatorKey identifies one navigable comment; comparable so
// consecutive display rows of the same comment collapse to one item.
type CommentNavigatorKey struct {
	Scope      CommentNavigatorScope
	FileIdx    int
	Line       uint32
	Side       model.LineSide
	CommentIdx int
	// RemoteIdx indexes VisibleRemoteThreads or VisibleRemoteSummaries for
	// the two remote scopes.
	RemoteIdx int
}

// NoTargetAnnotation marks a navigator item whose comment is not currently
// rendered, because its file or hunk is collapsed. There is no annotation row
// to jump to, so Enter opens the peek panel instead.
const NoTargetAnnotation = -1

// CommentNavigatorItem is one row of the comment navigator panel.
type CommentNavigatorItem struct {
	Key CommentNavigatorKey
	// CommentType is the local comment's type; zero for remote items,
	// which carry IsRemote instead.
	CommentType model.CommentType
	// IsRemote marks a forge-owned discussion: navigable and rendered, but
	// never editable or deletable.
	IsRemote bool
	// TargetAnnotation is the first annotation row of the comment.
	TargetAnnotation int
	Path             *string
	Line             *uint32
	Side             *model.LineSide
	Author           string
}

// commentNavigatorKeyFor maps an annotation to its navigator key.
func commentNavigatorKeyFor(ann *AnnotatedLine) (CommentNavigatorKey, bool) {
	switch ann.Kind {
	case AnnReviewComment:
		return CommentNavigatorKey{Scope: NavScopeReview, CommentIdx: ann.CommentIdx}, true
	case AnnFileComment:
		return CommentNavigatorKey{Scope: NavScopeFile, FileIdx: ann.FileIdx, CommentIdx: ann.CommentIdx}, true
	case AnnLineComment:
		return CommentNavigatorKey{
			Scope: NavScopeLine, FileIdx: ann.FileIdx,
			Line: ann.Line, Side: ann.Side, CommentIdx: ann.CommentIdx,
		}, true
	case AnnRemoteThreadLine:
		return CommentNavigatorKey{
			Scope: NavScopeRemoteThread, FileIdx: ann.FileIdx, RemoteIdx: ann.ThreadIdx,
		}, true
	case AnnRemoteReviewSummaryLine:
		return CommentNavigatorKey{Scope: NavScopeRemoteSummary, RemoteIdx: ann.SummaryIdx}, true
	default:
		return CommentNavigatorKey{}, false
	}
}

// commentNavigatorItemForKey resolves a key to a full navigator item.
func (a *App) commentNavigatorItemForKey(key CommentNavigatorKey, targetAnnotation int) (CommentNavigatorItem, bool) {
	switch key.Scope {
	case NavScopeReview:
		if key.CommentIdx >= len(a.Session.ReviewComments) {
			return CommentNavigatorItem{}, false
		}
		comment := a.Session.ReviewComments[key.CommentIdx]
		return CommentNavigatorItem{
			Key: key, CommentType: comment.CommentType,
			TargetAnnotation: targetAnnotation, Author: comment.Author,
		}, true
	case NavScopeFile:
		if key.FileIdx >= len(a.DiffFiles) {
			return CommentNavigatorItem{}, false
		}
		path := a.DiffFiles[key.FileIdx].DisplayPath()
		review := a.Session.File(path)
		if review == nil || key.CommentIdx >= len(review.FileComments) {
			return CommentNavigatorItem{}, false
		}
		comment := review.FileComments[key.CommentIdx]
		return CommentNavigatorItem{
			Key: key, CommentType: comment.CommentType,
			TargetAnnotation: targetAnnotation, Path: &path, Author: comment.Author,
		}, true
	case NavScopeLine:
		if key.FileIdx >= len(a.DiffFiles) {
			return CommentNavigatorItem{}, false
		}
		path := a.DiffFiles[key.FileIdx].DisplayPath()
		review := a.Session.File(path)
		if review == nil {
			return CommentNavigatorItem{}, false
		}
		comments := review.LineComments[key.Line]
		if key.CommentIdx >= len(comments) {
			return CommentNavigatorItem{}, false
		}
		comment := comments[key.CommentIdx]
		line, side := key.Line, key.Side
		return CommentNavigatorItem{
			Key: key, CommentType: comment.CommentType,
			TargetAnnotation: targetAnnotation, Path: &path,
			Line: &line, Side: &side, Author: comment.Author,
		}, true
	case NavScopeRemoteThread:
		threads := a.VisibleRemoteThreads()
		if key.RemoteIdx >= len(threads) {
			return CommentNavigatorItem{}, false
		}
		thread := &threads[key.RemoteIdx]
		item := CommentNavigatorItem{
			Key: key, IsRemote: true, TargetAnnotation: targetAnnotation,
		}
		if root := thread.Root(); root != nil {
			item.Author = root.Author
		}
		if thread.Path != "" {
			path := thread.Path
			item.Path = &path
		}
		if thread.Line != nil {
			line, side := *thread.Line, remoteSideToModel(thread.Side)
			item.Line, item.Side = &line, &side
		}
		return item, true
	case NavScopeRemoteSummary:
		summaries := a.VisibleRemoteSummaries()
		if key.RemoteIdx >= len(summaries) {
			return CommentNavigatorItem{}, false
		}
		return CommentNavigatorItem{
			Key: key, IsRemote: true, TargetAnnotation: targetAnnotation,
			Author: summaries[key.RemoteIdx].Author,
		}, true
	default:
		return CommentNavigatorItem{}, false
	}
}

// BuildCommentNavigatorItems walks the annotation stream and returns one
// item per rendered comment, in display order — local drafts and the
// forge's own read-only discussions alike.
func (a *App) BuildCommentNavigatorItems() []CommentNavigatorItem {
	var items []CommentNavigatorItem
	var lastKey *CommentNavigatorKey
	commitSet, hasCommitSet := a.selectedCommitSet()

	// Marking a file (or hunk) reviewed collapses it, and the annotation
	// stream then holds nothing for its comments — which used to empty them
	// out of the navigator too, losing the list exactly when a reviewer had
	// finished with a file and wanted to keep their notes on it.
	//
	// The collapse happens after the header row is emitted, so splicing the
	// skipped comments in at that header puts them back in the position the
	// annotations would have given them. Their TargetAnnotation is
	// NoTargetAnnotation: there is no row to jump to while the file is folded.
	appendCollapsed := func(keys []CommentNavigatorKey) {
		for _, key := range keys {
			if item, ok := a.commentNavigatorItemForKey(key, NoTargetAnnotation); ok {
				items = append(items, item)
			}
		}
		lastKey = nil
	}

	for idx := range a.LineAnnotations {
		ann := &a.LineAnnotations[idx]
		switch ann.Kind {
		case AnnFileHeader:
			if a.isFileCollapsed(ann.FileIdx) {
				appendCollapsed(a.collapsedFileNavigatorKeys(ann.FileIdx, commitSet, hasCommitSet))
			}
			lastKey = nil
			continue
		case AnnHunkHeader:
			if a.IsHunkReviewed(ann.FileIdx, ann.HunkIdx) {
				appendCollapsed(a.collapsedHunkNavigatorKeys(ann.FileIdx, ann.HunkIdx, commitSet, hasCommitSet))
			}
			lastKey = nil
			continue
		}

		key, ok := commentNavigatorKeyFor(ann)
		if !ok {
			lastKey = nil
			continue
		}
		if lastKey != nil && *lastKey == key {
			continue
		}
		if item, ok := a.commentNavigatorItemForKey(key, idx); ok {
			items = append(items, item)
			k := key
			lastKey = &k
		}
	}
	return items
}

// isFileCollapsed reports whether the annotation builder skipped a file's
// content because it is marked reviewed. Single-file view ignores the
// reviewed-collapse, so nothing is skipped there.
func (a *App) isFileCollapsed(fileIdx int) bool {
	if a.IsSingleFileView || fileIdx < 0 || fileIdx >= len(a.DiffFiles) {
		return false
	}
	return a.Session.IsFileReviewed(a.DiffFiles[fileIdx].DisplayPath())
}

// collapsedFileNavigatorKeys returns every navigator key the annotation
// builder would have emitted for a file, in the same order: the forge's
// file-anchored threads, then file comments, then each hunk's lines.
func (a *App) collapsedFileNavigatorKeys(fileIdx int, commitSet map[string]bool, hasCommitSet bool) []CommentNavigatorKey {
	if fileIdx < 0 || fileIdx >= len(a.DiffFiles) {
		return nil
	}
	file := &a.DiffFiles[fileIdx]
	path := file.DisplayPath()
	var keys []CommentNavigatorKey

	for _, threadIdx := range a.remoteThreadsByFile[path] {
		keys = append(keys, CommentNavigatorKey{
			Scope: NavScopeRemoteThread, FileIdx: fileIdx, RemoteIdx: threadIdx,
		})
	}
	if review := a.Session.File(path); review != nil {
		for commentIdx, comment := range review.FileComments {
			if !commentVisibleWith(comment, commitSet, hasCommitSet) {
				continue
			}
			keys = append(keys, CommentNavigatorKey{
				Scope: NavScopeFile, FileIdx: fileIdx, CommentIdx: commentIdx,
			})
		}
	}
	for hunkIdx := range file.Hunks {
		keys = append(keys, a.collapsedHunkNavigatorKeys(fileIdx, hunkIdx, commitSet, hasCommitSet)...)
	}
	return keys
}

// collapsedHunkNavigatorKeys returns the navigator keys for one hunk's lines,
// mirroring pushLineComments: for each diff line the forge's threads come
// before the local drafts, and the old side before the new.
func (a *App) collapsedHunkNavigatorKeys(fileIdx, hunkIdx int,
	commitSet map[string]bool, hasCommitSet bool) []CommentNavigatorKey {
	if fileIdx < 0 || fileIdx >= len(a.DiffFiles) {
		return nil
	}
	file := &a.DiffFiles[fileIdx]
	if hunkIdx < 0 || hunkIdx >= len(file.Hunks) {
		return nil
	}
	path := file.DisplayPath()
	var lineComments map[uint32][]*model.Comment
	if review := a.Session.File(path); review != nil {
		lineComments = review.LineComments
	}

	var keys []CommentNavigatorKey
	for _, line := range file.Hunks[hunkIdx].Lines {
		for _, sided := range []struct {
			lineNo *uint32
			side   model.LineSide
		}{
			{line.OldLineno, model.LineSideOld},
			{line.NewLineno, model.LineSideNew},
		} {
			if sided.lineNo == nil {
				continue
			}
			for _, threadIdx := range a.remoteThreadsByLine[remoteThreadAnchor{
				Path: path, Side: sided.side, Line: *sided.lineNo,
			}] {
				keys = append(keys, CommentNavigatorKey{
					Scope: NavScopeRemoteThread, FileIdx: fileIdx, RemoteIdx: threadIdx,
				})
			}
			for commentIdx, comment := range lineComments[*sided.lineNo] {
				matchesSide := (comment.Side != nil && *comment.Side == sided.side) ||
					(sided.side == model.LineSideNew && comment.Side == nil)
				if !matchesSide || !commentVisibleWith(comment, commitSet, hasCommitSet) {
					continue
				}
				keys = append(keys, CommentNavigatorKey{
					Scope: NavScopeLine, FileIdx: fileIdx,
					Line: *sided.lineNo, Side: sided.side, CommentIdx: commentIdx,
				})
			}
		}
	}
	return keys
}

// NavigableCommentItems returns the navigator items that have a diff row to
// jump to — everything except comments inside a collapsed file or hunk. The
// navigator panel lists all items; m / M move the diff cursor, so they can
// only visit rows that actually exist.
func (a *App) NavigableCommentItems() []CommentNavigatorItem {
	items := a.BuildCommentNavigatorItems()
	navigable := make([]CommentNavigatorItem, 0, len(items))
	for _, item := range items {
		if item.TargetAnnotation != NoTargetAnnotation {
			navigable = append(navigable, item)
		}
	}
	return navigable
}

// HasCommentNavigatorItems reports whether any navigable comment exists.
func (a *App) HasCommentNavigatorItems() bool {
	return len(a.BuildCommentNavigatorItems()) > 0
}

// jumpToCommentItem moves the cursor to a navigator item's first annotation
// row, syncing the focused file and centering the viewport.
func (a *App) jumpToCommentItem(item *CommentNavigatorItem) {
	fileIdx := -1
	if item.Path != nil {
		for i := range a.DiffFiles {
			if a.DiffFiles[i].DisplayPath() == *item.Path {
				fileIdx = i
				break
			}
		}
	}
	if item.TargetAnnotation == NoTargetAnnotation {
		// Nothing rendered to move to: the caller should have opened the peek
		// panel instead of jumping.
		return
	}
	a.MoveCursorToAnnotation(item.TargetAnnotation)
	if fileIdx >= 0 {
		fileChanged := a.DiffState.CurrentFileIdx != fileIdx
		a.DiffState.CurrentFileIdx = fileIdx
		if a.IsSingleFileView && fileChanged {
			a.RebuildAnnotations()
		}
	}
	a.CenterCursor()
	a.FocusedPanel = PanelDiff
}

// NextComment jumps to the first comment after the cursor, wrapping to the
// first comment overall.
func (a *App) NextComment() {
	items := a.NavigableCommentItems()
	if len(items) == 0 {
		a.SetMessage("No comments")
		return
	}

	cursor := min(a.DiffState.CursorLine, satSub(len(a.LineAnnotations), 1))
	targetIdx := 0
	for i := range items {
		if items[i].TargetAnnotation > cursor {
			targetIdx = i
			break
		}
	}
	a.jumpToCommentItem(&items[targetIdx])
	a.SetMessage(fmt.Sprintf("Comment %d/%d", targetIdx+1, len(items)))
}

// PrevComment jumps to the last comment before the cursor (skipping the
// comment the cursor is on), wrapping to the last comment overall.
func (a *App) PrevComment() {
	items := a.NavigableCommentItems()
	if len(items) == 0 {
		a.SetMessage("No comments")
		return
	}

	cursor := min(a.DiffState.CursorLine, satSub(len(a.LineAnnotations), 1))
	var currentKey *CommentNavigatorKey
	if cursor < len(a.LineAnnotations) {
		if key, ok := commentNavigatorKeyFor(&a.LineAnnotations[cursor]); ok {
			currentKey = &key
		}
	}
	targetIdx := len(items) - 1
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].TargetAnnotation < cursor && (currentKey == nil || items[i].Key != *currentKey) {
			targetIdx = i
			break
		}
	}
	a.jumpToCommentItem(&items[targetIdx])
	a.SetMessage(fmt.Sprintf("Comment %d/%d", targetIdx+1, len(items)))
}

// --- Comment at cursor: find / delete / edit ---

// CommentLocationKind discriminates CommentLocation.
type CommentLocationKind int

// Comment location kinds.
const (
	CommentAtReview CommentLocationKind = iota
	CommentAtFile
	CommentAtLine
)

// CommentLocation addresses one stored comment. Path/Line/Side apply per
// Kind; Index is the absolute index into the backing slice.
type CommentLocation struct {
	Kind  CommentLocationKind
	Path  string
	Line  uint32
	Side  model.LineSide
	Index int
}

// FindCommentAtCursor resolves the cursor to a stored comment location.
// Comments hidden by the current commit selection are treated as absent to
// guard against stale annotations from an async selection reload.
func (a *App) FindCommentAtCursor() (CommentLocation, bool) {
	target := a.DiffState.CursorLine
	if target >= len(a.LineAnnotations) {
		return CommentLocation{}, false
	}
	commitSet, hasSet := a.selectedCommitSet()
	ann := &a.LineAnnotations[target]
	switch ann.Kind {
	case AnnReviewComment:
		return CommentLocation{Kind: CommentAtReview, Index: ann.CommentIdx}, true
	case AnnFileComment:
		if ann.FileIdx >= len(a.DiffFiles) {
			return CommentLocation{}, false
		}
		path := a.DiffFiles[ann.FileIdx].DisplayPath()
		review := a.Session.File(path)
		if review == nil || ann.CommentIdx >= len(review.FileComments) ||
			!commentVisibleWith(review.FileComments[ann.CommentIdx], commitSet, hasSet) {
			return CommentLocation{}, false
		}
		return CommentLocation{Kind: CommentAtFile, Path: path, Index: ann.CommentIdx}, true
	case AnnLineComment:
		if ann.FileIdx >= len(a.DiffFiles) {
			return CommentLocation{}, false
		}
		path := a.DiffFiles[ann.FileIdx].DisplayPath()
		review := a.Session.File(path)
		if review == nil {
			return CommentLocation{}, false
		}
		comments := review.LineComments[ann.Line]
		if ann.CommentIdx >= len(comments) ||
			!commentVisibleWith(comments[ann.CommentIdx], commitSet, hasSet) {
			return CommentLocation{}, false
		}
		return CommentLocation{
			Kind: CommentAtLine, Path: path, Line: ann.Line,
			Side: ann.Side, Index: ann.CommentIdx,
		}, true
	default:
		return CommentLocation{}, false
	}
}

// commentSide is a comment's effective side (nil means New).
func commentSide(c *model.Comment) model.LineSide {
	if c.Side != nil {
		return *c.Side
	}
	return model.LineSideNew
}

// commentAtLocation resolves a location back to the stored comment,
// verifying the side for line comments.
func (a *App) commentAtLocation(loc CommentLocation) *model.Comment {
	switch loc.Kind {
	case CommentAtReview:
		if loc.Index < len(a.Session.ReviewComments) {
			return a.Session.ReviewComments[loc.Index]
		}
	case CommentAtFile:
		if review := a.Session.File(loc.Path); review != nil && loc.Index < len(review.FileComments) {
			return review.FileComments[loc.Index]
		}
	case CommentAtLine:
		if review := a.Session.File(loc.Path); review != nil {
			comments := review.LineComments[loc.Line]
			if loc.Index < len(comments) && commentSide(comments[loc.Index]) == loc.Side {
				return comments[loc.Index]
			}
		}
	}
	return nil
}

// CursorOnLockedComment reports whether the cursor sits on a local comment
// already pushed/submitted to the forge. Such comments are locked from
// edit/delete so local state cannot drift from the remote.
func (a *App) CursorOnLockedComment() bool {
	loc, ok := a.FindCommentAtCursor()
	if !ok {
		return false
	}
	c := a.commentAtLocation(loc)
	return c != nil && c.IsLocked()
}

// DeleteCommentAtCursor deletes the comment under the cursor. Locked
// comments and empty cursors set an explanatory message and return false.
func (a *App) DeleteCommentAtCursor() bool {
	loc, ok := a.FindCommentAtCursor()
	if !ok {
		if a.CursorOnRemoteComment() {
			a.SetMessage("Existing forge comments are read-only")
			return false
		}
		a.SetMessage("No comment at cursor")
		return false
	}
	if c := a.commentAtLocation(loc); c != nil && c.IsLocked() {
		a.SetMessage("Comment already pushed — read only")
		return false
	}

	switch loc.Kind {
	case CommentAtReview:
		if loc.Index < len(a.Session.ReviewComments) {
			a.Session.ReviewComments = slices.Delete(a.Session.ReviewComments, loc.Index, loc.Index+1)
			a.Dirty = true
			a.SetMessage("Review comment deleted")
			a.RebuildAnnotations()
			return true
		}
	case CommentAtFile:
		if review := a.Session.File(loc.Path); review != nil && loc.Index < len(review.FileComments) {
			review.FileComments = slices.Delete(review.FileComments, loc.Index, loc.Index+1)
			a.Dirty = true
			a.SetMessage("Comment deleted")
			a.RebuildAnnotations()
			return true
		}
	case CommentAtLine:
		review := a.Session.File(loc.Path)
		if review == nil {
			break
		}
		comments := review.LineComments[loc.Line]
		// CommentIdx from the annotation is the absolute index into the
		// stored slice, so delete directly after verifying the side.
		if loc.Index < len(comments) && commentSide(comments[loc.Index]) == loc.Side {
			comments = slices.Delete(comments, loc.Index, loc.Index+1)
			if len(comments) == 0 {
				delete(review.LineComments, loc.Line)
			} else {
				review.LineComments[loc.Line] = comments
			}
			a.Dirty = true
			a.SetMessage(fmt.Sprintf("Comment on line %d deleted", loc.Line))
			a.RebuildAnnotations()
			return true
		}
	}
	return false
}

// ClearComments removes comments (and reviewed state per scope) via the
// session primitive, reporting counts in the status bar.
func (a *App) ClearComments(scope model.ClearScope) {
	cleared, unreviewed := a.Session.ClearComments(scope)
	if cleared == 0 && unreviewed == 0 {
		a.SetMessage("No comments to clear")
		return
	}

	a.Dirty = true
	a.RebuildAnnotations()
	var msg string
	switch {
	case cleared == 0:
		msg = fmt.Sprintf("Unreviewed %d files", unreviewed)
	case unreviewed == 0:
		msg = fmt.Sprintf("Cleared %d comments", cleared)
	default:
		msg = fmt.Sprintf("Cleared %d comments, unreviewed %d files", cleared, unreviewed)
	}
	a.SetMessage(msg)
}

// sameComment reports whether two annotation rows belong to the same
// rendered comment.
func sameComment(a, b *AnnotatedLine) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case AnnReviewComment:
		return a.CommentIdx == b.CommentIdx
	case AnnFileComment:
		return a.FileIdx == b.FileIdx && a.CommentIdx == b.CommentIdx
	case AnnLineComment:
		return a.FileIdx == b.FileIdx && a.Line == b.Line &&
			a.Side == b.Side && a.CommentIdx == b.CommentIdx
	default:
		return false
	}
}

// commentBlockStart is the first annotation row of the comment rendered at
// cursorLine (or cursorLine itself when it isn't on a comment).
func (a *App) commentBlockStart(cursorLine int) int {
	if cursorLine >= len(a.LineAnnotations) {
		return cursorLine
	}
	cur := &a.LineAnnotations[cursorLine]
	start := cursorLine
	for start > 0 && sameComment(&a.LineAnnotations[start-1], cur) {
		start--
	}
	return start
}

// commentCurrentLineCursor is the byte offset in the loaded CommentBuffer
// for the start (cursorAtEnd false) or end of the comment line the diff
// cursor is on. The comment's block begins at annotation row blockStart
// (row 0 = top border, then one row per wrapped segment, then the bottom
// border).
func (a *App) commentCurrentLineCursor(blockStart int, cursorAtEnd bool) int {
	content := a.CommentBuffer
	contentArea := satSub(a.DiffState.ViewportWidth, 10)
	// Visual content row under the cursor (skip the top border at row 0).
	visualTarget := satSub(satSub(a.DiffState.CursorLine, blockStart), 1)

	visual := 0
	byteIdx := 0
	lineStart := 0
	lineLen := 0
	for line := range strings.SplitSeq(content, "\n") {
		lineStart = byteIdx
		lineLen = len(line)
		segs := max(len(WrapSegments(line, contentArea)), 1)
		if visualTarget < visual+segs {
			if cursorAtEnd {
				return lineStart + lineLen
			}
			return lineStart
		}
		visual += segs
		byteIdx += len(line) + 1
	}
	// Cursor on the bottom border or past the content: use the last line.
	if cursorAtEnd {
		return lineStart + lineLen
	}
	return lineStart
}

// --- Comment mode transitions ---

// enterCommentInput is the shared comment-mode setup: input mode, scroll
// snap, and a cleared buffer.
func (a *App) enterCommentInput() {
	a.InputMode = input.ModeComment
	// Snap horizontal scroll back to the left edge so the inline input box
	// renders inside the viewport on long lines.
	a.DiffState.ScrollX = 0
	a.CommentBuffer = ""
	a.CommentCursor = 0
	a.CommentType = a.DefaultCommentType()
}

// EnterCommentMode opens the comment input for a new comment: file-level
// when fileLevel is set, otherwise anchored to the diff line under the
// cursor (warning and no mode change when the cursor is not on one).
func (a *App) EnterCommentMode(fileLevel bool) {
	var line *CommentAnchor
	if !fileLevel {
		lineno, side, ok := a.LineAtCursor()
		if !ok {
			a.SetMessage("Move cursor to a diff line to add a line comment")
			return
		}
		line = &CommentAnchor{Line: lineno, Side: side}
	}
	a.enterCommentInput()
	a.CommentIsReviewLevel = false
	a.CommentIsFileLevel = fileLevel
	a.CommentLine = line
	a.CommentLineRange = nil
	a.EditingCommentID = nil
}

// EnterReviewCommentMode opens the comment input for a new review-level
// comment.
func (a *App) EnterReviewCommentMode() {
	a.enterCommentInput()
	a.CommentIsReviewLevel = true
	a.CommentIsFileLevel = false
	a.CommentLine = nil
	a.CommentLineRange = nil
	a.EditingCommentID = nil
}

// EnterEditMode enters edit mode for the comment at the cursor.
// cursorAtEnd places the text cursor at the end of the current comment line
// (vim A / the default behavior); otherwise at its start (vim i). Reports
// whether a comment was found and edit mode entered.
func (a *App) EnterEditMode(cursorAtEnd bool) bool {
	loc, ok := a.FindCommentAtCursor()
	if !ok {
		if a.CursorOnRemoteComment() {
			a.SetMessage("Existing forge comments are read-only")
		}
		return false
	}
	comment := a.commentAtLocation(loc)
	if comment == nil {
		return false
	}
	// First annotation row of the comment under the cursor, so the text
	// cursor lands on the line the diff cursor is actually pointing at.
	blockStart := a.commentBlockStart(a.DiffState.CursorLine)

	a.InputMode = input.ModeComment
	a.DiffState.ScrollX = 0
	a.CommentBuffer = comment.Content
	a.CommentCursor = a.commentCurrentLineCursor(blockStart, cursorAtEnd)
	a.CommentType = comment.CommentType
	a.CommentIsReviewLevel = loc.Kind == CommentAtReview
	a.CommentIsFileLevel = loc.Kind == CommentAtFile
	if loc.Kind == CommentAtLine {
		a.CommentLine = &CommentAnchor{Line: loc.Line, Side: loc.Side}
	} else {
		a.CommentLine = nil
	}
	id := comment.ID
	a.EditingCommentID = &id
	return true
}

// ExitCommentMode leaves comment mode, discarding the buffer.
func (a *App) ExitCommentMode() {
	a.InputMode = input.ModeNormal
	a.CommentBuffer = ""
	a.CommentCursor = 0
	a.CommentIsReviewLevel = false
	a.EditingCommentID = nil
	a.CommentLineRange = nil
}

// usernameOrDefault is the author stamped on new comments.
func (a *App) usernameOrDefault() string {
	if a.Username != "" {
		return a.Username
	}
	return model.DefaultAuthor
}

// findCommentByID returns the comment with the given id, or nil.
func findCommentByID(comments []*model.Comment, id string) *model.Comment {
	for _, c := range comments {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// SaveComment persists the comment buffer: in-place update when
// EditingCommentID is set, otherwise insertion through the shared
// reviewcli.AddCommentToSession primitive with the target derived from the
// comment scope state. New comments are stamped with the configured
// username and, when a single commit is selected, that commit's SHA. Marks
// the app dirty, rebuilds annotations, exits comment mode, and returns the
// saved comment (nil on validation failure) so the UI layer can run its
// session-save hook.
func (a *App) SaveComment() *model.Comment {
	content := strings.TrimSpace(a.CommentBuffer)
	if content == "" {
		a.SetMessage("Comment cannot be empty")
		return nil
	}

	message := "Error: Could not save comment"
	var saved *model.Comment

	switch {
	case a.EditingCommentID != nil:
		saved, message = a.saveEditedComment(content)
	case a.CommentIsReviewLevel:
		c, err := reviewcli.AddCommentToSession(a.Session, reviewcli.AddCommentRequest{
			Target:      reviewcli.CommentTarget{Kind: reviewcli.TargetReview},
			Content:     content,
			CommentType: a.CommentType,
			Author:      a.usernameOrDefault(),
		})
		if err != nil {
			message = fmt.Sprintf("Error: Could not save comment: %v", err)
		} else {
			saved, message = c, "Review comment added"
		}
	default:
		if path, ok := a.CurrentFilePath(); ok {
			var target reviewcli.CommentTarget
			var successMessage string
			switch {
			case a.CommentIsFileLevel:
				target = reviewcli.CommentTarget{Kind: reviewcli.TargetFile, Path: path}
				successMessage = "File comment added"
			case a.CommentLineRange != nil:
				rng := *a.CommentLineRange
				if rng.Range.IsSingle() {
					successMessage = fmt.Sprintf("Comment added to line %d", rng.Range.End)
				} else {
					successMessage = fmt.Sprintf("Comment added to lines %d-%d", rng.Range.Start, rng.Range.End)
				}
				target = reviewcli.CommentTarget{
					Kind: reviewcli.TargetLineRange, Path: path,
					Range: rng.Range, Side: rng.Side,
				}
			case a.CommentLine != nil:
				target = reviewcli.CommentTarget{
					Kind: reviewcli.TargetLine, Path: path,
					Line: a.CommentLine.Line, Side: a.CommentLine.Side,
				}
				successMessage = fmt.Sprintf("Comment added to line %d", a.CommentLine.Line)
			default:
				target = reviewcli.CommentTarget{Kind: reviewcli.TargetFile, Path: path}
				successMessage = "File comment added"
			}

			c, err := reviewcli.AddCommentToSession(a.Session, reviewcli.AddCommentRequest{
				Target:      target,
				Content:     content,
				CommentType: a.CommentType,
				Author:      a.usernameOrDefault(),
				CommitID:    a.CommitIDForNewComment(),
			})
			if err != nil {
				message = fmt.Sprintf("Error: Could not save comment: %v", err)
			} else {
				saved, message = c, successMessage
			}
		}
	}

	if !strings.HasPrefix(message, "Error:") {
		a.Dirty = true
	}
	a.SetMessage(message)
	a.RebuildAnnotations()
	a.ExitCommentMode()
	return saved
}

// saveEditedComment updates the comment identified by EditingCommentID in
// place, searching review, file, then line comments like tuicr.
func (a *App) saveEditedComment(content string) (*model.Comment, string) {
	editingID := *a.EditingCommentID
	if c := findCommentByID(a.Session.ReviewComments, editingID); c != nil {
		c.Content = content
		c.CommentType = a.CommentType
		return c, "Review comment updated"
	}
	path, ok := a.CurrentFilePath()
	if !ok {
		return nil, "Error: Could not save comment"
	}
	review := a.Session.File(path)
	if review == nil {
		return nil, "Error: Could not save comment"
	}
	if c := findCommentByID(review.FileComments, editingID); c != nil {
		c.Content = content
		c.CommentType = a.CommentType
		return c, "Comment updated"
	}
	for _, comments := range review.LineComments {
		if c := findCommentByID(comments, editingID); c != nil {
			c.Content = content
			c.CommentType = a.CommentType
			if a.CommentLine != nil {
				return c, fmt.Sprintf("Comment on line %d updated", a.CommentLine.Line)
			}
			return c, "Comment updated"
		}
	}
	return nil, "Error: Comment to edit not found"
}

// --- Comment type cycling ---

// commentTypeIndex is the index of the current comment type in the cycle.
func (a *App) commentTypeIndex() int {
	currentID := a.CommentType.ID()
	for i := range a.CommentTypes {
		if a.CommentTypes[i].ID == currentID {
			return i
		}
	}
	return 0
}

// CycleCommentType advances to the next configured comment type, wrapping.
func (a *App) CycleCommentType() {
	if len(a.CommentTypes) == 0 {
		return
	}
	if !a.CanCycleCommentTypes() {
		a.SetMessage("Only one comment type configured")
		return
	}
	next := (a.commentTypeIndex() + 1) % len(a.CommentTypes)
	a.CommentType = model.CommentTypeFromID(a.CommentTypes[next].ID)
	a.announceCommentType()
}

// CycleCommentTypeReverse steps back to the previous comment type, wrapping.
func (a *App) CycleCommentTypeReverse() {
	if len(a.CommentTypes) == 0 {
		return
	}
	if !a.CanCycleCommentTypes() {
		a.SetMessage("Only one comment type configured")
		return
	}
	prev := a.commentTypeIndex() - 1
	if prev < 0 {
		prev = len(a.CommentTypes) - 1
	}
	a.CommentType = model.CommentTypeFromID(a.CommentTypes[prev].ID)
	a.announceCommentType()
}

// announceCommentType emits a status message naming the current comment
// type. None has an empty label, so fall back to its id for legible
// feedback.
func (a *App) announceCommentType() {
	display := a.CommentTypeLabel(a.CommentType)
	if display == "" {
		display = a.CommentType.ID()
	}
	a.SetMessage("Comment type: " + display)
}
