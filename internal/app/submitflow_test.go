package app

import (
	"context"
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
	info := &vcs.Info{RootPath: "/tmp", HeadCommit: "abc123", BranchName: strPtr("main"), Type: vcs.TypeGit}
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
	if a.Message == nil || !strings.Contains(a.Message.Content, "pull request") {
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
