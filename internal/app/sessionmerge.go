// Package app holds mrman's application state machine. This file ports
// tuicr's merge_external_session_changes (src/app/session.rs): the 3-way
// merge that folds externally written session changes (e.g. an agent running
// `mrman review add`) into the live in-memory session.
package app

import (
	"github.com/infrashift/mrman/internal/model"
)

// storedCommentLocation identifies where a comment lives inside a session.
type storedCommentLocation struct {
	kind string // "review" | "file" | "line"
	path string
	line uint32
}

type storedComment struct {
	location storedCommentLocation
	comment  *model.Comment
}

func (a storedComment) equal(b storedComment) bool {
	return a.location == b.location && a.comment.Equal(b.comment)
}

// MergeExternalSessionChanges merges latest (the on-disk session) into
// current, using base (the last persisted snapshot) to detect which side
// changed. Returns the number of applied changes.
//
// Rules, ported exactly from tuicr:
//   - files present in latest but not current are adopted wholesale;
//   - a file's reviewed flag follows latest only when current still agrees
//     with base (the local user hasn't touched it);
//   - comments are merged by id: external additions insert unless the id
//     exists locally; external edits apply only when the local copy equals
//     base (local edits win, local deletions win); external deletions apply
//     only when the local copy is unmodified from base.
func MergeExternalSessionChanges(current, base, latest *model.ReviewSession) int {
	changed := 0

	for path, latestReview := range latest.Files {
		if _, ok := current.Files[path]; !ok {
			current.Files[path] = latestReview.Clone()
			changed += latestReview.CommentCount()
			continue
		}

		currentReview := current.Files[path]
		baseReview, baseHasFile := base.Files[path]
		if baseHasFile &&
			currentReview.Reviewed == baseReview.Reviewed &&
			currentReview.Reviewed != latestReview.Reviewed {
			currentReview.Reviewed = latestReview.Reviewed
			changed++
		}
	}

	baseComments := collectStoredComments(base)
	currentComments := collectStoredComments(current)
	latestComments := collectStoredComments(latest)

	for id, latestComment := range latestComments {
		baseComment, inBase := baseComments[id]
		switch {
		case !inBase:
			if _, exists := currentComments[id]; !exists {
				upsertStoredComment(current, latestComment)
				changed++
			}
		case !latestComment.equal(baseComment):
			currentComment, inCurrent := currentComments[id]
			if inCurrent && currentComment.equal(baseComment) {
				upsertStoredComment(current, latestComment)
				changed++
			}
			// Local deletion or local edit wins over the external edit.
		}
	}

	for id, baseComment := range baseComments {
		if _, stillThere := latestComments[id]; stillThere {
			continue
		}
		currentComment, inCurrent := currentComments[id]
		if inCurrent && currentComment.equal(baseComment) && removeStoredComment(current, id) {
			changed++
		}
	}

	return changed
}

// SessionCommentCount counts all comments in a session at every scope.
func SessionCommentCount(session *model.ReviewSession) int {
	n := len(session.ReviewComments)
	for _, review := range session.Files {
		n += review.CommentCount()
	}
	return n
}

func collectStoredComments(session *model.ReviewSession) map[string]storedComment {
	comments := make(map[string]storedComment)
	for _, c := range session.ReviewComments {
		comments[c.ID] = storedComment{
			location: storedCommentLocation{kind: "review"},
			comment:  c,
		}
	}
	for path, review := range session.Files {
		for _, c := range review.FileComments {
			comments[c.ID] = storedComment{
				location: storedCommentLocation{kind: "file", path: path},
				comment:  c,
			}
		}
		for line, lineComments := range review.LineComments {
			for _, c := range lineComments {
				comments[c.ID] = storedComment{
					location: storedCommentLocation{kind: "line", path: path, line: line},
					comment:  c,
				}
			}
		}
	}
	return comments
}

func upsertStoredComment(session *model.ReviewSession, stored storedComment) {
	removeStoredComment(session, stored.comment.ID)
	comment := stored.comment.Clone()
	switch stored.location.kind {
	case "review":
		session.ReviewComments = append(session.ReviewComments, comment)
	case "file":
		if review, ok := session.Files[stored.location.path]; ok {
			review.FileComments = append(review.FileComments, comment)
		}
	case "line":
		if review, ok := session.Files[stored.location.path]; ok {
			review.AddLineComment(stored.location.line, comment)
		}
	}
}

func removeStoredComment(session *model.ReviewSession, id string) bool {
	for i, c := range session.ReviewComments {
		if c.ID == id {
			session.ReviewComments = append(session.ReviewComments[:i], session.ReviewComments[i+1:]...)
			return true
		}
	}
	for _, review := range session.Files {
		for i, c := range review.FileComments {
			if c.ID == id {
				review.FileComments = append(review.FileComments[:i], review.FileComments[i+1:]...)
				return true
			}
		}
		for line, comments := range review.LineComments {
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
		}
	}
	return false
}
