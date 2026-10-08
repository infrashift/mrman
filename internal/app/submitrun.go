// submitrun.go holds the parts of submitting a review that are the same
// whether a person pressed a key or an agent ran a command: building the
// review body from the user's template, and issuing the CreateReview call.
//
// It lives here rather than in the UI because the TUI is no longer the only
// caller. What stays in the UI is the bubbletea plumbing — the generation
// guard, the spinner, the status message — none of which a CLI has.

package app

import (
	"context"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/output"
)

// BuildReviewBody renders the review body through the user's template,
// returning any template warnings alongside it so the caller can surface
// them however it surfaces things.
//
// A template that fails to parse falls back to the default rather than
// losing the review, which is why warnings are returned separately from the
// error: a warning means "we used the default", an error means "we have no
// body".
func BuildReviewBody(a *App, templatePath string) (body string, warnings []string, err error) {
	if a.Submit == nil {
		return "", nil, nil
	}
	data := &output.ReviewBodyData{}
	for _, c := range a.Submit.ReviewComments {
		data.ReviewComments = append(data.ReviewComments, output.ReviewBodyComment{
			Type:       submitTypeID(c.CommentType.ID()),
			Content:    c.Content,
			Author:     c.Author,
			ShowAuthor: a.ShowsAuthor(c.Author),
		})
	}
	for _, item := range a.Submit.MovedToSummary() {
		data.MovedToSummary = append(data.MovedToSummary, output.ReviewBodyComment{
			Type:       submitTypeID(item.Comment.CommentType.ID()),
			Path:       item.Path,
			Content:    item.Comment.Content,
			Author:     item.Comment.Author,
			ShowAuthor: a.ShowsAuthor(item.Comment.Author),
		})
	}
	tmpl, warnings := output.LoadReviewBodyTemplate(templatePath)
	body, err = output.RenderReviewBody(tmpl, data)
	return body, warnings, err
}

// SubmitRequest packages the prepared submit for a driver.
func SubmitRequest(a *App, body string) forge.CreateReviewRequest {
	return forge.CreateReviewRequest{
		Event:    a.Submit.Event,
		CommitID: a.Submit.CommitID,
		Body:     body,
		Comments: a.Submit.Mappable,
	}
}

// SubmitReviewOutcome posts the prepared review, applies the result to the
// session (locking what the forge accepted), and reports the outcome, so a
// headless caller can tell a partial post from a complete one.
//
// This is the synchronous form, for callers with no event loop. The TUI
// does not use it: it runs the call inside a tea.Cmd so the interface stays
// responsive, behind a staleness guard that only matters when the user can
// navigate mid-flight.
func SubmitReviewOutcome(a *App, templatePath string) (*forge.SubmitResult, SubmitOutcome, []string, error) {
	body, warnings, err := BuildReviewBody(a, templatePath)
	if err != nil {
		return nil, SubmitOutcome{}, warnings, err
	}
	result, err := a.Pr.Backend.CreateReview(
		context.Background(), a.Pr.Details, SubmitRequest(a, body))
	if err != nil {
		return nil, SubmitOutcome{}, warnings, err
	}
	outcome := a.ApplySubmitResult(result, a.Submit.Event)
	return result, outcome, warnings, nil
}

// submitTypeID drops the sentinel "none" so an untyped comment carries no
// [TYPE] prefix.
func submitTypeID(id string) string {
	if id == "none" {
		return ""
	}
	return id
}
