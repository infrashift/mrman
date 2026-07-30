package app

import (
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
		a.SetError("Submitting requires an open pull request (mrman pr <target>)")
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
		a.SetError("Submitting requires an open pull request (mrman pr <target>)")
		return false
	}
	if a.Pr.Details.IsReadOnly() {
		a.SetError("Pull request is " + a.Pr.Details.ReadOnlyReason() + " — read only")
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
				side := model.LineSideNew
				if c.Side != nil {
					side = *c.Side
				}
				anchor := submit.LineAnchor(line, side)
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
	state := model.LifecycleSubmitted
	if event == forge.SubmitDraft {
		state = model.LifecyclePushedDraft
	}
	reviewID := result.ReviewID
	sent := make(map[string]bool, len(a.Submit.SentCommentIDs))
	for _, id := range a.Submit.SentCommentIDs {
		sent[id] = true
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
}
