// Package model holds mrman's pure domain types, ported from tuicr's
// src/model. The JSON shapes are kept structurally identical to tuicr's
// session format v1.3: every optional field is a pointer without omitempty
// (serde serializes Option as null), and fields with non-zero serde defaults
// are restored in UnmarshalJSON hooks.
package model

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

// LineSide says which side of the diff a line comment belongs to.
type LineSide string

const (
	// LineSideOld anchors to a deleted line (keyed by old lineno).
	LineSideOld LineSide = "old"
	// LineSideNew anchors to an added or context line (keyed by new lineno).
	LineSideNew LineSide = "new"
)

// CommentLifecycleState is where a comment sits in its remote forge
// lifecycle. Only local drafts are editable; pushed/submitted comments are
// locked because local edits would diverge from the remote.
type CommentLifecycleState string

const (
	// LifecycleLocalDraft is the editable default.
	LifecycleLocalDraft CommentLifecycleState = "local_draft"
	// LifecyclePushedDraft has been pushed to the forge as a pending review.
	LifecyclePushedDraft CommentLifecycleState = "pushed_draft"
	// LifecycleSubmitted has been submitted in a completed review.
	LifecycleSubmitted CommentLifecycleState = "submitted"
)

// IsLocked reports whether the comment has been written to the remote forge.
func (s CommentLifecycleState) IsLocked() bool {
	return s != LifecycleLocalDraft
}

// CommentTypeNoneID is the reserved id of the typeless default comment type.
const CommentTypeNoneID = "none"

// CommentType classifies a comment. The "none" value is the typeless default
// and emits no [TYPE] prefix or badge anywhere; other values are
// user-configured type ids.
type CommentType string

// CommentTypeFromID normalizes an id: empty or (case-insensitively) "none"
// resolves to the typeless default, everything else is trimmed and kept.
func CommentTypeFromID(id string) CommentType {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" || strings.EqualFold(trimmed, CommentTypeNoneID) {
		return CommentTypeNoneID
	}
	return CommentType(trimmed)
}

// ID returns the canonical id string.
func (t CommentType) ID() string {
	if t == "" {
		return CommentTypeNoneID
	}
	return string(t)
}

// IsNone reports whether this is the typeless default.
func (t CommentType) IsNone() bool {
	return t.ID() == CommentTypeNoneID
}

// Display returns the uppercased id used in [TYPE] badges.
func (t CommentType) Display() string {
	return strings.ToUpper(t.ID())
}

// MarshalJSON serializes the type as its bare id string.
func (t CommentType) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.ID())
}

// UnmarshalJSON parses and normalizes the id.
func (t *CommentType) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*t = CommentTypeFromID(s)
	return nil
}

// LineRange is an inclusive range of lines.
type LineRange struct {
	Start uint32 `json:"start"`
	End   uint32 `json:"end"`
}

// NewLineRange builds a range, normalizing reversed bounds.
func NewLineRange(start, end uint32) LineRange {
	if start > end {
		start, end = end, start
	}
	return LineRange{Start: start, End: end}
}

// SingleLineRange builds a single-line range.
func SingleLineRange(line uint32) LineRange {
	return LineRange{Start: line, End: line}
}

// IsSingle reports whether the range covers exactly one line.
func (r LineRange) IsSingle() bool { return r.Start == r.End }

// Contains reports whether line falls inside the range.
func (r LineRange) Contains(line uint32) bool { return line >= r.Start && line <= r.End }

// SideOf reports which side of the diff a comment anchors to.
//
// A nil Side means the new side. That default is written out at several call
// sites across the app, submit and export paths, and every one of them has to
// agree: read it the other way and a comment lands on the deleted line
// opposite the one it was written about.
func SideOf(c *Comment) LineSide {
	if c != nil && c.Side != nil {
		return *c.Side
	}
	return LineSideNew
}

// LineContext captures the anchored line's numbers and content as of the last
// time the anchor was known good: comment creation, or the most recent
// re-anchoring after the diff moved underneath it.
type LineContext struct {
	NewLine *uint32 `json:"new_line"`
	OldLine *uint32 `json:"old_line"`
	Content string  `json:"content"`
}

// Clone returns a deep copy sharing no pointers with the original.
func (lc *LineContext) Clone() *LineContext {
	if lc == nil {
		return nil
	}
	return &LineContext{
		NewLine: clonePtr(lc.NewLine),
		OldLine: clonePtr(lc.OldLine),
		Content: lc.Content,
	}
}

// Equal reports deep value equality with other.
func (lc *LineContext) Equal(other *LineContext) bool {
	if lc == nil || other == nil {
		return lc == other
	}
	return ptrEq(lc.NewLine, other.NewLine) &&
		ptrEq(lc.OldLine, other.OldLine) &&
		lc.Content == other.Content
}

// DefaultAuthor is used when a comment is created or deserialized without an
// explicit author; agents and remote comments set their own.
const DefaultAuthor = "user"

// Comment is one review comment at any scope (review, file, line, or range).
type Comment struct {
	ID              string                `json:"id"`
	Content         string                `json:"content"`
	CommentType     CommentType           `json:"comment_type"`
	CreatedAt       time.Time             `json:"created_at"`
	LineContext     *LineContext          `json:"line_context"`
	Side            *LineSide             `json:"side"`
	LineRange       *LineRange            `json:"line_range"`
	Author          string                `json:"author"`
	LifecycleState  CommentLifecycleState `json:"lifecycle_state"`
	RemoteReviewID  *string               `json:"remote_review_id"`
	RemoteCommentID *string               `json:"remote_comment_id"`
	CommitID        *string               `json:"commit_id"`
}

// commentAlias breaks the UnmarshalJSON recursion.
type commentAlias Comment

// UnmarshalJSON restores tuicr's serde defaults for fields that predate the
// current schema: author "user" and lifecycle_state "local_draft".
func (c *Comment) UnmarshalJSON(data []byte) error {
	alias := commentAlias{
		Author:         DefaultAuthor,
		LifecycleState: LifecycleLocalDraft,
	}
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	if alias.Author == "" {
		alias.Author = DefaultAuthor
	}
	if alias.LifecycleState == "" {
		alias.LifecycleState = LifecycleLocalDraft
	}
	*c = Comment(alias)
	return nil
}

// newIDFn and nowFn are injection seams for deterministic tests.
var (
	newIDFn = uuid.NewString
	nowFn   = func() time.Time { return time.Now().UTC() }
)

// NewComment creates a local-draft comment with a fresh id.
func NewComment(content string, commentType CommentType, side *LineSide) *Comment {
	return &Comment{
		ID:             newIDFn(),
		Content:        content,
		CommentType:    commentType,
		CreatedAt:      nowFn(),
		Side:           side,
		Author:         DefaultAuthor,
		LifecycleState: LifecycleLocalDraft,
	}
}

// NewCommentWithRange creates a local-draft comment anchored to a line range.
func NewCommentWithRange(content string, commentType CommentType, side *LineSide, lineRange LineRange) *Comment {
	c := NewComment(content, commentType, side)
	c.LineRange = &lineRange
	return c
}

// WithAuthor sets the author and returns the comment for chaining.
func (c *Comment) WithAuthor(author string) *Comment {
	c.Author = author
	return c
}

// WithCommitID scopes the comment to one commit and returns it for chaining.
func (c *Comment) WithCommitID(commitID string) *Comment {
	c.CommitID = &commitID
	return c
}

// IsLocked reports whether the comment has been pushed or submitted.
func (c *Comment) IsLocked() bool {
	return c.LifecycleState.IsLocked()
}

// Clone returns a deep copy sharing no pointers with the original.
func (c *Comment) Clone() *Comment {
	clone := *c
	clone.LineContext = c.LineContext.Clone()
	clone.Side = clonePtr(c.Side)
	clone.LineRange = clonePtr(c.LineRange)
	clone.RemoteReviewID = clonePtr(c.RemoteReviewID)
	clone.RemoteCommentID = clonePtr(c.RemoteCommentID)
	clone.CommitID = clonePtr(c.CommitID)
	return &clone
}

// Equal reports deep value equality with other.
func (c *Comment) Equal(other *Comment) bool {
	if c == nil || other == nil {
		return c == other
	}
	return c.ID == other.ID &&
		c.Content == other.Content &&
		c.CommentType.ID() == other.CommentType.ID() &&
		c.CreatedAt.Equal(other.CreatedAt) &&
		c.LineContext.Equal(other.LineContext) &&
		ptrEq(c.Side, other.Side) &&
		ptrEq(c.LineRange, other.LineRange) &&
		c.Author == other.Author &&
		c.LifecycleState == other.LifecycleState &&
		ptrEq(c.RemoteReviewID, other.RemoteReviewID) &&
		ptrEq(c.RemoteCommentID, other.RemoteCommentID) &&
		ptrEq(c.CommitID, other.CommitID)
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func ptrEq[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
