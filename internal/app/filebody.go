package app

import "github.com/infrashift/mrman/internal/model"

// fileBodyVisitor receives one file's body in render order: the remote
// threads and file comments above the content, then per hunk the context
// gap, the header and the rows with their line comments, the end-of-file
// gap, and the trailing spacing row.
//
// Two things need this sequence and must agree on it to the row: the
// annotation list the renderer draws from (RebuildAnnotations) and the
// height arithmetic that positions files without building annotations
// (fileRenderBodyHeight). They used to be two hand-mirrored loops with a
// comment asking that they be kept identical; they had already drifted in
// two places. Now there is one walk, and each side is a visitor.
type fileBodyVisitor interface {
	remoteThreads(fileIdx int, threadIdxs []int)
	fileComment(fileIdx, commentIdx int, comment *model.Comment)
	binaryOrEmpty(fileIdx int)
	gap(gapID GapID, gap int, isTopOfFile, isEndOfFile bool)
	hunkHeader(fileIdx, hunkIdx int)
	unifiedLine(fileIdx, hunkIdx, lineIdx int, line *model.DiffLine)
	sideBySideRow(fileIdx, hunkIdx int, delIdx, addIdx *int, oldLineno, newLineno *uint32)
	lineComments(fileIdx int, path string, lineNo *uint32, lineComments map[uint32][]*model.Comment, side model.LineSide)
	spacing()
}

// walkFileBody drives v over the body of file — everything after the file
// header. The reviewed-collapse decision is the caller's: the renderer
// skips a reviewed file's body, single-file view shows it under a banner.
func (a *App) walkFileBody(fileIdx int, file *model.DiffFile, ctx *annBuildCtx, v fileBodyVisitor) {
	path := file.DisplayPath()

	// Remote threads whose line anchor is gone render at file scope, so an
	// outdated discussion stays reachable instead of vanishing.
	v.remoteThreads(fileIdx, a.remoteThreadsByFile[path])

	var lineComments map[uint32][]*model.Comment
	if review := a.Session.File(path); review != nil {
		for commentIdx, comment := range review.FileComments {
			if !commentVisibleWith(comment, ctx.commitSet, ctx.hasCommitSet) {
				continue
			}
			if !commentBelongsToFile(comment, file) {
				continue
			}
			v.fileComment(fileIdx, commentIdx, comment)
		}
		lineComments = review.LineComments
	}

	if file.IsBinary || len(file.Hunks) == 0 {
		v.binaryOrEmpty(fileIdx)
		v.spacing()
		return
	}

	gapsEnabled := a.contextGapsEnabled()
	for hunkIdx := range file.Hunks {
		hunk := &file.Hunks[hunkIdx]

		var prevHunk *model.DiffHunk
		if hunkIdx > 0 {
			prevHunk = &file.Hunks[hunkIdx-1]
		}
		gap := calculateGap(prevHunk, hunk.NewStart)
		gapID := GapID{FileIdx: fileIdx, HunkIdx: hunkIdx}
		if gap > 0 && gapsEnabled && a.ShouldRenderGapBeforeHunk(fileIdx, hunkIdx) {
			v.gap(gapID, int(gap), hunkIdx == 0, false)
		}

		v.hunkHeader(fileIdx, hunkIdx)
		if a.IsHunkReviewed(fileIdx, hunkIdx) {
			continue
		}
		switch a.DiffViewMode {
		case ViewUnified:
			walkUnifiedHunk(fileIdx, path, hunkIdx, hunk.Lines, lineComments, v)
		case ViewSideBySide:
			walkSideBySideHunk(fileIdx, path, hunkIdx, hunk.Lines, lineComments, v)
		}
	}

	// End-of-file gap (after all hunks, not for deleted files).
	if file.Status != model.StatusDeleted {
		lastHunk := &file.Hunks[len(file.Hunks)-1]
		eofStart := lastHunk.NewStart + lastHunk.NewCount
		if total, ok := a.FileLineCountCache[fileIdx]; ok && eofStart <= total {
			v.gap(GapID{FileIdx: fileIdx, HunkIdx: len(file.Hunks)}, int(total-eofStart+1), false, true)
		}
	}

	v.spacing()
}

// walkUnifiedHunk emits one row per diff line, each followed by the
// comments on its old side and then its new side.
func walkUnifiedHunk(fileIdx int, path string, hunkIdx int, lines []model.DiffLine,
	lineComments map[uint32][]*model.Comment, v fileBodyVisitor) {
	for lineIdx := range lines {
		line := &lines[lineIdx]
		v.unifiedLine(fileIdx, hunkIdx, lineIdx, line)
		v.lineComments(fileIdx, path, line.OldLineno, lineComments, model.LineSideOld)
		v.lineComments(fileIdx, path, line.NewLineno, lineComments, model.LineSideNew)
	}
}

// walkSideBySideHunk pairs deletions with the additions that follow them
// into aligned rows; context and lone additions are rows of their own.
func walkSideBySideHunk(fileIdx int, path string, hunkIdx int, lines []model.DiffLine,
	lineComments map[uint32][]*model.Comment, v fileBodyVisitor) {
	i := 0
	for i < len(lines) {
		line := &lines[i]
		switch line.Origin {
		case model.OriginContext:
			idx := i
			v.sideBySideRow(fileIdx, hunkIdx, &idx, &idx, line.OldLineno, line.NewLineno)
			v.lineComments(fileIdx, path, line.NewLineno, lineComments, model.LineSideNew)
			i++

		case model.OriginDeletion:
			delStart := i
			delEnd := i + 1
			for delEnd < len(lines) && lines[delEnd].Origin == model.OriginDeletion {
				delEnd++
			}
			addStart := delEnd
			addEnd := addStart
			for addEnd < len(lines) && lines[addEnd].Origin == model.OriginAddition {
				addEnd++
			}
			delCount := delEnd - delStart
			addCount := addEnd - addStart

			for offset := range max(delCount, addCount) {
				var delIdx, addIdx *int
				var oldLineno, newLineno *uint32
				if offset < delCount {
					idx := delStart + offset
					delIdx = &idx
					oldLineno = lines[idx].OldLineno
				}
				if offset < addCount {
					idx := addStart + offset
					addIdx = &idx
					newLineno = lines[idx].NewLineno
				}
				v.sideBySideRow(fileIdx, hunkIdx, delIdx, addIdx, oldLineno, newLineno)
				v.lineComments(fileIdx, path, oldLineno, lineComments, model.LineSideOld)
				v.lineComments(fileIdx, path, newLineno, lineComments, model.LineSideNew)
			}
			i = addEnd

		case model.OriginAddition:
			idx := i
			v.sideBySideRow(fileIdx, hunkIdx, nil, &idx, nil, line.NewLineno)
			v.lineComments(fileIdx, path, line.NewLineno, lineComments, model.LineSideNew)
			i++
		}
	}
}

// annotationVisitor appends the rows RebuildAnnotations renders from.
type annotationVisitor struct {
	a   *App
	ctx *annBuildCtx
}

func (v *annotationVisitor) remoteThreads(fileIdx int, threadIdxs []int) {
	v.a.pushRemoteThreads(fileIdx, threadIdxs)
}

func (v *annotationVisitor) fileComment(fileIdx, commentIdx int, comment *model.Comment) {
	for range CommentDisplayLines(comment, v.a.DiffState.ViewportWidth) {
		v.a.LineAnnotations = append(v.a.LineAnnotations,
			AnnotatedLine{Kind: AnnFileComment, FileIdx: fileIdx, CommentIdx: commentIdx})
	}
}

func (v *annotationVisitor) binaryOrEmpty(fileIdx int) {
	v.a.LineAnnotations = append(v.a.LineAnnotations, AnnotatedLine{Kind: AnnBinaryOrEmpty, FileIdx: fileIdx})
}

func (v *annotationVisitor) gap(gapID GapID, gap int, isTopOfFile, isEndOfFile bool) {
	v.a.appendGapAnnotations(gapID, gap, isTopOfFile, isEndOfFile)
}

func (v *annotationVisitor) hunkHeader(fileIdx, hunkIdx int) {
	v.a.LineAnnotations = append(v.a.LineAnnotations,
		AnnotatedLine{Kind: AnnHunkHeader, FileIdx: fileIdx, HunkIdx: hunkIdx})
}

func (v *annotationVisitor) unifiedLine(fileIdx, hunkIdx, lineIdx int, line *model.DiffLine) {
	v.a.LineAnnotations = append(v.a.LineAnnotations, AnnotatedLine{
		Kind:      AnnDiffLine,
		FileIdx:   fileIdx,
		HunkIdx:   hunkIdx,
		LineIdx:   lineIdx,
		OldLineno: line.OldLineno,
		NewLineno: line.NewLineno,
	})
}

func (v *annotationVisitor) sideBySideRow(fileIdx, hunkIdx int, delIdx, addIdx *int, oldLineno, newLineno *uint32) {
	v.a.LineAnnotations = append(v.a.LineAnnotations, AnnotatedLine{
		Kind:       AnnSideBySideLine,
		FileIdx:    fileIdx,
		HunkIdx:    hunkIdx,
		DelLineIdx: delIdx,
		AddLineIdx: addIdx,
		OldLineno:  oldLineno,
		NewLineno:  newLineno,
	})
}

func (v *annotationVisitor) lineComments(fileIdx int, path string, lineNo *uint32,
	lineComments map[uint32][]*model.Comment, side model.LineSide) {
	v.a.pushLineComments(fileIdx, path, lineNo, lineComments, side, v.ctx)
}

func (v *annotationVisitor) spacing() {
	v.a.LineAnnotations = append(v.a.LineAnnotations, AnnotatedLine{Kind: AnnSpacing})
}

// heightVisitor counts the rows the same walk would have appended.
type heightVisitor struct {
	a        *App
	ctx      *annBuildCtx
	content  int
	comments int
}

func (v *heightVisitor) remoteThreads(_ int, threadIdxs []int) {
	v.comments += v.a.remoteThreadsHeight(threadIdxs)
}

func (v *heightVisitor) fileComment(_, _ int, comment *model.Comment) {
	v.comments += CommentDisplayLines(comment, v.a.DiffState.ViewportWidth)
}

func (v *heightVisitor) binaryOrEmpty(int) { v.content++ }

func (v *heightVisitor) gap(gapID GapID, gap int, isTopOfFile, isEndOfFile bool) {
	topLen := len(v.a.ExpandedTop[gapID])
	botLen := len(v.a.ExpandedBottom[gapID])
	v.content += topLen + botLen
	v.content += gapAnnotationLineCount(isTopOfFile, isEndOfFile, satSub(gap, topLen+botLen))
}

func (v *heightVisitor) hunkHeader(_, _ int) { v.content++ }

func (v *heightVisitor) unifiedLine(_, _, _ int, _ *model.DiffLine) { v.content++ }

func (v *heightVisitor) sideBySideRow(_, _ int, _, _ *int, _, _ *uint32) { v.content++ }

func (v *heightVisitor) lineComments(_ int, path string, lineNo *uint32,
	lineComments map[uint32][]*model.Comment, side model.LineSide) {
	v.comments += v.a.lineCommentsHeightAt(path, lineComments, lineNo, side, v.ctx.commitSet, v.ctx.hasCommitSet)
}

func (v *heightVisitor) spacing() { v.content++ }
