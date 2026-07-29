// navigation.go ports tuicr's src/app/navigation.rs: the scroll/cursor
// engine, the layout constants shared with the renderer, source-line jumps,
// hunk and file navigation, and the render-height math that keeps
// TotalLines in lockstep with the annotation stream.
package app

import (
	"fmt"

	"github.com/infrashift/mrman/internal/model"
)

// MinLinenoWidth is the minimum line-number column width (covers files up
// to 9999 lines).
const MinLinenoWidth = 4

// LinenoWidth is the number of characters needed to display maxLineno in
// decimal, minimum MinLinenoWidth.
func LinenoWidth(maxLineno uint32) int {
	if maxLineno == 0 {
		return MinLinenoWidth
	}
	digits := 0
	for n := maxLineno; n > 0; n /= 10 {
		digits++
	}
	return max(digits, MinLinenoWidth)
}

// UnifiedGutter is the unified diff gutter width:
// indicator(1) + lineno(w) + space(1) + prefix(1) + space(1).
func UnifiedGutter(w int) int {
	return w + 4
}

// SbsLeftGutter is the side-by-side leading width before Old content:
// indicator(1) + lineno(w) + space(1) + prefix(1).
func SbsLeftGutter(w int) int {
	return w + 3
}

// SbsOverhead is the side-by-side fixed overhead (both gutters plus the
// " | " divider): left indicator(1)+lineno(w)+space(1)+prefix(1), right
// lineno(w)+space(1)+prefix(1), divider(3).
func SbsOverhead(w int) int {
	return 2*w + 8
}

// LinenoWidth is the gutter width for the current diff: the widest line
// number reachable from hunk headers or the file line-count cache.
func (a *App) LinenoWidth() int {
	var hunkMax uint32
	for i := range a.DiffFiles {
		hunkMax = max(hunkMax, a.DiffFiles[i].MaxLineno())
	}
	var cacheMax uint32
	for _, v := range a.FileLineCountCache {
		cacheMax = max(cacheMax, v)
	}
	return LinenoWidth(max(hunkMax, cacheMax))
}

// FindResultKind discriminates FindSourceLineResult.
type FindResultKind int

// Find result kinds.
const (
	// FindNotFound means no matching lines exist in the current file.
	FindNotFound FindResultKind = iota
	// FindExact means an exact match was found at Index.
	FindExact
	// FindNearest means no exact match; the nearest line is at Index.
	FindNearest
)

// FindSourceLineResult is the result of searching for a source line number
// in the annotations.
type FindSourceLineResult struct {
	Kind  FindResultKind
	Index int
}

// findSourceLine searches annotations for the annotation whose line number
// on side best matches targetLineno within currentFile. This is the core
// matching algorithm over DiffLine/SideBySideLine annotations; production
// code goes through findSourceLineInDiff, which also resolves
// ExpandedContext lines.
//
//nolint:unparam // currentFile mirrors tuicr's signature; ported tests search file 0
func findSourceLine(annotations []AnnotatedLine, currentFile int, targetLineno uint32, side model.LineSide) FindSourceLineResult {
	bestIdx, bestDist, haveBest := 0, uint32(0), false

	for idx := range annotations {
		ann := &annotations[idx]
		if ann.Kind != AnnDiffLine && ann.Kind != AnnSideBySideLine {
			continue
		}
		if ann.FileIdx != currentFile {
			continue
		}
		var candidate *uint32
		if side == model.LineSideNew {
			candidate = ann.NewLineno
		} else {
			candidate = ann.OldLineno
		}
		if candidate == nil {
			continue
		}
		dist := absDiffU32(*candidate, targetLineno)
		if dist == 0 {
			return FindSourceLineResult{Kind: FindExact, Index: idx}
		}
		if !haveBest || dist < bestDist {
			bestIdx, bestDist, haveBest = idx, dist, true
		}
	}

	if haveBest {
		return FindSourceLineResult{Kind: FindNearest, Index: bestIdx}
	}
	return FindSourceLineResult{Kind: FindNotFound}
}

// skipDecorationForward walks start forward (capped at maxLine) to the
// nearest non-decoration annotation so scroll and jump motions land on
// actionable content.
func skipDecorationForward(annotations []AnnotatedLine, start, maxLine int) int {
	line := start
	for line < maxLine && line < len(annotations) && annotations[line].IsDecoration() {
		line++
	}
	return line
}

// skipDecorationBackward walks start backward to the nearest non-decoration
// annotation.
func skipDecorationBackward(annotations []AnnotatedLine, start int) int {
	line := start
	for line > 0 && line < len(annotations) && annotations[line].IsDecoration() {
		line--
	}
	return line
}

// CursorDown moves the cursor down. In single-file view the first overflow
// press arms PrimedWalkNext and parks the cursor on max; on terminals with
// key-release reporting the walk consumes only after the key was released
// between the two presses, so held-key auto-repeat never walks.
func (a *App) CursorDown(lines int) {
	maxLine := a.MaxCursorLine()
	prevCursor := a.DiffState.CursorLine
	prevScroll := a.DiffState.ScrollOffset
	target := a.DiffState.CursorLine + lines
	a.PrimedWalkPrev = false
	if a.IsSingleFileView && target > maxLine {
		releaseGateOK := !a.SupportsKeyboardEnhancement || a.DownReleasedSinceArm
		if a.PrimedWalkNext && releaseGateOK {
			nextIdx := a.DiffState.CurrentFileIdx + 1
			if nextIdx < len(a.DiffFiles) {
				overflow := target - maxLine
				a.PrimedWalkNext = false
				a.DownReleasedSinceArm = false
				a.JumpToFile(nextIdx)
				newTop := a.DiffState.CursorLine
				newMax := a.MaxCursorLine()
				a.DiffState.CursorLine = min(newTop+satSub(overflow, 1), newMax)
				a.ensureCursorVisible()
				return
			}
			a.PrimedWalkNext = false
			a.DownReleasedSinceArm = false
		} else {
			a.PrimedWalkNext = true
			a.DownReleasedSinceArm = false
			a.DiffState.CursorLine = maxLine
			a.ensureCursorVisible()
			return
		}
	} else if target != prevCursor {
		a.PrimedWalkNext = false
		a.DownReleasedSinceArm = false
	}
	a.DiffState.CursorLine = min(target, maxLine)
	if a.DiffState.CursorLine != prevCursor {
		a.ensureCursorVisible()
		// Cap scroll change to cursor movement to prevent multi-line jumps
		// when the view is catching up from a non-steady-state position.
		cursorMoved := a.DiffState.CursorLine - prevCursor
		if a.DiffState.ScrollOffset > prevScroll+cursorMoved {
			a.DiffState.ScrollOffset = prevScroll + cursorMoved
		}
	}
	a.updateCurrentFileFromCursor()
}

// CursorUp moves the cursor up, with the symmetric single-file two-press
// walk gate to the previous file.
func (a *App) CursorUp(lines int) {
	a.PrimedWalkNext = false
	if a.IsSingleFileView {
		fileTop := a.calculateFileScrollOffset(a.DiffState.CurrentFileIdx)
		if a.DiffState.CursorLine < fileTop+lines && a.DiffState.CurrentFileIdx > 0 {
			releaseGateOK := !a.SupportsKeyboardEnhancement || a.UpReleasedSinceArm
			if a.PrimedWalkPrev && releaseGateOK {
				underflow := (fileTop + lines) - a.DiffState.CursorLine
				prevIdx := a.DiffState.CurrentFileIdx - 1
				a.PrimedWalkPrev = false
				a.UpReleasedSinceArm = false
				a.JumpToFile(prevIdx)
				newMax := a.MaxCursorLine()
				a.DiffState.CursorLine = satSub(newMax, satSub(underflow, 1))
				a.ensureCursorVisible()
				return
			}
			a.PrimedWalkPrev = true
			a.UpReleasedSinceArm = false
			a.DiffState.CursorLine = fileTop
			a.ensureCursorVisible()
			return
		}
		a.PrimedWalkPrev = false
		a.UpReleasedSinceArm = false
	}
	a.DiffState.CursorLine = satSub(a.DiffState.CursorLine, lines)
	visibleLines := a.DiffState.EffectiveVisibleLines()
	scrollMargin := a.DiffState.EffectiveScrollMargin(a.ScrollOffset)
	// Enforce top margin.
	if a.DiffState.CursorLine < a.DiffState.ScrollOffset+scrollMargin {
		a.DiffState.ScrollOffset = satSub(a.DiffState.CursorLine, scrollMargin)
	}
	// Ensure cursor is at least within the viewport (no bottom margin
	// enforcement, just basic visibility — handles viewport shrink or
	// wrap-mode changes).
	if a.DiffState.CursorLine >= a.DiffState.ScrollOffset+visibleLines {
		a.DiffState.ScrollOffset = a.DiffState.CursorLine - visibleLines + 1
	}
	a.updateCurrentFileFromCursor()
}

// ScrollDown moves both cursor and scroll for half-page/page scrolling.
func (a *App) ScrollDown(lines int) {
	maxLine := a.MaxCursorLine()
	maxScroll := a.MaxScrollOffset()
	a.DiffState.CursorLine = min(a.DiffState.CursorLine+lines, maxLine)
	a.DiffState.CursorLine = skipDecorationForward(a.LineAnnotations, a.DiffState.CursorLine, maxLine)
	a.DiffState.ScrollOffset = min(a.DiffState.ScrollOffset+lines, maxScroll)
	a.ensureCursorVisible()
	a.updateCurrentFileFromCursor()
}

// ScrollUp moves both cursor and scroll for half-page/page scrolling.
func (a *App) ScrollUp(lines int) {
	a.DiffState.CursorLine = satSub(a.DiffState.CursorLine, lines)
	a.DiffState.CursorLine = skipDecorationBackward(a.LineAnnotations, a.DiffState.CursorLine)
	a.DiffState.ScrollOffset = satSub(a.DiffState.ScrollOffset, lines)
	a.ensureCursorVisible()
	a.updateCurrentFileFromCursor()
}

// ScrollViewDown scrolls the view without moving the cursor unless it would
// leave the scroll margin.
func (a *App) ScrollViewDown(lines int) {
	maxScroll := a.MaxScrollOffset()
	a.DiffState.ScrollOffset = min(a.DiffState.ScrollOffset+lines, maxScroll)
	scrollMargin := a.DiffState.EffectiveScrollMargin(a.ScrollOffset)
	minCursor := min(a.DiffState.ScrollOffset+scrollMargin, a.MaxCursorLine())
	if a.DiffState.CursorLine < minCursor {
		a.DiffState.CursorLine = minCursor
		a.updateCurrentFileFromCursor()
	}
}

// ScrollViewUp scrolls the view up without moving the cursor unless it
// would fall below the viewport bottom.
func (a *App) ScrollViewUp(lines int) {
	a.DiffState.ScrollOffset = satSub(a.DiffState.ScrollOffset, lines)
	visibleLines := a.DiffState.VisibleLineCount
	if visibleLines == 0 {
		visibleLines = max(a.DiffState.ViewportHeight, 1)
	}
	bottom := a.DiffState.ScrollOffset + satSub(visibleLines, 1)
	if a.DiffState.CursorLine > bottom {
		a.DiffState.CursorLine = bottom
		a.updateCurrentFileFromCursor()
	}
}

// ScrollLeft scrolls the diff horizontally (no-op when wrapping).
func (a *App) ScrollLeft(cols int) {
	if a.DiffState.WrapLines {
		return
	}
	a.DiffState.ScrollX = satSub(a.DiffState.ScrollX, cols)
}

// ScrollRight scrolls the diff horizontally (no-op when wrapping).
func (a *App) ScrollRight(cols int) {
	if a.DiffState.WrapLines {
		return
	}
	maxScrollX := satSub(a.DiffState.MaxContentWidth, a.DiffState.ViewportWidth)
	a.DiffState.ScrollX = min(a.DiffState.ScrollX+cols, maxScrollX)
}

// ToggleDiffWrap flips line wrapping in the diff panel.
func (a *App) ToggleDiffWrap() {
	a.SetDiffWrap(!a.DiffState.WrapLines)
}

// SetDiffWrap sets line wrapping and reports the state in the status bar.
func (a *App) SetDiffWrap(enabled bool) {
	a.DiffState.WrapLines = enabled
	if enabled {
		a.DiffState.ScrollX = 0
	}
	status := "off"
	if a.DiffState.WrapLines {
		status = "on"
	}
	a.SetMessage("Diff wrapping: " + status)
}

// ensureCursorVisible adjusts ScrollOffset so the cursor stays within the
// visible viewport, respecting the configured scroll margin.
func (a *App) ensureCursorVisible() {
	visibleLines := a.DiffState.EffectiveVisibleLines()
	maxScroll := a.MaxScrollOffset()
	scrollMargin := a.DiffState.EffectiveScrollMargin(a.ScrollOffset)
	// Cursor too close to the top edge — scroll up.
	if a.DiffState.CursorLine < a.DiffState.ScrollOffset+scrollMargin {
		a.DiffState.ScrollOffset = satSub(a.DiffState.CursorLine, scrollMargin)
	}
	// Cursor too close to the bottom edge — scroll down. Reduce the margin
	// near EOF so we don't scroll to show empty space when the last line is
	// already visible (matches Vim behavior).
	linesBelow := satSub(a.MaxCursorLine(), a.DiffState.CursorLine)
	bottomMargin := min(scrollMargin, linesBelow)
	if a.DiffState.CursorLine+bottomMargin >= a.DiffState.ScrollOffset+visibleLines {
		a.DiffState.ScrollOffset = min(a.DiffState.CursorLine+bottomMargin-visibleLines+1, maxScroll)
	}
}

// CenterCursor centers the viewport on the cursor (zz), clamped to the max
// scroll offset.
func (a *App) CenterCursor() {
	viewport := max(a.DiffState.ViewportHeight, 1)
	halfViewport := viewport / 2
	maxScroll := a.MaxScrollOffset()
	a.DiffState.ScrollOffset = min(satSub(a.DiffState.CursorLine, halfViewport), maxScroll)
}

// CursorToTop scrolls so the cursor sits at the top margin (zt), clamped.
func (a *App) CursorToTop() {
	scrollMargin := a.DiffState.EffectiveScrollMargin(a.ScrollOffset)
	maxScroll := a.MaxScrollOffset()
	a.DiffState.ScrollOffset = min(satSub(a.DiffState.CursorLine, scrollMargin), maxScroll)
}

// CursorToBottom scrolls so the cursor sits at the bottom margin (zb),
// clamped.
func (a *App) CursorToBottom() {
	visibleLines := a.DiffState.EffectiveVisibleLines()
	scrollMargin := a.DiffState.EffectiveScrollMargin(a.ScrollOffset)
	maxScroll := a.MaxScrollOffset()
	a.DiffState.ScrollOffset = min(
		satSub(a.DiffState.CursorLine, satSub(visibleLines, 1+scrollMargin)), maxScroll)
}

// GoToSourceLine jumps the cursor to the annotation matching targetLineno on
// side within the current file, auto-expanding a collapsed gap when the
// target line lives behind an expander.
func (a *App) GoToSourceLine(targetLineno uint32, side model.LineSide) {
	currentFile := a.DiffState.CurrentFileIdx
	result := a.findSourceLineInDiff(targetLineno, side)
	sideLabel := ""
	if side == model.LineSideOld {
		sideLabel = " (old)"
	}

	// If the line isn't already annotated, see whether it falls inside a
	// collapsed (or partially collapsed) gap between hunks. If so, expand
	// toward the target from whichever side the cursor is on; the unreached
	// half of the gap stays collapsed behind an expander.
	if result.Kind != FindExact {
		if gapID, ok := a.findGapContainingLineno(currentFile, targetLineno, side); ok {
			direction, limit := a.expandPlanToReach(gapID, targetLineno, side)
			if err := a.ExpandGap(gapID, direction, limit); err != nil {
				a.SetError(fmt.Sprintf("Expand failed: %v", err))
				return
			}
			result = a.findSourceLineInDiff(targetLineno, side)
		}
	}

	switch result.Kind {
	case FindExact, FindNearest:
		a.DiffState.CursorLine = result.Index
		a.ensureCursorVisible()
		a.CenterCursor()
		a.updateCurrentFileFromCursor()
		if result.Kind == FindNearest {
			a.SetMessage(fmt.Sprintf("Line %d%s not in diff, jumped to nearest", targetLineno, sideLabel))
		}
	case FindNotFound:
		a.SetWarning(fmt.Sprintf("Line %d%s not found in current file", targetLineno, sideLabel))
	}
}

// findSourceLineInDiff is findSourceLine plus resolution of ExpandedContext
// annotations through GetExpandedLine so newly-revealed context lines count
// toward the match.
func (a *App) findSourceLineInDiff(targetLineno uint32, side model.LineSide) FindSourceLineResult {
	currentFile := a.DiffState.CurrentFileIdx
	bestIdx, bestDist, haveBest := 0, uint32(0), false

	for idx := range a.LineAnnotations {
		ann := &a.LineAnnotations[idx]
		var fileIdx int
		var candidate *uint32
		switch ann.Kind {
		case AnnDiffLine, AnnSideBySideLine:
			fileIdx = ann.FileIdx
			if side == model.LineSideNew {
				candidate = ann.NewLineno
			} else {
				candidate = ann.OldLineno
			}
		case AnnExpandedContext:
			line := a.GetExpandedLine(ann.GapID, ann.LineIdx)
			if line == nil {
				continue
			}
			fileIdx = ann.GapID.FileIdx
			if side == model.LineSideNew {
				candidate = line.NewLineno
			} else {
				candidate = line.OldLineno
			}
		default:
			continue
		}
		if fileIdx != currentFile || candidate == nil {
			continue
		}
		dist := absDiffU32(*candidate, targetLineno)
		if dist == 0 {
			return FindSourceLineResult{Kind: FindExact, Index: idx}
		}
		if !haveBest || dist < bestDist {
			bestIdx, bestDist, haveBest = idx, dist, true
		}
	}

	if haveBest {
		return FindSourceLineResult{Kind: FindNearest, Index: bestIdx}
	}
	return FindSourceLineResult{Kind: FindNotFound}
}

// MoveCursorToAnnotation places the cursor on annotation idx, syncing
// CurrentFileIdx so the file list selection follows when the new cursor
// lands on an annotation belonging to a file.
func (a *App) MoveCursorToAnnotation(idx int) {
	if idx >= len(a.LineAnnotations) {
		return
	}
	a.DiffState.CursorLine = idx
	if fileIdx, ok := annotationFileIdx(&a.LineAnnotations[idx]); ok {
		a.DiffState.CurrentFileIdx = fileIdx
	}
	viewport := max(a.DiffState.ViewportHeight, 1)
	if idx < a.DiffState.ScrollOffset {
		a.DiffState.ScrollOffset = idx
	} else if idx >= a.DiffState.ScrollOffset+viewport {
		a.DiffState.ScrollOffset = idx + 1 - viewport
	}
}

// IsCursorVisible mirrors ensureCursorVisible's notion of visibility (uses
// the renderer's VisibleLineCount when present so wrapping is honored).
func (a *App) IsCursorVisible() bool {
	visible := a.DiffState.VisibleLineCount
	if visible == 0 {
		visible = max(a.DiffState.ViewportHeight, 1)
	}
	cursor := a.DiffState.CursorLine
	return cursor >= a.DiffState.ScrollOffset && cursor < a.DiffState.ScrollOffset+visible
}

// JumpToFile focuses file idx: cursor to its first content line, scroll to
// the cursor, tree ancestors expanded and the tree row selected.
func (a *App) JumpToFile(idx int) {
	if idx >= len(a.DiffFiles) {
		return
	}
	// Deliberate jump cancels any in-flight two-press walk arming.
	a.PrimedWalkNext = false
	a.PrimedWalkPrev = false
	a.DownReleasedSinceArm = false
	a.UpReleasedSinceArm = false
	a.DiffState.CurrentFileIdx = idx
	a.DiffState.CursorLine = a.calculateFileScrollOffset(idx)
	a.DiffState.CursorLine = skipDecorationForward(
		a.LineAnnotations, a.DiffState.CursorLine, satSub(len(a.LineAnnotations), 1))
	maxScroll := a.MaxScrollOffset()
	a.DiffState.ScrollOffset = min(a.DiffState.CursorLine, maxScroll)

	for _, parent := range pathAncestors(a.DiffFiles[idx].DisplayPath()) {
		a.ExpandedDirs[parent] = true
	}

	if treeIdx, ok := a.fileIdxToTreeIdx(idx); ok {
		a.FileListState.Select(treeIdx)
	}

	// Single-file view filters LineAnnotations by CurrentFileIdx, so a file
	// switch must rebuild them or click hit-testing would resolve against
	// the previous file.
	if a.IsSingleFileView {
		a.RebuildAnnotations()
	}
}

// JumpToBottom moves the cursor to the last navigable line and positions it
// at the bottom of the viewport.
func (a *App) JumpToBottom() {
	maxLine := a.MaxCursorLine()
	a.DiffState.CursorLine = maxLine
	viewport := max(a.DiffState.ViewportHeight, 1)
	a.DiffState.ScrollOffset = satSub(maxLine+1, viewport)
	a.updateCurrentFileFromCursor()
}

// NextFile jumps to the next file visible in the tree.
func (a *App) NextFile() {
	currentFileIdx := a.DiffState.CurrentFileIdx
	for _, item := range a.BuildVisibleItems() {
		if !item.IsDir && item.FileIdx > currentFileIdx {
			a.JumpToFile(item.FileIdx)
			return
		}
	}
}

// PrevFile jumps to the previous file visible in the tree.
func (a *App) PrevFile() {
	visibleItems := a.BuildVisibleItems()
	currentFileIdx := a.DiffState.CurrentFileIdx
	for i := len(visibleItems) - 1; i >= 0; i-- {
		item := visibleItems[i]
		if !item.IsDir && item.FileIdx < currentFileIdx {
			a.JumpToFile(item.FileIdx)
			return
		}
	}
}

// fileIdxToTreeIdx maps a file index to its row in the visible tree.
func (a *App) fileIdxToTreeIdx(targetFileIdx int) (int, bool) {
	for treeIdx, item := range a.BuildVisibleItems() {
		if !item.IsDir && item.FileIdx == targetFileIdx {
			return treeIdx, true
		}
	}
	return 0, false
}

// HunkPositions returns the render-line indices of every visible hunk
// header. Respects single-file view (only the current file's hunks) and the
// reviewed-collapse behavior in multi-file view (skipped entirely) versus
// single-file view (body rendered under a banner).
func (a *App) HunkPositions() []int {
	single := a.IsSingleFileView
	currentIdx := a.DiffState.CurrentFileIdx
	var positions []int
	cumulative := a.reviewCommentsRenderHeight()
	for fileIdx := range a.DiffFiles {
		file := &a.DiffFiles[fileIdx]
		if single && fileIdx != currentIdx {
			continue
		}
		path := file.DisplayPath()
		isReviewed := a.Session.IsFileReviewed(path)

		if !single {
			cumulative++ // file header
		}
		if !single && isReviewed {
			// Multi-file collapsed: no body, no trailing spacing.
			continue
		}
		if single && isReviewed {
			cumulative++ // banner
		}
		// M4 hook: file-level comment rows are counted here once comments
		// land (tuicr adds review.file_comments.len()).
		if file.IsBinary || len(file.Hunks) == 0 {
			cumulative++
		} else {
			for hunkIdx := range file.Hunks {
				positions = append(positions, cumulative)
				cumulative++
				if !a.IsHunkReviewed(fileIdx, hunkIdx) {
					cumulative += len(file.Hunks[hunkIdx].Lines)
				}
			}
		}
		cumulative++ // trailing spacing or "next file" hint
	}
	return positions
}

// NextHunk moves the cursor to the next hunk header; in single-file view it
// crosses into the next file's first hunk.
func (a *App) NextHunk() {
	// Hunk navigation is a deliberate move, not a continuation of a
	// boundary walk. Clear any in-flight cursor-walk arming.
	a.PrimedWalkNext = false
	a.PrimedWalkPrev = false
	a.DownReleasedSinceArm = false
	a.UpReleasedSinceArm = false
	for _, pos := range a.HunkPositions() {
		if pos > a.DiffState.CursorLine {
			a.DiffState.CursorLine = pos
			a.ensureCursorVisible()
			a.updateCurrentFileFromCursor()
			return
		}
	}
	// No further hunks in the current frame. Single-file view crosses into
	// the next file's first hunk so ] can step the codebase hunk-by-hunk
	// without breaking on file boundaries.
	if a.IsSingleFileView {
		nextIdx := a.DiffState.CurrentFileIdx + 1
		if nextIdx < len(a.DiffFiles) {
			a.JumpToFile(nextIdx)
			if positions := a.HunkPositions(); len(positions) > 0 {
				a.DiffState.CursorLine = positions[0]
				a.ensureCursorVisible()
				a.updateCurrentFileFromCursor()
			}
		}
	}
}

// PrevHunk moves the cursor to the previous hunk header; in single-file
// view it crosses into the previous file's last hunk.
func (a *App) PrevHunk() {
	a.PrimedWalkNext = false
	a.PrimedWalkPrev = false
	a.DownReleasedSinceArm = false
	a.UpReleasedSinceArm = false
	positions := a.HunkPositions()
	for i := len(positions) - 1; i >= 0; i-- {
		if positions[i] < a.DiffState.CursorLine {
			a.DiffState.CursorLine = positions[i]
			a.ensureCursorVisible()
			a.updateCurrentFileFromCursor()
			return
		}
	}
	// Symmetric to NextHunk: in single-file view, fall through to the
	// previous file's last hunk so [ keeps stepping backward across files.
	if a.IsSingleFileView && a.DiffState.CurrentFileIdx > 0 {
		prevIdx := a.DiffState.CurrentFileIdx - 1
		a.JumpToFile(prevIdx)
		if positions := a.HunkPositions(); len(positions) > 0 {
			a.DiffState.CursorLine = positions[len(positions)-1]
			a.ensureCursorVisible()
			a.updateCurrentFileFromCursor()
			return
		}
	}
	a.DiffState.CursorLine = 0
	a.ensureCursorVisible()
	a.updateCurrentFileFromCursor()
}

// calculateFileScrollOffset is the render line where file fileIdx starts.
func (a *App) calculateFileScrollOffset(fileIdx int) int {
	offset := a.reviewCommentsRenderHeight()
	for i := range a.DiffFiles {
		if i == fileIdx {
			break
		}
		offset += a.effectiveFileHeight(i, &a.DiffFiles[i])
	}
	return offset
}

// reviewCommentsRenderHeight is the height of the review-scope area above
// all files. The header line is only rendered in multi-file view.
func (a *App) reviewCommentsRenderHeight() int {
	height := 1
	if a.IsSingleFileView {
		height = 0
	}
	// M4 hook: remote review summaries, review-level comments, review-level
	// remote threads, and the inline comment input box add rows here —
	// mirroring the emission order in RebuildAnnotations exactly, or scroll
	// offsets fall out of sync.
	return height
}

// fileRenderHeight is the multi-file rendered height of one file: header
// only when collapsed-as-reviewed, otherwise header + body.
func (a *App) fileRenderHeight(fileIdx int, file *model.DiffFile) int {
	if a.Session.IsFileReviewed(file.DisplayPath()) {
		return 1 // collapsed: header only
	}
	return 1 + a.fileRenderBodyHeight(fileIdx, file) // header + body
}

// fileRenderBodyHeight is the file body height in lines (content + trailing
// spacing), excluding the file header and ignoring the reviewed-collapse
// short-circuit. Used by effectiveFileHeight to size the body of the
// focused file in single-file view, where the header is hidden and reviewed
// files render the body under a banner.
//
// M4 hook: comment rows (file comments, line comments, remote threads) are
// counted here once comments land — mirroring RebuildAnnotations exactly.
func (a *App) fileRenderBodyHeight(fileIdx int, file *model.DiffFile) int {
	const spacingLines = 1 // trailing blank or "next file" hint
	contentLines := 0

	if file.IsBinary || len(file.Hunks) == 0 {
		contentLines = 1
	} else {
		for hunkIdx := range file.Hunks {
			hunk := &file.Hunks[hunkIdx]

			var prevHunk *model.DiffHunk
			if hunkIdx > 0 {
				prevHunk = &file.Hunks[hunkIdx-1]
			}
			gap := calculateGap(prevHunk, hunk.NewStart)
			gapID := GapID{FileIdx: fileIdx, HunkIdx: hunkIdx}

			if gap > 0 && a.ShouldRenderGapBeforeHunk(fileIdx, hunkIdx) {
				topLen := len(a.ExpandedTop[gapID])
				botLen := len(a.ExpandedBottom[gapID])
				remaining := satSub(int(gap), topLen+botLen)
				contentLines += topLen + botLen
				contentLines += gapAnnotationLineCount(hunkIdx == 0, false, remaining)
			}

			contentLines++ // hunk header
			if a.IsHunkReviewed(fileIdx, hunkIdx) {
				continue
			}

			switch a.DiffViewMode {
			case ViewUnified:
				contentLines += len(hunk.Lines)
			case ViewSideBySide:
				contentLines += sideBySideRowCount(hunk.Lines)
			}
		}

		// End-of-file gap (not for deleted files).
		if file.Status != model.StatusDeleted && len(file.Hunks) > 0 {
			lastHunk := &file.Hunks[len(file.Hunks)-1]
			eofStart := lastHunk.NewStart + lastHunk.NewCount
			if total, ok := a.FileLineCountCache[fileIdx]; ok && eofStart <= total {
				gap := int(total - eofStart + 1)
				eofGapID := GapID{FileIdx: fileIdx, HunkIdx: len(file.Hunks)}
				topLen := len(a.ExpandedTop[eofGapID])
				botLen := len(a.ExpandedBottom[eofGapID])
				remaining := satSub(gap, topLen+botLen)
				contentLines += topLen + botLen
				contentLines += gapAnnotationLineCount(false, true, remaining)
			}
		}
	}

	return contentLines + spacingLines
}

// sideBySideRowCount is the number of paired rows a hunk renders in
// side-by-side mode: deletions pair with following additions, taking the
// max of the two run lengths.
func sideBySideRowCount(lines []model.DiffLine) int {
	rows := 0
	i := 0
	for i < len(lines) {
		switch lines[i].Origin {
		case model.OriginContext, model.OriginAddition:
			rows++
			i++
		case model.OriginDeletion:
			delEnd := i + 1
			for delEnd < len(lines) && lines[delEnd].Origin == model.OriginDeletion {
				delEnd++
			}
			addEnd := delEnd
			for addEnd < len(lines) && lines[addEnd].Origin == model.OriginAddition {
				addEnd++
			}
			rows += max(delEnd-i, addEnd-delEnd)
			i = addEnd
		}
	}
	return rows
}

// effectiveFileHeight is the render-aware file height that knows about
// single-file view. Multi-file view: same as fileRenderHeight. Single-file
// view: non-current files are 0; the current file is the body (no header)
// plus a one-line banner when the file is reviewed.
func (a *App) effectiveFileHeight(fileIdx int, file *model.DiffFile) int {
	if !a.IsSingleFileView {
		return a.fileRenderHeight(fileIdx, file)
	}
	if fileIdx != a.DiffState.CurrentFileIdx {
		return 0
	}
	banner := 0
	if a.Session.IsFileReviewed(file.DisplayPath()) {
		banner = 1
	}
	return banner + a.fileRenderBodyHeight(fileIdx, file)
}

// EffectiveFileHeight is the exported render-aware file height (see
// effectiveFileHeight).
func (a *App) EffectiveFileHeight(fileIdx int, file *model.DiffFile) int {
	return a.effectiveFileHeight(fileIdx, file)
}

// FileRenderHeight is the exported multi-file rendered height (see
// fileRenderHeight).
func (a *App) FileRenderHeight(fileIdx int, file *model.DiffFile) int {
	return a.fileRenderHeight(fileIdx, file)
}

// updateCurrentFileFromCursor re-derives CurrentFileIdx from the cursor's
// render line in multi-file view. Single-file view is a no-op: the "which
// file is visible" decision is owned by JumpToFile / toggle / list-follow.
func (a *App) updateCurrentFileFromCursor() {
	if a.IsSingleFileView {
		return
	}
	cumulative := a.reviewCommentsRenderHeight()
	if a.DiffState.CursorLine < cumulative {
		if len(a.DiffFiles) > 0 {
			a.DiffState.CurrentFileIdx = 0
			a.FileListState.Select(0)
		}
		return
	}
	for i := range a.DiffFiles {
		height := a.fileRenderHeight(i, &a.DiffFiles[i])
		if cumulative+height > a.DiffState.CursorLine {
			a.DiffState.CurrentFileIdx = i
			a.FileListState.Select(i)
			return
		}
		cumulative += height
	}
	if len(a.DiffFiles) > 0 {
		a.DiffState.CurrentFileIdx = len(a.DiffFiles) - 1
		a.FileListState.Select(len(a.DiffFiles) - 1)
	}
}

// TotalLines is the total rendered line count, kept in lockstep with
// len(LineAnnotations).
func (a *App) TotalLines() int {
	total := a.reviewCommentsRenderHeight()
	for i := range a.DiffFiles {
		total += a.effectiveFileHeight(i, &a.DiffFiles[i])
	}
	return total
}

// MaxCursorLine is the last line the cursor can occupy. If the final
// annotation is a Spacing separator it is not navigable content and is
// excluded.
func (a *App) MaxCursorLine() int {
	total := a.TotalLines()
	if n := len(a.LineAnnotations); n > 0 && a.LineAnnotations[n-1].Kind == AnnSpacing {
		return satSub(total, 2)
	}
	return satSub(total, 1)
}

// MaxScrollOffset allows scrolling until the last line of content is at the
// top of the viewport. This permits empty space below content (e.g. when
// centering the cursor near EOF) while ensuring at least one line of
// content stays visible at the top.
func (a *App) MaxScrollOffset() int {
	return satSub(a.TotalLines(), 1)
}

// SyncViewportWidth updates the viewport width and rebuilds annotations if
// it changed — keeps len(LineAnnotations) in sync with the rendered lines.
func (a *App) SyncViewportWidth(newWidth int) {
	if a.DiffState.ViewportWidth != newWidth {
		a.DiffState.ViewportWidth = newWidth
		a.RebuildAnnotations()
	}
}

// LineAtCursor returns the source line number and side at the cursor when
// it rests on a diff line, preferring the new line number.
func (a *App) LineAtCursor() (uint32, model.LineSide, bool) {
	target := a.DiffState.CursorLine
	if target >= len(a.LineAnnotations) {
		return 0, model.LineSideNew, false
	}
	ann := &a.LineAnnotations[target]
	if ann.Kind != AnnDiffLine && ann.Kind != AnnSideBySideLine {
		return 0, model.LineSideNew, false
	}
	// Prefer the new line number (added/context lines), falling back to old
	// (deleted lines).
	if ann.NewLineno != nil {
		return *ann.NewLineno, model.LineSideNew, true
	}
	if ann.OldLineno != nil {
		return *ann.OldLineno, model.LineSideOld, true
	}
	return 0, model.LineSideNew, false
}
