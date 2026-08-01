package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/input"
)

// remoteForge serves canned review threads and summaries.
type remoteForge struct {
	*uiFakeForge
	threads     []forge.RemoteReviewThread
	summaries   []forge.RemoteReviewSummary
	threadsErr  error
	hasSummary  bool
	threadCalls int
}

func (f *remoteForge) Capabilities() forge.Capabilities {
	caps := f.uiFakeForge.Capabilities()
	caps.ReviewSummaries = f.hasSummary
	return caps
}

func (f *remoteForge) ListReviewThreads(context.Context, *forge.PullRequestDetails) ([]forge.RemoteReviewThread, error) {
	f.threadCalls++
	if f.threadsErr != nil {
		return nil, f.threadsErr
	}
	return f.threads, nil
}

func (f *remoteForge) ListReviewSummaries(context.Context, *forge.PullRequestDetails) ([]forge.RemoteReviewSummary, error) {
	return f.summaries, nil
}

// remotePrModel puts the model in PR mode against a fake forge and drains
// the discussion fetch.
func remotePrModel(t *testing.T, f *remoteForge) *Model {
	t.Helper()
	m := testModel(t)
	// Thread boxes are tall; give the pane room so assertions about the
	// rendered view are not really assertions about clipping.
	m.height = 60
	m.App.Pr = &app.PrState{
		Backend: f,
		Details: &forge.PullRequestDetails{
			PullRequestSummary: forge.PullRequestSummary{Repository: testRepo(), Number: 7, Title: "the pr"},
			HeadSHA:            "head", BaseSHA: "base",
		},
		Repository: ptrRepo(),
	}
	m.App.DiffSource = app.DiffSource{Kind: app.DiffSourcePullRequest}
	m.syncViewport()
	runCmd(t, m, m.loadRemoteCommentsOnOpen())
	return m
}

func ptrRepo() *forgetypes.Repository {
	r := testRepo()
	return &r
}

// remoteThread anchors to the shared test diff's only file.
func remoteThread(line uint32, resolved bool, author, body string) forge.RemoteReviewThread {
	l := line
	return forge.RemoteReviewThread{
		ID: author + body, Path: "src/x.go", Line: &l, Side: forge.SideNew,
		IsResolved: resolved,
		Comments:   []forge.RemoteReviewComment{{ID: "c1", Author: author, Body: body}},
	}
}

func TestRemoteThreadsRenderInTheDiff(t *testing.T) {
	f := &remoteForge{
		uiFakeForge: &uiFakeForge{},
		threads:     []forge.RemoteReviewThread{remoteThread(2, false, "ana", "why 10 here?")},
	}
	m := remotePrModel(t, f)

	out := viewString(m)
	for _, want := range []string{"@ana", "why 10 here?", "╒══", "║"} {
		if !strings.Contains(out, want) {
			t.Errorf("remote thread missing %q in view", want)
		}
	}
}

func TestRemoteSummaryRendersWithStateBadge(t *testing.T) {
	f := &remoteForge{
		uiFakeForge: &uiFakeForge{},
		hasSummary:  true,
		summaries: []forge.RemoteReviewSummary{
			{ID: "r1", Author: "bo", Body: "Ship it.", State: forge.ReviewApproved},
		},
	}
	m := remotePrModel(t, f)

	out := viewString(m)
	for _, want := range []string{"@bo", "Ship it.", "approved"} {
		if !strings.Contains(out, want) {
			t.Errorf("remote summary missing %q in view", want)
		}
	}
}

func TestReviewSummariesSkippedWithoutTheCapability(t *testing.T) {
	f := &remoteForge{
		uiFakeForge: &uiFakeForge{},
		hasSummary:  false,
		summaries: []forge.RemoteReviewSummary{
			{ID: "r1", Author: "bo", Body: "Ship it.", State: forge.ReviewApproved},
		},
		threads: []forge.RemoteReviewThread{remoteThread(2, false, "ana", "inline")},
	}
	m := remotePrModel(t, f)

	if len(m.App.Pr.Summaries) != 0 {
		t.Error("a forge without ReviewSummaries must not be asked for them")
	}
	if len(m.App.Pr.Threads) != 1 {
		t.Error("inline threads must still load on a forge without summaries")
	}
}

func TestCommentsVisibilityCommands(t *testing.T) {
	f := &remoteForge{
		uiFakeForge: &uiFakeForge{},
		threads: []forge.RemoteReviewThread{
			remoteThread(2, false, "ana", "still open here"),
			remoteThread(3, true, "bo", "already settled"),
		},
	}
	m := remotePrModel(t, f)

	// Default is unresolved-only.
	if out := viewString(m); strings.Contains(out, "already settled") {
		t.Error("resolved threads must be hidden by default")
	}

	m.runCommand(input.ParseCommand("comments all"))
	runCmd(t, m, m.takeQueued())
	if out := viewString(m); !strings.Contains(out, "already settled") {
		t.Error(":comments all must reveal resolved threads")
	}

	m.runCommand(input.ParseCommand("comments hide"))
	runCmd(t, m, m.takeQueued())
	out := viewString(m)
	if strings.Contains(out, "still open here") || strings.Contains(out, "already settled") {
		t.Error(":comments hide must suppress every remote thread")
	}

	m.runCommand(input.ParseCommand("comments unresolved"))
	runCmd(t, m, m.takeQueued())
	if out := viewString(m); !strings.Contains(out, "still open here") {
		t.Error(":comments unresolved must restore active threads")
	}
}

func TestCommentsCommandOutsidePrModeExplains(t *testing.T) {
	m := testModel(t)
	m.runCommand(input.ParseCommand("comments all"))
	if m.App.Message == nil || !strings.Contains(m.App.Message.Content, "merge request") {
		t.Errorf("expected an explanatory message, got %+v", m.App.Message)
	}
}

func TestRemoteThreadFetchFailureWarns(t *testing.T) {
	f := &remoteForge{uiFakeForge: &uiFakeForge{}, threadsErr: errors.New("rate limited")}
	m := remotePrModel(t, f)

	if m.App.Message == nil || !strings.Contains(m.App.Message.Content, "rate limited") {
		t.Errorf("a failed discussion fetch must warn, got %+v", m.App.Message)
	}
	if m.App.Pr.ThreadsLoading {
		t.Error("a failed fetch must clear the loading flag")
	}
}

func TestRemoteThreadsAreNotFetchedTwiceOnOpen(t *testing.T) {
	f := &remoteForge{
		uiFakeForge: &uiFakeForge{},
		threads:     []forge.RemoteReviewThread{remoteThread(2, false, "ana", "x")},
	}
	m := remotePrModel(t, f)

	// Init runs on the first frame; it must not duplicate the open fetch.
	runCmd(t, m, m.loadRemoteCommentsOnOpen())
	if f.threadCalls > 2 {
		t.Errorf("discussion fetches = %d; opening must not fan out", f.threadCalls)
	}
	if len(m.App.Pr.Threads) != 1 {
		t.Errorf("threads = %d, want 1", len(m.App.Pr.Threads))
	}
}

func TestRemoteThreadRowsAreNotEditable(t *testing.T) {
	f := &remoteForge{
		uiFakeForge: &uiFakeForge{},
		threads:     []forge.RemoteReviewThread{remoteThread(2, false, "ana", "why 10?")},
	}
	m := remotePrModel(t, f)

	for i := range m.App.LineAnnotations {
		if m.App.LineAnnotations[i].Kind == app.AnnRemoteThreadLine {
			m.App.DiffState.CursorLine = i
			break
		}
	}
	pressRune(m, 'd')
	pressRune(m, 'd')
	if m.App.Message == nil || !strings.Contains(m.App.Message.Content, "read-only") {
		t.Errorf("dd on a forge comment must explain why, got %+v", m.App.Message)
	}
	if len(m.App.Pr.Threads) != 1 {
		t.Error("dd must not remove a forge thread")
	}
}

func TestPrHeaderShowsIdentityAndState(t *testing.T) {
	f := &remoteForge{
		uiFakeForge: &uiFakeForge{},
		threads: []forge.RemoteReviewThread{
			remoteThread(2, false, "ana", "open one"),
			remoteThread(3, true, "bo", "closed one"),
		},
	}
	m := remotePrModel(t, f)

	out := viewString(m)
	for _, want := range []string{"acme/widget#7", "the pr", "OPEN"} {
		if !strings.Contains(out, want) {
			t.Errorf("PR header missing %q", want)
		}
	}
	// Default visibility is unresolved-only, so the count reflects what is
	// on screen rather than what was fetched.
	if !strings.Contains(out, "1 thread") {
		t.Errorf("header must report the visible thread count, got:\n%s", out)
	}
}

func TestPrHeaderMarksReadOnlyPullRequests(t *testing.T) {
	m := remotePrModel(t, &remoteForge{uiFakeForge: &uiFakeForge{}})
	merged := time.Now()
	m.App.Pr.Details.MergedAt = &merged

	out := viewString(m)
	for _, want := range []string{"MERGED", "read only"} {
		if !strings.Contains(out, want) {
			t.Errorf("a merged PR must be marked %q in the header, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "OPEN") {
		t.Error("a merged PR must not read as open")
	}
}

func TestPrHeaderShowsInFlightWork(t *testing.T) {
	m := remotePrModel(t, &remoteForge{uiFakeForge: &uiFakeForge{}})
	for _, tc := range []struct {
		set  func()
		want string
	}{
		{func() { m.App.Pr.Submitting = true }, "pushing review"},
		{func() { m.App.Pr.Submitting, m.App.Pr.Reloading = false, true }, "reloading"},
		{func() { m.App.Pr.Reloading, m.App.Pr.Opening = false, true }, "opening"},
	} {
		tc.set()
		if out := viewString(m); !strings.Contains(out, tc.want) {
			t.Errorf("header must report %q while it is in flight", tc.want)
		}
	}
}

// TestQueuedWorkFromAMessageIsIssued pins the bug that only showed on
// screen: handlers reached by a message — not a keypress — queue async work
// too, and draining only in handleKey stranded it. The visible symptom was a
// "reloading…" spinner that never cleared, because the reload it belonged to
// was never issued.
func TestQueuedWorkFromAMessageIsIssued(t *testing.T) {
	f := &remoteForge{uiFakeForge: &uiFakeForge{}}
	m := remotePrModel(t, f)

	// Stand in for any handler that queues from a message path.
	m.queue(func() tea.Msg { return nil })

	_, cmd := m.Update(remoteCommentsResultMsg{
		Gen: m.App.Pr.Gens.PrThreads + 1, // stale on purpose: the drain must
		Key: app.PrKey{},                 // happen regardless of the outcome
	})
	if cmd == nil {
		t.Fatal("Update must issue work queued by a message handler")
	}
	if m.queuedCmd != nil {
		t.Error("the queue must be empty after Update drains it")
	}
}

// TestReviewMetadataReloadClearsTheSpinner covers the same bug end to end:
// applying review metadata queues a reload, and if that reload is never
// issued the Reloading flag it set stays raised forever.
func TestReviewMetadataReloadClearsTheSpinner(t *testing.T) {
	f := &remoteForge{uiFakeForge: &uiFakeForge{}}
	m := remotePrModel(t, f)
	m.App.ShowCommitSelector = true
	m.App.SetupPrCommitSelector([]forge.Commit{
		{OID: "c1", ShortOID: "c1", Summary: "one"},
		{OID: "c2", ShortOID: "c2", Summary: "two"},
	})

	_, cmd := m.Update(remoteCommentsResultMsg{
		Gen: m.App.Pr.Gens.PrThreads,
		Key: mustPrKey(t, m),
		Meta: &forge.ReviewMetadata{
			ViewerLogin: "ryancraig",
			Reviews:     []forge.ReviewRecord{{Author: "ryancraig", CommitOID: "c1"}},
		},
	})
	runCmd(t, m, cmd)

	if m.App.Pr.Reloading {
		t.Error("a queued reload must actually run, or its spinner never clears")
	}
}

func mustPrKey(t *testing.T, m *Model) app.PrKey {
	t.Helper()
	key, ok := m.App.Pr.CurrentPrKey()
	if !ok {
		t.Fatal("expected a PR key")
	}
	return key
}
