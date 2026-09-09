// anchors.go validates a reopened session's line anchors against the diff
// actually in front of the reviewer.
//
// A session outlives the diff it was written against. An amend, a rebase, or
// a plain edit can move the line a comment was pinned to, or delete it
// outright, and a line number on its own cannot tell those two apart from
// "nothing happened". That is why comment creation snapshots the anchored
// line's content (model.LineContext); this file is the consumer of that
// snapshot.
//
// Every line comment gets one of three outcomes: the snapshot still matches
// the line it is keyed by (nothing to do), the snapshot is found at exactly
// one other line (the comment is re-anchored there), or it is not findable
// (the comment is flagged outdated and never silently posted inline).
//
// Flagging is the point of the exercise. A stale comment that announces
// itself is safe to carry across a rewrite; one that quietly keeps its old
// line number is criticism pointing at code it was not written about.

package app

import (
	"fmt"
	"slices"
	"sort"

	"github.com/infrashift/mrman/internal/model"
)

// AnchorVerdict is the outcome of validating one comment's line anchor
// against the current diff.
type AnchorVerdict int

// Anchor verdicts.
const (
	// AnchorUnverified means no verdict was reached: the comment carries no
	// LineContext snapshot (it predates snapshotting, or an agent wrote it
	// through `mrman review add`), or its file is not in the diff on screen.
	// Absence of evidence, not evidence of staleness.
	AnchorUnverified AnchorVerdict = iota
	// AnchorIntact means the line the comment is keyed by still holds the
	// content that was snapshotted when the comment was written.
	AnchorIntact
	// AnchorMoved means the snapshotted content was found at exactly one
	// other line and the comment was re-anchored to it.
	AnchorMoved
	// AnchorGone means the snapshotted content is nowhere in the file's
	// current diff.
	AnchorGone
	// AnchorAmbiguous means the snapshotted content appears at several lines,
	// so which one the comment was about cannot be recovered. Treated as
	// outdated rather than guessed at: a `}` that shows up eleven times tells
	// us nothing about which `}` was under discussion.
	AnchorAmbiguous
)

// IsOutdated reports whether the anchor can no longer be trusted, using the
// same word the forge side uses for a remote thread whose anchor has gone
// stale (forge.RemoteReviewThread.IsOutdated).
func (v AnchorVerdict) IsOutdated() bool {
	return v == AnchorGone || v == AnchorAmbiguous
}

// Label is the short badge text for a verdict, empty when there is nothing
// worth saying.
func (v AnchorVerdict) Label() string {
	switch v {
	case AnchorMoved:
		return "moved"
	case AnchorGone, AnchorAmbiguous:
		return "outdated"
	}
	return ""
}

// AnchorStats summarizes one validation pass.
type AnchorStats struct {
	// Checked is how many comments had a snapshot to compare.
	Checked int
	// Moved is how many were re-anchored to a new line.
	Moved int
	// Outdated is how many could not be placed at all.
	Outdated int
}

// Message renders the stats for the status line, empty when nothing moved
// and nothing went stale — the common case deserves silence.
func (s AnchorStats) Message() string {
	switch {
	case s.Moved > 0 && s.Outdated > 0:
		return fmt.Sprintf("%d comment(s) re-anchored, %d outdated", s.Moved, s.Outdated)
	case s.Moved > 0:
		return fmt.Sprintf("%d comment(s) re-anchored", s.Moved)
	case s.Outdated > 0:
		return fmt.Sprintf("%d comment(s) outdated — the code they point at changed", s.Outdated)
	}
	return ""
}

// AnchorVerdictFor returns the verdict recorded for a comment id by the last
// validation pass.
func (a *App) AnchorVerdictFor(commentID string) AnchorVerdict {
	return a.anchorVerdicts[commentID]
}

// HasOutdatedAnchor reports whether a comment's anchor failed validation.
func (a *App) HasOutdatedAnchor(commentID string) bool {
	return a.anchorVerdicts[commentID].IsOutdated()
}

// ClearAnchorVerdicts drops every verdict. Callers that swap in a diff of a
// different scope must call this: a verdict computed against the full review
// target says nothing about a narrowed one, and keeping it would let a
// comment show "outdated" only because the reviewer filtered it out of view.
func (a *App) ClearAnchorVerdicts() {
	a.anchorVerdicts = nil
	a.AnchorStats = AnchorStats{}
}

// ValidateCommentAnchors compares every line comment's snapshot with the
// current diff, re-anchoring what it can place and flagging what it cannot.
// It replaces any previous verdicts and returns what it found.
//
// Comments whose file is absent from the diff on screen are left without a
// verdict rather than marked outdated: a path filter, an ignore rule, or a
// narrower review target all remove whole files, and none of them says
// anything about whether the code a comment describes still exists.
func (a *App) ValidateCommentAnchors() AnchorStats {
	a.anchorVerdicts = map[string]AnchorVerdict{}
	stats := AnchorStats{}
	if a.Session == nil {
		a.AnchorStats = stats
		return stats
	}

	paths := make([]string, 0, len(a.Session.Files))
	for path := range a.Session.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		review := a.Session.Files[path]
		if review == nil || len(review.LineComments) == 0 {
			continue
		}
		fileIdx := a.fileIdxForPath(path)
		if fileIdx < 0 {
			continue
		}
		file := &a.DiffFiles[fileIdx]

		lines := make([]uint32, 0, len(review.LineComments))
		for line := range review.LineComments {
			lines = append(lines, line)
		}
		slices.Sort(lines)

		// Re-anchoring rekeys LineComments, so the moves are collected first
		// and applied after the walk rather than mutating the map under it.
		type pendingMove struct {
			from    uint32
			comment *model.Comment
			target  *model.LineContext
			side    model.LineSide
		}
		var moves []pendingMove

		for _, line := range lines {
			for _, c := range review.LineComments[line] {
				verdict, target, side := classifyAnchor(file, c, line)
				if verdict == AnchorUnverified {
					continue
				}
				a.anchorVerdicts[c.ID] = verdict
				stats.Checked++
				switch {
				case verdict == AnchorMoved:
					stats.Moved++
					moves = append(moves, pendingMove{from: line, comment: c, target: target, side: side})
				case verdict.IsOutdated():
					stats.Outdated++
				}
			}
		}
		for _, mv := range moves {
			if reanchor(review, mv.from, mv.comment, mv.target, mv.side) {
				continue
			}
			// The move was promised by the classifier and could not be
			// carried out. Rather than report a relocation that did not
			// happen, the comment falls back to the flagged case — the same
			// treatment as an anchor that could not be found at all.
			a.anchorVerdicts[mv.comment.ID] = AnchorGone
			stats.Moved--
			stats.Outdated++
		}
	}

	a.AnchorStats = stats
	return stats
}

// classifyAnchor decides one comment's verdict. On AnchorMoved it also
// returns the diff line to re-anchor to and the side it was matched on.
func classifyAnchor(file *model.DiffFile, c *model.Comment, line uint32) (AnchorVerdict, *model.LineContext, model.LineSide) {
	side := model.LineSideNew
	if c.Side != nil {
		side = *c.Side
	}
	if c.LineContext == nil {
		return AnchorUnverified, nil, side
	}
	if current := lineContextIn(file, line, side); current != nil &&
		current.Content == c.LineContext.Content {
		return AnchorIntact, nil, side
	}
	matches := linesMatching(file, side, c.LineContext.Content)
	switch len(matches) {
	case 0:
		return AnchorGone, nil, side
	case 1:
		return AnchorMoved, matches[0], side
	default:
		return AnchorAmbiguous, nil, side
	}
}

// linesMatching returns every diff line in file whose content equals content
// and which is numbered on side.
func linesMatching(file *model.DiffFile, side model.LineSide, content string) []*model.LineContext {
	var matches []*model.LineContext
	for hunkIdx := range file.Hunks {
		for _, dl := range file.Hunks[hunkIdx].Lines {
			if dl.Content != content || linenoOn(&dl, side) == nil {
				continue
			}
			matches = append(matches, &model.LineContext{
				NewLine: clonePtrU32(dl.NewLineno),
				OldLine: clonePtrU32(dl.OldLineno),
				Content: dl.Content,
			})
			// Two candidates already settle it as ambiguous; a file of
			// identical lines does not need to be collected in full.
			if len(matches) > 1 {
				return matches
			}
		}
	}
	return matches
}

// reanchor rekeys a comment onto the line its snapshot was found at and
// refreshes the snapshot to match. It reports whether the move happened, so a
// caller never counts a relocation it did not perform.
//
// A range comment is keyed by (and snapshots) its end line, so only the end
// is verified; the start follows by the same delta. That is an inference, but
// the alternative is leaving the range on a line number nothing corroborates.
func reanchor(review *model.FileReview, from uint32, c *model.Comment, target *model.LineContext, side model.LineSide) bool {
	to := target.NewLine
	if side == model.LineSideOld {
		to = target.OldLine
	}
	if to == nil {
		return false
	}
	if !removeLineComment(review, from, c.ID) {
		return false
	}
	if c.LineRange != nil {
		span := c.LineRange.End - c.LineRange.Start
		start := uint32(1)
		if *to > span {
			start = *to - span
		}
		*c.LineRange = model.NewLineRange(start, *to)
	}
	c.LineContext = target.Clone()
	review.AddLineComment(*to, c)
	return true
}

// removeLineComment unhooks one comment from a line key, dropping the key
// when it empties. Reports whether it was there.
func removeLineComment(review *model.FileReview, line uint32, id string) bool {
	comments := review.LineComments[line]
	for i, c := range comments {
		if c.ID != id {
			continue
		}
		comments = append(comments[:i], comments[i+1:]...)
		if len(comments) == 0 {
			delete(review.LineComments, line)
		} else {
			review.LineComments[line] = comments
		}
		return true
	}
	return false
}

// linenoOn returns the line's number on the requested diff side.
func linenoOn(dl *model.DiffLine, side model.LineSide) *uint32 {
	if side == model.LineSideOld {
		return dl.OldLineno
	}
	return dl.NewLineno
}
