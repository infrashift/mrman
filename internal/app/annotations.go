// annotations.go ports tuicr's src/app/annotations.rs: the AnnotatedLine
// stream describing what each rendered line represents, rebuilt whenever the
// diff, expansion state, or view mode changes. It is the single source of
// truth for O(1) cursor queries and all scroll math.
package app

import (
	"github.com/infrashift/mrman/internal/model"
)

// AnnKind discriminates the AnnotatedLine variants, one per tuicr enum arm.
type AnnKind int

// Annotated line kinds. The comment and remote-thread kinds exist in the
// enum for forward compatibility, but the M3 builder emits none of them —
// comments land in a later milestone.
const (
	// AnnReviewCommentsHeader is the review comments section header line.
	AnnReviewCommentsHeader AnnKind = iota
	// AnnReviewComment is a review-level comment line (multi-line box).
	AnnReviewComment
	// AnnRemoteReviewSummaryLine is a read-only rendered remote review
	// summary line (PR review body).
	AnnRemoteReviewSummaryLine
	// AnnFileHeader is a file header line.
	AnnFileHeader
	// AnnFileComment is a file-level comment line (multi-line box).
	AnnFileComment
	// AnnExpander is an expander line showing hidden context with a
	// direction arrow.
	AnnExpander
	// AnnHiddenLines is the informational "N lines hidden" line between
	// expanders.
	AnnHiddenLines
	// AnnExpandedContext is an expanded context line (muted text).
	AnnExpandedContext
	// AnnHunkHeader is a hunk header (@@...@@).
	AnnHunkHeader
	// AnnDiffLine is an actual diff line with line numbers.
	AnnDiffLine
	// AnnSideBySideLine is a side-by-side paired diff line.
	AnnSideBySideLine
	// AnnLineComment is a line comment (multi-line box).
	AnnLineComment
	// AnnRemoteThreadLine is a read-only line of a rendered remote review
	// thread.
	AnnRemoteThreadLine
	// AnnBinaryOrEmpty is the binary or empty file indicator.
	AnnBinaryOrEmpty
	// AnnSpacing is the spacing between files.
	AnnSpacing
)

// AnnotatedLine describes one rendered line. Only the fields relevant to
// Kind are meaningful; the rest stay zero.
type AnnotatedLine struct {
	Kind AnnKind
	// FileIdx applies to FileHeader, FileComment, HunkHeader, DiffLine,
	// SideBySideLine, LineComment and BinaryOrEmpty.
	FileIdx int
	// HunkIdx applies to HunkHeader, DiffLine and SideBySideLine.
	HunkIdx int
	// LineIdx applies to DiffLine (index into the hunk's lines) and
	// ExpandedContext (sequential index across top + bottom expansions).
	LineIdx int
	// DelLineIdx / AddLineIdx apply to SideBySideLine.
	DelLineIdx *int
	AddLineIdx *int
	// OldLineno / NewLineno apply to DiffLine and SideBySideLine.
	OldLineno *uint32
	NewLineno *uint32
	// GapID applies to Expander, HiddenLines and ExpandedContext.
	GapID GapID
	// Direction applies to Expander.
	Direction ExpandDirection
	// Count applies to HiddenLines.
	Count int
	// CommentIdx applies to ReviewComment, FileComment and LineComment.
	CommentIdx int
	// ThreadIdx applies to RemoteThreadLine.
	ThreadIdx int
	// SummaryIdx applies to RemoteReviewSummaryLine.
	SummaryIdx int
	// Line and Side apply to LineComment (the anchor line number and side).
	Line uint32
	Side model.LineSide
}

// IsDecoration reports whether the cursor should never rest on this line:
// spacing between files and file header rows.
func (a *AnnotatedLine) IsDecoration() bool {
	return a.Kind == AnnSpacing || a.Kind == AnnFileHeader
}

// annotationFileIdx returns the file index an annotation belongs to, if any.
func annotationFileIdx(a *AnnotatedLine) (int, bool) {
	switch a.Kind {
	case AnnFileHeader, AnnFileComment, AnnHunkHeader, AnnDiffLine,
		AnnSideBySideLine, AnnLineComment, AnnBinaryOrEmpty:
		return a.FileIdx, true
	default:
		return 0, false
	}
}

// calculateGap ports vcs::git::calculate_gap: the number of hidden context
// lines between the end of prev (nil for top-of-file) and currentNewStart.
func calculateGap(prev *model.DiffHunk, currentNewStart uint32) uint32 {
	if prev == nil {
		// Gap from line 1 to first hunk.
		return satSubU32(currentNewStart, 1)
	}
	return satSubU32(currentNewStart, prev.NewStart+prev.NewCount)
}

// gapAnnotationLineCount is the number of expander/hidden-lines rows a gap
// with `remaining` unexpanded lines renders.
func gapAnnotationLineCount(isTopOfFile, isEndOfFile bool, remaining int) int {
	switch {
	case remaining == 0:
		return 0
	case isTopOfFile, isEndOfFile:
		// One directional expander, plus a HiddenLines row when remaining
		// exceeds the batch.
		if remaining > GapExpandBatch {
			return 2
		}
		return 1
	default:
		// Between hunks: down + HiddenLines + up when >= batch, else a
		// single merged expander.
		if remaining >= GapExpandBatch {
			return 3
		}
		return 1
	}
}

// ensureFileLineCountCached populates the file line count cache for one file.
func (a *App) ensureFileLineCountCached(fileIdx int) {
	if !a.eofGapEnabled() {
		return
	}
	if _, ok := a.FileLineCountCache[fileIdx]; ok {
		return
	}
	if fileIdx >= len(a.DiffFiles) {
		return
	}
	file := &a.DiffFiles[fileIdx]
	count, err := a.contextProvider().FileLineCount(file.OldPath, file.NewPath, file.Status)
	if err == nil {
		a.FileLineCountCache[fileIdx] = count
	}
}

// populateFileLineCountCache fills the cache for all eligible files. Only
// enabled for diff sources where the worktree/index is the correct snapshot.
func (a *App) populateFileLineCountCache() {
	a.FileLineCountCache = map[int]uint32{}
	if !a.eofGapEnabled() {
		return
	}
	for fileIdx := range a.DiffFiles {
		file := &a.DiffFiles[fileIdx]
		if len(file.Hunks) > 0 && file.Status != model.StatusDeleted {
			a.ensureFileLineCountCached(fileIdx)
		}
	}
}

// RebuildAnnotations rebuilds the line annotations cache. Call when diff
// files change, expansion state changes, or the diff view mode changes.
// (Later milestones also call it when comments change.)
func (a *App) RebuildAnnotations() {
	if len(a.FileLineCountCache) == 0 {
		a.populateFileLineCountCache()
	}

	a.LineAnnotations = a.LineAnnotations[:0]

	// The review-comments header is omitted in single-file view, so the
	// annotation list mirrors the render.
	if !a.IsSingleFileView {
		a.LineAnnotations = append(a.LineAnnotations, AnnotatedLine{Kind: AnnReviewCommentsHeader})
	}
	// M4 hook: remote review summaries (AnnRemoteReviewSummaryLine),
	// review-level comments (AnnReviewComment) and review-level remote
	// threads (AnnRemoteThreadLine) render here once comments land. The
	// same rows must be mirrored in reviewCommentsRenderHeight.

	for fileIdx := range a.DiffFiles {
		file := &a.DiffFiles[fileIdx]

		// Single-file view renders only the currently focused file, so the
		// annotation stream must skip every other file or click handling
		// lands on lines that aren't visible.
		if a.IsSingleFileView && fileIdx != a.DiffState.CurrentFileIdx {
			continue
		}
		path := file.DisplayPath()

		// File header (only when shown — same gate as the renderer).
		if !a.IsSingleFileView {
			a.LineAnnotations = append(a.LineAnnotations, AnnotatedLine{Kind: AnnFileHeader, FileIdx: fileIdx})
		}

		// If reviewed, skip all content for this file. Single-file view
		// ignores the reviewed-collapse since the user explicitly focused
		// this file.
		if a.Session.IsFileReviewed(path) && !a.IsSingleFileView {
			continue
		}

		// M4 hook: file-level comments (AnnFileComment) render here.

		if file.IsBinary || len(file.Hunks) == 0 {
			a.LineAnnotations = append(a.LineAnnotations, AnnotatedLine{Kind: AnnBinaryOrEmpty, FileIdx: fileIdx})
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
					a.appendGapAnnotations(gapID, int(gap), hunkIdx == 0, false)
				}

				a.LineAnnotations = append(a.LineAnnotations,
					AnnotatedLine{Kind: AnnHunkHeader, FileIdx: fileIdx, HunkIdx: hunkIdx})
				if a.IsHunkReviewed(fileIdx, hunkIdx) {
					continue
				}

				switch a.DiffViewMode {
				case ViewUnified:
					a.buildUnifiedDiffAnnotations(fileIdx, hunkIdx, hunk.Lines)
				case ViewSideBySide:
					a.buildSideBySideAnnotations(fileIdx, hunkIdx, hunk.Lines)
				}
			}

			// End-of-file gap (after all hunks, not for deleted files).
			if file.Status != model.StatusDeleted && len(file.Hunks) > 0 {
				lastHunk := &file.Hunks[len(file.Hunks)-1]
				eofStart := lastHunk.NewStart + lastHunk.NewCount
				if total, ok := a.FileLineCountCache[fileIdx]; ok && eofStart <= total {
					gap := int(total - eofStart + 1)
					eofGapID := GapID{FileIdx: fileIdx, HunkIdx: len(file.Hunks)}
					a.appendGapAnnotations(eofGapID, gap, false, true)
				}
			}
		}

		// Spacing line.
		a.LineAnnotations = append(a.LineAnnotations, AnnotatedLine{Kind: AnnSpacing})
	}
}

// appendGapAnnotations emits the expanded-context rows, expander rows, and
// hidden-lines row for one gap, following tuicr's exact layout rules.
func (a *App) appendGapAnnotations(gapID GapID, gap int, isTopOfFile, isEndOfFile bool) {
	topLen := len(a.ExpandedTop[gapID])
	botLen := len(a.ExpandedBottom[gapID])
	remaining := satSub(gap, topLen+botLen)

	// Sequential line index counter across top + bottom.
	ctxIdx := 0

	// Top expanded lines (down direction).
	for range topLen {
		a.LineAnnotations = append(a.LineAnnotations,
			AnnotatedLine{Kind: AnnExpandedContext, GapID: gapID, LineIdx: ctxIdx})
		ctxIdx++
	}

	// Expanders / hidden lines.
	if remaining > 0 {
		switch {
		case isTopOfFile:
			// Top-of-file: HiddenLines (if > batch) + up expander.
			if remaining > GapExpandBatch {
				a.LineAnnotations = append(a.LineAnnotations,
					AnnotatedLine{Kind: AnnHiddenLines, GapID: gapID, Count: remaining})
			}
			a.LineAnnotations = append(a.LineAnnotations,
				AnnotatedLine{Kind: AnnExpander, GapID: gapID, Direction: ExpandUp})
		case isEndOfFile:
			// End-of-file: down expander + HiddenLines (if > batch).
			a.LineAnnotations = append(a.LineAnnotations,
				AnnotatedLine{Kind: AnnExpander, GapID: gapID, Direction: ExpandDown})
			if remaining > GapExpandBatch {
				a.LineAnnotations = append(a.LineAnnotations,
					AnnotatedLine{Kind: AnnHiddenLines, GapID: gapID, Count: remaining})
			}
		case remaining >= GapExpandBatch:
			// Between-hunk, large: down + HiddenLines + up.
			a.LineAnnotations = append(a.LineAnnotations,
				AnnotatedLine{Kind: AnnExpander, GapID: gapID, Direction: ExpandDown},
				AnnotatedLine{Kind: AnnHiddenLines, GapID: gapID, Count: remaining},
				AnnotatedLine{Kind: AnnExpander, GapID: gapID, Direction: ExpandUp})
		default:
			// Between-hunk, small: merged expander.
			a.LineAnnotations = append(a.LineAnnotations,
				AnnotatedLine{Kind: AnnExpander, GapID: gapID, Direction: ExpandBoth})
		}
	}

	// Bottom expanded lines (up direction).
	for range botLen {
		a.LineAnnotations = append(a.LineAnnotations,
			AnnotatedLine{Kind: AnnExpandedContext, GapID: gapID, LineIdx: ctxIdx})
		ctxIdx++
	}
}

// buildUnifiedDiffAnnotations emits one annotation per diff line.
// M4 hook: line comments (old side then new side) and remote threads
// interleave after each diff line once comments land.
func (a *App) buildUnifiedDiffAnnotations(fileIdx, hunkIdx int, lines []model.DiffLine) {
	for lineIdx := range lines {
		line := &lines[lineIdx]
		a.LineAnnotations = append(a.LineAnnotations, AnnotatedLine{
			Kind:      AnnDiffLine,
			FileIdx:   fileIdx,
			HunkIdx:   hunkIdx,
			LineIdx:   lineIdx,
			OldLineno: line.OldLineno,
			NewLineno: line.NewLineno,
		})
	}
}

// buildSideBySideAnnotations pairs deletions and additions into aligned
// rows. M4 hook: line comments and remote threads interleave per row once
// comments land.
func (a *App) buildSideBySideAnnotations(fileIdx, hunkIdx int, lines []model.DiffLine) {
	i := 0
	for i < len(lines) {
		line := &lines[i]
		switch line.Origin {
		case model.OriginContext:
			idx := i
			a.LineAnnotations = append(a.LineAnnotations, AnnotatedLine{
				Kind:       AnnSideBySideLine,
				FileIdx:    fileIdx,
				HunkIdx:    hunkIdx,
				DelLineIdx: &idx,
				AddLineIdx: &idx,
				OldLineno:  line.OldLineno,
				NewLineno:  line.NewLineno,
			})
			i++

		case model.OriginDeletion:
			// Find consecutive deletions.
			delStart := i
			delEnd := i + 1
			for delEnd < len(lines) && lines[delEnd].Origin == model.OriginDeletion {
				delEnd++
			}
			// Find consecutive additions following deletions.
			addStart := delEnd
			addEnd := addStart
			for addEnd < len(lines) && lines[addEnd].Origin == model.OriginAddition {
				addEnd++
			}

			delCount := delEnd - delStart
			addCount := addEnd - addStart
			maxLines := max(delCount, addCount)

			for offset := range maxLines {
				var delIdx, addIdx *int
				if offset < delCount {
					v := delStart + offset
					delIdx = &v
				}
				if offset < addCount {
					v := addStart + offset
					addIdx = &v
				}

				var oldLineno, newLineno *uint32
				if delIdx != nil {
					oldLineno = lines[*delIdx].OldLineno
				}
				if addIdx != nil {
					newLineno = lines[*addIdx].NewLineno
				}

				a.LineAnnotations = append(a.LineAnnotations, AnnotatedLine{
					Kind:       AnnSideBySideLine,
					FileIdx:    fileIdx,
					HunkIdx:    hunkIdx,
					DelLineIdx: delIdx,
					AddLineIdx: addIdx,
					OldLineno:  oldLineno,
					NewLineno:  newLineno,
				})
			}

			i = addEnd

		case model.OriginAddition:
			idx := i
			a.LineAnnotations = append(a.LineAnnotations, AnnotatedLine{
				Kind:       AnnSideBySideLine,
				FileIdx:    fileIdx,
				HunkIdx:    hunkIdx,
				AddLineIdx: &idx,
				NewLineno:  line.NewLineno,
			})
			i++
		}
	}
}
