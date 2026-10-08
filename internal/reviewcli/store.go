// Package reviewcli implements the non-interactive `mrman review` commands:
// JSON session listings, comment additions, comment dumps, and the watch
// stream of changes. This is the documented integration surface for agents
// collaborating on a review; submitting lives in agentsubmit, behind its
// grant.
package reviewcli

import (
	"fmt"
	"strings"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/model"
)

// TargetKind discriminates CommentTarget.
type TargetKind int

// Target kinds.
const (
	TargetReview TargetKind = iota
	TargetFile
	TargetLine
	TargetLineRange
)

// CommentTarget says where a comment attaches.
type CommentTarget struct {
	Kind  TargetKind
	Path  string
	Line  uint32
	Range model.LineRange
	Side  model.LineSide
}

// AddCommentRequest carries everything needed to insert a comment.
type AddCommentRequest struct {
	Target      CommentTarget
	Content     string
	CommentType model.CommentType
	Author      string
	CommitID    *string
	// LineContext snapshots the anchored line's numbers and content so a
	// later session can tell whether the anchor still points at the code the
	// comment was written about. Only callers with the diff in hand can
	// supply it: the TUI does, `mrman review add` cannot and leaves it nil.
	LineContext *model.LineContext
}

// AddCommentToSession is the single comment-insertion primitive shared by
// the CLI and (later) the TUI, ported from tuicr's review_store. It trims
// and validates content, requires the target file to exist in the session,
// keys range comments by their end line, stamps the author, and bumps
// updated_at.
func AddCommentToSession(session *model.ReviewSession, req AddCommentRequest) (*model.Comment, error) {
	content := strings.TrimSpace(req.Content)
	if content == "" {
		return nil, &errs.InvalidInput{Detail: "comment text must not be empty"}
	}

	var comment *model.Comment
	switch req.Target.Kind {
	case TargetReview:
		comment = model.NewComment(content, req.CommentType, nil)
		session.ReviewComments = append(session.ReviewComments, comment)
	case TargetFile:
		review, err := requireFile(session, req.Target.Path)
		if err != nil {
			return nil, err
		}
		comment = model.NewComment(content, req.CommentType, nil)
		review.AddFileComment(comment)
	case TargetLine:
		review, err := requireFile(session, req.Target.Path)
		if err != nil {
			return nil, err
		}
		side := req.Target.Side
		comment = model.NewComment(content, req.CommentType, &side)
		review.AddLineComment(req.Target.Line, comment)
	case TargetLineRange:
		review, err := requireFile(session, req.Target.Path)
		if err != nil {
			return nil, err
		}
		side := req.Target.Side
		comment = model.NewCommentWithRange(content, req.CommentType, &side, req.Target.Range)
		review.AddLineComment(req.Target.Range.End, comment)
	default:
		return nil, &errs.InvalidInput{Detail: "unknown comment target"}
	}

	comment.Author = req.Author
	comment.CommitID = req.CommitID
	comment.LineContext = req.LineContext.Clone()
	session.UpdatedAt = nowFn()
	return comment, nil
}

func requireFile(session *model.ReviewSession, path string) (*model.FileReview, error) {
	if review := session.File(path); review != nil {
		return review, nil
	}
	return nil, &errs.InvalidInput{
		Detail: fmt.Sprintf("file '%s' is not part of this review session", path),
	}
}
