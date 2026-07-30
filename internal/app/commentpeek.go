// commentpeek.go backs the read-only comment peek panel: the way to read a
// comment whose file you already marked reviewed, without toggling r off and
// losing your place.
package app

import (
	"fmt"
	"strings"

	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
)

// peekContextRadius is how many diff lines of context the panel shows on
// either side of a comment's anchor. Three is enough to recognise where you
// are without turning the panel into a second diff pane.
const peekContextRadius = 3

// CommentPeekState is the open peek panel: which navigator item it is
// showing, plus its own scroll offset. Nil when the panel is closed.
type CommentPeekState struct {
	Item CommentNavigatorItem
	// Lines is the rendered body: context rows and the comment, built once on
	// open so scrolling does not re-derive it.
	Lines []PeekLine
	// AnchorRow indexes Lines for the commented line, so the renderer can
	// mark it.
	AnchorRow      int
	ScrollOffset   int
	ViewportHeight int
}

// PeekLineKind discriminates a peek row for styling.
type PeekLineKind int

// Peek row kinds.
const (
	// PeekContext is a line of the file around the anchor.
	PeekContext PeekLineKind = iota
	// PeekAnchor is the commented line itself.
	PeekAnchor
	// PeekCommentHeader is the author/type line above a comment body.
	PeekCommentHeader
	// PeekCommentBody is one line of comment text.
	PeekCommentBody
	// PeekSeparator is a blank spacer.
	PeekSeparator
	// PeekNotice explains why there is no context to show.
	PeekNotice
)

// PeekLine is one row of the peek panel.
type PeekLine struct {
	Kind PeekLineKind
	// Lineno is the file line number for context and anchor rows, 0 when the
	// row is not a file line.
	Lineno uint32
	Text   string
	// CommentType styles a comment header row.
	CommentType model.CommentType
}

// OpenCommentPeek opens the peek panel for a navigator item. Returns false
// when there is nothing to show, so the caller can fall back to jumping.
func (a *App) OpenCommentPeek(item CommentNavigatorItem) bool {
	lines, anchorRow := a.buildPeekLines(item)
	if len(lines) == 0 {
		return false
	}
	a.CommentPeek = &CommentPeekState{Item: item, Lines: lines, AnchorRow: anchorRow}
	a.InputMode = input.ModeCommentPeek
	return true
}

// CloseCommentPeek dismisses the panel and returns to normal mode.
func (a *App) CloseCommentPeek() {
	a.CommentPeek = nil
	a.InputMode = input.ModeNormal
}

// PeekScroll moves the panel's scroll offset by delta, clamped.
func (a *App) PeekScroll(delta int) {
	p := a.CommentPeek
	if p == nil {
		return
	}
	maxOffset := satSub(len(p.Lines), max(p.ViewportHeight, 1))
	p.ScrollOffset = min(max(p.ScrollOffset+delta, 0), maxOffset)
}

// PeekTitle is the panel's border title: the comment's location.
func (a *App) PeekTitle() string {
	p := a.CommentPeek
	if p == nil {
		return ""
	}
	switch {
	case p.Item.Path != nil && p.Item.Line != nil:
		return fmt.Sprintf(" %s:%d ", *p.Item.Path, *p.Item.Line)
	case p.Item.Path != nil:
		return fmt.Sprintf(" %s ", *p.Item.Path)
	}
	return " review comment "
}

// buildPeekLines renders the panel body: file context around the anchor with
// the comment(s) at that location beneath it.
func (a *App) buildPeekLines(item CommentNavigatorItem) (lines []PeekLine, anchorRow int) {
	anchorRow = -1
	if item.Path == nil {
		return a.peekCommentLines(item), -1
	}
	fileIdx := a.fileIdxForPath(*item.Path)
	if fileIdx < 0 {
		return a.peekCommentLines(item), -1
	}

	if item.Line != nil {
		context, anchor := a.peekContextLines(fileIdx, *item.Line, item.Side)
		lines = append(lines, context...)
		anchorRow = anchor
	}
	if len(lines) == 0 {
		lines = append(lines, PeekLine{
			Kind: PeekNotice,
			Text: "no file context for this comment",
		})
	}
	lines = append(lines, PeekLine{Kind: PeekSeparator})
	return append(lines, a.peekCommentLines(item)...), anchorRow
}

// peekContextLines pulls the anchor line and peekContextRadius rows either
// side out of the in-memory diff, so the panel needs no forge round trip even
// for a folded file.
func (a *App) peekContextLines(fileIdx int, line uint32, side *model.LineSide) ([]PeekLine, int) {
	rows, anchorIdx := a.flattenDiffLines(fileIdx, line, side)
	if anchorIdx < 0 {
		return nil, -1
	}

	start := max(anchorIdx-peekContextRadius, 0)
	end := min(anchorIdx+peekContextRadius+1, len(rows))
	out := make([]PeekLine, 0, end-start)
	anchorRow := -1
	for i := start; i < end; i++ {
		kind := PeekContext
		if i == anchorIdx {
			kind = PeekAnchor
			anchorRow = len(out)
		}
		out = append(out, PeekLine{Kind: kind, Lineno: rows[i].lineno, Text: rows[i].text})
	}
	return out, anchorRow
}

// peekCommentLines renders the comment itself: a header naming the author and
// type, then its body. Remote items render the forge thread's root comment.
func (a *App) peekCommentLines(item CommentNavigatorItem) []PeekLine {
	if item.IsRemote {
		return a.peekRemoteLines(item)
	}
	comment := a.commentForNavigatorKey(item.Key)
	if comment == nil {
		return nil
	}
	// The peek panel puts the anchored source line right next to the comment,
	// so an anchor that no longer checks out shows the wrong code as though it
	// were the subject. Say so in the header.
	label := peekAuthorLabel(comment.Author, a.CommentTypeLabel(comment.CommentType))
	if verdict := a.AnchorVerdictFor(comment.ID).Label(); verdict != "" {
		label += " (" + verdict + ")"
	}
	return append([]PeekLine{{
		Kind:        PeekCommentHeader,
		Text:        label,
		CommentType: comment.CommentType,
	}}, peekBodyLines(comment.Content)...)
}

// peekRemoteLines renders a forge thread: every comment in it, oldest first,
// so a peeked discussion reads in order.
func (a *App) peekRemoteLines(item CommentNavigatorItem) []PeekLine {
	threads := a.VisibleRemoteThreads()
	if item.Key.RemoteIdx < 0 || item.Key.RemoteIdx >= len(threads) {
		return nil
	}
	thread := &threads[item.Key.RemoteIdx]
	var lines []PeekLine
	for i := range thread.Comments {
		c := &thread.Comments[i]
		label := peekAuthorLabel(c.Author, "")
		if i > 0 {
			label = "↳ " + label
		}
		lines = append(lines, PeekLine{Kind: PeekCommentHeader, Text: label})
		lines = append(lines, peekBodyLines(c.Body)...)
	}
	return lines
}

// commentForNavigatorKey resolves a local navigator key back to its comment.
func (a *App) commentForNavigatorKey(key CommentNavigatorKey) *model.Comment {
	switch key.Scope {
	case NavScopeReview:
		if key.CommentIdx < len(a.Session.ReviewComments) {
			return a.Session.ReviewComments[key.CommentIdx]
		}
	case NavScopeFile:
		if review := a.reviewForFileIdx(key.FileIdx); review != nil &&
			key.CommentIdx < len(review.FileComments) {
			return review.FileComments[key.CommentIdx]
		}
	case NavScopeLine:
		if review := a.reviewForFileIdx(key.FileIdx); review != nil {
			comments := review.LineComments[key.Line]
			if key.CommentIdx < len(comments) {
				return comments[key.CommentIdx]
			}
		}
	}
	return nil
}

func (a *App) reviewForFileIdx(fileIdx int) *model.FileReview {
	if fileIdx < 0 || fileIdx >= len(a.DiffFiles) {
		return nil
	}
	return a.Session.File(a.DiffFiles[fileIdx].DisplayPath())
}

func (a *App) fileIdxForPath(path string) int {
	for i := range a.DiffFiles {
		if a.DiffFiles[i].DisplayPath() == path {
			return i
		}
	}
	return -1
}

func peekAuthorLabel(author, typeLabel string) string {
	if author == "" {
		author = "user"
	}
	if typeLabel != "" {
		return fmt.Sprintf("[%s] @%s", typeLabel, author)
	}
	return "@" + author
}

func peekBodyLines(content string) []PeekLine {
	var lines []PeekLine
	for line := range strings.SplitSeq(content, "\n") {
		lines = append(lines, PeekLine{Kind: PeekCommentBody, Text: line})
	}
	return lines
}

// flatDiffLine is one diff row flattened out of a file's hunks, carrying the
// line number on the requested side.
type flatDiffLine struct {
	lineno uint32
	text   string
	ok     bool // the line exists on the requested side
}

// flattenDiffLines flattens a file's hunks into one sequence and reports the
// index of the row anchored at line on side, or -1 when that line is not in
// the diff. Flattening across hunks is deliberate: the diff pane shows hunks
// as one continuous body, so context should read the same way.
func (a *App) flattenDiffLines(fileIdx int, line uint32, side *model.LineSide) ([]flatDiffLine, int) {
	if fileIdx < 0 || fileIdx >= len(a.DiffFiles) {
		return nil, -1
	}
	file := &a.DiffFiles[fileIdx]
	wantOld := side != nil && *side == model.LineSideOld

	var rows []flatDiffLine
	anchorIdx := -1
	for hunkIdx := range file.Hunks {
		for _, dl := range file.Hunks[hunkIdx].Lines {
			no := dl.NewLineno
			if wantOld {
				no = dl.OldLineno
			}
			row := flatDiffLine{text: dl.Content}
			if no != nil {
				row.lineno, row.ok = *no, true
			}
			if row.ok && row.lineno == line && anchorIdx < 0 {
				anchorIdx = len(rows)
			}
			rows = append(rows, row)
		}
	}
	return rows, anchorIdx
}

// LineContextAt snapshots the diff line anchored at line on side: both line
// numbers and the line's content, as they are right now.
//
// This is what lets a later session distinguish "the anchor still points at
// the code this comment was written about" from "the line moved" and from
// "the code is gone" — a distinction the line number alone cannot make, and
// the reason a review must not silently carry anchors across a rewrite.
//
// Returns nil when the line is not in the current diff, so a comment that
// could not be snapshotted is simply unverifiable rather than wrongly marked.
func (a *App) LineContextAt(path string, line uint32, side model.LineSide) *model.LineContext {
	fileIdx := a.fileIdxForPath(path)
	if fileIdx < 0 {
		return nil
	}
	return lineContextIn(&a.DiffFiles[fileIdx], line, side)
}

// lineContextIn is LineContextAt against an already-resolved file, so the
// anchor validation pass does not repeat the path lookup per comment.
func lineContextIn(file *model.DiffFile, line uint32, side model.LineSide) *model.LineContext {
	for hunkIdx := range file.Hunks {
		for _, dl := range file.Hunks[hunkIdx].Lines {
			if no := linenoOn(&dl, side); no == nil || *no != line {
				continue
			}
			return &model.LineContext{
				NewLine: clonePtrU32(dl.NewLineno),
				OldLine: clonePtrU32(dl.OldLineno),
				Content: dl.Content,
			}
		}
	}
	return nil
}

func clonePtrU32(p *uint32) *uint32 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
