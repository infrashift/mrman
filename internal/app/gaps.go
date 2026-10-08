// gaps.go ports tuicr's src/app/gaps.rs: the hidden-context expansion
// engine. A gap is the run of unchanged lines between hunks (or before the
// first hunk / after the last one); expanding fetches those lines through a
// ContextProvider and stores them keyed by GapID.

package app

import (
	"fmt"

	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs"
)

// GapExpandBatch is the number of lines one expander press reveals.
const GapExpandBatch = 20

// GapID identifies a gap between hunks in a file (for context expansion).
// HunkIdx is the hunk the gap precedes (0 = gap before the first hunk;
// len(hunks) = the end-of-file gap). It is a value type usable as a map key.
type GapID struct {
	FileIdx int
	HunkIdx int
}

// ExpandDirection is the direction of gap expansion.
type ExpandDirection int

// Expand directions.
const (
	// ExpandDown expands downward from the upper boundary.
	ExpandDown ExpandDirection = iota
	// ExpandUp expands upward from the lower boundary.
	ExpandUp
	// ExpandBoth expands all remaining lines (merged expander).
	ExpandBoth
)

// GapHitKind discriminates GapCursorHit.
type GapHitKind int

// Gap cursor hit kinds.
const (
	// GapHitExpander means the cursor is on a directional expander.
	GapHitExpander GapHitKind = iota
	// GapHitHiddenLines means the cursor is on the "N lines hidden" row.
	GapHitHiddenLines
	// GapHitExpandedContent means the cursor is on already-expanded context.
	GapHitExpandedContent
)

// GapCursorHit is the result of checking what the cursor is on in a gap
// region. Direction is meaningful only for GapHitExpander.
type GapCursorHit struct {
	Kind      GapHitKind
	GapID     GapID
	Direction ExpandDirection
}

// ContextProvider fetches unchanged context lines and file line counts for
// gap expansion, abstracting over local VCS and (later) forge backends.
type ContextProvider interface {
	// FetchContextLines returns context lines [start, end] (1-indexed,
	// inclusive, new-side coordinates) for the given file.
	FetchContextLines(oldPath, newPath *string, status model.FileStatus, start, end uint32) ([]model.DiffLine, error)
	// FileLineCount returns the total line count of the file snapshot.
	FileLineCount(oldPath, newPath *string, status model.FileStatus) (uint32, error)
	// CanExpand reports whether there is anything outside the diff to expand
	// into. A patch artifact carries only the context inside its own hunks
	// and has no tree to read the rest from, so its expanders would be dead
	// keys — the reviewer presses one and nothing happens, with no
	// explanation. Making this a capability of the provider rather than a
	// switch on the diff source means the answer travels with the thing that
	// would have to do the fetching.
	CanExpand() bool
}

// VcsContextProvider adapts a vcs.Backend into a ContextProvider, reading
// from RefCommit when set (commit-range reviews) or the worktree/index.
type VcsContextProvider struct {
	Backend   vcs.Backend
	RefCommit *string
}

func providerPath(oldPath, newPath *string) string {
	if newPath != nil {
		return *newPath
	}
	if oldPath != nil {
		return *oldPath
	}
	return ""
}

// FetchContextLines implements ContextProvider via the VCS backend.
func (p VcsContextProvider) FetchContextLines(oldPath, newPath *string, status model.FileStatus, start, end uint32) ([]model.DiffLine, error) {
	return p.Backend.FetchContextLines(providerPath(oldPath, newPath), status, p.RefCommit, start, end)
}

// FileLineCount implements ContextProvider via the VCS backend.
func (p VcsContextProvider) FileLineCount(oldPath, newPath *string, status model.FileStatus) (uint32, error) {
	return p.Backend.FileLineCount(providerPath(oldPath, newPath), status, p.RefCommit)
}

// CanExpand is true: a checkout has the rest of the file on disk.
func (p VcsContextProvider) CanExpand() bool { return true }

// noContextProvider serves a diff that is all there is — a patch artifact.
// It answers every request emptily and reports that expansion is impossible,
// so the gap rows are never drawn in the first place.
type noContextProvider struct{}

// FetchContextLines returns nothing; there is no snapshot to read.
func (noContextProvider) FetchContextLines(_, _ *string, _ model.FileStatus, _, _ uint32) ([]model.DiffLine, error) {
	return nil, nil
}

// FileLineCount reports zero, which suppresses the end-of-file gap.
func (noContextProvider) FileLineCount(_, _ *string, _ model.FileStatus) (uint32, error) {
	return 0, nil
}

// CanExpand is false.
func (noContextProvider) CanExpand() bool { return false }

// GapSize is the number of hidden lines in a gap (new-side coordinates).
func (a *App) GapSize(gapID GapID) (uint32, bool) {
	if gapID.FileIdx >= len(a.DiffFiles) {
		return 0, false
	}
	file := &a.DiffFiles[gapID.FileIdx]

	if gapID.HunkIdx == len(file.Hunks) {
		// End-of-file gap.
		if len(file.Hunks) == 0 {
			return 0, false
		}
		lastHunk := &file.Hunks[len(file.Hunks)-1]
		start := lastHunk.NewStart + lastHunk.NewCount
		end, ok := a.FileLineCountCache[gapID.FileIdx]
		if !ok {
			return 0, false
		}
		if start > end {
			return 0, true
		}
		return end - start + 1, true
	}

	if gapID.HunkIdx >= len(file.Hunks) {
		return 0, false
	}
	hunk := &file.Hunks[gapID.HunkIdx]
	var prevHunk *model.DiffHunk
	if gapID.HunkIdx > 0 {
		prevHunk = &file.Hunks[gapID.HunkIdx-1]
	}
	return calculateGap(prevHunk, hunk.NewStart), true
}

// findGapContainingLineno returns the gap in fileIdx whose line range on
// side contains targetLineno, if any. Used by GoToSourceLine to auto-expand
// collapsed context when the user jumps to a hidden line.
func (a *App) findGapContainingLineno(fileIdx int, targetLineno uint32, side model.LineSide) (GapID, bool) {
	if fileIdx >= len(a.DiffFiles) {
		return GapID{}, false
	}
	file := &a.DiffFiles[fileIdx]
	for hunkIdx := range file.Hunks {
		hunk := &file.Hunks[hunkIdx]
		var prev *model.DiffHunk
		if hunkIdx > 0 {
			prev = &file.Hunks[hunkIdx-1]
		}
		var start, end uint32
		if side == model.LineSideNew {
			if prev == nil {
				start, end = 1, satSubU32(hunk.NewStart, 1)
			} else {
				start, end = prev.NewStart+prev.NewCount, satSubU32(hunk.NewStart, 1)
			}
		} else {
			if prev == nil {
				start, end = 1, satSubU32(hunk.OldStart, 1)
			} else {
				start, end = prev.OldStart+prev.OldCount, satSubU32(hunk.OldStart, 1)
			}
		}
		if start <= end && targetLineno >= start && targetLineno <= end {
			return GapID{FileIdx: fileIdx, HunkIdx: hunkIdx}, true
		}
	}

	// Check the EOF gap (after the last hunk).
	if len(file.Hunks) > 0 {
		if total, ok := a.FileLineCountCache[fileIdx]; ok {
			last := &file.Hunks[len(file.Hunks)-1]
			var start, end uint32
			if side == model.LineSideNew {
				start, end = last.NewStart+last.NewCount, total
			} else {
				delta := int64(last.NewStart+last.NewCount) - int64(last.OldStart+last.OldCount)
				newStart := last.NewStart + last.NewCount
				start = uint32(int64(newStart) - delta) //nolint:gosec // G115: the old side lags the new by delta, so the result is non-negative
				end = uint32(int64(total) - delta)      //nolint:gosec // G115: total lags the new side by delta, so the result is non-negative
			}
			if start <= end && targetLineno >= start && targetLineno <= end {
				return GapID{FileIdx: fileIdx, HunkIdx: len(file.Hunks)}, true
			}
		}
	}
	return GapID{}, false
}

// expandPlanToReach chooses an ExpandDirection and line-count limit so that
// expanding gapID reveals exactly the lines between the cursor and
// targetLineno on side. Cursor above the gap expands Down from the previous
// hunk; cursor at-or-below the gap expands Up from the next hunk.
//
// ExpandGap operates in new-side coordinates, so an old-side target is
// translated using the offset that holds across an unchanged-context gap:
// new - old = hunk.NewStart - hunk.OldStart of the hunk after the gap.
func (a *App) expandPlanToReach(gapID GapID, targetLineno uint32, side model.LineSide) (ExpandDirection, *int) {
	if gapID.FileIdx >= len(a.DiffFiles) {
		return ExpandBoth, nil
	}
	file := &a.DiffFiles[gapID.FileIdx]

	// EOF gap: no next hunk, only expand downward from the last hunk.
	if gapID.HunkIdx == len(file.Hunks) {
		if len(file.Hunks) == 0 {
			return ExpandBoth, nil
		}
		last := &file.Hunks[len(file.Hunks)-1]
		gapStartNew := last.NewStart + last.NewCount
		offsetNewMinusOld := int64(last.NewStart+last.NewCount) - int64(last.OldStart+last.OldCount)
		targetNew := int64(targetLineno)
		if side == model.LineSideOld {
			targetNew += offsetNewMinusOld
		}
		topLen := int64(len(a.ExpandedTop[gapID]))
		innerStart := int64(gapStartNew) + topLen
		limit := max(int(targetNew-innerStart+1), 0)
		return ExpandDown, &limit
	}

	hunk := &file.Hunks[gapID.HunkIdx]
	var prevHunk *model.DiffHunk
	if gapID.HunkIdx > 0 {
		prevHunk = &file.Hunks[gapID.HunkIdx-1]
	}
	gapStartNew := uint32(1)
	if prevHunk != nil {
		gapStartNew = prevHunk.NewStart + prevHunk.NewCount
	}
	gapEndNew := satSubU32(hunk.NewStart, 1)
	// offset := new - old, constant across the unchanged context gap.
	offsetNewMinusOld := int64(hunk.NewStart) - int64(hunk.OldStart)
	targetNew := int64(targetLineno)
	if side == model.LineSideOld {
		targetNew += offsetNewMinusOld
	}

	cursorBelowGap := false
	if headerIdx, ok := a.findHunkHeaderAnnotationIdx(gapID); ok {
		cursorBelowGap = a.DiffState.CursorLine >= headerIdx
	}

	if cursorBelowGap {
		botLen := int64(len(a.ExpandedBottom[gapID]))
		innerEnd := int64(gapEndNew) - botLen
		limit := max(int(innerEnd-targetNew+1), 0)
		return ExpandUp, &limit
	}
	topLen := int64(len(a.ExpandedTop[gapID]))
	innerStart := int64(gapStartNew) + topLen
	limit := max(int(targetNew-innerStart+1), 0)
	return ExpandDown, &limit
}

// findHunkHeaderAnnotationIdx locates the header annotation of the hunk a
// gap precedes.
func (a *App) findHunkHeaderAnnotationIdx(gapID GapID) (int, bool) {
	for idx := range a.LineAnnotations {
		ann := &a.LineAnnotations[idx]
		if ann.Kind == AnnHunkHeader && ann.FileIdx == gapID.FileIdx && ann.HunkIdx == gapID.HunkIdx {
			return idx, true
		}
	}
	return 0, false
}

// gapBoundaries returns the (startLine, endLine) of a gap in new-side
// coordinates, or false when the gap is empty or unknown.
func (a *App) gapBoundaries(gapID GapID) (uint32, uint32, bool) {
	if gapID.FileIdx >= len(a.DiffFiles) {
		return 0, 0, false
	}
	file := &a.DiffFiles[gapID.FileIdx]

	if gapID.HunkIdx == len(file.Hunks) {
		// End-of-file gap: starts after last hunk, ends at file end.
		if len(file.Hunks) == 0 {
			return 0, 0, false
		}
		lastHunk := &file.Hunks[len(file.Hunks)-1]
		start := lastHunk.NewStart + lastHunk.NewCount
		end, ok := a.FileLineCountCache[gapID.FileIdx]
		if !ok || start > end {
			return 0, 0, false
		}
		return start, end, true
	}

	if gapID.HunkIdx >= len(file.Hunks) {
		return 0, 0, false
	}
	hunk := &file.Hunks[gapID.HunkIdx]
	var prevHunk *model.DiffHunk
	if gapID.HunkIdx > 0 {
		prevHunk = &file.Hunks[gapID.HunkIdx-1]
	}
	start := uint32(1)
	if prevHunk != nil {
		start = prevHunk.NewStart + prevHunk.NewCount
	}
	end := satSubU32(hunk.NewStart, 1)
	if start > end {
		return 0, 0, false
	}
	return start, end, true
}

// GetExpandedLine looks up an expanded context line by sequential index
// across top + bottom.
func (a *App) GetExpandedLine(gapID GapID, idx int) *model.DiffLine {
	top := a.ExpandedTop[gapID]
	if idx < len(top) {
		return &top[idx]
	}
	bottom := a.ExpandedBottom[gapID]
	idx -= len(top)
	if idx < len(bottom) {
		return &bottom[idx]
	}
	return nil
}

// ExpandGap expands a gap in the given direction. If limit is non-nil, up
// to *limit lines are revealed; nil expands all remaining.
func (a *App) ExpandGap(gapID GapID, direction ExpandDirection, limit *int) error {
	// Ensure the file line count is cached for EOF gaps.
	a.ensureFileLineCountCached(gapID.FileIdx)

	gapStart, gapEnd, ok := a.gapBoundaries(gapID)
	if !ok {
		return fmt.Errorf("invalid gap: %+v", gapID)
	}

	file := &a.DiffFiles[gapID.FileIdx]
	oldPath := file.OldPath
	newPath := file.NewPath
	fileStatus := file.Status

	topLen := uint32(len(a.ExpandedTop[gapID]))    //nolint:gosec // G115: line numbers fit uint32
	botLen := uint32(len(a.ExpandedBottom[gapID])) //nolint:gosec // G115: line numbers fit uint32

	// The unexpanded region runs from gapStart+topLen to gapEnd-botLen.
	innerStart := gapStart + topLen
	innerEnd := satSubU32(gapEnd, botLen)

	if innerStart > innerEnd {
		return nil // fully expanded
	}

	// Delta between new-side and old-side line numbers for this gap.
	// Expanded context is fetched in new-side coordinates; the old-side
	// number is newLineno - delta.
	var delta int64
	if gapID.HunkIdx > 0 {
		prev := &file.Hunks[gapID.HunkIdx-1]
		oldEnd := int64(prev.OldStart) + int64(prev.OldCount)
		newEnd := int64(prev.NewStart) + int64(prev.NewCount)
		delta = newEnd - oldEnd
	}

	provider := a.contextProvider()
	fetch := func(start, end uint32) ([]model.DiffLine, error) {
		lines, err := provider.FetchContextLines(oldPath, newPath, fileStatus, start, end)
		if err != nil {
			return nil, err
		}
		for i := range lines {
			if lines[i].NewLineno != nil {
				old := uint32(int64(*lines[i].NewLineno) - delta) //nolint:gosec // G115: line numbers fit uint32
				lines[i].OldLineno = &old
			}
		}
		return lines, nil
	}

	switch direction {
	case ExpandDown:
		fetchEnd := innerEnd
		if limit != nil {
			fetchEnd = min(innerStart+satSubU32(uint32(*limit), 1), innerEnd) //nolint:gosec // G115: line numbers fit uint32
		}
		newLines, err := fetch(innerStart, fetchEnd)
		if err != nil {
			return err
		}
		a.ExpandedTop[gapID] = append(a.ExpandedTop[gapID], newLines...)
	case ExpandUp:
		fetchStart := innerStart
		if limit != nil {
			fetchStart = max(satSubU32(innerEnd, uint32(*limit)-1), innerStart) //nolint:gosec // G115: line numbers fit uint32
		}
		newLines, err := fetch(fetchStart, innerEnd)
		if err != nil {
			return err
		}
		// Prepend: new lines go before existing bottom lines.
		existing := a.ExpandedBottom[gapID]
		combined := make([]model.DiffLine, 0, len(newLines)+len(existing))
		combined = append(combined, newLines...)
		combined = append(combined, existing...)
		a.ExpandedBottom[gapID] = combined
	case ExpandBoth:
		// Fetch everything remaining.
		newLines, err := fetch(innerStart, innerEnd)
		if err != nil {
			return err
		}
		a.ExpandedTop[gapID] = append(a.ExpandedTop[gapID], newLines...)
	}

	a.RebuildAnnotations()
	return nil
}

// refCommit is the snapshot context expansion reads the new side from: the
// newest commit of a commit-range review, the index for a staged-only
// review, and nil (the working tree) otherwise. Reading the working tree for
// a staged review showed unstaged edits as context, numbered as if staged.
func (a *App) refCommit() *string {
	switch a.DiffSource.Kind {
	case DiffSourceCommitRange:
		if n := len(a.DiffSource.Commits); n > 0 {
			return &a.DiffSource.Commits[n-1]
		}
	case DiffSourceStaged:
		index := vcs.IndexRef
		return &index
	}
	return nil
}

// contextProvider resolves the ContextProvider for the current diff source.
// PR mode reads from the forge snapshot cache (prcontext.go); everything
// else reads the local VCS backend.
func (a *App) contextProvider() ContextProvider {
	switch {
	case a.InPrMode():
		return a.ensurePrContext()
	case a.DiffSource.Kind == DiffSourcePatch:
		return noContextProvider{}
	}
	return VcsContextProvider{Backend: a.VCS, RefCommit: a.refCommit()}
}

// contextGapsEnabled reports whether hidden-context rows are worth drawing.
func (a *App) contextGapsEnabled() bool { return a.contextProvider().CanExpand() }

// CollapseGap collapses an expanded gap.
func (a *App) CollapseGap(gapID GapID) {
	delete(a.ExpandedTop, gapID)
	delete(a.ExpandedBottom, gapID)
	a.RebuildAnnotations()
}

// ClearExpandedGaps clears all expanded gaps (called when reloading diffs).
func (a *App) ClearExpandedGaps() {
	a.ExpandedTop = map[GapID][]model.DiffLine{}
	a.ExpandedBottom = map[GapID][]model.DiffLine{}
	a.FileLineCountCache = map[int]uint32{}
}

// eofGapEnabled reports whether end-of-file gap expansion is meaningful for
// the current diff source (the worktree/index or ref commit is the correct
// snapshot to read context from).
func (a *App) eofGapEnabled() bool {
	switch a.DiffSource.Kind {
	case DiffSourceWorkingTree, DiffSourceUnstaged, DiffSourceStagedAndUnstaged,
		DiffSourceStagedUnstagedAndCommits, DiffSourceCommitRange, DiffSourcePullRequest:
		return true
	case DiffSourceStaged:
		return false
	case DiffSourcePatch:
		// No file on disk to measure. This switch also gates the eager
		// per-file FileLineCount that NewApp runs at startup, which the patch
		// backend cannot answer — so returning false here spares it too.
		return false
	}
	return false
}

// GapAtCursor reports what the cursor is on in a gap region, if anything.
func (a *App) GapAtCursor() (GapCursorHit, bool) {
	target := a.DiffState.CursorLine
	if target >= len(a.LineAnnotations) {
		return GapCursorHit{}, false
	}
	ann := &a.LineAnnotations[target]
	switch ann.Kind {
	case AnnExpander:
		return GapCursorHit{Kind: GapHitExpander, GapID: ann.GapID, Direction: ann.Direction}, true
	case AnnHiddenLines:
		return GapCursorHit{Kind: GapHitHiddenLines, GapID: ann.GapID}, true
	case AnnExpandedContext:
		return GapCursorHit{Kind: GapHitExpandedContent, GapID: ann.GapID}, true
	default:
		return GapCursorHit{}, false
	}
}
