package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/config"
	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/vcs"
)

// selectorBackend extends stubBackend with selector-relevant methods.
type selectorBackend struct {
	stubBackend
	commits []vcs.CommitInfo
}

func (s *selectorBackend) ChangeStatus() (vcs.ChangeStatus, error) {
	return vcs.ChangeStatus{Staged: true, Unstaged: true}, nil
}

func (s *selectorBackend) RecentCommits(offset, limit int) ([]vcs.CommitInfo, error) {
	if offset >= len(s.commits) {
		return nil, nil
	}
	end := min(offset+limit, len(s.commits))
	return s.commits[offset:end], nil
}

func makeCommit(id, summary string) vcs.CommitInfo {
	branch := "main"
	return vcs.CommitInfo{
		ID: id + strings.Repeat("0", 33), ShortID: id,
		BranchName: &branch, Summary: summary, Author: "tester",
		Time: time.Now().Add(-2 * time.Hour),
	}
}

func selectorModel(t *testing.T) *Model {
	t.Helper()
	m := testModel(t)
	backend := &selectorBackend{
		stubBackend: stubBackend{info: vcs.Info{RootPath: "/repo", HeadCommit: "abc", Type: vcs.TypeGit}},
		commits:     []vcs.CommitInfo{makeCommit("aaa1111", "first"), makeCommit("bbb2222", "second")},
	}
	m.App.VCS = backend
	if err := m.App.EnterTargetSelector(app.TargetTabLocal); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestSelectorViewRenders(t *testing.T) {
	m := selectorModel(t)
	out := strings.Join(m.selectorView(), "\n")
	for _, want := range []string{"Local", "Merge Requests", "SELECT", "first", "second", "staged", "unstaged"} {
		if !strings.Contains(out, want) {
			t.Errorf("selector missing %q", want)
		}
	}
}

func TestSelectorNavigationAndToggle(t *testing.T) {
	m := selectorModel(t)
	if m.App.InputMode != input.ModeCommitSelect {
		t.Fatal("selector must be active")
	}
	pressRune(m, 'j')
	pressRune(m, 'k')
	press(m, " ", tea.KeySpace, 0)
	if m.App.CommitSelectionRange == nil {
		t.Fatal("space must select the cursor row")
	}
	out := strings.Join(m.selectorView(), "\n")
	if !strings.Contains(out, "selected") {
		t.Error("footer must show selection count")
	}
}

func TestSelectorTabCycle(t *testing.T) {
	m := selectorModel(t)
	press(m, "", tea.KeyTab, 0)
	if m.App.TargetTab != app.TargetTabPullRequests {
		t.Fatal("tab must cycle to PR tab")
	}
	// Entering the tab arms the lazy fetch; with no rows back yet the body
	// reports the in-flight load rather than "no pull requests".
	out := strings.Join(m.selectorView(), "\n")
	if !strings.Contains(out, "Loading merge requests") {
		t.Errorf("PR tab must show the loading state, got:\n%s", out)
	}
	if m.App.Pr == nil || !m.App.Pr.TabLoading {
		t.Error("entering the PR tab must arm a listing fetch")
	}
	press(m, "", tea.KeyTab, tea.ModShift)
	if m.App.TargetTab != app.TargetTabLocal {
		t.Fatal("backtab must cycle back")
	}
}

func TestSelectorViewInFullViewPath(t *testing.T) {
	m := selectorModel(t)
	out := viewString(m)
	if !strings.Contains(out, "SELECT") {
		t.Error("View must render the selector in CommitSelect mode")
	}
}

func TestRelativeTime(t *testing.T) {
	cases := []struct {
		age  time.Duration
		want string
	}{
		{30 * time.Second, "just now"},
		{5 * time.Minute, "5m"},
		{3 * time.Hour, "3h"},
		{2 * 24 * time.Hour, "2d"},
		{2 * 7 * 24 * time.Hour, "2w"},
		{40 * 24 * time.Hour, "1mo"},
		{2 * 365 * 24 * time.Hour, "2y"},
	}
	for _, c := range cases {
		if got := relativeTime(time.Now().Add(-c.age)); got != c.want {
			t.Errorf("relativeTime(-%v) = %q, want %q", c.age, got, c.want)
		}
	}
}

func TestApplyConfig(t *testing.T) {
	m := testModel(t)
	cfg := config.Default()
	cfg.Username = "ryan"
	cfg.Leader = ","
	cfg.CommentVim = true
	cfg.DiffView = "side-by-side"
	cfg.CommitOrder = "ascending"
	cfg.InitialCommitSelection = "oldest"
	cfg.Wrap = true
	cfg.CursorLine = false
	cfg.ScrollOffset = 5
	cfg.ExportLegend = false
	cfg.CommentTypes = []config.CommentTypeConfig{
		{ID: "issue", Label: "ISSUE", Definition: "must fix", Color: "red"},
	}
	applyConfig(cfg, m.App, m)

	if m.App.Username != "ryan" || m.leader != ',' || !m.CommentVimMode {
		t.Fatalf("identity knobs wrong: %+v leader=%c", m.App.Username, m.leader)
	}
	if m.App.DiffViewMode != app.ViewSideBySide || m.App.CommitOrder != app.CommitAscending ||
		m.App.CommitSelectionStart != app.CommitSelectionOldest {
		t.Fatal("view knobs wrong")
	}
	if !m.App.DiffState.WrapLines || m.App.CursorLineHighlight || m.App.ScrollOffset != 5 {
		t.Fatal("diff knobs wrong")
	}
	if m.export.ShowLegend {
		t.Fatal("legend knob wrong")
	}
	if len(m.App.CommentTypes) < 2 || m.App.CommentTypes[0].ID != "issue" {
		t.Fatalf("comment types wrong: %+v", m.App.CommentTypes)
	}
}

func TestApplyConfigWatchInterval(t *testing.T) {
	_, m := testLifecycle(t)
	cfg := config.Default()
	cfg.ReviewWatchIntervalMS = 0
	applyConfig(cfg, m.App, m)
	if !m.session.watchDisabled {
		t.Fatal("interval 0 must disable the watcher")
	}
	cfg.ReviewWatchIntervalMS = 2500
	m.session.watchDisabled = false
	applyConfig(cfg, m.App, m)
	if m.session.watchEvery != 2500*time.Millisecond {
		t.Fatalf("watchEvery = %v", m.session.watchEvery)
	}
}

// TestApplyLoadedSelectionResyncsViewport pins the fix for a rendering bug
// reported from manual testing: after confirming a commit range in the target
// selector, comment bodies rendered one character per line until the next
// terminal resize.
//
// ApplyLoadedSelection replaces DiffState wholesale, zeroing the viewport
// dimensions, and no WindowSizeMsg follows a selector confirm. Anything
// sizing itself from ViewportWidth then computed against zero — comment wrap
// width clamps to a 1-column minimum, so "This is a comment" rendered as one
// character per row. The comment text itself was stored correctly; only the
// rendering was wrong.
func TestApplyLoadedSelectionResyncsViewport(t *testing.T) {
	m := testModel(t)
	a := m.App
	want := m.diffInnerWidth()
	if want <= 11 {
		t.Fatalf("test model viewport %d is too small to detect the bug", want)
	}

	// Reproduce exactly what the selector confirm does to DiffState.
	a.ApplyLoadedSelection(a.DiffFiles, a.Session, app.DiffSource{Kind: app.DiffSourceCommitRange})
	if got := a.DiffState.ViewportWidth; got != 0 {
		t.Fatalf("ApplyLoadedSelection left ViewportWidth = %d; this test assumes it resets to 0", got)
	}

	// The line the fix adds.
	m.syncViewport()

	if got := a.DiffState.ViewportWidth; got != want {
		t.Errorf("ViewportWidth = %d, want %d", got, want)
	}
	// The wrap width comment bodies are measured against must leave room for
	// real text, not collapse to the 1-column floor.
	if contentArea := a.DiffState.ViewportWidth - 10; contentArea < 2 {
		t.Errorf("comment content area = %d; a value below 2 wraps every character onto its own line", contentArea)
	}
	if a.DiffState.ViewportHeight <= 0 {
		t.Errorf("ViewportHeight = %d, want positive", a.DiffState.ViewportHeight)
	}
}
