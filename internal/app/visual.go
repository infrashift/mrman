// visual.go ports tuicr's src/app/visual.rs: the visual selection model
// (anchor/head SelPoint pairs), enter/exit/extend transitions, the
// line-range projection used to anchor range comments, and char-accurate
// copy extraction. The app layer returns the selected text; putting it on
// the clipboard is the UI's job.
package app

import (
	"fmt"
	"strings"

	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
)

// SelPoint is one end of a visual selection: an annotation row, a char
// offset into its content, and the diff side the selection reads from.
type SelPoint struct {
	AnnotationIdx int
	CharOffset    int
	Side          model.LineSide
}

// VisualSelection is an anchor/head pair; head follows the cursor.
type VisualSelection struct {
	Anchor SelPoint
	Head   SelPoint
}

// CollapsedSelection is a zero-width selection at point.
func CollapsedSelection(point SelPoint) VisualSelection {
	return VisualSelection{Anchor: point, Head: point}
}

// Ordered returns (start, end) with start <= end by (annotation, offset).
func (v VisualSelection) Ordered() (start, end SelPoint) {
	a, h := v.Anchor, v.Head
	if a.AnnotationIdx < h.AnnotationIdx ||
		(a.AnnotationIdx == h.AnnotationIdx && a.CharOffset <= h.CharOffset) {
		return a, h
	}
	return h, a
}

// CharRange is the char range [lo, hi) of totalChars covered by this
// selection on annotation annIdx. Annotations strictly between start and
// end are fully covered.
func (v VisualSelection) CharRange(annIdx, totalChars int) (lo, hi int) {
	start, end := v.Ordered()
	lo = 0
	if annIdx == start.AnnotationIdx {
		lo = min(start.CharOffset, totalChars)
	}
	hi = totalChars
	if annIdx == end.AnnotationIdx {
		hi = min(end.CharOffset, totalChars)
	}
	return lo, hi
}

// charSlice slices s by char (rune) offsets [loChar, hiChar).
func charSlice(s string, loChar, hiChar int) string {
	if hiChar <= loChar {
		return ""
	}
	loByte, hiByte := len(s), len(s)
	charIdx := 0
	for byteIdx := range s {
		if charIdx == loChar {
			loByte = byteIdx
		}
		if charIdx == hiChar {
			hiByte = byteIdx
			break
		}
		charIdx++
	}
	if loByte >= hiByte {
		return ""
	}
	return s[loByte:hiByte]
}

// ContentForSide is the selectable content of annotation annIdx. In
// side-by-side mode it picks Old or New per side, falling back to the other
// pane when the requested one is empty; unified diff rows ignore side.
func (a *App) ContentForSide(annIdx int, side model.LineSide) (string, bool) {
	if annIdx >= len(a.LineAnnotations) {
		return "", false
	}
	ann := &a.LineAnnotations[annIdx]
	switch ann.Kind {
	case AnnDiffLine:
		if ann.FileIdx >= len(a.DiffFiles) {
			return "", false
		}
		file := &a.DiffFiles[ann.FileIdx]
		if ann.HunkIdx >= len(file.Hunks) || ann.LineIdx >= len(file.Hunks[ann.HunkIdx].Lines) {
			return "", false
		}
		return file.Hunks[ann.HunkIdx].Lines[ann.LineIdx].Content, true
	case AnnSideBySideLine:
		if ann.FileIdx >= len(a.DiffFiles) {
			return "", false
		}
		file := &a.DiffFiles[ann.FileIdx]
		if ann.HunkIdx >= len(file.Hunks) {
			return "", false
		}
		lines := file.Hunks[ann.HunkIdx].Lines
		content := func(idx *int) (string, bool) {
			if idx == nil || *idx >= len(lines) {
				return "", false
			}
			return lines[*idx].Content, true
		}
		add, addOK := content(ann.AddLineIdx)
		del, delOK := content(ann.DelLineIdx)
		if side == model.LineSideNew {
			if addOK {
				return add, true
			}
			return del, delOK
		}
		if delOK {
			return del, true
		}
		return add, addOK
	case AnnExpandedContext:
		if line := a.GetExpandedLine(ann.GapID, ann.LineIdx); line != nil {
			return line.Content, true
		}
		return "", false
	default:
		return "", false
	}
}

// atomicTextForAnnotation is the copy text for annotations rendered outside
// the content gutter (hunk headers, file headers). The selection's char
// range is meaningless for these — they are emitted whole or not at all.
func (a *App) atomicTextForAnnotation(annIdx int) (string, bool) {
	if annIdx >= len(a.LineAnnotations) {
		return "", false
	}
	ann := &a.LineAnnotations[annIdx]
	switch ann.Kind {
	case AnnHunkHeader:
		if ann.FileIdx >= len(a.DiffFiles) {
			return "", false
		}
		file := &a.DiffFiles[ann.FileIdx]
		if ann.HunkIdx >= len(file.Hunks) {
			return "", false
		}
		return file.Hunks[ann.HunkIdx].Header, true
	case AnnFileHeader:
		if ann.FileIdx >= len(a.DiffFiles) {
			return "", false
		}
		file := &a.DiffFiles[ann.FileIdx]
		if file.IsCommitMessage {
			return file.DisplayPath(), true
		}
		return fmt.Sprintf("%s [%c]", file.DisplayPath(), file.Status.Char()), true
	default:
		return "", false
	}
}

// AnnotationContentLen is the char count of the annotation's selectable
// content on side (0 when it has none).
func (a *App) AnnotationContentLen(annIdx int, side model.LineSide) int {
	content, ok := a.ContentForSide(annIdx, side)
	if !ok {
		return 0
	}
	return len([]rune(content))
}

// RowSelection is how much of one annotation row the active selection covers,
// for the renderer to paint.
type RowSelection struct {
	// Lo, Hi are rune offsets into the row's selectable content, ignored when
	// WholeRow is set.
	Lo, Hi int
	// Side is the diff side the selection reads from, which picks the column
	// to paint in side-by-side mode.
	Side model.LineSide
	// WholeRow marks rows copied atomically (hunk and file headers). They have
	// no content gutter to measure offsets against, so they highlight entire.
	WholeRow bool
}

// SelectionForRow reports the part of annotation annIdx the active selection
// covers. Its branching mirrors CopyVisualSelection exactly, so the highlight
// always shows what a yank would actually put on the clipboard.
func (a *App) SelectionForRow(annIdx int) (RowSelection, bool) {
	sel := a.VisualSelection
	if sel == nil {
		return RowSelection{}, false
	}
	start, end := sel.Ordered()
	if annIdx < start.AnnotationIdx || annIdx > end.AnnotationIdx {
		return RowSelection{}, false
	}
	side := sel.Anchor.Side
	if _, ok := a.ContentForSide(annIdx, side); ok {
		lo, hi := sel.CharRange(annIdx, a.AnnotationContentLen(annIdx, side))
		if hi <= lo {
			return RowSelection{}, false
		}
		return RowSelection{Lo: lo, Hi: hi, Side: side}, true
	}
	if _, ok := a.atomicTextForAnnotation(annIdx); ok {
		return RowSelection{Side: side, WholeRow: true}, true
	}
	return RowSelection{}, false
}

// EnterVisualModeAtCursor starts visual mode with the whole cursor line
// selected.
func (a *App) EnterVisualModeAtCursor() {
	// A hunk header carries no line number of its own, so a selection
	// anchored on one cannot project onto source lines at all. Start from the
	// hunk's first line instead, which is what "select from here" means when
	// here is the header.
	if cur := a.DiffState.CursorLine; cur < len(a.LineAnnotations) &&
		a.LineAnnotations[cur].Kind == AnnHunkHeader {
		if first, _, ok := a.hunkLineBounds(cur); ok {
			a.DiffState.CursorLine = first
			a.ensureCursorVisible()
		}
	}
	idx := a.DiffState.CursorLine
	side := model.LineSideNew
	if _, s, ok := a.LineAtCursor(); ok {
		side = s
	}
	length := a.AnnotationContentLen(idx, side)
	a.InputMode = input.ModeVisualSelect
	a.VisualSelection = &VisualSelection{
		Anchor: SelPoint{AnnotationIdx: idx, CharOffset: 0, Side: side},
		Head:   SelPoint{AnnotationIdx: idx, CharOffset: length, Side: side},
	}
}

// ExitVisualMode leaves visual mode and drops the selection.
func (a *App) ExitVisualMode() {
	a.InputMode = input.ModeNormal
	a.VisualSelection = nil
}

// GetVisualSelection is the active selection, nil outside visual mode.
func (a *App) GetVisualSelection() *VisualSelection {
	if a.InputMode != input.ModeVisualSelect {
		return nil
	}
	return a.VisualSelection
}

// ExtendVisualToCursor grows the selection line-wise to the cursor: full
// lines between anchor and cursor, with the offsets oriented by direction.
func (a *App) ExtendVisualToCursor() {
	sel := a.VisualSelection
	if sel == nil {
		return
	}
	anchorIdx := sel.Anchor.AnnotationIdx
	cursorIdx := a.DiffState.CursorLine
	side := sel.Anchor.Side
	anchorLen := a.AnnotationContentLen(anchorIdx, side)
	cursorLen := a.AnnotationContentLen(cursorIdx, side)
	anchorChar, headChar := 0, cursorLen
	if cursorIdx < anchorIdx {
		anchorChar, headChar = anchorLen, 0
	}
	a.VisualSelection = &VisualSelection{
		Anchor: SelPoint{AnnotationIdx: anchorIdx, CharOffset: anchorChar, Side: side},
		Head:   SelPoint{AnnotationIdx: cursorIdx, CharOffset: headChar, Side: side},
	}
}

// hunkLineBounds returns the first and last annotation indices of the diff
// lines belonging to the hunk that annotation annIdx sits in. Reports false
// when annIdx is not part of a hunk, and for a hunk marked reviewed, which
// renders its header and no lines at all.
func (a *App) hunkLineBounds(annIdx int) (first, last int, ok bool) {
	if annIdx < 0 || annIdx >= len(a.LineAnnotations) {
		return 0, 0, false
	}
	ann := &a.LineAnnotations[annIdx]
	switch ann.Kind {
	case AnnHunkHeader, AnnDiffLine, AnnSideBySideLine:
		return a.hunkLineBoundsAt(ann.FileIdx, ann.HunkIdx)
	default:
		return 0, 0, false
	}
}

// hunkLineBoundsAt is hunkLineBounds addressed by (file, hunk) index.
func (a *App) hunkLineBoundsAt(fileIdx, hunkIdx int) (first, last int, ok bool) {
	for i := range a.LineAnnotations {
		other := &a.LineAnnotations[i]
		if other.Kind != AnnDiffLine && other.Kind != AnnSideBySideLine {
			continue
		}
		if other.FileIdx != fileIdx || other.HunkIdx != hunkIdx {
			continue
		}
		if !ok {
			first, ok = i, true
		}
		last = i
	}
	return first, last, ok
}

// moveVisualHeadTo parks the cursor on annotation idx and re-extends the
// selection to it, the way j and k do.
func (a *App) moveVisualHeadTo(idx int) {
	a.DiffState.CursorLine = idx
	a.ensureCursorVisible()
	a.updateCurrentFileFromCursor()
	a.ExtendVisualToCursor()
}

// ExtendVisualToNextHunk grows the selection to the last line of the hunk
// under the cursor, and from there to the end of each following hunk — the
// visual-mode counterpart of ] in normal mode.
//
// It stops on diff lines rather than hunk headers because a header cannot
// project onto a source line, which would leave the selection uncommentable.
func (a *App) ExtendVisualToNextHunk() {
	if a.VisualSelection == nil {
		return
	}
	cursor := a.DiffState.CursorLine
	if _, last, ok := a.hunkLineBounds(cursor); ok && cursor < last {
		a.moveVisualHeadTo(last)
		return
	}
	// Already at the end of this hunk, or on a collapsed one that renders no
	// lines: the next hunk with a body is the next stop.
	for i := cursor + 1; i < len(a.LineAnnotations); i++ {
		ann := &a.LineAnnotations[i]
		if ann.Kind != AnnDiffLine && ann.Kind != AnnSideBySideLine {
			continue
		}
		if _, last, ok := a.hunkLineBoundsAt(ann.FileIdx, ann.HunkIdx); ok {
			a.moveVisualHeadTo(last)
			return
		}
	}
}

// ExtendVisualToPrevHunk is ExtendVisualToNextHunk backwards: to the first
// line of the hunk under the cursor, then to each preceding hunk's first line.
func (a *App) ExtendVisualToPrevHunk() {
	if a.VisualSelection == nil {
		return
	}
	cursor := a.DiffState.CursorLine
	if first, _, ok := a.hunkLineBounds(cursor); ok && cursor > first {
		a.moveVisualHeadTo(first)
		return
	}
	for i := cursor - 1; i >= 0; i-- {
		ann := &a.LineAnnotations[i]
		if ann.Kind != AnnDiffLine && ann.Kind != AnnSideBySideLine {
			continue
		}
		if first, _, ok := a.hunkLineBoundsAt(ann.FileIdx, ann.HunkIdx); ok {
			a.moveVisualHeadTo(first)
			return
		}
	}
}

// annotationLineForSide is the source line number of annotation idx on
// side, when it is a diff row.
func (a *App) annotationLineForSide(idx int, side model.LineSide) (uint32, bool) {
	if idx >= len(a.LineAnnotations) {
		return 0, false
	}
	ann := &a.LineAnnotations[idx]
	if ann.Kind != AnnDiffLine && ann.Kind != AnnSideBySideLine {
		return 0, false
	}
	var lineno *uint32
	if side == model.LineSideNew {
		lineno = ann.NewLineno
	} else {
		lineno = ann.OldLineno
	}
	if lineno == nil {
		return 0, false
	}
	return *lineno, true
}

// VisualSelectionLineRange projects the active selection onto source line
// numbers: the inclusive range between the start and end rows on the
// anchor's side.
func (a *App) VisualSelectionLineRange() (model.LineRange, model.LineSide, bool) {
	sel := a.GetVisualSelection()
	if sel == nil {
		return model.LineRange{}, model.LineSideNew, false
	}
	start, end := sel.Ordered()
	startLn, ok := a.annotationLineForSide(start.AnnotationIdx, start.Side)
	if !ok {
		return model.LineRange{}, model.LineSideNew, false
	}
	endLn, ok := a.annotationLineForSide(end.AnnotationIdx, end.Side)
	if !ok {
		return model.LineRange{}, model.LineSideNew, false
	}
	return model.NewLineRange(startLn, endLn), start.Side, true
}

// CopyVisualSelection extracts the selected source text char-accurately,
// joining rows with newlines. Returns the text and its char count; the UI
// layer owns putting it on the clipboard.
func (a *App) CopyVisualSelection() (string, int, error) {
	sel := a.VisualSelection
	if sel == nil {
		return "", 0, nil
	}
	start, end := sel.Ordered()
	side := sel.Anchor.Side
	var out strings.Builder
	emitted := 0
	for idx := start.AnnotationIdx; idx <= end.AnnotationIdx; idx++ {
		var snippet string
		if content, ok := a.ContentForSide(idx, side); ok {
			total := len([]rune(content))
			lo, hi := sel.CharRange(idx, total)
			snippet = charSlice(content, lo, hi)
		} else if text, ok := a.atomicTextForAnnotation(idx); ok {
			snippet = text
		} else {
			continue
		}
		if emitted > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(snippet)
		emitted++
	}
	text := out.String()
	if text == "" {
		return "", 0, nil
	}
	return text, len([]rune(text)), nil
}

// EnterCommentFromVisual converts the visual selection into a range-comment
// input: the range anchors the comment, its end line is the display anchor.
func (a *App) EnterCommentFromVisual() {
	rng, side, ok := a.VisualSelectionLineRange()
	if !ok {
		a.SetWarning("Invalid visual selection")
		a.ExitVisualMode()
		return
	}
	a.CommentLineRange = &CommentRangeAnchor{Range: rng, Side: side}
	a.CommentLine = &CommentAnchor{Line: rng.End, Side: side}
	a.InputMode = input.ModeComment
	a.DiffState.ScrollX = 0
	a.CommentBuffer = ""
	a.CommentCursor = 0
	a.CommentType = a.DefaultCommentType()
	a.CommentIsReviewLevel = false
	a.CommentIsFileLevel = false
	a.VisualSelection = nil
}
