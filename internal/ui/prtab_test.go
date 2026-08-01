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
	"github.com/infrashift/mrman/internal/vcs"
)

// prTabForge serves canned PR listings and pull requests, recording the
// queries it saw so tests can assert on scope and paging.
type prTabForge struct {
	*uiFakeForge
	pages    []*forge.PullRequestPage
	listErr  error
	queries  []forge.ListQuery
	details  *forge.PullRequestDetails
	diff     string
	openErr  error
	openSeen int
}

func (f *prTabForge) ListPullRequests(_ context.Context, q forge.ListQuery) (*forge.PullRequestPage, error) {
	f.queries = append(f.queries, q)
	if f.listErr != nil {
		return nil, f.listErr
	}
	if len(f.pages) == 0 {
		return &forge.PullRequestPage{}, nil
	}
	page := f.pages[0]
	if len(f.pages) > 1 {
		f.pages = f.pages[1:]
	}
	return page, nil
}

func (f *prTabForge) GetPullRequest(context.Context, forge.Target) (*forge.PullRequestDetails, error) {
	f.openSeen++
	if f.openErr != nil {
		return nil, f.openErr
	}
	return f.details, nil
}

func (f *prTabForge) GetDiff(context.Context, *forge.PullRequestDetails) (string, error) {
	return f.diff, nil
}

func testRepo() forgetypes.Repository {
	return forgetypes.Repository{
		Kind: forgetypes.KindGitHub, Host: "github.com", Owner: "acme", Name: "widget",
	}
}

func testPrSummary(number uint64, title, author, branch string) forge.PullRequestSummary {
	return forge.PullRequestSummary{
		Repository: testRepo(), Number: number, Title: title,
		Author: author, HeadRefName: branch, State: "open",
	}
}

// prTabModel opens the selector on the Pull Requests tab wired to a fake
// forge, and drains the initial listing so rows are present.
func prTabModel(t *testing.T, f *prTabForge) *Model {
	t.Helper()
	m := testModel(t)
	backend := &selectorBackend{
		stubBackend: stubBackend{info: vcs.Info{RootPath: "/repo", HeadCommit: "abc", Type: vcs.TypeGit}},
		commits:     []vcs.CommitInfo{makeCommit("aaa1111", "first")},
	}
	m.App.VCS = backend
	m.forge = staticForgeResolver(f, testRepo())
	if err := m.App.EnterTargetSelector(app.TargetTabPullRequests); err != nil {
		t.Fatal(err)
	}
	runCmd(t, m, m.drainPrTabLoad())
	return m
}

// runCmd executes a tea.Cmd and feeds its message back through Update, the
// way the bubbletea runtime would.
func runCmd(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	if msg == nil {
		return
	}
	// tea.Batch yields a BatchMsg the runtime expands into its members;
	// the model never sees it, so the helper has to do the same.
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, sub := range batch {
			runCmd(t, m, sub)
		}
		return
	}
	m.Update(msg)
}

// pressPr sends a key and round-trips whatever command it returns, so async
// forge work completes before the assertion — the shared press() helper
// deliberately drops commands.
func pressPr(t *testing.T, m *Model, text string, code rune) {
	t.Helper()
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Text: text, Code: code}))
	runCmd(t, m, cmd)
	runCmd(t, m, m.takeQueued())
}

func pressPrRune(t *testing.T, m *Model, r rune) { pressPr(t, m, string(r), r) }

func newPrTabForge(rows ...forge.PullRequestSummary) *prTabForge {
	return &prTabForge{
		uiFakeForge: &uiFakeForge{},
		pages:       []*forge.PullRequestPage{{Items: rows}},
	}
}

func TestPrTabRendersLoadedPullRequests(t *testing.T) {
	f := newPrTabForge(
		testPrSummary(42, "Add retry logic", "ana", "feat/retry"),
		testPrSummary(43, "Fix flaky test", "bo", "fix/flake"),
	)
	m := prTabModel(t, f)

	out := strings.Join(m.selectorView(), "\n")
	for _, want := range []string{"#42", "Add retry logic", "feat/retry", "ana", "#43", "acme/widget"} {
		if !strings.Contains(out, want) {
			t.Errorf("PR tab missing %q in:\n%s", want, out)
		}
	}
	if len(f.queries) != 1 || f.queries[0].Scope != forge.ScopeOpen {
		t.Errorf("expected one open-scope listing query, got %+v", f.queries)
	}
}

func TestPrTabListFailureRendersInTabBody(t *testing.T) {
	f := &prTabForge{uiFakeForge: &uiFakeForge{}, listErr: errors.New("token expired")}
	m := prTabModel(t, f)

	out := strings.Join(m.selectorView(), "\n")
	if !strings.Contains(out, "token expired") {
		t.Errorf("listing failure must surface in the tab body, got:\n%s", out)
	}
	if m.App.Message != nil && m.App.Message.Type == app.MessageError {
		t.Error("a tab-local failure must not hijack the status bar")
	}
}

func TestPrTabFilterPromptConsumesKeys(t *testing.T) {
	f := newPrTabForge(
		testPrSummary(1, "alpha", "ana", "a"),
		testPrSummary(2, "beta", "bo", "b"),
	)
	m := prTabModel(t, f)

	pressRune(m, '/')
	if !m.App.PrTabFilterEditing() {
		t.Fatal("/ must open the filter prompt")
	}
	// "j" is a navigation key outside the prompt; inside it must type.
	for _, r := range "beta" {
		pressRune(m, r)
	}
	if m.App.Pr.TabFilter != "beta" {
		t.Fatalf("filter buffer = %q", m.App.Pr.TabFilter)
	}
	if got := len(m.App.PrTabFilteredRows()); got != 1 {
		t.Errorf("filter must narrow to 1 row, got %d", got)
	}

	press(m, "", tea.KeyEnter, 0)
	if m.App.PrTabFilterEditing() {
		t.Error("Enter must close the prompt")
	}
	if m.App.Pr.TabFilter != "beta" {
		t.Error("Enter must keep the query")
	}

	out := strings.Join(m.selectorView(), "\n")
	if strings.Contains(out, "alpha") {
		t.Error("filtered-out rows must not render")
	}
}

func TestPrTabScopeToggleRefetches(t *testing.T) {
	f := newPrTabForge(testPrSummary(1, "alpha", "ana", "a"))
	m := prTabModel(t, f)

	pressPrRune(t, m, 'r')
	if len(f.queries) < 2 {
		t.Fatalf("r must refetch, saw %d queries", len(f.queries))
	}
	if f.queries[len(f.queries)-1].Scope != forge.ScopeReviewRequested {
		t.Errorf("r must switch to the review-requested scope, got %+v", f.queries)
	}
}

func TestPrTabLoadMorePagesWithToken(t *testing.T) {
	f := &prTabForge{
		uiFakeForge: &uiFakeForge{},
		pages: []*forge.PullRequestPage{
			{Items: []forge.PullRequestSummary{testPrSummary(1, "one", "ana", "a")}, NextPageToken: "cursor-2"},
			{Items: []forge.PullRequestSummary{testPrSummary(2, "two", "bo", "b")}},
		},
	}
	m := prTabModel(t, f)

	out := strings.Join(m.selectorView(), "\n")
	if !strings.Contains(out, "load more") {
		t.Fatalf("a next-page token must render the load-more row, got:\n%s", out)
	}

	pressPrRune(t, m, 'j') // onto the load-more row
	if !m.App.IsOnPrLoadMoreRow() {
		t.Fatal("cursor must reach the load-more row")
	}
	pressPr(t, m, "", tea.KeyEnter)

	if len(f.queries) != 2 || f.queries[1].PageToken != "cursor-2" {
		t.Fatalf("load-more must page with the token, got %+v", f.queries)
	}
	if len(m.App.Pr.TabRows) != 2 {
		t.Errorf("paging must append, got %d rows", len(m.App.Pr.TabRows))
	}
}

func TestPrTabEnterOpensPullRequest(t *testing.T) {
	f := newPrTabForge(testPrSummary(42, "Add retry logic", "ana", "feat/retry"))
	f.details = &forge.PullRequestDetails{
		PullRequestSummary: testPrSummary(42, "Add retry logic", "ana", "feat/retry"),
		HeadSHA:            "head1234",
		BaseSHA:            "base1234",
	}
	f.diff = "diff --git a/a.go b/a.go\n" +
		"--- a/a.go\n+++ b/a.go\n" +
		"@@ -1,2 +1,3 @@\n package a\n+var X = 1\n func B() {}\n"
	m := prTabModel(t, f)

	pressPr(t, m, "", tea.KeyEnter)

	if f.openSeen != 1 {
		t.Fatalf("Enter must fetch the pull request once, got %d", f.openSeen)
	}
	if !m.App.InPrMode() {
		t.Fatal("opening a PR must put the app in PR mode")
	}
	if m.App.Pr.Details.Number != 42 {
		t.Errorf("wrong PR opened: %d", m.App.Pr.Details.Number)
	}
	if m.App.InputMode != input.ModeNormal {
		t.Error("opening a PR must leave the selector")
	}
	if m.App.DiffSource.Kind != app.DiffSourcePullRequest {
		t.Error("diff source must become DiffSourcePullRequest")
	}
	if len(m.App.DiffFiles) == 0 {
		t.Error("the PR diff must be parsed into files")
	}
}

func TestPrTabOpenFailureKeepsSelectorUsable(t *testing.T) {
	f := newPrTabForge(testPrSummary(42, "Add retry logic", "ana", "feat/retry"))
	f.openErr = errors.New("pull request not found")
	m := prTabModel(t, f)

	pressPr(t, m, "", tea.KeyEnter)

	if m.App.InPrMode() {
		t.Fatal("a failed open must not enter PR mode")
	}
	if m.App.Pr.Opening {
		t.Error("a failed open must clear the spinner flag")
	}
	if m.App.Message == nil || !strings.Contains(m.App.Message.Content, "pull request not found") {
		t.Errorf("the failure must reach the status bar, got %+v", m.App.Message)
	}
}

func TestPrTabOpenDiscardsStaleResult(t *testing.T) {
	f := newPrTabForge(testPrSummary(42, "x", "ana", "a"))
	m := prTabModel(t, f)

	cmd := m.openSelectedPr()
	if cmd == nil {
		t.Fatal("expected an open command")
	}
	msg := cmd()

	// The user opened something else in the meantime.
	m.App.Pr.Gens.PrOpen++
	m.Update(msg)

	if m.App.InPrMode() {
		t.Error("a superseded open result must be discarded")
	}
}

func TestPrTabEscReturnsToLocalTab(t *testing.T) {
	m := prTabModel(t, newPrTabForge())
	press(m, "", tea.KeyEscape, 0)
	if m.App.TargetTab != app.TargetTabLocal {
		t.Error("Esc on the PR tab must step back to Local, not close the selector")
	}
	if m.App.InputMode != input.ModeCommitSelect {
		t.Error("Esc on the PR tab must keep the selector open")
	}
}

func TestPrTabEmptyStatesExplainWhy(t *testing.T) {
	m := prTabModel(t, newPrTabForge())
	if out := strings.Join(m.selectorView(), "\n"); !strings.Contains(out, "No open merge requests") {
		t.Errorf("empty listing must say so, got:\n%s", out)
	}

	m.App.Pr.TabScope = forge.ScopeReviewRequested
	if out := strings.Join(m.selectorView(), "\n"); !strings.Contains(out, "awaiting your review") {
		t.Errorf("empty review-requested listing needs its own message, got:\n%s", out)
	}

	m.App.Pr.TabScope = forge.ScopeOpen
	m.App.Pr.TabRows = []forge.PullRequestSummary{testPrSummary(1, "alpha", "ana", "a")}
	m.App.Pr.TabFilter = "zzz"
	if out := strings.Join(m.selectorView(), "\n"); !strings.Contains(out, "match") {
		t.Errorf("an over-narrow filter must say so, got:\n%s", out)
	}
}

func TestForgeResolverMemoizesFailure(t *testing.T) {
	calls := 0
	r := &forgeResolver{
		resolve: func() (forge.Forge, forgetypes.Repository, error) {
			calls++
			return nil, forgetypes.Repository{}, errors.New("no token")
		},
	}
	for i := 0; i < 3; i++ {
		if _, _, err := r.Get(); err == nil {
			t.Fatal("expected the resolver failure")
		}
	}
	if calls != 1 {
		t.Errorf("a broken token must be resolved once, not per keystroke (calls=%d)", calls)
	}
}

func TestNilForgeResolverReportsMissingRemote(t *testing.T) {
	var r *forgeResolver
	if _, _, err := r.Get(); !errors.Is(err, errNoForgeRepository) {
		t.Errorf("a checkout with no forge remote must say so, got %v", err)
	}
}

func TestPrsCommandOpensSelectorOnPrTab(t *testing.T) {
	f := newPrTabForge(testPrSummary(7, "seven", "ana", "a"))
	m := testModel(t)
	backend := &selectorBackend{
		stubBackend: stubBackend{info: vcs.Info{RootPath: "/repo", HeadCommit: "abc", Type: vcs.TypeGit}},
		commits:     []vcs.CommitInfo{makeCommit("aaa1111", "first")},
	}
	m.App.VCS = backend
	m.forge = staticForgeResolver(f, testRepo())

	m.runCommand(input.ParseCommand("prs"))
	runCmd(t, m, m.takeQueued())

	if m.App.InputMode != input.ModeCommitSelect || m.App.TargetTab != app.TargetTabPullRequests {
		t.Fatal(":prs must open the selector on the Pull Requests tab")
	}
	if len(m.App.Pr.TabRows) != 1 {
		t.Errorf(":prs must load the listing, got %d rows", len(m.App.Pr.TabRows))
	}
}

func TestCommitsCommandOpensSelectorOnLocalTab(t *testing.T) {
	m := testModel(t)
	backend := &selectorBackend{
		stubBackend: stubBackend{info: vcs.Info{RootPath: "/repo", HeadCommit: "abc", Type: vcs.TypeGit}},
		commits:     []vcs.CommitInfo{makeCommit("aaa1111", "first")},
	}
	m.App.VCS = backend

	m.runCommand(input.ParseCommand("commits"))
	if m.App.InputMode != input.ModeCommitSelect || m.App.TargetTab != app.TargetTabLocal {
		t.Fatal(":commits must open the selector on the Local tab")
	}
}
