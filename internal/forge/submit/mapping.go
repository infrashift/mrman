// Package submit holds the forge-agnostic comment-mapping pipeline, ported
// from tuicr's src/forge/submit.rs. It converts local-draft model.Comments
// plus the parsed PR diff into neutral InlineComments that each forge driver
// encodes into its own wire format, and builds the review body text with the
// "Unplaced comments" section.
package submit

import (
	"github.com/infrashift/mrman/internal/model"
)

// Side is the neutral diff side an inline comment anchors to. Drivers map
// it to their wire encoding (GitHub LEFT/RIGHT, GitLab old_line/new_line,
// Forgejo old_position/new_position, ADO left/right file positions).
type Side string

// Neutral sides.
const (
	// SideOld anchors to the base side of the diff (deleted lines).
	SideOld Side = "old"
	// SideNew anchors to the head side of the diff (added/context lines).
	SideNew Side = "new"
)

// SideFromLineSide converts the model's line side to the neutral side.
func SideFromLineSide(s model.LineSide) Side {
	if s == model.LineSideOld {
		return SideOld
	}
	return SideNew
}

// InlineComment is a single inline review comment ready for a driver to
// serialize. Bodies already include the "[TYPE]" prefix when the active
// configuration enables it.
type InlineComment struct {
	// Path is the display path of the file the comment anchors to.
	Path string
	// Line is the anchor line; the range end for multi-line comments.
	Line uint32
	// Side is the diff side Line refers to.
	Side Side
	// CounterpartLine is the line number on the other diff side for
	// context (unchanged) lines, nil for purely added or deleted lines.
	// GitLab and Forgejo need both sides for context lines; GitHub
	// ignores it.
	CounterpartLine *uint32
	// StartLine is the multi-line range start, nil for single-line
	// comments.
	StartLine *uint32
	// StartSide is the side of StartLine, nil for single-line comments.
	StartSide *Side
	// OldPath is the base-side path when the file was renamed or copied
	// under a new name; nil when both sides share Path.
	OldPath *string
	// Body is the comment text to post.
	Body string
	// CommentID is the source model.Comment ID this inline was derived
	// from, used after a successful submit to flip the comment's
	// lifecycle state. Internal — drivers must not send it.
	CommentID string
}

// UnmappableReason says why the mapper could not produce an inline comment
// for a local comment.
type UnmappableReason int

// Unmappable reasons.
const (
	// MixedSideRange means the range spans both diff sides (or lacks a
	// side); inline range comments must stay on a single side.
	MixedSideRange UnmappableReason = iota
	// FileLevelNoAnchor means a file-level comment found no first-valid
	// line on the New side to anchor to.
	FileLevelNoAnchor
	// BinaryFile means the file is binary; no anchor can be derived.
	BinaryFile
	// TooLargeFile means the file exceeded the too-large threshold and
	// was not diffed.
	TooLargeFile
	// LineNotInDiff means the anchor line is outside every hunk.
	LineNotInDiff
	// StaleAnchor means the comment's line anchor failed validation against
	// the current diff: the code it was written about moved out of reach or
	// is no longer identifiable. The line number would still map, which is
	// precisely why this is refused rather than posted — an inline comment on
	// the wrong line is worse than one in the summary body.
	StaleAnchor
)

// HumanLabel returns the short human-readable reason shown by the resolver.
func (r UnmappableReason) HumanLabel() string {
	switch r {
	case MixedSideRange:
		return "range spans both diff sides"
	case FileLevelNoAnchor:
		return "no valid anchor line"
	case BinaryFile:
		return "binary file"
	case TooLargeFile:
		return "file too large"
	case LineNotInDiff:
		return "line not in current diff"
	case StaleAnchor:
		return "anchored line changed since the comment was written"
	}
	return "unmappable"
}

// anchorKind discriminates CommentAnchor variants.
type anchorKind int

const (
	anchorFileLevel anchorKind = iota
	anchorLine
	anchorRange
)

// CommentAnchor says where a local comment is anchored. The caller knows
// this from how it walked the session (file comments vs line-comment map
// keys); supplying it explicitly avoids inferring file-level-ness from
// missing Comment fields.
type CommentAnchor struct {
	kind anchorKind
	line uint32
	side model.LineSide
}

// FileLevelAnchor is a file-level comment with no line anchor; mapping
// falls back to the file's first valid New-side line.
func FileLevelAnchor() CommentAnchor {
	return CommentAnchor{kind: anchorFileLevel}
}

// LineAnchor is a single-line comment anchored at line on side.
func LineAnchor(line uint32, side model.LineSide) CommentAnchor {
	return CommentAnchor{kind: anchorLine, line: line, side: side}
}

// RangeAnchor is a multi-line range comment; the range itself comes from
// the comment's LineRange field.
func RangeAnchor() CommentAnchor {
	return CommentAnchor{kind: anchorRange}
}

// UnmappableItem bundles an unmappable comment with its file path and
// reason for the resolver UI.
type UnmappableItem struct {
	Comment *model.Comment
	File    string
	Reason  UnmappableReason
}

// MappedComment is the outcome of mapping one local comment against the
// displayed diff: exactly one of Inline or Unmappable is non-nil.
type MappedComment struct {
	Inline     *InlineComment
	Unmappable *UnmappableItem
}

// ResolverAction is what the resolver decided to do with an unmappable
// comment.
type ResolverAction int

// Resolver actions.
const (
	// MoveToSummary renders the comment into the review body under
	// "Unplaced comments". Default action.
	MoveToSummary ResolverAction = iota
	// Omit drops the comment from this submit entirely.
	Omit
)

// MovedItem is a single entry in the "Unplaced comments" section of the
// review body.
type MovedItem struct {
	Comment *model.Comment
	File    string
}

// MapComment maps a single local comment to either an inline comment or an
// unmappable outcome. file must be the diff file that produced the comment;
// the lookup is the caller's responsibility.
func MapComment(comment *model.Comment, anchor CommentAnchor, file *model.DiffFile, commentTypePrefix bool) MappedComment {
	path := file.DisplayPath()

	if file.IsBinary {
		return unmappable(comment, path, BinaryFile)
	}
	if file.IsTooLarge {
		return unmappable(comment, path, TooLargeFile)
	}

	oldPath := renamedOldPath(file)
	switch anchor.kind {
	case anchorFileLevel:
		line, ok := file.FirstValidLine(model.LineSideNew)
		if !ok {
			return unmappable(comment, path, FileLevelNoAnchor)
		}
		// Look the anchor line up again so context lines carry both
		// side numbers for position-based forges.
		counterpart, _ := findLineWithCounterpart(file, line, model.LineSideNew)
		return inline(&InlineComment{
			Path:            path,
			Line:            line,
			Side:            SideNew,
			CounterpartLine: counterpart,
			OldPath:         oldPath,
			Body:            BuildInlineBody(comment, true, commentTypePrefix),
			CommentID:       comment.ID,
		})
	case anchorRange:
		if comment.LineRange == nil {
			return unmappable(comment, path, MixedSideRange)
		}
		return mapRange(comment, file, commentTypePrefix, *comment.LineRange)
	case anchorLine:
		counterpart, found := findLineWithCounterpart(file, anchor.line, anchor.side)
		if !found {
			return unmappable(comment, path, LineNotInDiff)
		}
		return inline(&InlineComment{
			Path:            path,
			Line:            anchor.line,
			Side:            SideFromLineSide(anchor.side),
			CounterpartLine: counterpart,
			OldPath:         oldPath,
			Body:            BuildInlineBody(comment, false, commentTypePrefix),
			CommentID:       comment.ID,
		})
	}
	return unmappable(comment, path, LineNotInDiff)
}

func inline(c *InlineComment) MappedComment {
	return MappedComment{Inline: c}
}

func unmappable(comment *model.Comment, file string, reason UnmappableReason) MappedComment {
	return MappedComment{Unmappable: &UnmappableItem{Comment: comment, File: file, Reason: reason}}
}

// renamedOldPath returns the base-side path for renamed/copied files when it
// differs from the display path, nil otherwise so consumers fall back to
// Path for both sides.
func renamedOldPath(file *model.DiffFile) *string {
	if file.Status != model.StatusRenamed && file.Status != model.StatusCopied {
		return nil
	}
	if file.OldPath == nil || file.NewPath == nil {
		return nil
	}
	if *file.OldPath == *file.NewPath {
		return nil
	}
	old := *file.OldPath
	return &old
}

// findLineWithCounterpart finds line on side in the file's hunks and returns
// the opposite-side line number for context lines (nil for purely added or
// deleted lines). found is false when the line is absent from every hunk.
func findLineWithCounterpart(file *model.DiffFile, line uint32, side model.LineSide) (counterpart *uint32, found bool) {
	for hi := range file.Hunks {
		for li := range file.Hunks[hi].Lines {
			dl := &file.Hunks[hi].Lines[li]
			var candidate, other *uint32
			if side == model.LineSideNew {
				candidate, other = dl.NewLineno, dl.OldLineno
			} else {
				candidate, other = dl.OldLineno, dl.NewLineno
			}
			if candidate == nil || *candidate != line {
				continue
			}
			if other == nil {
				return nil, true
			}
			v := *other
			return &v, true
		}
	}
	return nil, false
}

// mapRange maps a multi-line range comment, validating that the range sits
// on a single diff side.
func mapRange(comment *model.Comment, file *model.DiffFile, commentTypePrefix bool, lineRange model.LineRange) MappedComment {
	path := file.DisplayPath()
	if comment.Side == nil {
		// No explicit side is ambiguous for a range; surface it through
		// the resolver rather than guessing.
		return unmappable(comment, path, MixedSideRange)
	}
	side := *comment.Side

	// Both ends of the range must exist on side. The hunks may not contain
	// every intermediate line, but the endpoints must be anchorable.
	if !rangeEndpointsPresent(file, lineRange, side) {
		return unmappable(comment, path, MixedSideRange)
	}

	oldPath := renamedOldPath(file)
	if lineRange.IsSingle() {
		return inline(&InlineComment{
			Path:      path,
			Line:      lineRange.Start,
			Side:      SideFromLineSide(side),
			OldPath:   oldPath,
			Body:      BuildInlineBody(comment, false, commentTypePrefix),
			CommentID: comment.ID,
		})
	}

	start := lineRange.Start
	startSide := SideFromLineSide(side)
	return inline(&InlineComment{
		Path:      path,
		Line:      lineRange.End,
		Side:      SideFromLineSide(side),
		StartLine: &start,
		StartSide: &startSide,
		OldPath:   oldPath,
		Body:      BuildInlineBody(comment, false, commentTypePrefix),
		CommentID: comment.ID,
	})
}

// rangeEndpointsPresent reports whether both the start and end of the range
// appear on the requested side somewhere in the file's hunks, using
// origin-restricted matching so ranges straddling a side boundary are
// detected.
func rangeEndpointsPresent(file *model.DiffFile, lineRange model.LineRange, side model.LineSide) bool {
	sawStart, sawEnd := false, false
	for hi := range file.Hunks {
		for li := range file.Hunks[hi].Lines {
			dl := &file.Hunks[hi].Lines[li]
			var lineno *uint32
			switch side {
			case model.LineSideNew:
				if dl.Origin == model.OriginContext || dl.Origin == model.OriginAddition {
					lineno = dl.NewLineno
				}
			case model.LineSideOld:
				if dl.Origin == model.OriginDeletion {
					lineno = dl.OldLineno
				}
			}
			if lineno == nil {
				continue
			}
			if *lineno == lineRange.Start {
				sawStart = true
			}
			if *lineno == lineRange.End {
				sawEnd = true
			}
		}
	}
	return sawStart && sawEnd
}
