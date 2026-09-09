package app

import (
	"testing"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs"
)

func prRepo() forgetypes.Repository {
	return forgetypes.Repository{Kind: forgetypes.KindGitHub, Host: "github.com", Owner: "acme", Name: "widget"}
}

func prSummary(number uint64, title, author, branch string) forge.PullRequestSummary {
	return forge.PullRequestSummary{
		Repository: prRepo(), Number: number, Title: title,
		Author: author, HeadRefName: branch, State: "open",
	}
}

// prTabApp returns an app whose PR tab already holds a loaded page, with the
// generation counter left where requestPrTabLoad would have set it.
func prTabApp(t *testing.T, rows []forge.PullRequestSummary, nextPage string) *App {
	t.Helper()
	a := &App{
		TargetTab:          TargetTabPullRequests,
		ExpandedDirs:       map[string]bool{},
		ExpandedTop:        map[GapID][]model.DiffLine{},
		ExpandedBottom:     map[GapID][]model.DiffLine{},
		FileLineCountCache: map[int]uint32{},
		CommitDiffCache:    map[model.IndexRange][]model.DiffFile{},
	}
	a.onTargetTabEntered()
	req, ok := a.TakePrTabLoad()
	if !ok {
		t.Fatal("entering the PR tab must arm a load")
	}
	a.ApplyPrTabPage(req.Gen, &forge.PullRequestPage{Items: rows, NextPageToken: nextPage}, false)
	return a
}

func TestEnteringPrTabArmsExactlyOneLoad(t *testing.T) {
	a := &App{TargetTab: TargetTabPullRequests}
	a.onTargetTabEntered()

	req, ok := a.TakePrTabLoad()
	if !ok {
		t.Fatal("first entry must arm a load")
	}
	if req.Append || req.PageToken != "" {
		t.Errorf("first load must be a fresh first page, got %+v", req)
	}
	if _, again := a.TakePrTabLoad(); again {
		t.Error("the pending load must be drained exactly once")
	}

	// Re-entering an already-loaded tab must not refetch.
	a.ApplyPrTabPage(req.Gen, &forge.PullRequestPage{}, false)
	a.onTargetTabEntered()
	if _, ok := a.TakePrTabLoad(); ok {
		t.Error("revisiting a loaded tab must not refetch")
	}
}

func TestPrTabFilterMatchesNumberTitleAuthorAndBranch(t *testing.T) {
	rows := []forge.PullRequestSummary{
		prSummary(11, "Add retry logic", "ana", "feat/retry"),
		prSummary(22, "Fix flaky test", "bo", "fix/flake"),
		prSummary(33, "Docs pass", "ana", "docs/pass"),
	}
	for _, tc := range []struct {
		query string
		want  int
	}{
		{"", 3},
		{"22", 1},      // number
		{"flaky", 1},   // title
		{"ana", 2},     // author
		{"feat/", 1},   // head branch
		{"FLAKY", 1},   // case-insensitive
		{"  ana  ", 2}, // trimmed
		{"nothing", 0}, // no match
	} {
		a := prTabApp(t, rows, "")
		a.Pr.TabFilter = tc.query
		if got := len(a.PrTabFilteredRows()); got != tc.want {
			t.Errorf("filter %q: got %d rows, want %d", tc.query, got, tc.want)
		}
	}
}

func TestPrTabFilterClampsCursorAndSurvivesCancel(t *testing.T) {
	rows := []forge.PullRequestSummary{
		prSummary(1, "one", "ana", "a"),
		prSummary(2, "two", "bo", "b"),
		prSummary(3, "three", "cy", "c"),
	}
	a := prTabApp(t, rows, "")
	a.Pr.TabCursor = 2

	a.BeginPrTabFilter()
	if !a.PrTabFilterEditing() {
		t.Fatal("/ must open the filter prompt")
	}
	for _, r := range "one" {
		a.InsertPrTabFilterChar(r)
	}
	if got := len(a.PrTabFilteredRows()); got != 1 {
		t.Fatalf("got %d filtered rows, want 1", got)
	}
	if a.Pr.TabCursor != 0 {
		t.Errorf("cursor must clamp into the shrunken list, got %d", a.Pr.TabCursor)
	}

	// Esc clears the query and restores the full list.
	a.CancelPrTabFilter()
	if a.PrTabFilterEditing() || a.Pr.TabFilter != "" {
		t.Error("cancel must close the prompt and clear the query")
	}
	if got := len(a.PrTabFilteredRows()); got != 3 {
		t.Errorf("got %d rows after cancel, want 3", got)
	}
}

func TestPrTabFilterEditingPrimitives(t *testing.T) {
	a := prTabApp(t, nil, "")
	for _, r := range "one two" {
		a.InsertPrTabFilterChar(r)
	}
	a.DeletePrTabFilterChar()
	if a.Pr.TabFilter != "one tw" {
		t.Errorf("backspace: got %q", a.Pr.TabFilter)
	}
	a.DeletePrTabFilterWord()
	if a.Pr.TabFilter != "one " {
		t.Errorf("delete-word: got %q", a.Pr.TabFilter)
	}
	a.ClearPrTabFilter()
	if a.Pr.TabFilter != "" {
		t.Errorf("clear-line: got %q", a.Pr.TabFilter)
	}
	// Enter keeps the query and closes the prompt.
	a.InsertPrTabFilterChar('x')
	a.CommitPrTabFilter()
	if a.PrTabFilterEditing() || a.Pr.TabFilter != "x" {
		t.Errorf("commit must keep %q and close the prompt", "x")
	}
}

func TestPrTabPagingAppendsAndTracksLoadMoreRow(t *testing.T) {
	a := prTabApp(t, []forge.PullRequestSummary{prSummary(1, "one", "ana", "a")}, "cursor-2")
	if !a.CanLoadMorePrs() {
		t.Fatal("a next-page token must surface the load-more row")
	}
	if got := a.PrTabRowCount(); got != 2 {
		t.Fatalf("row count with load-more: got %d, want 2", got)
	}

	a.PrTabDown()
	if !a.IsOnPrLoadMoreRow() {
		t.Fatal("cursor must be able to reach the load-more row")
	}
	if _, ok := a.PrTabSelected(); ok {
		t.Error("the load-more row is not a selectable pull request")
	}

	if !a.LoadMorePrs() {
		t.Fatal("load-more must arm a fetch")
	}
	req, ok := a.TakePrTabLoad()
	if !ok || !req.Append || req.PageToken != "cursor-2" {
		t.Fatalf("load-more must append from the page token, got %+v ok=%v", req, ok)
	}

	a.ApplyPrTabPage(req.Gen, &forge.PullRequestPage{
		Items: []forge.PullRequestSummary{prSummary(2, "two", "bo", "b")},
	}, true)
	if got := len(a.Pr.TabRows); got != 2 {
		t.Errorf("append must keep the first page, got %d rows", got)
	}
	if a.CanLoadMorePrs() {
		t.Error("an empty next-page token must retire the load-more row")
	}
}

func TestPrTabDiscardsStaleResults(t *testing.T) {
	a := prTabApp(t, []forge.PullRequestSummary{prSummary(1, "one", "ana", "a")}, "")
	staleGen := a.Pr.Gens.PrList

	// A scope toggle supersedes any page still in flight.
	a.TogglePrTabScope()
	if a.Pr.TabScope != forge.ScopeReviewRequested {
		t.Fatal("r must toggle to the review-requested scope")
	}
	fresh, _ := a.TakePrTabLoad()

	a.ApplyPrTabPage(staleGen, &forge.PullRequestPage{
		Items: []forge.PullRequestSummary{prSummary(99, "stale", "zz", "z")},
	}, false)
	if len(a.Pr.TabRows) != 0 {
		t.Error("a page from a superseded generation must be dropped")
	}
	a.SetPrTabError(staleGen, "stale failure")
	if a.Pr.TabError != "" {
		t.Error("an error from a superseded generation must be dropped")
	}

	// The current generation still applies.
	a.ApplyPrTabPage(fresh.Gen, &forge.PullRequestPage{
		Items: []forge.PullRequestSummary{prSummary(7, "seven", "ana", "a")},
	}, false)
	if len(a.Pr.TabRows) != 1 || a.Pr.TabRows[0].Number != 7 {
		t.Errorf("the current generation must apply, got %+v", a.Pr.TabRows)
	}
	if a.Pr.TabLoading {
		t.Error("a delivered page must clear the loading flag")
	}
}

func TestPrTabScopeToggleClearsRowsAndRefetchesFirstPage(t *testing.T) {
	a := prTabApp(t, []forge.PullRequestSummary{prSummary(1, "one", "ana", "a")}, "next")
	a.Pr.TabCursor = 1

	a.TogglePrTabScope()
	if len(a.Pr.TabRows) != 0 || a.Pr.NextPage != "" || a.Pr.TabCursor != 0 {
		t.Error("a scope change must reset rows, paging and cursor")
	}
	req, ok := a.TakePrTabLoad()
	if !ok || req.Append || req.Scope != forge.ScopeReviewRequested {
		t.Fatalf("scope toggle must refetch page one in the new scope, got %+v", req)
	}

	a.TogglePrTabScope()
	if a.Pr.TabScope != forge.ScopeOpen {
		t.Error("toggling twice must return to the open scope")
	}
}

func TestPrTabSetErrorSurfacesFailure(t *testing.T) {
	a := prTabApp(t, nil, "")
	a.ReloadPrTab()
	req, _ := a.TakePrTabLoad()

	a.SetPrTabError(req.Gen, "boom")
	if a.Pr.TabError != "boom" || a.Pr.TabLoading {
		t.Errorf("error must land and clear loading, got %q loading=%v", a.Pr.TabError, a.Pr.TabLoading)
	}
}

func TestPrTabCursorNavigationScrolls(t *testing.T) {
	var rows []forge.PullRequestSummary
	for i := 1; i <= 10; i++ {
		rows = append(rows, prSummary(uint64(i), "pr", "ana", "b"))
	}
	a := prTabApp(t, rows, "")
	a.Pr.TabViewportHeight = 3

	for range 5 {
		a.PrTabDown()
	}
	if a.Pr.TabCursor != 5 {
		t.Fatalf("cursor: got %d, want 5", a.Pr.TabCursor)
	}
	if a.Pr.TabScrollOffset != 3 {
		t.Errorf("scroll must follow the cursor past the viewport edge, got %d", a.Pr.TabScrollOffset)
	}

	for range 10 {
		a.PrTabUp()
	}
	if a.Pr.TabCursor != 0 || a.Pr.TabScrollOffset != 0 {
		t.Errorf("cursor/scroll must return to the top, got %d/%d", a.Pr.TabCursor, a.Pr.TabScrollOffset)
	}

	// Down stops at the last row when there is no load-more row.
	for range 50 {
		a.PrTabDown()
	}
	if a.Pr.TabCursor != 9 {
		t.Errorf("cursor must clamp to the last row, got %d", a.Pr.TabCursor)
	}
}

func TestPrTabSelectedReturnsFilteredRow(t *testing.T) {
	rows := []forge.PullRequestSummary{
		prSummary(1, "alpha", "ana", "a"),
		prSummary(2, "beta", "bo", "b"),
		prSummary(3, "gamma", "cy", "c"),
	}
	a := prTabApp(t, rows, "")
	a.Pr.TabFilter = "gamma"

	// The cursor indexes the filtered list, not the raw rows.
	got, ok := a.PrTabSelected()
	if !ok || got.Number != 3 {
		t.Errorf("selection must index the filtered rows, got %+v ok=%v", got, ok)
	}
}

func TestBeginPrTabFilterIgnoredOnLocalTab(t *testing.T) {
	a := &App{TargetTab: TargetTabLocal}
	a.BeginPrTabFilter()
	if a.PrTabFilterEditing() {
		t.Error("/ must not open the PR filter from the Local tab")
	}
}

func TestApplyPullRequestResetsReviewStateAndKeepsTabRows(t *testing.T) {
	// newTestApp gives a real VCS-backed app; arm the PR tab on top of it.
	a := newTestApp(t)
	a.TargetTab = TargetTabPullRequests
	a.onTargetTabEntered()
	req, _ := a.TakePrTabLoad()
	a.ApplyPrTabPage(req.Gen, &forge.PullRequestPage{
		Items: []forge.PullRequestSummary{prSummary(1, "one", "ana", "a")},
	}, false)

	// Pretend a local commit-range review was in progress.
	a.ReviewCommits = []vcs.CommitInfo{{ID: "c1", ShortID: "c1"}, {ID: "c2", ShortID: "c2"}}
	a.CommitSelectionRange = &model.IndexRange{0, 1}
	a.ShowCommitSelector = true
	a.DiffState.CursorLine = 7
	a.ExpandedTop[GapID{FileIdx: 0}] = []model.DiffLine{{Content: "ctx"}}

	path := "a.go"
	files := []model.DiffFile{{
		NewPath: &path, Status: model.StatusModified,
		Hunks: []model.DiffHunk{{Lines: []model.DiffLine{{Origin: model.OriginContext, Content: "x"}}}},
	}}
	details := &forge.PullRequestDetails{
		PullRequestSummary: prSummary(42, "the pr", "ana", "feat"),
		HeadSHA:            "head", BaseSHA: "base",
	}
	session := model.NewReviewSession("forge:github.com/acme/widget", "head", nil, model.SourcePullRequest)

	a.ApplyPullRequest(PullRequestLoad{
		Details: details,
		Files:   files,
		Commits: []forge.Commit{{OID: "abc", ShortOID: "abc"}},
	}, session)

	if !a.InPrMode() {
		t.Fatal("applying a pull request must enter PR mode")
	}
	if a.DiffSource.Kind != DiffSourcePullRequest {
		t.Error("diff source must become DiffSourcePullRequest")
	}
	if a.DiffState.CursorLine != 0 {
		t.Error("navigation state must reset")
	}
	if len(a.ExpandedTop) != 0 {
		t.Error("expanded gaps must be cleared for the new diff")
	}
	// The outgoing local commits must not leak: the selector is rebuilt
	// from the PR's own commits instead.
	if len(a.ReviewCommits) != 1 || a.ReviewCommits[0].ID != "abc" {
		t.Errorf("the selector must hold the PR's commits, got %+v", a.ReviewCommits)
	}
	if a.ShowCommitSelector {
		t.Error("a single-commit pull request must not show the commit strip")
	}
	if len(a.Pr.TabRows) != 1 {
		t.Error("the selector's PR listing must survive so reopening it does not refetch")
	}
}
