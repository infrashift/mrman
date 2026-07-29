package ui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/output"
	"github.com/infrashift/mrman/internal/render"
)

// prSubmitResultMsg carries the async CreateReview outcome.
type prSubmitResultMsg struct {
	Gen    uint64
	Key    app.PrKey
	Event  forge.SubmitEvent
	Result *forge.SubmitResult
	Err    error
}

// dispatchSubmitPicker handles the submit action picker modal.
func (m *Model) dispatchSubmitPicker(action input.Action) (bool, tea.Cmd) {
	a := m.App
	switch action.Kind {
	case input.Quit:
		return true, nil
	case input.SubmitPickerDown:
		if a.Submit != nil && a.Submit.PickerCursor < len(app.SubmitPickerEvents)-1 {
			a.Submit.PickerCursor++
		}
	case input.SubmitPickerUp:
		if a.Submit != nil && a.Submit.PickerCursor > 0 {
			a.Submit.PickerCursor--
		}
	case input.SubmitPickerConfirm:
		if a.Submit == nil {
			break
		}
		event := app.SubmitPickerEvents[a.Submit.PickerCursor]
		if a.StartSubmitWith(event, true) && len(a.Submit.Unmappable) == 0 {
			return false, m.spawnSubmit()
		}
	case input.ExitMode:
		a.CancelSubmit()
	}
	return false, nil
}

// dispatchSubmitResolver handles the unmappable-comment resolver modal.
func (m *Model) dispatchSubmitResolver(action input.Action) tea.Cmd {
	a := m.App
	switch action.Kind {
	case input.SubmitResolverDown:
		if a.Submit != nil && a.Submit.ResolverCursor < len(a.Submit.Unmappable)-1 {
			a.Submit.ResolverCursor++
		}
	case input.SubmitResolverUp:
		if a.Submit != nil && a.Submit.ResolverCursor > 0 {
			a.Submit.ResolverCursor--
		}
	case input.SubmitResolverToggle:
		a.ResolverToggle()
	case input.SubmitResolverAdvance:
		a.ResolverAdvance()
		if a.Submit != nil && a.Submit.SkipConfirm {
			return m.spawnSubmit()
		}
	case input.ExitMode:
		a.CancelSubmit()
	}
	return nil
}

// dispatchSubmitConfirm handles the final confirmation modal.
func (m *Model) dispatchSubmitConfirm(action input.Action) tea.Cmd {
	a := m.App
	switch action.Kind {
	case input.ConfirmYes:
		return m.spawnSubmit()
	case input.SubmitReloadPr:
		// The confirmation warns when the head moved since the diff was
		// loaded; r refetches so the review anchors to the current head
		// instead of being rejected or landing on stale lines.
		a.CancelSubmit()
		return m.reloadPullRequest()
	case input.ConfirmNo, input.ExitMode:
		a.CancelSubmit()
	}
	return nil
}

// spawnSubmit builds the review body, saves the session, and issues the
// CreateReview call as a tea.Cmd with staleness identity attached.
func (m *Model) spawnSubmit() tea.Cmd {
	a := m.App
	if a.Submit == nil || !a.InPrMode() {
		return nil
	}
	key, ok := a.Pr.CurrentPrKey()
	if !ok {
		return nil
	}

	// Review body through the user-overridable template.
	data := &output.ReviewBodyData{}
	for _, c := range a.Submit.ReviewComments {
		data.ReviewComments = append(data.ReviewComments,
			output.ReviewBodyComment{Type: typeID(c.CommentType.ID()), Content: c.Content})
	}
	for _, item := range a.Submit.MovedToSummary() {
		data.MovedToSummary = append(data.MovedToSummary, output.ReviewBodyComment{
			Type: typeID(item.Comment.CommentType.ID()), Path: item.Path, Content: item.Comment.Content,
		})
	}
	tmpl, tmplWarnings := output.LoadReviewBodyTemplate(m.export.ReviewBodyTemplatePath)
	for _, w := range tmplWarnings {
		a.SetWarning(w)
	}
	body, err := output.RenderReviewBody(tmpl, data)
	if err != nil {
		a.SetError("Review body template failed: " + err.Error())
		return nil
	}

	// Save the session before the network call (crash-safe drafts).
	if saveErr := m.saveSession(); saveErr != nil {
		a.SetError("Save failed: " + saveErr.Error())
		return nil
	}

	a.Pr.Gens.PrSubmit++
	gen := a.Pr.Gens.PrSubmit
	a.Pr.Submitting = true
	a.InputMode = input.ModeNormal

	backend := a.Pr.Backend
	details := a.Pr.Details
	req := forge.CreateReviewRequest{
		Event:    a.Submit.Event,
		CommitID: a.Submit.CommitID,
		Body:     body,
		Comments: a.Submit.Mappable,
	}
	event := a.Submit.Event
	return func() tea.Msg {
		result, callErr := backend.CreateReview(context.Background(), details, req)
		return prSubmitResultMsg{Gen: gen, Key: key, Event: event, Result: result, Err: callErr}
	}
}

func typeID(id string) string {
	if id == "none" {
		return ""
	}
	return id
}

// handleSubmitResult applies an async submit outcome, discarding stale ones.
func (m *Model) handleSubmitResult(msg prSubmitResultMsg) {
	a := m.App
	if !a.InPrMode() {
		return
	}
	key, ok := a.Pr.CurrentPrKey()
	if !ok || msg.Gen != a.Pr.Gens.PrSubmit || msg.Key != key {
		return // stale result (PR was reloaded)
	}
	a.Pr.Submitting = false
	if msg.Err != nil {
		a.SetError("Submit failed: " + msg.Err.Error())
		return
	}
	a.ApplySubmitSuccess(msg.Result, msg.Event)
	m.autosave()
	a.SetMessage(fmt.Sprintf("Review submitted (%s)", msg.Event.HumanLabel()))
}

// submitModalView renders the active submit modal as overlay rows.
func (m *Model) submitModalView(width int) []string {
	a := m.App
	if a.Submit == nil {
		return nil
	}
	t := m.Theme
	emitter := &render.Emitter{}
	var rows []string
	line := func(spans ...render.Span) { rows = append(rows, emitter.Line(spans)) }
	title := func(text string) {
		line(render.Span{Text: text, Style: render.Style{Fg: t.BorderFocused, Bold: true}})
	}

	switch a.InputMode {
	case input.ModeSubmitActionPicker:
		title(fmt.Sprintf(" Submit review to %s? ", a.Pr.Details.Repository.Host))
		for i, event := range app.SubmitPickerEvents {
			marker, style := "  ", render.Style{Fg: t.FgPrimary}
			if i == a.Submit.PickerCursor {
				marker, style = "> ", render.Style{Fg: t.FgPrimary, Bg: t.BgHighlight, Bold: true}
			}
			line(render.Span{Text: marker + event.HumanLabel(), Style: style})
		}
		line(render.Span{Text: "Enter: submit   Esc: cancel", Style: render.Style{Fg: t.FgSecondary}})
	case input.ModeSubmitResolver:
		title(fmt.Sprintf(" %d comment(s) cannot be posted inline ", len(a.Submit.Unmappable)))
		for i, item := range a.Submit.Unmappable {
			marker, style := "  ", render.Style{Fg: t.FgPrimary}
			if i == a.Submit.ResolverCursor {
				marker, style = "> ", render.Style{Fg: t.FgPrimary, Bg: t.BgHighlight, Bold: true}
			}
			actionLabel := "[x] Move to summary"
			if item.Action != 0 { // submit.Omit
				actionLabel = "[ ] Omit           "
			}
			preview := item.Comment.Content
			if len(preview) > 40 {
				preview = preview[:40] + "…"
			}
			preview = strings.ReplaceAll(preview, "\n", " ")
			line(render.Span{
				Text:  fmt.Sprintf("%s%s  %s %q (%s)", marker, actionLabel, item.Path, preview, item.Reason.HumanLabel()),
				Style: style,
			})
		}
		line(render.Span{Text: "Enter: toggle   s: submit   Esc: cancel", Style: render.Style{Fg: t.FgSecondary}})
	case input.ModeSubmitConfirm:
		verb := "Submit review to"
		if a.Submit.Event == forge.SubmitDraft {
			verb = "Push pending review to"
		}
		title(fmt.Sprintf(" %s %s? ", verb, a.Pr.Details.Repository.Host))
		line(render.Span{Text: "Event:  " + a.Submit.Event.HumanLabel(), Style: render.Style{Fg: t.FgPrimary}})
		line(render.Span{Text: fmt.Sprintf("Inline: %d", len(a.Submit.Mappable)), Style: render.Style{Fg: t.FgPrimary}})
		moved := len(a.Submit.MovedToSummary())
		line(render.Span{Text: fmt.Sprintf("Moved to summary: %d · Omitted: %d",
			moved, len(a.Submit.Unmappable)-moved), Style: render.Style{Fg: t.FgPrimary}})
		line(render.Span{Text: fmt.Sprintf("Head:   %.7s", a.Submit.CommitID), Style: render.Style{Fg: t.FgDim}})
		line(render.Span{Text: "[y] submit    [n] cancel", Style: render.Style{Fg: t.FgSecondary, Bold: true}})
	}

	for i, row := range rows {
		rows[i] = " " + row
	}
	_ = width
	return rows
}
