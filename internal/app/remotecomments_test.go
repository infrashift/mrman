package app

import (
	"strings"
	"testing"
	"time"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

func thread(id, path string, line uint32, side forge.Side, resolved, outdated bool,
	comments ...forge.RemoteReviewComment) forge.RemoteReviewThread {
	t := forge.RemoteReviewThread{
		ID: id, Path: path, Side: side,
		IsResolved: resolved, IsOutdated: outdated, Comments: comments,
	}
	if line > 0 {
		t.Line = u32(line)
	}
	return t
}

func rComment(author, body string) forge.RemoteReviewComment {
	return forge.RemoteReviewComment{ID: author + body, Author: author, Body: body}
}

// prModeApp returns an app in PR mode over the standard one-file test diff.
func prModeApp(t *testing.T) *App {
	t.Helper()
	a := newTestApp(t)
	a.Pr = &PrState{
		Backend: &fakeForge{},
		Details: &forge.PullRequestDetails{
			PullRequestSummary: forge.PullRequestSummary{Repository: prRepo(), Number: 7},
			HeadSHA:            "head", BaseSHA: "base",
		},
	}
	a.DiffSource = DiffSource{Kind: DiffSourcePullRequest}
	a.DiffState.ViewportWidth = 80
	return a
}

// loadThreads applies threads and summaries through the real staleness path.
func loadThreads(t *testing.T, a *App, threads []forge.RemoteReviewThread,
	summaries []forge.RemoteReviewSummary) {
	t.Helper()
	if !a.RequestRemoteComments() {
		t.Fatal("expected a remote-comments fetch to be armed")
	}
	req, ok := a.TakeRemoteCommentsLoad()
	if !ok {
		t.Fatal("expected a pending remote-comments request")
	}
	a.ApplyRemoteComments(req.Gen, req.Key, threads, summaries)
}

func TestRemoteCommentsVisibilityFiltersThreads(t *testing.T) {
	a := prModeApp(t)
	threads := []forge.RemoteReviewThread{
		thread("t1", "src/x.go", 1, forge.SideNew, false, false, rComment("ana", "active")),
		thread("t2", "src/x.go", 2, forge.SideNew, true, false, rComment("bo", "resolved")),
		thread("t3", "src/x.go", 3, forge.SideNew, false, true, rComment("cy", "outdated")),
	}
	loadThreads(t, a, threads, nil)

	// Default: unresolved only.
	if got := len(a.VisibleRemoteThreads()); got != 1 {
		t.Errorf("unresolved visibility must show only active threads, got %d", got)
	}

	a.SetRemoteCommentsVisibility(forgetypes.VisibilityAll)
	if got := len(a.VisibleRemoteThreads()); got != 3 {
		t.Errorf("all visibility must show every thread, got %d", got)
	}

	a.SetRemoteCommentsVisibility(forgetypes.VisibilityHide)
	if got := len(a.VisibleRemoteThreads()); got != 0 {
		t.Errorf("hide visibility must show none, got %d", got)
	}
	if got := len(a.VisibleRemoteSummaries()); got != 0 {
		t.Errorf("hide must suppress summaries too, got %d", got)
	}
}

func TestRemoteCommentsVisibilityPersistsOnSession(t *testing.T) {
	a := prModeApp(t)
	a.SetRemoteCommentsVisibility(forgetypes.VisibilityAll)

	if a.Session.RemoteCommentsVisibility != forgetypes.VisibilityAll {
		t.Error("visibility must be recorded on the session so it survives a restart")
	}
	if !a.Dirty {
		t.Error("changing visibility must mark the session dirty")
	}
}

func TestSetRemoteCommentsVisibilityOutsidePrModeIsRejected(t *testing.T) {
	a := newTestApp(t)
	if a.SetRemoteCommentsVisibility(forgetypes.VisibilityAll) {
		t.Error("no fetch should be requested outside PR mode")
	}
	if a.Message == nil || !strings.Contains(a.Message.Content, "merge request") {
		t.Errorf("expected an explanatory message, got %+v", a.Message)
	}
}

func TestRemoteThreadsAnnotateAtTheirAnchorLine(t *testing.T) {
	a := prModeApp(t)
	// The shared test diff has src/x.go with new-side lines 1..3.
	loadThreads(t, a, []forge.RemoteReviewThread{
		thread("t1", "src/x.go", 2, forge.SideNew, false, false, rComment("ana", "why 10?")),
	}, nil)

	var threadRows int
	for i := range a.LineAnnotations {
		if a.LineAnnotations[i].Kind == AnnRemoteThreadLine {
			threadRows++
		}
	}
	if threadRows == 0 {
		t.Fatal("a remote thread anchored inside the diff must render")
	}
	// Height must match the declared display height, or scroll math drifts.
	want := RemoteThreadDisplayLines(&a.Pr.Threads[0], a.DiffState.ViewportWidth)
	if threadRows != want {
		t.Errorf("thread rows = %d, declared height = %d", threadRows, want)
	}
}

func TestUnanchoredRemoteThreadsRenderAtFileScope(t *testing.T) {
	a := prModeApp(t)
	// Line == nil means the forge could no longer anchor the thread.
	loadThreads(t, a, []forge.RemoteReviewThread{
		thread("t1", "src/x.go", 0, forge.SideNew, false, false, rComment("ana", "stale")),
	}, nil)

	found := false
	for i := range a.LineAnnotations {
		if a.LineAnnotations[i].Kind == AnnRemoteThreadLine {
			found = true
			break
		}
	}
	if !found {
		t.Error("a thread whose anchor is gone must stay reachable at file scope")
	}
}

func TestRemoteSummariesRenderAtReviewScopeAndMatchHeightMath(t *testing.T) {
	a := prModeApp(t)
	loadThreads(t, a, nil, []forge.RemoteReviewSummary{
		{ID: "r1", Author: "ana", Body: "Looks good overall.", State: forge.ReviewApproved},
	})

	var summaryRows int
	for i := range a.LineAnnotations {
		if a.LineAnnotations[i].Kind == AnnRemoteReviewSummaryLine {
			summaryRows++
		}
	}
	want := RemoteSummaryDisplayLines(&a.Pr.Summaries[0], a.DiffState.ViewportWidth)
	if summaryRows != want {
		t.Fatalf("summary rows = %d, declared height = %d", summaryRows, want)
	}

	// reviewCommentsRenderHeight must mirror the emitted rows exactly, or
	// every scroll offset below the review area is wrong.
	header := 1
	if got := a.reviewCommentsRenderHeight(); got != header+want {
		t.Errorf("reviewCommentsRenderHeight = %d, want %d", got, header+want)
	}
}

func TestRemoteCommentsDiscardStaleResults(t *testing.T) {
	a := prModeApp(t)
	a.RequestRemoteComments()
	stale, _ := a.TakeRemoteCommentsLoad()

	// A reload supersedes the in-flight fetch.
	a.RequestRemoteComments()
	fresh, _ := a.TakeRemoteCommentsLoad()

	a.ApplyRemoteComments(stale.Gen, stale.Key, []forge.RemoteReviewThread{
		thread("stale", "src/x.go", 1, forge.SideNew, false, false, rComment("zz", "old")),
	}, nil)
	if len(a.Pr.Threads) != 0 {
		t.Error("a superseded fetch must not overwrite fresher state")
	}

	a.ApplyRemoteComments(fresh.Gen, fresh.Key, []forge.RemoteReviewThread{
		thread("fresh", "src/x.go", 1, forge.SideNew, false, false, rComment("ana", "new")),
	}, nil)
	if len(a.Pr.Threads) != 1 || a.Pr.Threads[0].ID != "fresh" {
		t.Errorf("the current fetch must apply, got %+v", a.Pr.Threads)
	}
	if a.Pr.ThreadsLoading {
		t.Error("a delivered fetch must clear the loading flag")
	}
}

func TestRemoteCommentsDiscardResultsForADifferentPullRequest(t *testing.T) {
	a := prModeApp(t)
	a.RequestRemoteComments()
	req, _ := a.TakeRemoteCommentsLoad()

	// The user switched pull requests while the fetch was in flight.
	other := req.Key
	other.Number = 999
	a.ApplyRemoteComments(req.Gen, other, []forge.RemoteReviewThread{
		thread("wrong", "src/x.go", 1, forge.SideNew, false, false, rComment("zz", "wrong pr")),
	}, nil)

	if len(a.Pr.Threads) != 0 {
		t.Error("a result for a different pull request must be discarded")
	}
}

func TestRemoteCommentsFetchSkippedWhenHidden(t *testing.T) {
	a := prModeApp(t)
	a.Session.RemoteCommentsVisibility = forgetypes.VisibilityHide
	if a.RequestRemoteComments() {
		t.Error("hidden discussions must not be fetched")
	}
}

func TestRemoteCommentsFailureWarnsWithoutClearingState(t *testing.T) {
	a := prModeApp(t)
	loadThreads(t, a, []forge.RemoteReviewThread{
		thread("t1", "src/x.go", 1, forge.SideNew, false, false, rComment("ana", "keep me")),
	}, nil)

	a.RequestRemoteComments()
	req, _ := a.TakeRemoteCommentsLoad()
	a.FailRemoteComments(req.Gen, req.Key, "rate limited")

	if len(a.Pr.Threads) != 1 {
		t.Error("a failed refresh must not discard already-loaded discussions")
	}
	if a.Message == nil || a.Message.Type != MessageWarning {
		t.Errorf("a failed fetch must warn, not error, got %+v", a.Message)
	}
	if a.Pr.ThreadsLoading {
		t.Error("a failed fetch must clear the loading flag")
	}
}

func TestRemoteCommentsAreReadOnly(t *testing.T) {
	a := prModeApp(t)
	loadThreads(t, a, []forge.RemoteReviewThread{
		thread("t1", "src/x.go", 2, forge.SideNew, false, false, rComment("ana", "why 10?")),
	}, nil)

	// Park the cursor on the thread box.
	for i := range a.LineAnnotations {
		if a.LineAnnotations[i].Kind == AnnRemoteThreadLine {
			a.DiffState.CursorLine = i
			break
		}
	}
	if !a.CursorOnRemoteComment() {
		t.Fatal("cursor should be on a remote comment row")
	}
	if a.DeleteCommentAtCursor() {
		t.Error("dd must not delete a forge comment")
	}
	if a.Message == nil || !strings.Contains(a.Message.Content, "read-only") {
		t.Errorf("deleting a forge comment must explain why, got %+v", a.Message)
	}
	if a.EnterEditMode(false) {
		t.Error("i must not edit a forge comment")
	}
}

func TestRemoteThreadsJoinCommentNavigation(t *testing.T) {
	a := prModeApp(t)
	loadThreads(t, a, []forge.RemoteReviewThread{
		thread("t1", "src/x.go", 2, forge.SideNew, false, false, rComment("ana", "why 10?")),
	}, []forge.RemoteReviewSummary{
		{ID: "r1", Author: "bo", Body: "LGTM", State: forge.ReviewApproved},
	})

	items := a.BuildCommentNavigatorItems()
	if len(items) != 2 {
		t.Fatalf("m/M must walk remote discussions too, got %d items", len(items))
	}
	for _, item := range items {
		if !item.IsRemote {
			t.Errorf("forge-owned item must be flagged remote: %+v", item)
		}
	}
	if items[0].Key.Scope != NavScopeRemoteSummary {
		t.Error("review summaries render first, so they navigate first")
	}
	if items[1].Author != "ana" {
		t.Errorf("thread item must carry its root author, got %q", items[1].Author)
	}
}

func TestRemoteThreadSegmentsRenderRepliesAndBadges(t *testing.T) {
	th := thread("t1", "a.go", 1, forge.SideNew, true, true,
		rComment("ana", "first"), rComment("bo", "reply"))
	when := time.Now().Add(-2 * time.Hour)
	th.Comments[0].CreatedAt = &when

	segments := RemoteThreadSegments(&th, 80)
	joined := strings.Join(segments, "\n")
	for _, want := range []string{"@ana", "first", "↳ @bo", "reply", "2h"} {
		if !strings.Contains(joined, want) {
			t.Errorf("thread segments missing %q in:\n%s", want, joined)
		}
	}
	if got := RemoteThreadBadge(&th); got != "resolved · outdated" {
		t.Errorf("badge = %q", got)
	}
}

func TestRemoteThreadBadgeVariants(t *testing.T) {
	for _, tc := range []struct {
		resolved, outdated bool
		want               string
	}{
		{false, false, ""},
		{true, false, "resolved"},
		{false, true, "outdated"},
		{true, true, "resolved · outdated"},
	} {
		th := thread("t", "a.go", 1, forge.SideNew, tc.resolved, tc.outdated)
		if got := RemoteThreadBadge(&th); got != tc.want {
			t.Errorf("resolved=%v outdated=%v: badge = %q, want %q",
				tc.resolved, tc.outdated, got, tc.want)
		}
	}
}

// TestRemoteThreadBadgeDisposition covers a forge that distinguishes more
// than resolved/unresolved. Azure DevOps has four terminal thread states and
// one open-but-waiting state, and collapsing them all to "resolved" throws
// away the difference between "fixed" and "won't fix". A driver supplies its
// own wording in Disposition and it wins over the generic label; drivers
// without one leave it empty and keep the old behavior.
func TestRemoteThreadBadgeDisposition(t *testing.T) {
	for _, tc := range []struct {
		name               string
		disposition        string
		resolved, outdated bool
		want               string
	}{
		{"wont fix", "won't fix", true, false, "won't fix"},
		{"by design", "by design", true, false, "by design"},
		{"closed", "closed", true, false, "closed"},
		{"fixed reads as resolved", "resolved", true, false, "resolved"},
		// Pending is unresolved, so it gets no badge today; a disposition
		// gives an open-but-waiting thread a label it could not otherwise
		// have.
		{"pending is unresolved but labelled", "pending", false, false, "pending"},
		{"disposition composes with outdated", "won't fix", true, true, "won't fix · outdated"},
		{"unresolved pending plus outdated", "pending", false, true, "pending · outdated"},
		// Forges with no richer state must be untouched.
		{"empty falls back to resolved", "", true, false, "resolved"},
		{"empty and unresolved stays bare", "", false, false, ""},
		{"empty unresolved outdated", "", false, true, "outdated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			th := thread("t", "a.go", 1, forge.SideNew, tc.resolved, tc.outdated)
			th.Disposition = tc.disposition
			if got := RemoteThreadBadge(&th); got != tc.want {
				t.Errorf("badge = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestTotalLinesAgreesWithAnnotationsWithRemoteComments pins the invariant
// that silently truncates the diff pane when broken: TotalLines() and the
// annotation stream are computed independently and must agree exactly.
func TestTotalLinesAgreesWithAnnotationsWithRemoteComments(t *testing.T) {
	for _, tc := range []struct {
		name      string
		threads   []forge.RemoteReviewThread
		summaries []forge.RemoteReviewSummary
		view      DiffViewMode
	}{
		{name: "no remote content", view: ViewUnified},
		{
			name: "anchored thread, unified",
			view: ViewUnified,
			threads: []forge.RemoteReviewThread{
				thread("t1", "src/x.go", 2, forge.SideNew, false, false, rComment("ana", "why 10?")),
			},
		},
		{
			name: "anchored thread, side-by-side",
			view: ViewSideBySide,
			threads: []forge.RemoteReviewThread{
				thread("t1", "src/x.go", 2, forge.SideNew, false, false, rComment("ana", "why 10?")),
			},
		},
		{
			name: "old-side thread",
			view: ViewUnified,
			threads: []forge.RemoteReviewThread{
				thread("t1", "src/x.go", 2, forge.SideOld, false, false, rComment("ana", "was this needed?")),
			},
		},
		{
			name: "file-scope thread with no anchor",
			view: ViewUnified,
			threads: []forge.RemoteReviewThread{
				thread("t1", "src/x.go", 0, forge.SideNew, false, false, rComment("ana", "outdated")),
			},
		},
		{
			name: "multiple threads plus summary",
			view: ViewUnified,
			threads: []forge.RemoteReviewThread{
				thread("t1", "src/x.go", 1, forge.SideNew, false, false, rComment("ana", "one")),
				thread("t2", "src/x.go", 2, forge.SideNew, false, false,
					rComment("bo", "two"), rComment("cy", "a reply\nspanning lines")),
			},
			summaries: []forge.RemoteReviewSummary{
				{ID: "r1", Author: "dee", Body: "Overall fine.\nA second paragraph.", State: forge.ReviewCommented},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := prModeApp(t)
			a.DiffViewMode = tc.view
			loadThreads(t, a, tc.threads, tc.summaries)

			if got, want := a.TotalLines(), len(a.LineAnnotations); got != want {
				t.Errorf("TotalLines() = %d but the stream has %d rows; the diff pane "+
					"would truncate at row %d", got, want, min(got, want))
			}
		})
	}
}
