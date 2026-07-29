package submit

import (
	"fmt"
	"strings"

	"github.com/infrashift/mrman/internal/model"
)

// BuildInlineBody computes the inline body for a comment honoring the
// "[TYPE]" prefix toggle. File-level bodies keep a "File-level: " marker so
// the reader knows where the comment applies even when the type tag is
// omitted (typeless comments never carry a tag).
func BuildInlineBody(comment *model.Comment, fileLevel bool, commentTypePrefix bool) string {
	if !commentTypePrefix {
		return comment.Content
	}
	typeTag := ""
	if !comment.CommentType.IsNone() {
		typeTag = "[" + comment.CommentType.Display() + "] "
	}
	if fileLevel {
		return typeTag + "File-level: " + comment.Content
	}
	return typeTag + comment.Content
}

// BuildReviewBody builds the review body: review-level comments separated by
// blank lines, followed by an "## Unplaced comments" section listing comments
// the resolver moved to the summary. Returns the empty string when there is
// nothing to say.
func BuildReviewBody(reviewComments []*model.Comment, movedToSummary []MovedItem, commentTypePrefix bool) string {
	var sections []string

	if len(reviewComments) > 0 {
		var block strings.Builder
		for i, c := range reviewComments {
			if i > 0 {
				block.WriteString("\n\n")
			}
			if commentTypePrefix && !c.CommentType.IsNone() {
				block.WriteString("[" + c.CommentType.Display() + "] ")
			}
			block.WriteString(c.Content)
		}
		sections = append(sections, block.String())
	}

	if len(movedToSummary) > 0 {
		var block strings.Builder
		block.WriteString("## Unplaced comments\n")
		for _, item := range movedToSummary {
			prefix := ""
			if commentTypePrefix && !item.Comment.CommentType.IsNone() {
				prefix = "[" + item.Comment.CommentType.Display() + "] "
			}
			fmt.Fprintf(&block, "- %s%s: %s\n", prefix, item.File, item.Comment.Content)
		}
		// Strip the trailing newline so joining sections produces one
		// blank line, not two.
		sections = append(sections, strings.TrimSuffix(block.String(), "\n"))
	}

	return strings.Join(sections, "\n\n")
}
