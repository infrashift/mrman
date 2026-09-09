// search.go ports tuicr's src/app/search.rs: case-insensitive substring
// search over the annotation stream (diff panel) and the help overlay, with
// from-cursor semantics and n/N stepping.

package app

import (
	"fmt"
	"strings"
)

// findSearchMatch scans line indices for a case-insensitive substring
// match. Forward searches run from the start index to the end; backward
// searches run from the start index to the beginning (no wrap-around,
// matching tuicr).
func findSearchMatch(totalLines, startIdx int, forward, includeCurrent bool, pattern string, lineText func(int) (string, bool)) (int, bool) {
	if totalLines == 0 {
		return 0, false
	}

	normalizedPattern := strings.ToLower(pattern)
	matches := func(lineIdx int) bool {
		text, ok := lineText(lineIdx)
		return ok && strings.Contains(strings.ToLower(text), normalizedPattern)
	}
	startIdx = min(startIdx, totalLines-1)
	if forward {
		first := startIdx
		if !includeCurrent {
			first = startIdx + 1
		}
		for lineIdx := first; lineIdx < totalLines; lineIdx++ {
			if matches(lineIdx) {
				return lineIdx, true
			}
		}
		return 0, false
	}
	first := startIdx
	if !includeCurrent {
		if startIdx == 0 {
			return 0, false
		}
		first = startIdx - 1
	}
	for lineIdx := first; lineIdx >= 0; lineIdx-- {
		if matches(lineIdx) {
			return lineIdx, true
		}
	}
	return 0, false
}

// search finds the next help match from the current position and centers it
// in the viewport; reports whether a match was found.
func (h *HelpState) search(pattern string, forward, includeCurrent bool) bool {
	startIdx := h.ScrollOffset
	if h.CurrentMatchLine != nil {
		startIdx = *h.CurrentMatchLine
	}
	line, ok := findSearchMatch(
		len(h.SearchableLines), startIdx, forward, includeCurrent, pattern,
		func(lineIdx int) (string, bool) {
			if lineIdx < len(h.SearchableLines) {
				return h.SearchableLines[lineIdx], true
			}
			return "", false
		})
	if !ok {
		return false
	}

	h.CurrentMatchLine = &line
	maxOffset := satSub(len(h.SearchableLines), h.ViewportHeight)
	h.ScrollOffset = min(satSub(line, h.ViewportHeight/2), maxOffset)
	return true
}

// SearchInHelpFromScroll runs the buffered search pattern over the help
// overlay from the current scroll position.
func (a *App) SearchInHelpFromScroll() bool {
	pattern := a.SearchBuffer
	if strings.TrimSpace(pattern) == "" {
		a.SetMessage("Search pattern is empty")
		return false
	}

	a.HelpState.LastSearchPattern = &pattern
	a.HelpState.CurrentMatchLine = nil
	if a.HelpState.search(pattern, true, true) {
		return true
	}
	a.SetMessage(fmt.Sprintf("No help matches for %q", pattern))
	return false
}

// SearchNextInHelp steps to the next help match (n).
func (a *App) SearchNextInHelp() bool {
	if a.HelpState.LastSearchPattern == nil {
		a.SetMessage("No previous help search")
		return false
	}
	pattern := *a.HelpState.LastSearchPattern
	if a.HelpState.search(pattern, true, false) {
		return true
	}
	a.SetMessage(fmt.Sprintf("No further help matches for %q", pattern))
	return false
}

// SearchPrevInHelp steps to the previous help match (N).
func (a *App) SearchPrevInHelp() bool {
	if a.HelpState.LastSearchPattern == nil {
		a.SetMessage("No previous help search")
		return false
	}
	pattern := *a.HelpState.LastSearchPattern
	if a.HelpState.search(pattern, false, false) {
		return true
	}
	a.SetMessage(fmt.Sprintf("No earlier help matches for %q", pattern))
	return false
}

// SearchInDiffFromCursor runs the buffered search pattern over the diff
// from the cursor (including the current line).
func (a *App) SearchInDiffFromCursor() bool {
	pattern := a.SearchBuffer
	if strings.TrimSpace(pattern) == "" {
		a.SetMessage("Search pattern is empty")
		return false
	}

	a.LastSearchPattern = &pattern
	return a.searchInDiff(pattern, a.DiffState.CursorLine, true, true)
}

// SearchNextInDiff steps to the next diff match (n).
func (a *App) SearchNextInDiff() bool {
	if a.LastSearchPattern == nil {
		a.SetMessage("No previous search")
		return false
	}
	return a.searchInDiff(*a.LastSearchPattern, a.DiffState.CursorLine, true, false)
}

// SearchPrevInDiff steps to the previous diff match (N).
func (a *App) SearchPrevInDiff() bool {
	if a.LastSearchPattern == nil {
		a.SetMessage("No previous search")
		return false
	}
	return a.searchInDiff(*a.LastSearchPattern, a.DiffState.CursorLine, false, false)
}

func (a *App) searchInDiff(pattern string, startIdx int, forward, includeCurrent bool) bool {
	totalLines := a.TotalLines()
	if totalLines == 0 {
		a.SetMessage("No diff content to search")
		return false
	}

	lineIdx, ok := findSearchMatch(totalLines, startIdx, forward, includeCurrent, pattern,
		a.lineTextForSearch)
	if !ok {
		a.SetMessage(fmt.Sprintf("No matches for %q", pattern))
		return false
	}

	a.DiffState.CursorLine = lineIdx
	a.ensureCursorVisible()
	a.CenterCursor()
	a.updateCurrentFileFromCursor()
	return true
}

// lineTextForSearch is the searchable text of one annotation. Spacing rows
// (and, until later milestones, remote-thread rows) have none.
func (a *App) lineTextForSearch(lineIdx int) (string, bool) {
	if lineIdx >= len(a.LineAnnotations) {
		return "", false
	}
	ann := &a.LineAnnotations[lineIdx]
	switch ann.Kind {
	case AnnReviewCommentsHeader:
		return "Review comments", true

	case AnnReviewComment:
		if ann.CommentIdx < len(a.Session.ReviewComments) {
			return a.Session.ReviewComments[ann.CommentIdx].Content, true
		}
		return "", false

	case AnnFileHeader:
		if ann.FileIdx < len(a.DiffFiles) {
			file := &a.DiffFiles[ann.FileIdx]
			return fmt.Sprintf("%s [%c]", file.DisplayPath(), file.Status.Char()), true
		}
		return "", false

	case AnnFileComment:
		if ann.FileIdx >= len(a.DiffFiles) {
			return "", false
		}
		review := a.Session.File(a.DiffFiles[ann.FileIdx].DisplayPath())
		if review != nil && ann.CommentIdx < len(review.FileComments) {
			return review.FileComments[ann.CommentIdx].Content, true
		}
		return "", false

	case AnnLineComment:
		if ann.FileIdx >= len(a.DiffFiles) {
			return "", false
		}
		review := a.Session.File(a.DiffFiles[ann.FileIdx].DisplayPath())
		if review == nil {
			return "", false
		}
		comments := review.LineComments[ann.Line]
		if ann.CommentIdx < len(comments) {
			return comments[ann.CommentIdx].Content, true
		}
		return "", false

	case AnnExpander:
		arrow := "↓"
		switch ann.Direction {
		case ExpandUp:
			arrow = "↑"
		case ExpandBoth:
			arrow = "↕"
		case ExpandDown:
		}
		gap, ok := a.GapSize(ann.GapID)
		if !ok {
			return "", false
		}
		topLen := len(a.ExpandedTop[ann.GapID])
		botLen := len(a.ExpandedBottom[ann.GapID])
		remaining := satSub(int(gap), topLen+botLen)
		count := min(remaining, GapExpandBatch)
		return fmt.Sprintf("... %s expand (%d lines) ...", arrow, count), true

	case AnnHiddenLines:
		return fmt.Sprintf("... %d lines hidden ...", ann.Count), true

	case AnnExpandedContext:
		if line := a.GetExpandedLine(ann.GapID, ann.LineIdx); line != nil {
			return line.Content, true
		}
		return "", false

	case AnnHunkHeader:
		if ann.FileIdx < len(a.DiffFiles) && ann.HunkIdx < len(a.DiffFiles[ann.FileIdx].Hunks) {
			return a.DiffFiles[ann.FileIdx].Hunks[ann.HunkIdx].Header, true
		}
		return "", false

	case AnnDiffLine:
		if ann.FileIdx < len(a.DiffFiles) && ann.HunkIdx < len(a.DiffFiles[ann.FileIdx].Hunks) {
			lines := a.DiffFiles[ann.FileIdx].Hunks[ann.HunkIdx].Lines
			if ann.LineIdx < len(lines) {
				return lines[ann.LineIdx].Content, true
			}
		}
		return "", false

	case AnnBinaryOrEmpty:
		if ann.FileIdx >= len(a.DiffFiles) {
			return "", false
		}
		file := &a.DiffFiles[ann.FileIdx]
		switch {
		case file.IsTooLarge:
			return "(file too large to display)", true
		case file.IsBinary:
			return "(binary file)", true
		default:
			return "(no changes)", true
		}

	case AnnSideBySideLine:
		if ann.FileIdx >= len(a.DiffFiles) || ann.HunkIdx >= len(a.DiffFiles[ann.FileIdx].Hunks) {
			return "", false
		}
		lines := a.DiffFiles[ann.FileIdx].Hunks[ann.HunkIdx].Lines
		content := func(idx *int) string {
			if idx != nil && *idx < len(lines) {
				return lines[*idx].Content
			}
			return ""
		}
		return content(ann.DelLineIdx) + " " + content(ann.AddLineIdx), true

	case AnnRemoteReviewSummaryLine, AnnRemoteThreadLine:
		// M5 hook: remote review summaries/threads become searchable once
		// forge comments land.
		return "", false

	case AnnSpacing:
		return "", false
	}
	return "", false
}
