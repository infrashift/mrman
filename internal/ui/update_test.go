package ui

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/reviewcli"
	"github.com/infrashift/mrman/internal/theme"
	"github.com/infrashift/mrman/internal/vcs"
)

// These drive the model through Update, the way Bubble Tea does, for the
// terminal events and input paths no other test sends.

func TestTickMergesAnAgentsCommentAndReschedules(t *testing.T) {
	lc, m := testLifecycle(t)
	if err := reviewcli.Add(lc.store, reviewcli.Options{
		Session: lc.path, Comment: "agent finding", Type: "issue",
	}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}

	_, cmd := m.Update(tickMsg{})
	if cmd == nil {
		t.Fatal("a tick must schedule the next one")
	}
	if m.App.Message == nil || !strings.Contains(m.App.Message.Content, "Merged 1 external change") {
		t.Errorf("message = %+v, want the merge notice", m.App.Message)
	}
	if !strings.Contains(plainView(m), "agent finding") {
		t.Error("the merged comment is not on screen")
	}
}

func TestResizeResyncsTheViewportAndHidesTheFileListWhenNarrow(t *testing.T) {
	m := testModel(t)
	m.App.ShowFileList = true
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 20})
	if m.App.ShowFileList {
		t.Error("a terminal under 100 columns hides the file list")
	}
	if m.App.DiffState.ViewportHeight != 16 || m.App.DiffState.ViewportWidth != m.diffInnerWidth() {
		t.Errorf("viewport = %dx%d, want it resized to the terminal",
			m.App.DiffState.ViewportWidth, m.App.DiffState.ViewportHeight)
	}
}

func TestPasteLandsInTheActiveBuffer(t *testing.T) {
	m := testModel(t)
	m.App.InputMode = input.ModeCommand
	m.Update(tea.PasteMsg{Content: "set wrap"})
	if m.App.CommandBuffer != "set wrap" {
		t.Errorf("command buffer = %q", m.App.CommandBuffer)
	}

	m.App.InputMode = input.ModeSearch
	m.Update(tea.PasteMsg{Content: "needle"})
	if m.App.SearchBuffer != "needle" {
		t.Errorf("search buffer = %q", m.App.SearchBuffer)
	}

	m.App.InputMode = input.ModeNormal
	m.App.EnterReviewCommentMode()
	m.Update(tea.PasteMsg{Content: "pasted\ntext"})
	if m.App.CommentBuffer != "pasted\ntext" {
		t.Errorf("comment buffer = %q", m.App.CommentBuffer)
	}
}

// numberedContextBackend serves numbered lines for gap expansion in a local review.
type numberedContextBackend struct {
	stubBackend
	total uint32
}

func (b *numberedContextBackend) FetchContextLines(_ string, _ model.FileStatus, _ *string, start, end uint32) ([]model.DiffLine, error) {
	var lines []model.DiffLine
	for n := start; n <= min(end, b.total); n++ {
		lines = append(lines, model.DiffLine{Origin: model.OriginContext,
			Content: fmt.Sprintf("hidden line %d", n), OldLineno: new(n), NewLineno: new(n)})
	}
	return lines, nil
}

func (b *numberedContextBackend) FileLineCount(string, model.FileStatus, *string) (uint32, error) {
	return b.total, nil
}

// TestExpandedContextRendersWithItsLineNumbers: the expanded-context row is
// how hidden lines appear once fetched, and nothing rendered one before.
func TestExpandedContextRendersWithItsLineNumbers(t *testing.T) {
	path := "src/y.go"
	file := model.DiffFile{NewPath: &path, Status: model.StatusModified, Hunks: []model.DiffHunk{{
		Lines: []model.DiffLine{
			{Origin: model.OriginContext, Content: "visible", OldLineno: new(uint32(30)), NewLineno: new(uint32(30))},
			{Origin: model.OriginAddition, Content: "added", NewLineno: new(uint32(31))},
		},
		OldStart: 30, OldCount: 1, NewStart: 30, NewCount: 2,
	}}}
	file.ContentHash = model.ComputeContentHash(file.Hunks)
	backend := &numberedContextBackend{stubBackend: stubBackend{info: vcs.Info{RootPath: "/repo", HeadCommit: "abc", Type: vcs.TypeGit}}, total: 40}
	session := model.NewReviewSession("/repo", "abc", nil, model.SourceWorkingTree)
	a := app.NewApp(backend, backend.Info(), []model.DiffFile{file}, session, app.DiffSource{Kind: app.DiffSourceWorkingTree})
	m := NewModel(a, theme.TokyoNightStorm())
	m.width, m.height = 120, 60
	m.syncViewport()

	cursorToExpander(t, m)
	if cmd := m.expandGapAtCursor(); cmd != nil {
		t.Fatal("a local expansion needs no command")
	}
	view := plainView(m)
	for _, want := range []string{"hidden line 29", "hidden line 10"} {
		if !strings.Contains(view, want) {
			t.Errorf("expanded context %q not rendered:\n%s", want, view)
		}
	}
}
