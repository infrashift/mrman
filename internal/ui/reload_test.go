package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/syntax"
	"github.com/infrashift/mrman/internal/vcs"
)

// reloadDiffBackend re-serves a working-tree diff on demand.
type reloadDiffBackend struct {
	stubBackend
	files []model.DiffFile
	err   error
	calls int
}

func (b *reloadDiffBackend) WorkingTreeDiff(*syntax.Highlighter) ([]model.DiffFile, error) {
	b.calls++
	return b.files, b.err
}

func newReloadBackend(files []model.DiffFile, err error) *reloadDiffBackend {
	return &reloadDiffBackend{
		// An empty root path keeps ignore.Load off the real filesystem.
		stubBackend: stubBackend{info: vcs.Info{HeadCommit: "abc", Type: vcs.TypeGit}},
		files:       files,
		err:         err,
	}
}

// prReloadModel puts the model in PR mode wired to a forge that can refetch.
func prReloadModel(t *testing.T, f *prTabForge) *Model {
	t.Helper()
	m := testModel(t)
	m.App.Pr = &app.PrState{
		Backend: f,
		Details: &forge.PullRequestDetails{
			PullRequestSummary: testPrSummary(7, "the pr", "ana", "feat"),
			HeadSHA:            "headsha", BaseSHA: "basesha",
		},
		Repository: ptrRepo(),
	}
	m.App.DiffSource = app.DiffSource{Kind: app.DiffSourcePullRequest}
	m.forge = staticForgeResolver(f, testRepo())
	m.syncViewport()
	return m
}

func TestReloadCommandRereadsALocalDiff(t *testing.T) {
	m := testModel(t)
	path := "src/x.go"
	fresh := model.DiffFile{
		NewPath: &path, Status: model.StatusModified,
		Hunks: []model.DiffHunk{{
			Lines: []model.DiffLine{
				{Origin: model.OriginAddition, Content: "brand new", NewLineno: new(uint32(1))},
			},
			NewStart: 1, NewCount: 1,
		}},
	}
	fresh.ContentHash = model.ComputeContentHash(fresh.Hunks)
	backend := newReloadBackend([]model.DiffFile{fresh}, nil)
	m.App.VCS = backend

	m.runCommand(input.ParseCommand("reload"))
	runCmd(t, m, m.takeQueued())

	if backend.calls != 1 {
		t.Fatalf(":reload must re-read the working tree, got %d calls", backend.calls)
	}
	if !strings.Contains(plainView(m), "brand new") {
		t.Error(":reload must show the new content")
	}
	if m.App.Message == nil || !strings.Contains(m.App.Message.Content, "Reloaded") {
		t.Errorf("expected a reload summary, got %+v", m.App.Message)
	}
}

func TestReloadCommandReportsFailure(t *testing.T) {
	m := testModel(t)
	m.App.VCS = newReloadBackend(nil, errors.New("index locked"))

	m.runCommand(input.ParseCommand("reload"))
	runCmd(t, m, m.takeQueued())

	if m.App.Message == nil || !strings.Contains(m.App.Message.Content, "index locked") {
		t.Errorf("a failed reload must surface the reason, got %+v", m.App.Message)
	}
}

// prDiff is a small refetchable patch for the PR reload tests.
const prDiff = "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n" +
	"@@ -1,1 +1,2 @@\n package a\n+var Y = 2\n"

func TestReloadInPrModeKeepsTheSessionOnTheSameHead(t *testing.T) {
	f := newPrTabForge()
	f.details = &forge.PullRequestDetails{
		PullRequestSummary: testPrSummary(7, "the pr", "ana", "feat"),
		HeadSHA:            "headsha", BaseSHA: "basesha",
	}
	f.diff = prDiff
	m := prReloadModel(t, f)

	m.runCommand(input.ParseCommand("reload"))
	runCmd(t, m, m.takeQueued())

	if m.App.Pr.Reloading {
		t.Error("a finished reload must clear the spinner")
	}
	if m.App.Pr.Details.HeadSHA != "headsha" {
		t.Error("an unchanged head must keep the same review")
	}
	if !strings.Contains(plainView(m), "var Y = 2") {
		t.Error("the refetched diff must render")
	}
}

func TestReloadInPrModeOpensANewSessionWhenTheHeadMoves(t *testing.T) {
	f := newPrTabForge()
	f.details = &forge.PullRequestDetails{
		PullRequestSummary: testPrSummary(7, "the pr", "ana", "feat"),
		HeadSHA:            "newhead", BaseSHA: "basesha",
	}
	f.diff = prDiff
	m := prReloadModel(t, f)

	m.runCommand(input.ParseCommand("reload"))
	runCmd(t, m, m.takeQueued())

	if m.App.Pr.Details.HeadSHA != "newhead" {
		t.Fatalf("head = %q, want the refetched head", m.App.Pr.Details.HeadSHA)
	}
	if m.App.Session.PrSessionKey == nil || m.App.Session.PrSessionKey.HeadSHA != "newhead" {
		t.Error("a moved head must open the session for the new head")
	}
	if m.App.Message == nil || !strings.Contains(m.App.Message.Content, "advanced") {
		t.Errorf("the reviewer must be told the PR moved, got %+v", m.App.Message)
	}
}

func TestStaleReloadResultIsDiscarded(t *testing.T) {
	f := newPrTabForge()
	f.details = &forge.PullRequestDetails{
		PullRequestSummary: testPrSummary(7, "the pr", "ana", "feat"),
		HeadSHA:            "newhead", BaseSHA: "basesha",
	}
	f.diff = prDiff
	m := prReloadModel(t, f)

	cmd := m.reloadPullRequest()
	msg := cmd()

	// The user switched to a different pull request meanwhile.
	m.App.Pr.Details.Number = 999
	m.Update(msg)

	if m.App.Pr.Details.HeadSHA == "newhead" {
		t.Error("a reload result for a different pull request must be discarded")
	}
}

func TestSubmitConfirmReloadKeyRefetchesTheStalePullRequest(t *testing.T) {
	f := newPrTabForge()
	f.details = &forge.PullRequestDetails{
		PullRequestSummary: testPrSummary(7, "the pr", "ana", "feat"),
		HeadSHA:            "headsha", BaseSHA: "basesha",
	}
	f.diff = prDiff
	m := prReloadModel(t, f)
	m.App.InputMode = input.ModeSubmitConfirm
	m.App.Submit = &app.SubmitState{}

	pressPrRune(t, m, 'r')

	if f.openSeen == 0 {
		t.Error("r in the submit confirmation must refetch the pull request")
	}
	if m.App.Submit != nil {
		t.Error("reloading must cancel the stale submit")
	}
}
