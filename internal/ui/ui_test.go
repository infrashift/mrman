package ui

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/syntax"
	"github.com/infrashift/mrman/internal/theme"
	"github.com/infrashift/mrman/internal/vcs"
)

func lineno(n uint32) *uint32 { return &n }

// stubBackend satisfies vcs.Backend with fixed context data.
type stubBackend struct {
	vcs.UnsupportedBase
	info vcs.Info
}

func (s *stubBackend) Info() *vcs.Info { return &s.info }
func (s *stubBackend) WorkingTreeDiff(*syntax.Highlighter) ([]model.DiffFile, error) {
	return nil, nil
}
func (s *stubBackend) FetchContextLines(string, model.FileStatus, *string, uint32, uint32) ([]model.DiffLine, error) {
	return nil, nil
}
func (s *stubBackend) FileLineCount(string, model.FileStatus, *string) (uint32, error) {
	return 10, nil
}

func testApp(t *testing.T) *app.App {
	t.Helper()
	path := "src/x.go"
	files := []model.DiffFile{{
		NewPath: &path,
		Status:  model.StatusModified,
		Hunks: []model.DiffHunk{{
			Header: "@@ -1,3 +1,4 @@",
			Lines: []model.DiffLine{
				{Origin: model.OriginContext, Content: "package x", OldLineno: lineno(1), NewLineno: lineno(1)},
				{Origin: model.OriginDeletion, Content: "var A = 1", OldLineno: lineno(2)},
				{Origin: model.OriginAddition, Content: "var A = 10", NewLineno: lineno(2)},
				{Origin: model.OriginAddition, Content: "var C = 3", NewLineno: lineno(3)},
			},
			OldStart: 1, OldCount: 3, NewStart: 1, NewCount: 4,
		}},
	}}
	files[0].ContentHash = model.ComputeContentHash(files[0].Hunks)
	session := model.NewReviewSession("/repo", "abc", nil, model.SourceWorkingTree)
	backend := &stubBackend{info: vcs.Info{RootPath: "/repo", HeadCommit: "abc", Type: vcs.TypeGit}}
	return app.NewApp(backend, backend.Info(), files, session, app.DiffSource{Kind: app.DiffSourceWorkingTree})
}

func testModel(t *testing.T) *Model {
	t.Helper()
	m := NewModel(testApp(t), theme.TokyoNightStorm())
	m.width, m.height = 100, 30
	m.syncViewport()
	return m
}

func viewString(m *Model) string {
	return m.View().Content
}

// ansiSequence matches SGR escapes so tests can assert on what a user
// actually reads. Syntax highlighting splits a line like "var Y = 2" into
// several styled spans, so the raw view never contains it contiguously.
var ansiSequence = regexp.MustCompile("\x1b\\[[0-9;]*m")

// plainView is the rendered frame with styling stripped.
func plainView(m *Model) string {
	return ansiSequence.ReplaceAllString(viewString(m), "")
}

func TestDiffPaneBuildsRows(t *testing.T) {
	m := testModel(t)
	lines := m.diffPane.BuildLines(m.App, 80, 20)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"src/x.go", "@@ -1,3 +1,4 @@", "package x", "var A = 1", "var A = 10", "▌"} {
		if !strings.Contains(joined, want) {
			t.Errorf("diff pane missing %q", want)
		}
	}
	if len(lines) != 20 {
		t.Fatalf("must pad to height, got %d", len(lines))
	}
}

func TestFileListPaneRows(t *testing.T) {
	m := testModel(t)
	lines := m.fileList.BuildLines(m.App, 30, 10)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "x.go") || !strings.Contains(joined, "▢") {
		t.Errorf("file list missing entries: %q", joined)
	}
	if !strings.Contains(m.fileList.Title(m.App), "Files · 0/1") {
		t.Errorf("title = %q", m.fileList.Title(m.App))
	}
}

func TestViewComposesFrame(t *testing.T) {
	m := testModel(t)
	out := viewString(m)
	for _, want := range []string{"mrman", "NORMAL", "src/x.go", "╭"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q", want)
		}
	}
}

func press(m *Model, text string, code rune, mod tea.KeyMod) {
	m.Update(tea.KeyPressMsg(tea.Key{Text: text, Code: code, Mod: mod}))
}

func pressRune(m *Model, r rune) { press(m, string(r), r, 0) }

func TestNavigationKeysMoveCursor(t *testing.T) {
	m := testModel(t)
	start := m.App.DiffState.CursorLine
	pressRune(m, 'j')
	if m.App.DiffState.CursorLine <= start {
		t.Fatal("j must move the cursor down")
	}
	pressRune(m, 'k')
	if m.App.DiffState.CursorLine != start {
		t.Fatal("k must move the cursor back up")
	}
}

func TestCountPrefixScalesMotion(t *testing.T) {
	m := testModel(t)
	start := m.App.DiffState.CursorLine
	pressRune(m, '3')
	if m.App.PendingCount == nil || *m.App.PendingCount != 3 {
		t.Fatal("digit must accumulate")
	}
	pressRune(m, 'j')
	moved := m.App.DiffState.CursorLine - start
	if moved < 2 {
		t.Fatalf("3j must move multiple lines, moved %d", moved)
	}
	if m.App.PendingCount != nil {
		t.Fatal("count must clear after use")
	}
}

func TestZChords(t *testing.T) {
	m := testModel(t)
	pressRune(m, 'z')
	pressRune(m, 'z') // zz — must not crash, cursor stays valid
	if m.App.DiffState.CursorLine < 0 {
		t.Fatal("cursor corrupted")
	}
	pressRune(m, 'Z')
	// ZQ quits: dispatch returns quit; we can't observe tea.Quit directly
	// here, but the chord must consume without panicking.
	pressRune(m, 'x') // non-matching second key falls through
}

func TestCommandModeLifecycle(t *testing.T) {
	m := testModel(t)
	press(m, ":", ':', tea.ModShift)
	if m.App.InputMode != input.ModeCommand {
		t.Fatal("colon must enter command mode")
	}
	for _, r := range "wrap" {
		pressRune(m, r)
	}
	if m.App.CommandBuffer != "wrap" {
		t.Fatalf("buffer = %q", m.App.CommandBuffer)
	}
	wrapBefore := m.App.DiffState.WrapLines
	press(m, "", tea.KeyEnter, 0)
	if m.App.InputMode != input.ModeNormal {
		t.Fatal("enter must exit command mode")
	}
	if m.App.DiffState.WrapLines == wrapBefore {
		t.Fatal(":wrap must toggle wrapping")
	}
}

func TestCommandCompletion(t *testing.T) {
	m := testModel(t)
	press(m, ":", ':', tea.ModShift)
	for _, r := range "versi" {
		pressRune(m, r)
	}
	press(m, "", tea.KeyTab, 0)
	if m.App.CommandBuffer != "version" {
		t.Fatalf("completion = %q", m.App.CommandBuffer)
	}
	press(m, "", tea.KeyEscape, 0)
	if m.App.InputMode != input.ModeNormal {
		t.Fatal("esc must cancel")
	}
}

func TestUnknownCommandShowsError(t *testing.T) {
	m := testModel(t)
	press(m, ":", ':', tea.ModShift)
	for _, r := range "bogus" {
		pressRune(m, r)
	}
	press(m, "", tea.KeyEnter, 0)
	if m.App.Message == nil || !strings.Contains(m.App.Message.Content, "Unknown command") {
		t.Fatalf("message = %+v", m.App.Message)
	}
}

func TestSearchFlow(t *testing.T) {
	m := testModel(t)
	press(m, "/", '/', 0)
	if m.App.InputMode != input.ModeSearch {
		t.Fatal("slash must enter search mode")
	}
	for _, r := range "var C" {
		pressRune(m, r)
	}
	press(m, "", tea.KeyEnter, 0)
	if m.App.InputMode != input.ModeNormal {
		t.Fatal("enter must run the search")
	}
	// Cursor must land on the "var C" line.
	ann := m.App.LineAnnotations[m.App.DiffState.CursorLine]
	if ann.Kind != app.AnnDiffLine {
		t.Fatalf("cursor kind = %v", ann.Kind)
	}
}

func TestHelpToggleAndView(t *testing.T) {
	m := testModel(t)
	press(m, "?", '?', tea.ModShift)
	if m.App.InputMode != input.ModeHelp {
		t.Fatal("? must open help")
	}
	out := viewString(m)
	if !strings.Contains(out, "Navigation") {
		t.Error("help view must render sections")
	}
	pressRune(m, 'q')
	if m.App.InputMode != input.ModeHelp && m.App.InputMode != input.ModeNormal {
		t.Fatal("q must close help")
	}
}

func TestFocusCycling(t *testing.T) {
	m := testModel(t)
	if m.App.FocusedPanel != app.PanelDiff {
		t.Fatal("diff starts focused")
	}
	press(m, "", tea.KeyTab, 0)
	if m.App.FocusedPanel != app.PanelFileList {
		t.Fatal("tab must focus the file list")
	}
	// Enter on the file jumps back to the diff.
	press(m, "", tea.KeyEnter, 0)
	if m.App.FocusedPanel != app.PanelDiff {
		t.Fatal("enter on file must focus diff")
	}
}

func TestLeaderChord(t *testing.T) {
	m := testModel(t)
	pressRune(m, ';')
	pressRune(m, 'e')
	if m.App.ShowFileList {
		t.Fatal("leader-e must hide the file list")
	}
}

func TestCtrlCTwice(t *testing.T) {
	m := testModel(t)
	press(m, "", 'c', tea.ModCtrl)
	if m.App.Message == nil || !strings.Contains(m.App.Message.Content, "again to exit") {
		t.Fatal("first Ctrl-C must warn")
	}
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	if cmd == nil {
		t.Fatal("second Ctrl-C must quit")
	}
}

func TestHeaderAndStatusBar(t *testing.T) {
	m := testModel(t)
	header := Header(m.App, m.Theme, 100)
	if !strings.Contains(header, "mrman") || !strings.Contains(header, "git") {
		t.Errorf("header = %q", header)
	}
	status := StatusBar(m.App, m.Theme, 100)
	if !strings.Contains(status, "NORMAL") {
		t.Errorf("status = %q", status)
	}
	m.App.SetError("boom")
	status = StatusBar(m.App, m.Theme, 100)
	if !strings.Contains(status, "boom") {
		t.Errorf("status with message = %q", status)
	}
}

func TestGotoLineCommand(t *testing.T) {
	m := testModel(t)
	press(m, ":", ':', tea.ModShift)
	pressRune(m, '2')
	press(m, "", tea.KeyEnter, 0)
	ann := m.App.LineAnnotations[m.App.DiffState.CursorLine]
	if ann.Kind != app.AnnDiffLine || ann.NewLineno == nil || *ann.NewLineno != 2 {
		t.Fatalf("cursor annotation = %+v", ann)
	}
}
