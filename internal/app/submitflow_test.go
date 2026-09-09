package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/forge/submit"
	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs"
)

// fakeForge satisfies forge.Forge with canned data.
type fakeForge struct {
	caps forge.Capabilities
}

func (f *fakeForge) ID() forgetypes.Kind              { return forgetypes.KindGitHub }
func (f *fakeForge) Capabilities() forge.Capabilities { return f.caps }
func (f *fakeForge) ListPullRequests(context.Context, forge.ListQuery) (*forge.PullRequestPage, error) {
	return &forge.PullRequestPage{}, nil
}
func (f *fakeForge) GetPullRequest(context.Context, forge.Target) (*forge.PullRequestDetails, error) {
	return nil, nil
}
func (f *fakeForge) GetDiff(context.Context, *forge.PullRequestDetails) (string, error) {
	return "", nil
}
func (f *fakeForge) GetCommitRangeDiff(context.Context, *forge.PullRequestDetails, string, string) (string, error) {
	return "", nil
}
func (f *fakeForge) ListCommits(context.Context, *forge.PullRequestDetails) ([]forge.Commit, error) {
	return nil, nil
}
func (f *fakeForge) FetchFileLines(context.Context, forge.FileLinesRequest) ([]model.DiffLine, error) {
	return nil, nil
}
func (f *fakeForge) FileLineCount(context.Context, forge.FileLinesRequest) (int, error) {
	return 0, nil
}
func (f *fakeForge) ListReviewThreads(context.Context, *forge.PullRequestDetails) ([]forge.RemoteReviewThread, error) {
	return nil, nil
}
func (f *fakeForge) ListReviewSummaries(context.Context, *forge.PullRequestDetails) ([]forge.RemoteReviewSummary, error) {
	return nil, nil
}
func (f *fakeForge) ReviewMetadata(context.Context, *forge.PullRequestDetails) (*forge.ReviewMetadata, error) {
	return nil, nil
}
func (f *fakeForge) CreateReview(context.Context, *forge.PullRequestDetails, forge.CreateReviewRequest) (*forge.SubmitResult, error) {
	return &forge.SubmitResult{ReviewID: "R1", State: "COMMENTED"}, nil
}
func (f *fakeForge) LocalCheckoutPath() string { return "" }

// newTestApp builds a minimal App with one file whose lines 1..5 exist on
// both sides.
func newTestApp(t *testing.T) *App {
	t.Helper()
	info := &vcs.Info{RootPath: "/tmp", HeadCommit: "abc123", BranchName: new("main"), Type: vcs.TypeGit}
	backend := &mockVcs{info: info, totalLines: 20}
	session := model.NewReviewSession(info.RootPath, info.HeadCommit, info.BranchName, model.SourceWorkingTree)
	files := []model.DiffFile{
		makeFileWithHunks("src/x.go", []model.DiffHunk{makeHunk(1, 5)}),
	}
	return NewApp(backend, info, files, session, DiffSource{Kind: DiffSourceWorkingTree})
}

func allCaps() forge.Capabilities {
	return forge.Capabilities{
		DraftReviews: true, Approve: true, RequestChanges: true,
		ReviewSummaries: true, ReviewThreads: true, ThreadResolution: true,
		ThreadOutdated: true, MultiLineComments: true, CommitRangeDiff: true,
		ReviewRequestedFilter: true, AtomicSubmit: true, CommitScopedReviews: true,
	}
}

// prTestApp builds an App in PR mode with one commented diff file.
func prTestApp(t *testing.T, caps forge.Capabilities) *App {
	t.Helper()
	a := newTestApp(t)
	details := &forge.PullRequestDetails{
		PullRequestSummary: forge.PullRequestSummary{
			Repository: forgetypes.Repository{
				Kind: forgetypes.KindGitHub, Host: "github.com", Owner: "o", Name: "r",
			},
			Number: 7,
			State:  "OPEN",
		},
		HeadSHA: "headsha", BaseSHA: "basesha",
	}
	a.Pr = &PrState{Backend: &fakeForge{caps: caps}, Details: details}
	return a
}

//nolint:unparam // path is fixed today; kept for future multi-file cases
func addLineComment(t *testing.T, a *App, path string, line uint32, content string) *model.Comment {
	t.Helper()
	review := a.Session.File(path)
	if review == nil {
		t.Fatalf("file %s missing from session", path)
	}
	side := model.LineSideNew
	c := model.NewComment(content, model.CommentTypeFromID("issue"), &side)
	review.AddLineComment(line, c)
	return c
}

func TestStartSubmitRequiresPrMode(t *testing.T) {
	a := newTestApp(t)
	if a.StartSubmitWith(forge.SubmitComment, false) {
		t.Fatal("submit must fail outside PR mode")
	}
	if a.Message == nil || !strings.Contains(a.Message.Content, "merge request") {
		t.Fatal("must explain PR requirement")
	}
}

func TestStartSubmitReadOnlyPr(t *testing.T) {
	a := prTestApp(t, allCaps())
	a.Pr.Details.Closed = true
	if a.StartSubmitWith(forge.SubmitComment, false) {
		t.Fatal("closed PR must refuse submit")
	}
}

func TestStartSubmitNothingToSubmit(t *testing.T) {
	a := prTestApp(t, allCaps())
	if a.StartSubmitWith(forge.SubmitComment, false) {
		t.Fatal("no comments → nothing to submit")
	}
	// Approve is allowed with no comments.
	if !a.StartSubmitWith(forge.SubmitApprove, false) {
		t.Fatal("approve must proceed without comments")
	}
	if a.InputMode != input.ModeSubmitConfirm {
		t.Fatalf("mode = %v", a.InputMode)
	}
}

func TestStartSubmitMapsComments(t *testing.T) {
	a := prTestApp(t, allCaps())
	// Line 2 exists in the test diff (context line "package x" is line 1).
	c := addLineComment(t, a, "src/x.go", 2, "fix this")
	a.Session.ReviewComments = append(a.Session.ReviewComments,
		model.NewComment("overall", model.CommentTypeFromID("note"), nil))

	if !a.StartSubmitWith(forge.SubmitComment, false) {
		t.Fatal("submit must start")
	}
	s := a.Submit
	if len(s.Mappable) != 1 || s.Mappable[0].Line != 2 {
		t.Fatalf("mappable = %+v", s.Mappable)
	}
	if !strings.Contains(s.Mappable[0].Body, "fix this") {
		t.Fatalf("body = %q", s.Mappable[0].Body)
	}
	if len(s.ReviewComments) != 1 {
		t.Fatalf("review comments = %d", len(s.ReviewComments))
	}
	if s.CommitID != "headsha" {
		t.Fatalf("commit id = %q", s.CommitID)
	}
	if a.InputMode != input.ModeSubmitConfirm {
		t.Fatalf("mode = %v", a.InputMode)
	}
	_ = c
}

func TestSubmitSkipsLockedComments(t *testing.T) {
	a := prTestApp(t, allCaps())
	c := addLineComment(t, a, "src/x.go", 2, "already sent")
	c.LifecycleState = model.LifecycleSubmitted
	if a.StartSubmitWith(forge.SubmitComment, false) {
		t.Fatal("locked-only comments → nothing to submit")
	}
}

func TestResolverFlowAndToggle(t *testing.T) {
	a := prTestApp(t, allCaps())
	// A comment on a line that is not in the diff becomes unmappable.
	addLineComment(t, a, "src/x.go", 9999, "orphan")
	if !a.StartSubmitWith(forge.SubmitComment, false) {
		t.Fatal("submit must start with unmappables")
	}
	if a.InputMode != input.ModeSubmitResolver {
		t.Fatalf("mode = %v", a.InputMode)
	}
	if len(a.Submit.Unmappable) != 1 || a.Submit.Unmappable[0].Action != submit.MoveToSummary {
		t.Fatalf("unmappable = %+v", a.Submit.Unmappable)
	}
	a.ResolverToggle()
	if a.Submit.Unmappable[0].Action != submit.Omit {
		t.Fatal("toggle must flip to omit")
	}
	a.ResolverToggle()
	if len(a.Submit.MovedToSummary()) != 1 {
		t.Fatal("moved-to-summary must include the item")
	}
	a.ResolverAdvance()
	if a.InputMode != input.ModeSubmitConfirm {
		t.Fatalf("mode = %v", a.InputMode)
	}
}

func TestDraftGatedByCapabilities(t *testing.T) {
	caps := allCaps()
	caps.DraftReviews = false
	a := prTestApp(t, caps)
	addLineComment(t, a, "src/x.go", 2, "x")
	if a.StartSubmitWith(forge.SubmitDraft, false) {
		t.Fatal("draft must be refused without the capability")
	}
	if a.Message == nil || !strings.Contains(a.Message.Content, "draft") {
		t.Fatalf("message = %+v", a.Message)
	}
}

func TestMultilineDowngrade(t *testing.T) {
	caps := allCaps()
	caps.MultiLineComments = false
	a := prTestApp(t, caps)
	review := a.Session.File("src/x.go")
	side := model.LineSideNew
	c := model.NewCommentWithRange("ranged", model.CommentTypeFromID("note"), &side, model.NewLineRange(2, 3))
	review.AddLineComment(3, c)

	if !a.StartSubmitWith(forge.SubmitComment, false) {
		t.Fatal("submit must start")
	}
	if len(a.Submit.Mappable) != 1 {
		t.Fatalf("mappable = %+v", a.Submit.Mappable)
	}
	inline := a.Submit.Mappable[0]
	if inline.StartLine != nil {
		t.Fatal("downgrade must collapse the range")
	}
	if !strings.Contains(inline.Body, "Lines 2–3") && !strings.Contains(inline.Body, "Lines 2-3") {
		t.Fatalf("downgraded body = %q", inline.Body)
	}
}

func TestApplySubmitSuccessLocksComments(t *testing.T) {
	a := prTestApp(t, allCaps())
	c := addLineComment(t, a, "src/x.go", 2, "lock me")
	rc := model.NewComment("summary", model.CommentTypeFromID("note"), nil)
	a.Session.ReviewComments = append(a.Session.ReviewComments, rc)
	if !a.StartSubmitWith(forge.SubmitComment, false) {
		t.Fatal("submit must start")
	}

	a.ApplySubmitSuccess(&forge.SubmitResult{ReviewID: "R99"}, forge.SubmitComment)
	if c.LifecycleState != model.LifecycleSubmitted || *c.RemoteReviewID != "R99" {
		t.Fatalf("line comment not locked: %+v", c)
	}
	if rc.LifecycleState != model.LifecycleSubmitted {
		t.Fatal("review comment not locked")
	}
	if a.Submit != nil {
		t.Fatal("submit state must clear")
	}

	// Draft locks as pushed_draft.
	a2 := prTestApp(t, allCaps())
	c2 := addLineComment(t, a2, "src/x.go", 2, "draft me")
	if !a2.StartSubmitWith(forge.SubmitDraft, false) {
		t.Fatal("draft must start")
	}
	a2.ApplySubmitSuccess(&forge.SubmitResult{ReviewID: "D1"}, forge.SubmitDraft)
	if c2.LifecycleState != model.LifecyclePushedDraft {
		t.Fatalf("draft lifecycle = %v", c2.LifecycleState)
	}
}

func TestCancelSubmit(t *testing.T) {
	a := prTestApp(t, allCaps())
	addLineComment(t, a, "src/x.go", 2, "x")
	a.StartSubmitWith(forge.SubmitComment, false)
	a.CancelSubmit()
	if a.Submit != nil || a.InputMode != input.ModeNormal {
		t.Fatal("cancel must clear state")
	}
}

// TestReviewBodyCarriesTheAuthorBadge is the wiring test for the review body:
// the badge rule lives in output, but nothing reaches it unless the submit
// path copies each comment's author across. It did not, so an agent's review
// posted to a forge read as if the submitter had written all of it.
func TestReviewBodyCarriesTheAuthorBadge(t *testing.T) {
	a := prTestApp(t, allCaps())
	a.Username = "ryan"
	theirs := model.NewComment("theirs", model.CommentTypeFromID("note"), nil)
	theirs.Author = "claude"
	mine := model.NewComment("mine", model.CommentTypeFromID("note"), nil)
	mine.Author = "ryan"
	a.Session.ReviewComments = append(a.Session.ReviewComments, theirs, mine)

	if !a.StartSubmitWith(forge.SubmitComment, false) {
		t.Fatal("submit must start")
	}
	body, warnings, err := BuildReviewBody(a, "")
	if err != nil || len(warnings) != 0 {
		t.Fatalf("BuildReviewBody: err=%v warnings=%v", err, warnings)
	}
	if !strings.Contains(body, "**[@claude]** theirs") {
		t.Errorf("someone else's review comment must be badged:\n%s", body)
	}
	if strings.Contains(body, "@ryan") {
		t.Errorf("your own review comment must not be badged:\n%s", body)
	}
}

// TestUnplacedCommentCarriesTheAuthorBadge covers the other half of the body:
// a comment the resolver moved to the summary keeps its attribution too.
func TestUnplacedCommentCarriesTheAuthorBadge(t *testing.T) {
	a := prTestApp(t, allCaps())
	a.Username = "ryan"
	orphan := addLineComment(t, a, "src/x.go", 9999, "orphan")
	orphan.Author = "claude"

	if !a.StartSubmitWith(forge.SubmitComment, false) {
		t.Fatal("submit must start with unmappables")
	}
	if len(a.Submit.MovedToSummary()) != 1 {
		t.Fatalf("expected the orphan to move to the summary: %+v", a.Submit.Unmappable)
	}
	body, _, err := BuildReviewBody(a, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "@claude] src/x.go: orphan") {
		t.Errorf("an unplaced comment must keep its author:\n%s", body)
	}
}

// TestApplySubmitResultPartialLocksOnlyWhatPosted covers the GitLab and
// Azure DevOps contract: the forge posted some inline comments, then
// refused one. Only the posted ones (and the body, which both drivers post
// first) may lock; the rest stay drafts so a second submit carries them.
func TestApplySubmitResultPartialLocksOnlyWhatPosted(t *testing.T) {
	a := prTestApp(t, allCaps())
	first := addLineComment(t, a, "src/x.go", 1, "first")
	second := addLineComment(t, a, "src/x.go", 2, "second")
	third := addLineComment(t, a, "src/x.go", 3, "third")
	body := model.NewComment("summary", model.CommentTypeFromID("note"), nil)
	a.Session.ReviewComments = append(a.Session.ReviewComments, body)
	if !a.StartSubmitWith(forge.SubmitComment, false) {
		t.Fatal("submit must start")
	}
	if len(a.Submit.SentCommentIDs) != 3 {
		t.Fatalf("sent = %v", a.Submit.SentCommentIDs)
	}

	cause := errors.New("HTTP 500")
	outcome := a.ApplySubmitResult(&forge.SubmitResult{
		State: "COMMENTED",
		Partial: &forge.PartialFailure{
			SucceededCommentIDs: []string{first.ID},
			FailedAt:            1,
			Cause:               cause,
		},
	}, forge.SubmitComment)

	if outcome.Complete() {
		t.Fatal("a partial result is not complete")
	}
	if outcome.Posted != 1 || outcome.Unposted != 2 {
		t.Fatalf("outcome = %+v", outcome)
	}
	if first.LifecycleState != model.LifecycleSubmitted {
		t.Error("the posted comment must lock")
	}
	if body.LifecycleState != model.LifecycleSubmitted {
		t.Error("the body posts before any inline comment, so it must lock")
	}
	for _, c := range []*model.Comment{second, third} {
		if c.LifecycleState != model.LifecycleLocalDraft {
			t.Errorf("unposted comment %q locked as %v", c.Content, c.LifecycleState)
		}
	}
	if a.Submit != nil {
		t.Fatal("submit state must clear so the user can submit again")
	}
	msg := outcome.Message()
	if !strings.Contains(msg, "1 of 3") || !strings.Contains(msg, "HTTP 500") || !strings.Contains(msg, "submit again") {
		t.Fatalf("message = %q", msg)
	}

	// A second submit picks up exactly the two that stayed local.
	if !a.StartSubmitWith(forge.SubmitComment, false) {
		t.Fatal("second submit must start")
	}
	if len(a.Submit.SentCommentIDs) != 2 {
		t.Fatalf("retry sent = %v, want the two unposted", a.Submit.SentCommentIDs)
	}
}

func TestApplySubmitResultFinalStepFailure(t *testing.T) {
	a := prTestApp(t, allCaps())
	c := addLineComment(t, a, "src/x.go", 1, "only")
	if !a.StartSubmitWith(forge.SubmitApprove, false) {
		t.Fatal("submit must start")
	}
	outcome := a.ApplySubmitResult(&forge.SubmitResult{
		Partial: &forge.PartialFailure{SucceededCommentIDs: []string{c.ID}, FailedAt: 1, Cause: errors.New("vote refused")},
	}, forge.SubmitApprove)
	if c.LifecycleState != model.LifecycleSubmitted {
		t.Fatal("every inline comment posted, so all lock")
	}
	if msg := outcome.Message(); !strings.Contains(msg, "final step failed") || !strings.Contains(msg, "vote refused") {
		t.Fatalf("message = %q", msg)
	}
}

func TestPickerEventsFollowCapabilities(t *testing.T) {
	caps := allCaps()
	caps.DraftReviews = false
	a := prTestApp(t, caps)
	for _, event := range a.PickerEvents() {
		if event == forge.SubmitDraft {
			t.Fatal("a forge without draft reviews must not offer Draft")
		}
	}
	if got := len(a.PickerEvents()); got != len(SubmitPickerEvents)-1 {
		t.Fatalf("picker has %d rows, want %d", got, len(SubmitPickerEvents)-1)
	}
	if got := len(prTestApp(t, allCaps()).PickerEvents()); got != len(SubmitPickerEvents) {
		t.Fatalf("full capabilities should keep every row, got %d", got)
	}
}
