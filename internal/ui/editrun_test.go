package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/config"
	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs"
	"github.com/infrashift/mrman/internal/version"
)

func TestVersionCommandReportsTheBuild(t *testing.T) {
	m := testModel(t)
	m.runCommand(input.ParseCommand("version"))
	if m.App.Message == nil || !strings.Contains(m.App.Message.Content, version.String()) {
		t.Errorf(":version must report the build, got %+v", m.App.Message)
	}
}

func TestEditCommandQueuesTheFocusedFile(t *testing.T) {
	t.Setenv("EDITOR", "vi")
	m := testModel(t)
	m.App.FocusPanel(app.PanelDiff)
	m.App.DiffState.CursorLine = 3

	// The fixture's file has no real path on disk, so the app refuses and
	// explains rather than handing the terminal to an editor for nothing.
	m.runCommand(input.ParseCommand("edit"))
	if cmd := m.takeQueued(); cmd != nil {
		t.Error("a file that is not on disk must not open an editor")
	}
	if m.App.Message == nil {
		t.Error("refusing to edit must say why")
	}
}

func TestEditCommandNoLongerReportsUnavailable(t *testing.T) {
	m := testModel(t)
	m.runCommand(input.ParseCommand("edit"))
	if m.App.Message != nil && strings.Contains(m.App.Message.Content, "Not available yet") {
		t.Error(":edit is implemented and must not report otherwise")
	}
}

func TestEditorFailureIsReported(t *testing.T) {
	m := testModel(t)
	m.handleEditorFinished(editorFinishedMsg{Err: errors.New("exit status 1")})
	if m.App.Message == nil || !strings.Contains(m.App.Message.Content, "exit status 1") {
		t.Errorf("an editor failure must surface, got %+v", m.App.Message)
	}
}

func TestEditorReloadsOnlyWhenTheWorktreeCanHaveChanged(t *testing.T) {
	// A commit range is immutable history: reloading would throw away the
	// reviewer's position for nothing.
	m := testModel(t)
	backend := newReloadBackend(nil, nil)
	m.App.VCS = backend
	m.App.DiffSource = app.DiffSource{Kind: app.DiffSourceCommitRange, Commits: []string{"c1"}}

	m.handleEditorFinished(editorFinishedMsg{})
	if backend.calls != 0 {
		t.Error("a commit-range review must not reload after an editor session")
	}

	// A worktree review can have changed underneath the editor.
	path := "src/x.go"
	fresh := model.DiffFile{
		NewPath: &path, Status: model.StatusModified,
		Hunks: []model.DiffHunk{{
			Lines:    []model.DiffLine{{Origin: model.OriginAddition, Content: "edited", NewLineno: lineno(1)}},
			NewStart: 1, NewCount: 1,
		}},
	}
	fresh.ContentHash = model.ComputeContentHash(fresh.Hunks)
	worktree := newReloadBackend([]model.DiffFile{fresh}, nil)
	m.App.VCS = worktree
	m.App.DiffSource = app.DiffSource{Kind: app.DiffSourceWorkingTree}

	m.handleEditorFinished(editorFinishedMsg{})
	if worktree.calls != 1 {
		t.Errorf("a worktree review must reload, got %d calls", worktree.calls)
	}
	if !strings.Contains(plainView(m), "edited") {
		t.Error("the reloaded content must render")
	}
}

// contextBackend serves real context lines so an expansion actually grows
// the stream; the shared stub returns none.
type contextBackend struct{ stubBackend }

func (b *contextBackend) FetchContextLines(
	_ string, _ model.FileStatus, _ *string, start, end uint32,
) ([]model.DiffLine, error) {
	var lines []model.DiffLine
	for n := start; n <= end; n++ {
		lineNo := n
		lines = append(lines, model.DiffLine{
			Origin: model.OriginContext, Content: "context", NewLineno: &lineNo,
		})
	}
	return lines, nil
}

func TestEnterAndSpaceBothExpandContextGaps(t *testing.T) {
	for _, tc := range []struct {
		name string
		send func(m *Model)
	}{
		{"space", func(m *Model) { press(m, " ", tea.KeySpace, 0) }},
		{"enter", func(m *Model) { press(m, "", tea.KeyEnter, 0) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := testModel(t)
			m.App.VCS = &contextBackend{stubBackend{
				info: vcs.Info{HeadCommit: "abc", Type: vcs.TypeGit},
			}}
			// Push the hunk down the file so a leading gap exists.
			m.App.DiffFiles[0].Hunks[0].OldStart = 20
			m.App.DiffFiles[0].Hunks[0].NewStart = 20
			m.App.RebuildAnnotations()
			m.App.FocusPanel(app.PanelDiff)

			expander := -1
			for i := range m.App.LineAnnotations {
				if m.App.LineAnnotations[i].Kind == app.AnnExpander {
					expander = i
					break
				}
			}
			if expander < 0 {
				t.Fatal("expected an expander row")
			}
			m.App.DiffState.CursorLine = expander
			before := len(m.App.LineAnnotations)

			tc.send(m)
			if len(m.App.LineAnnotations) <= before {
				t.Errorf("%s must expand the gap", tc.name)
			}
		})
	}
}

func TestEnterStillOpensFilesInTheTree(t *testing.T) {
	m := testModel(t)
	m.App.FocusPanel(app.PanelFileList)
	items := m.App.BuildVisibleItems()
	fileRow := -1
	for i := range items {
		if !items[i].IsDir {
			fileRow = i
			break
		}
	}
	if fileRow < 0 {
		t.Fatal("expected a file row")
	}
	m.App.FileListState.Select(fileRow)

	press(m, "", tea.KeyEnter, 0)
	if m.App.FocusedPanel != app.PanelDiff {
		t.Error("Enter in the tree must still open the file, not expand a gap")
	}
}

func TestWhitespaceModeFollowsTheConfig(t *testing.T) {
	if got := whitespaceMode(config.Config{IgnoreWhitespace: false}); got != vcs.WhitespaceNormal {
		t.Errorf("default = %v, want WhitespaceNormal", got)
	}
	if got := whitespaceMode(config.Config{IgnoreWhitespace: true}); got != vcs.WhitespaceIgnoreAll {
		t.Errorf("ignore_whitespace = true must reach the VCS layer, got %v", got)
	}
}

func TestCommentTabWidthReachesTheVimEditor(t *testing.T) {
	m := testModel(t)
	m.CommentVimMode = true
	m.commentTabWidth = 8
	m.enterComposeMode()

	if m.vim == nil {
		t.Fatal("vim mode must build an editor")
	}
	if m.vim.editor.TabWidth != 8 {
		t.Errorf("comment_tab_width = %d, want 8", m.vim.editor.TabWidth)
	}

	// Unset config keeps vimtext's own default rather than zeroing it.
	m2 := testModel(t)
	m2.CommentVimMode = true
	m2.enterComposeMode()
	if m2.vim.editor.TabWidth <= 0 {
		t.Errorf("an unset width must keep the built-in default, got %d", m2.vim.editor.TabWidth)
	}
}

func TestApplyConfigWiresThePreviouslyUnreadKeys(t *testing.T) {
	m := testModel(t)
	cfg := config.Default()
	cfg.Mouse = false
	cfg.CommentTabWidth = 2
	applyConfig(cfg, m.App, m)

	if m.mouseEnabled {
		t.Error("mouse = false must reach the model")
	}
	if m.commentTabWidth != 2 {
		t.Errorf("comment_tab_width = %d, want 2", m.commentTabWidth)
	}
}
