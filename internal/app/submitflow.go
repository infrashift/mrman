package app

import (
	"fmt"
	"sort"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/submit"
	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
)

// SubmitPickerEvents is the action picker's row order (tuicr parity).
var SubmitPickerEvents = []forge.SubmitEvent{
	forge.SubmitComment, forge.SubmitApprove, forge.SubmitRequestChanges, forge.SubmitDraft,
}

// PickerEvents is SubmitPickerEvents narrowed to what the current forge
// can do, so the picker never offers an action that fails after it is
// chosen (Azure DevOps has no draft reviews).
func (a *App) PickerEvents() []forge.SubmitEvent {
	if !a.InPrMode() {
		return SubmitPickerEvents
	}
	caps := a.Pr.Backend.Capabilities()
	events := make([]forge.SubmitEvent, 0, len(SubmitPickerEvents))
	for _, event := range SubmitPickerEvents {
		switch event {
		case forge.SubmitDraft:
			if !caps.DraftReviews {
				continue
			}
		case forge.SubmitApprove:
			if !caps.Approve {
				continue
			}
		case forge.SubmitRequestChanges:
			if !caps.RequestChanges {
				continue
			}
		}
		events = append(events, event)
	}
	return events
}

// SubmitOutcome is what applying a forge's submit result amounted to.
type SubmitOutcome struct {
	// Partial is the forge's account of a mid-sequence failure, nil when
	// everything posted.
	Partial *forge.PartialFailure
	// Posted counts the inline comments the forge accepted.
	Posted int
	// Unposted counts the inline comments still local because the
	// sequence stopped before them.
	Unposted int
}

// Complete reports whether every comment reached the forge.
func (o SubmitOutcome) Complete() bool { return o.Partial == nil }

// Message describes a partial outcome for the status bar: what landed,
// what did not, and why.
func (o SubmitOutcome) Message() string {
	if o.Partial == nil {
		return ""
	}
	total := o.Posted + o.Unposted
	msg := fmt.Sprintf("Posted %d of %d inline comments before the forge refused", o.Posted, total)
	if o.Partial.FailedAt >= total {
		msg = fmt.Sprintf("Posted all %d inline comments but the final step failed", total)
	}
	if o.Partial.Cause != nil {
		msg += ": " + o.Partial.Cause.Error()
	}
	return msg + " — the rest stay local; submit again to retry them"
}

// UnmappableItem is one comment that cannot post inline, awaiting a
// resolver decision.
type UnmappableItem struct {
	Comment *model.Comment
	Path    string
	Reason  submit.UnmappableReason
	Action  submit.ResolverAction // default MoveToSummary
}

// SubmitState carries an in-progress submit flow.
type SubmitState struct {
	Event          forge.SubmitEvent
	CommitID       string
	Mappable       []submit.InlineComment
	Unmappable     []UnmappableItem
	ReviewComments []*model.Comment
	ResolverCursor int
	PickerCursor   int
	SkipConfirm    bool
	// SentCommentIDs maps what was sent so success can lock them.
	SentCommentIDs []string
}

// StartSubmitPicker opens the submit action picker.
func (a *App) StartSubmitPicker() bool {
	if !a.InPrMode() {
		a.SetError("Submitting requires an open merge request (mrman pr <target>)")
		return false
	}
	a.Submit = &SubmitState{}
	a.InputMode = input.ModeSubmitActionPicker
	return true
}

// StartSubmitWith begins the submit flow for event, mapping every visible
// draft comment. Returns false when preflight fails.
func (a *App) StartSubmitWith(event forge.SubmitEvent, skipConfirm bool) bool {
	if !a.InPrMode() {
		a.SetError("Submitting requires an open merge request (mrman pr <target>)")
		return false
	}
	if a.Pr.Details.IsReadOnly() {
		a.SetError("Merge request is " + a.Pr.Details.ReadOnlyReason() + " — read only")
		return false
	}
	caps := a.Pr.Backend.Capabilities()
	if event == forge.SubmitDraft && !caps.DraftReviews {
		a.SetError("This forge does not support draft reviews")
		return false
	}

	state := &SubmitState{Event: event, SkipConfirm: skipConfirm, CommitID: a.Pr.Details.HeadSHA}
	if IsStrictCommitSelection(a.CommitSelectionRange, len(a.Pr.Commits)) {
		// A strict subset anchors the review at the newest selected commit.
		idx := a.CommitSelectionRange[0]
		if idx >= 0 && idx < len(a.Pr.Commits) {
			state.CommitID = a.Pr.Commits[idx].OID
		}
	}

	prefix := a.CommentTypePrefix
	commitSet, hasSet := a.SelectedCommitSet()

	paths := make([]string, 0, len(a.Session.Files))
	for path := range a.Session.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		review := a.Session.Files[path]
		file := a.diffFileByPath(path)
		for _, c := range review.FileComments {
			a.bucketComment(state, c, submit.FileLevelAnchor(), file, path, prefix, commitSet, hasSet)
		}
		lines := make([]uint32, 0, len(review.LineComments))
		for line := range review.LineComments {
			lines = append(lines, line)
		}
		sort.Slice(lines, func(i, j int) bool { return lines[i] < lines[j] })
		for _, line := range lines {
			for _, c := range review.LineComments[line] {
				anchor := submit.LineAnchor(line, model.SideOf(c))
				if c.LineRange != nil && !c.LineRange.IsSingle() {
					anchor = submit.RangeAnchor()
				}
				a.bucketComment(state, c, anchor, file, path, prefix, commitSet, hasSet)
			}
		}
	}
	for _, c := range a.Session.ReviewComments {
		if !c.IsLocked() {
			state.ReviewComments = append(state.ReviewComments, c)
		}
	}

	if event != forge.SubmitApprove &&
		len(state.Mappable) == 0 && len(state.Unmappable) == 0 && len(state.ReviewComments) == 0 {
		a.SetMessage("Nothing to submit — no local-draft comments")
		return false
	}

	if !caps.MultiLineComments {
		state.Mappable = submit.DowngradeMultiline(state.Mappable)
	}

	a.Submit = state
	if len(state.Unmappable) > 0 {
		a.InputMode = input.ModeSubmitResolver
	} else if skipConfirm {
		a.InputMode = input.ModeNormal // dispatch happens in the UI layer
	} else {
		a.InputMode = input.ModeSubmitConfirm
	}
	return true
}

func (a *App) bucketComment(state *SubmitState, c *model.Comment, anchor submit.CommentAnchor,
	file *model.DiffFile, path string, prefix bool, commitSet map[string]bool, hasSet bool) {
	if c.IsLocked() {
		return
	}
	if !commentVisibleWith(c, commitSet, hasSet) {
		return
	}
	if file == nil {
		state.Unmappable = append(state.Unmappable, UnmappableItem{
			Comment: c, Path: path, Reason: submit.LineNotInDiff,
			Action: submit.MoveToSummary,
		})
		return
	}
	// A comment whose anchor failed validation is refused an inline position
	// even though its line number still maps. It goes to the resolver, which
	// makes the reviewer decide per comment whether it belongs in the summary
	// body or nowhere — mrman does not get to guess where criticism lands.
	if a.HasOutdatedAnchor(c.ID) {
		state.Unmappable = append(state.Unmappable, UnmappableItem{
			Comment: c, Path: path, Reason: submit.StaleAnchor,
			Action: submit.MoveToSummary,
		})
		return
	}
	mapped := submit.MapComment(c, anchor, file, prefix)
	if mapped.Inline != nil {
		state.Mappable = append(state.Mappable, *mapped.Inline)
		state.SentCommentIDs = append(state.SentCommentIDs, c.ID)
	} else if mapped.Unmappable != nil {
		state.Unmappable = append(state.Unmappable, UnmappableItem{
			Comment: c, Path: path, Reason: mapped.Unmappable.Reason,
			Action: submit.MoveToSummary,
		})
	}
}

func (a *App) diffFileByPath(path string) *model.DiffFile {
	for i := range a.DiffFiles {
		if a.DiffFiles[i].DisplayPath() == path {
			return &a.DiffFiles[i]
		}
	}
	return nil
}

// ResolverToggle flips the cursor item between MoveToSummary and Omit.
func (a *App) ResolverToggle() {
	if a.Submit == nil || a.Submit.ResolverCursor >= len(a.Submit.Unmappable) {
		return
	}
	item := &a.Submit.Unmappable[a.Submit.ResolverCursor]
	if item.Action == submit.MoveToSummary {
		item.Action = submit.Omit
	} else {
		item.Action = submit.MoveToSummary
	}
}

// ResolverAdvance moves from the resolver to confirmation.
func (a *App) ResolverAdvance() {
	if a.Submit == nil {
		return
	}
	if a.Submit.SkipConfirm {
		a.InputMode = input.ModeNormal
	} else {
		a.InputMode = input.ModeSubmitConfirm
	}
}

// MovedToSummary returns resolver items kept for the summary body.
func (s *SubmitState) MovedToSummary() []UnmappableItem {
	var moved []UnmappableItem
	for _, item := range s.Unmappable {
		if item.Action == submit.MoveToSummary {
			moved = append(moved, item)
		}
	}
	return moved
}

// CancelSubmit abandons the submit flow.
func (a *App) CancelSubmit() {
	a.Submit = nil
	a.InputMode = input.ModeNormal
}

// ApplySubmitSuccess locks every sent comment and stamps the remote review
// id, mirroring tuicr's post-submit bookkeeping.
func (a *App) ApplySubmitSuccess(result *forge.SubmitResult, event forge.SubmitEvent) {
	a.ApplySubmitResult(result, event)
}

// ApplySubmitResult locks the comments the forge accepted and reports what
// happened. On a complete result every sent comment locks. On a partial
// one — GitLab and Azure DevOps post comment by comment and can fail
// midway — only the comments the forge names as posted lock, plus the
// review body, which both drivers post first and fail outright on; the
// rest stay drafts so a second submit can carry them.
func (a *App) ApplySubmitResult(result *forge.SubmitResult, event forge.SubmitEvent) SubmitOutcome {
	state := model.LifecycleSubmitted
	if event == forge.SubmitDraft {
		state = model.LifecyclePushedDraft
	}
	reviewID := result.ReviewID
	outcome := SubmitOutcome{Partial: result.Partial}
	sent := make(map[string]bool, len(a.Submit.SentCommentIDs))
	if result.Partial == nil {
		for _, id := range a.Submit.SentCommentIDs {
			sent[id] = true
		}
		outcome.Posted = len(a.Submit.SentCommentIDs)
	} else {
		for _, id := range result.Partial.SucceededCommentIDs {
			sent[id] = true
		}
		outcome.Posted = len(result.Partial.SucceededCommentIDs)
		outcome.Unposted = len(a.Submit.SentCommentIDs) - outcome.Posted
	}
	for _, c := range a.Submit.ReviewComments {
		sent[c.ID] = true
	}
	lock := func(c *model.Comment) {
		if sent[c.ID] {
			c.LifecycleState = state
			c.RemoteReviewID = &reviewID
		}
	}
	for _, c := range a.Session.ReviewComments {
		lock(c)
	}
	for _, review := range a.Session.Files {
		for _, c := range review.FileComments {
			lock(c)
		}
		for _, comments := range review.LineComments {
			for _, c := range comments {
				lock(c)
			}
		}
	}
	a.Submit = nil
	a.Dirty = true
	a.RebuildAnnotations()
	return outcome
}
