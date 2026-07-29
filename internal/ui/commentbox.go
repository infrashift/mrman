package ui

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/render"
)

// borderPrefix is the comment box body lead-in: 4-space pad + │ + 2 spaces.
const borderPrefix = "    │  "

// commentTypeColor maps a comment type to its theme slot.
func (p *DiffPane) commentTypeColor(t model.CommentType) color.Color {
	switch t.ID() {
	case "note":
		return p.Theme.CommentNote
	case "suggestion":
		return p.Theme.CommentSuggestion
	case "issue":
		return p.Theme.CommentIssue
	case "praise":
		return p.Theme.CommentPraise
	}
	return p.Theme.FgSecondary
}

// resolveCommentRow locates the comment for a comment annotation row and the
// row's display-line index within the comment's box (0 = top border).
func resolveCommentRow(a *app.App, idx int) (*model.Comment, int, int) {
	ann := &a.LineAnnotations[idx]
	displayIdx := 0
	for i := idx - 1; i >= 0; i-- {
		prev := &a.LineAnnotations[i]
		if prev.Kind != ann.Kind || prev.CommentIdx != ann.CommentIdx ||
			prev.FileIdx != ann.FileIdx || prev.Line != ann.Line || prev.Side != ann.Side {
			break
		}
		displayIdx++
	}

	var comment *model.Comment
	switch ann.Kind {
	case app.AnnReviewComment:
		if ann.CommentIdx < len(a.Session.ReviewComments) {
			comment = a.Session.ReviewComments[ann.CommentIdx]
		}
	case app.AnnFileComment:
		if review := a.Session.File(a.DiffFiles[ann.FileIdx].DisplayPath()); review != nil &&
			ann.CommentIdx < len(review.FileComments) {
			comment = review.FileComments[ann.CommentIdx]
		}
	case app.AnnLineComment:
		if review := a.Session.File(a.DiffFiles[ann.FileIdx].DisplayPath()); review != nil {
			comments := review.LineComments[ann.Line]
			if ann.CommentIdx < len(comments) {
				comment = comments[ann.CommentIdx]
			}
		}
	}
	if comment == nil {
		return nil, 0, 0
	}
	return comment, displayIdx, app.CommentDisplayLines(comment, a.DiffState.ViewportWidth)
}

// commentBoxRow renders one display line of a comment box.
func (p *DiffPane) commentBoxRow(a *app.App, ann *app.AnnotatedLine, idx int, ind render.Span, width int) render.LogicalLine {
	t := p.Theme
	comment, displayIdx, total := resolveCommentRow(a, idx)
	if comment == nil {
		return render.LogicalLine{Kind: render.RowBlank, Spans: []render.Span{ind}}
	}
	borderStyle := render.Style{Fg: t.FgPrimary, Bold: true}
	typeStyle := render.Style{Fg: p.commentTypeColor(comment.CommentType), Bold: true}

	switch displayIdx {
	case 0:
		// Top border: corner + badge + line info + rule fill.
		corner := "╭"
		if comment.LineRange != nil {
			corner = "├"
		}
		badge := ""
		if !comment.CommentType.IsNone() {
			badge = "[" + comment.CommentType.Display()
			if comment.Author != "" && comment.Author != a.Username {
				badge += " @" + comment.Author
			}
			badge += "] "
		} else if comment.Author != "" && comment.Author != a.Username {
			badge = "[@" + comment.Author + "] "
		}
		lineInfo := ""
		if ann.Kind == app.AnnLineComment {
			if comment.LineRange != nil && !comment.LineRange.IsSingle() {
				lineInfo = fmt.Sprintf("L%d-L%d ", comment.LineRange.Start, comment.LineRange.End)
			} else {
				lineInfo = fmt.Sprintf("L%d ", ann.Line)
			}
		}
		head := "    " + corner + "── "
		fillWidth := width - render.StringWidth(head) - render.StringWidth(badge) -
			render.StringWidth(lineInfo) - render.StringWidth(ind.Text)
		if fillWidth < 0 {
			fillWidth = 0
		}
		return render.LogicalLine{Kind: render.RowCommentTop, Spans: []render.Span{
			ind,
			{Text: head, Style: borderStyle},
			{Text: badge, Style: typeStyle},
			{Text: lineInfo, Style: render.Style{Fg: t.FgDim}},
			{Text: strings.Repeat("─", fillWidth), Style: borderStyle},
		}}
	case total - 1:
		fillWidth := width - 5 - render.StringWidth(ind.Text)
		if fillWidth < 0 {
			fillWidth = 0
		}
		return render.LogicalLine{Kind: render.RowCommentBottom, Spans: []render.Span{
			ind,
			{Text: "    ╰" + strings.Repeat("─", fillWidth), Style: borderStyle},
		}}
	default:
		segments := commentSegments(a, comment)
		text := ""
		if segIdx := displayIdx - 1; segIdx >= 0 && segIdx < len(segments) {
			text = segments[segIdx]
		}
		return render.LogicalLine{Kind: render.RowCommentMiddle, Spans: []render.Span{
			ind,
			{Text: borderPrefix, Style: borderStyle},
			{Text: text, Style: render.Style{Fg: t.FgPrimary}},
		}}
	}
}

// commentSegments returns the wrapped body segments of a comment at the
// current viewport width.
func commentSegments(a *app.App, comment *model.Comment) []string {
	contentArea := a.DiffState.ViewportWidth - 10
	if contentArea < 1 {
		contentArea = 1
	}
	var segments []string
	for _, line := range strings.Split(comment.Content, "\n") {
		segments = append(segments, app.WrapSegments(line, contentArea)...)
	}
	return segments
}

// commentInputOverlay renders the comment composition box shown while in
// Comment mode: header with mode/type, body, bottom rule. Rendered above
// the status bar spanning the given width.
func (p *DiffPane) commentInputOverlay(a *app.App, vim *vimState, width int) []string {
	t := p.Theme
	emitter := &render.Emitter{}
	borderStyle := render.Style{Fg: t.BorderFocused, Bold: true}
	typeStyle := render.Style{Fg: p.commentTypeColor(a.CommentType), Bold: true}

	verb := "Add"
	if a.EditingCommentID != nil {
		verb = "Edit"
	}
	badge := ""
	if !a.CommentType.IsNone() {
		badge = "[" + a.CommentType.Display() + "] "
	}
	modeTag := ""
	hint := "(Tab:type Enter:save Shift-Enter:newline Esc:cancel)"
	if vim != nil {
		modeTag = "[" + vim.label() + "] "
		hint = "(i:insert  Ctrl-S:save  Esc:normal  :w save  :q discard)"
	}
	scope := "line"
	switch {
	case a.CommentIsReviewLevel:
		scope = "review"
	case a.CommentIsFileLevel:
		scope = "file"
	case a.CommentLineRange != nil:
		scope = fmt.Sprintf("L%d-L%d", a.CommentLineRange.Range.Start, a.CommentLineRange.Range.End)
	case a.CommentLine != nil:
		scope = fmt.Sprintf("L%d", a.CommentLine.Line)
	}

	head := fmt.Sprintf("    ╭── %s %s comment ", verb, scope)
	lines := []string{emitter.Line([]render.Span{
		{Text: head, Style: borderStyle},
		{Text: badge, Style: typeStyle},
		{Text: modeTag, Style: typeStyle},
		{Text: hint, Style: render.Style{Fg: t.FgDim}},
	})}

	contentArea := width - 10
	if contentArea < 1 {
		contentArea = 1
	}
	body := a.CommentBuffer
	if body == "" {
		lines = append(lines, emitter.Line([]render.Span{
			{Text: borderPrefix, Style: borderStyle},
			{Text: "Type your comment...", Style: render.Style{Fg: t.FgDim}},
		}))
	} else {
		cursorPos := a.CommentCursor
		offset := 0
		for _, rawLine := range strings.Split(body, "\n") {
			for _, seg := range app.WrapSegments(rawLine, contentArea) {
				spans := []render.Span{{Text: borderPrefix, Style: borderStyle}}
				segStart, segEnd := offset, offset+len(seg)
				if cursorPos >= segStart && cursorPos <= segEnd {
					before := seg[:cursorPos-segStart]
					after := seg[cursorPos-segStart:]
					cursorChar, rest := " ", ""
					if after != "" {
						runes := []rune(after)
						cursorChar, rest = string(runes[0]), string(runes[1:])
					}
					spans = append(spans,
						render.Span{Text: before, Style: render.Style{Fg: t.FgPrimary}},
						render.Span{Text: cursorChar, Style: render.Style{Fg: t.CursorColor, Underline: true}},
						render.Span{Text: rest, Style: render.Style{Fg: t.FgPrimary}})
				} else {
					spans = append(spans, render.Span{Text: seg, Style: render.Style{Fg: t.FgPrimary}})
				}
				lines = append(lines, emitter.Line(spans))
				offset += len(seg)
			}
			offset++ // the newline
		}
	}

	fill := width - 5
	if fill < 0 {
		fill = 0
	}
	lines = append(lines, emitter.Line([]render.Span{
		{Text: "    ╰" + strings.Repeat("─", fill), Style: borderStyle},
	}))
	return lines
}
