package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
)

// uiFakeForge satisfies forge.Forge for submit-path tests.
type uiFakeForge struct {
	result    *forge.SubmitResult
	err       error
	lastReq   *forge.CreateReviewRequest
	callCount int
}

func (f *uiFakeForge) ID() forgetypes.Kind { return forgetypes.KindGitHub }
func (f *uiFakeForge) Capabilities() forge.Capabilities {
	return forge.Capabilities{
		DraftReviews: true, Approve: true, RequestChanges: true, ReviewThreads: true,
		MultiLineComments: true, AtomicSubmit: true,
	}
}
func (f *uiFakeForge) ListPullRequests(context.Context, forge.ListQuery) (*forge.PullRequestPage, error) {
	return &forge.PullRequestPage{}, nil
}
func (f *uiFakeForge) GetPullRequest(context.Context, forge.Target) (*forge.PullRequestDetails, error) {
	return nil, nil
}
func (f *uiFakeForge) GetDiff(context.Context, *forge.PullRequestDetails) (string, error) {
	return "", nil
}
func (f *uiFakeForge) GetCommitRangeDiff(context.Context, *forge.PullRequestDetails, string, string) (string, error) {
	return "", nil
}
func (f *uiFakeForge) ListCommits(context.Context, *forge.PullRequestDetails) ([]forge.Commit, error) {
	return nil, nil
}
func (f *uiFakeForge) FetchFileLines(context.Context, forge.FileLinesRequest) ([]model.DiffLine, error) {
	return nil, nil
}
func (f *uiFakeForge) FileLineCount(context.Context, forge.FileLinesRequest) (int, error) {
	return 0, nil
}
func (f *uiFakeForge) ListReviewThreads(context.Context, *forge.PullRequestDetails) ([]forge.RemoteReviewThread, error) {
	return nil, nil
}
func (f *uiFakeForge) ListReviewSummaries(context.Context, *forge.PullRequestDetails) ([]forge.RemoteReviewSummary, error) {
	return nil, nil
}
func (f *uiFakeForge) ReviewMetadata(context.Context, *forge.PullRequestDetails) (*forge.ReviewMetadata, error) {
	return nil, nil
}
func (f *uiFakeForge) CreateReview(_ context.Context, _ *forge.PullRequestDetails, req forge.CreateReviewRequest) (*forge.SubmitResult, error) {
	f.callCount++
	f.lastReq = &req
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}
func (f *uiFakeForge) LocalCheckoutPath() string { return "" }

// prModel builds a Model in PR mode with one line comment ready to submit.
func prModel(t *testing.T) (*Model, *uiFakeForge) {
	t.Helper()
	_, m := testLifecycle(t)
	fake := &uiFakeForge{result: &forge.SubmitResult{ReviewID: "R7", State: "COMMENTED"}}
	m.App.Pr = &app.PrState{
		Backend: fake,
		Details: &forge.PullRequestDetails{
			PullRequestSummary: forge.PullRequestSummary{
				Repository: forgetypes.Repository{
					Kind: forgetypes.KindGitHub, Host: "github.com", Owner: "o", Name: "r",
				},
				Number: 7, State: "OPEN",
			},
			HeadSHA: "headsha", BaseSHA: "basesha",
		},
	}
	side := model.LineSideNew
	c := model.NewComment("inline note", model.CommentTypeFromID("issue"), &side)
	m.App.Session.File("src/x.go").AddLineComment(1, c)
	return m, fake
}

func TestSubmitPickerFlow(t *testing.T) {
	m, fake := prModel(t)
	press(m, ":", ':', 0)
	for _, r := range "submit" {
		pressRune(m, r)
	}
	press(m, "", tea.KeyEnter, 0)
	if m.App.InputMode != input.ModeSubmitActionPicker {
		t.Fatalf("mode = %v", m.App.InputMode)
	}
	out := viewString(m)
	if !strings.Contains(out, "Submit review to github.com") || !strings.Contains(out, "Comment") {
		t.Error("picker modal must render")
	}
	pressRune(m, 'j') // → Approve
	pressRune(m, 'k') // → Comment
	// Confirm dispatches immediately (picker path skips confirm).
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if cmd == nil {
		t.Fatal("picker confirm must spawn the submit cmd")
	}
	msg := cmd()
	result, ok := msg.(prSubmitResultMsg)
	if !ok || result.Err != nil {
		t.Fatalf("msg = %#v", msg)
	}
	if fake.callCount != 1 || fake.lastReq.CommitID != "headsha" {
		t.Fatalf("req = %+v calls=%d", fake.lastReq, fake.callCount)
	}
	if len(fake.lastReq.Comments) != 1 {
		t.Fatalf("comments = %+v", fake.lastReq.Comments)
	}

	// Applying the result locks the comment.
	m.Update(msg)
	stored := m.App.Session.File("src/x.go").LineComments[1][0]
	if stored.LifecycleState != model.LifecycleSubmitted {
		t.Fatalf("lifecycle = %v", stored.LifecycleState)
	}
	if m.App.Message == nil || !strings.Contains(m.App.Message.Content, "submitted") {
		t.Fatalf("message = %+v", m.App.Message)
	}
}

func TestSubmitConfirmFlow(t *testing.T) {
	m, fake := prModel(t)
	press(m, ":", ':', 0)
	for _, r := range "submit approve" {
		pressRune(m, r)
	}
	press(m, "", tea.KeyEnter, 0)
	if m.App.InputMode != input.ModeSubmitConfirm {
		t.Fatalf("mode = %v", m.App.InputMode)
	}
	out := viewString(m)
	if !strings.Contains(out, "Approve") {
		t.Error("confirm modal must show the event")
	}
	// n cancels.
	pressRune(m, 'n')
	if m.App.Submit != nil || fake.callCount != 0 {
		t.Fatal("cancel must not submit")
	}

	// y submits.
	press(m, ":", ':', 0)
	for _, r := range "submit approve" {
		pressRune(m, r)
	}
	press(m, "", tea.KeyEnter, 0)
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Text: "y", Code: 'y'}))
	if cmd == nil {
		t.Fatal("y must spawn submit")
	}
	cmd()
	if fake.callCount != 1 {
		t.Fatal("CreateReview not called")
	}
}

func TestSubmitResultStaleGuard(t *testing.T) {
	m, _ := prModel(t)
	msg := prSubmitResultMsg{
		Gen:    99, // wrong generation
		Key:    app.PrKey{Repository: m.App.Pr.Details.Repository, Number: 7, HeadSHA: "headsha"},
		Result: &forge.SubmitResult{ReviewID: "STALE"},
	}
	m.Update(msg)
	stored := m.App.Session.File("src/x.go").LineComments[1][0]
	if stored.LifecycleState != model.LifecycleLocalDraft {
		t.Fatal("stale result must be discarded")
	}
}

func TestSubmitErrorSurfaces(t *testing.T) {
	m, fake := prModel(t)
	fake.err = errors.New("403 scope missing")
	press(m, ":", ':', 0)
	for _, r := range "submit comment" {
		pressRune(m, r)
	}
	press(m, "", tea.KeyEnter, 0)
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Text: "y", Code: 'y'}))
	if cmd == nil {
		t.Fatal("y must spawn submit")
	}
	m.Update(cmd())
	if m.App.Message == nil || !strings.Contains(m.App.Message.Content, "Submit failed") {
		t.Fatalf("message = %+v", m.App.Message)
	}
	stored := m.App.Session.File("src/x.go").LineComments[1][0]
	if stored.LifecycleState != model.LifecycleLocalDraft {
		t.Fatal("failed submit must not lock comments")
	}
}

func TestReviewBodyThroughTemplate(t *testing.T) {
	m, fake := prModel(t)
	m.App.Session.ReviewComments = append(m.App.Session.ReviewComments,
		model.NewComment("overall thoughts", model.CommentTypeFromID("note"), nil))
	press(m, ":", ':', 0)
	for _, r := range "submit comment" {
		pressRune(m, r)
	}
	press(m, "", tea.KeyEnter, 0)
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Text: "y", Code: 'y'}))
	cmd()
	if fake.lastReq == nil || !strings.Contains(fake.lastReq.Body, "overall thoughts") {
		t.Fatalf("body = %q", fake.lastReq.Body)
	}
}
