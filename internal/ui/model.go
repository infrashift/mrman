package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/output"
	"github.com/infrashift/mrman/internal/persistence"
	"github.com/infrashift/mrman/internal/theme"
)

// tickMsg drives message expiry and periodic housekeeping (100ms heartbeat).
type tickMsg time.Time

const ctrlCWindow = 2 * time.Second

// Model is the root Bubbletea model owning the App state.
type Model struct {
	App    *app.App
	Theme  *theme.Theme
	Styles theme.Styles

	diffPane DiffPane
	fileList FileListPane

	pendingZ, pendingShiftZ, pendingD, pendingLeader bool
	pendingCtrlC                                     time.Time
	leader                                           rune

	completion *input.CompletionState

	// session is the persisted-session lifecycle; nil in tests that run
	// without a store.
	session *sessionLifecycle
	// store backs new lifecycles opened after a selector confirm; may be
	// nil (no persistence).
	store *persistence.Store

	// export configures notes export; PendingStdout collects --stdout
	// output printed by Run after the program exits.
	export        exportOptions
	PendingStdout string

	// vim is the active comment-vim wrapper; nil when composing without
	// vim mode or not composing at all.
	vim            *vimState
	CommentVimMode bool

	width, height int
}

// enterComposeMode initializes the vim wrapper when enabled.
func (m *Model) enterComposeMode() {
	if m.CommentVimMode {
		m.vim = newVimState(m.App)
	}
}

// saveComment persists a finished comment and autosaves the session.
func (m *Model) saveComment() {
	m.vim = nil
	if comment := m.App.SaveComment(); comment != nil {
		if err := m.saveSession(); err != nil {
			m.App.SetError("Save failed: " + err.Error())
			return
		}
		m.App.SetMessage("Comment saved")
	}
}

// autosave persists after review-state mutations, matching tuicr's
// save-on-toggle behavior.
func (m *Model) autosave() {
	if err := m.saveSession(); err != nil {
		m.App.SetError("Save failed: " + err.Error())
	}
}

// saveSession persists the live session when a lifecycle is attached.
func (m *Model) saveSession() error {
	if m.session == nil {
		return nil
	}
	return m.session.save(m.App)
}

// NewModel wires a Model around app state and a resolved theme.
func NewModel(a *app.App, t *theme.Theme) *Model {
	return &Model{
		App:      a,
		Theme:    t,
		Styles:   theme.NewStyles(t),
		diffPane: DiffPane{Theme: t},
		fileList: FileListPane{Theme: t},
		leader:   ';',
	}
}

// Init requests the first tick.
func (m *Model) Init() tea.Cmd {
	return tick()
}

func tick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// Update is the event pipeline, transliterating tuicr's main loop ordering.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.syncViewport()
		if msg.Width < 100 {
			m.App.ShowFileList = false
		}
	case tea.KeyboardEnhancementsMsg:
		m.App.SupportsKeyboardEnhancement = msg.SupportsEventTypes()
	case tea.KeyReleaseMsg:
		k := tea.Key(msg)
		if k.Code == 'j' || k.Code == tea.KeyDown {
			m.App.DownReleasedSinceArm = true
		}
		if k.Code == 'k' || k.Code == tea.KeyUp {
			m.App.UpReleasedSinceArm = true
		}
	case tickMsg:
		m.App.ClearExpiredMessage()
		if !m.pendingCtrlC.IsZero() && time.Since(m.pendingCtrlC) > ctrlCWindow {
			m.pendingCtrlC = time.Time{}
		}
		if m.session != nil {
			composing := m.App.InputMode == input.ModeComment
			if merged := m.session.pollExternalChanges(m.App, composing); merged > 0 {
				m.App.SetMessage(fmt.Sprintf("Merged %d external change(s)", merged))
			}
		}
		return m, tick()
	case tea.KeyPressMsg:
		return m.handleKey(tea.Key(msg))
	case tea.PasteMsg:
		m.handlePaste(msg.Content)
	}
	return m, nil
}

// handlePaste routes bracketed-paste text to the active input surface.
func (m *Model) handlePaste(text string) {
	a := m.App
	switch a.InputMode {
	case input.ModeComment:
		if m.vim != nil {
			m.vim.editor.InsertText(text)
			a.CommentBuffer = m.vim.editor.Text()
			a.CommentCursor = m.vim.editor.Cursor()
		} else {
			a.InsertCommentText(text)
		}
	case input.ModeCommand:
		a.CommandBuffer += text
	case input.ModeSearch:
		a.SearchBuffer += text
	}
}

func (m *Model) syncViewport() {
	innerH := m.height - 4 // header + status + diff borders
	if innerH < 1 {
		innerH = 1
	}
	m.App.DiffState.ViewportHeight = innerH
	m.App.SyncViewportWidth(m.diffInnerWidth())
}

func (m *Model) diffInnerWidth() int {
	w := m.width
	if m.App.ShowFileList {
		w -= m.fileListWidth()
	}
	w -= 2 // borders
	if w < 10 {
		w = 10
	}
	return w
}

func (m *Model) fileListWidth() int {
	return m.width / 5
}

func (m *Model) handleKey(k tea.Key) (tea.Model, tea.Cmd) {
	a := m.App

	// 1. Ctrl-C twice to exit.
	if k.Mod == tea.ModCtrl && k.Code == 'c' {
		if !m.pendingCtrlC.IsZero() && time.Since(m.pendingCtrlC) <= ctrlCWindow {
			return m, tea.Quit
		}
		m.pendingCtrlC = time.Now()
		a.SetWarning("Press Ctrl+C again to exit")
		return m, nil
	}
	m.pendingCtrlC = time.Time{}

	// 2. Pending chords.
	if m.pendingZ {
		m.pendingZ = false
		if r, ok := printable(k); ok {
			switch r {
			case 'z':
				a.CenterCursor()
				return m, nil
			case 't':
				a.CursorToTop()
				return m, nil
			case 'b':
				a.CursorToBottom()
				return m, nil
			}
		}
	}
	if m.pendingShiftZ {
		m.pendingShiftZ = false
		if r, ok := printable(k); ok && (r == 'Z' || r == 'Q') {
			if r == 'Z' {
				// ZZ saves before quitting; ZQ quits without saving.
				if err := m.saveSession(); err != nil {
					a.SetError("Save failed: " + err.Error())
					return m, nil
				}
			}
			return m, tea.Quit
		}
	}
	if m.pendingD {
		m.pendingD = false
		if r, ok := printable(k); ok && r == 'd' {
			if a.DeleteCommentAtCursor() {
				m.autosave()
			}
			return m, nil
		}
	}

	// Comment-vim routing intercepts everything while composing with vim on.
	if a.InputMode == input.ModeComment && m.vim != nil {
		switch m.vim.handleKey(a, k) {
		case vimSave:
			m.saveComment()
		case vimCancel:
			m.vim = nil
			a.ExitCommentMode()
		case vimCycleType:
			a.CycleCommentType()
		case vimCycleTypeReverse:
			a.CycleCommentTypeReverse()
		}
		return m, nil
	}
	if m.pendingLeader {
		m.pendingLeader = false
		if r, ok := printable(k); ok {
			m.handleLeader(r)
			return m, nil
		}
	}

	// 3. Keymap lookup.
	action := input.MapKey(k, a.InputMode, m.leader)

	// 4. Pending-setter actions.
	switch action.Kind {
	case input.PendingZCommand:
		m.pendingZ = true
		a.PendingCount = nil
		return m, nil
	case input.PendingShiftZCommand:
		m.pendingShiftZ = true
		return m, nil
	case input.PendingDCommand:
		m.pendingD = true
		return m, nil
	case input.PendingLeaderCommand:
		m.pendingLeader = true
		return m, nil
	}

	// 5. Count prefix (Normal mode only).
	if a.InputMode == input.ModeNormal {
		if action.Kind == input.Digit {
			n := 0
			if a.PendingCount != nil {
				n = *a.PendingCount
			}
			n = n*10 + action.N
			if n > 999_999 {
				n = 999_999
			}
			a.PendingCount = &n
			return m, nil
		}
		if a.PendingCount != nil {
			count := *a.PendingCount
			a.PendingCount = nil
			switch action.Kind {
			case input.GoToBottom:
				a.GoToSourceLine(uint32(count), "new")
				return m, nil
			case input.CursorDown, input.CursorUp, input.ScrollLeft, input.ScrollRight,
				input.ScrollViewDown, input.ScrollViewUp:
				action.N *= count
			case input.NextFile, input.PrevFile, input.NextHunk, input.PrevHunk:
				for i := 0; i < count-1; i++ {
					m.dispatch(action)
				}
			}
		}
	}

	// 6. Dispatch.
	if quit := m.dispatch(action); quit {
		return m, tea.Quit
	}
	return m, nil
}

func printable(k tea.Key) (rune, bool) {
	if k.Mod&^tea.ModShift != 0 {
		return 0, false
	}
	runes := []rune(k.Text)
	if len(runes) != 1 {
		return 0, false
	}
	return runes[0], true
}

func (m *Model) handleLeader(r rune) {
	a := m.App
	switch r {
	case 'e':
		a.ToggleFileList()
	case 'h':
		if a.ShowFileList {
			a.FocusedPanel = app.PanelFileList
		}
	case 'l':
		a.FocusedPanel = app.PanelDiff
	case 'j':
		a.FocusedPanel = app.PanelDiff
	case 'c':
		a.EnterReviewCommentMode()
		m.enterComposeMode()
	case 'f':
		a.ToggleSingleFileView()
	}
}

// dispatch routes an action; returns true to quit.
func (m *Model) dispatch(action input.Action) bool {
	a := m.App
	switch a.InputMode {
	case input.ModeHelp:
		return m.dispatchHelp(action)
	case input.ModeCommand:
		return m.dispatchCommand(action)
	case input.ModeSearch:
		return m.dispatchSearch(action)
	case input.ModeComment:
		return m.dispatchComment(action)
	case input.ModeVisualSelect:
		return m.dispatchVisual(action)
	case input.ModeCommitSelect:
		return m.dispatchCommitSelect(action)
	case input.ModeNormal:
		if a.FocusedPanel == app.PanelFileList {
			if handled := m.dispatchFileList(action); handled {
				return false
			}
		}
		return m.dispatchNormal(action)
	}
	return false
}

func (m *Model) dispatchHelp(action input.Action) bool {
	a := m.App
	switch action.Kind {
	case input.ToggleHelp:
		a.ToggleHelp()
	case input.CursorDown:
		a.HelpScrollDown(action.N)
	case input.CursorUp:
		a.HelpScrollUp(action.N)
	case input.PageDown, input.HalfPageDown:
		a.HelpScrollDown(a.HelpState.ViewportHeight / 2)
	case input.PageUp, input.HalfPageUp:
		a.HelpScrollUp(a.HelpState.ViewportHeight / 2)
	case input.GoToTop:
		a.HelpScrollToTop()
	case input.GoToBottom:
		a.HelpScrollToBottom()
	case input.EnterSearchMode:
		a.EnterSearchMode()
	case input.SearchNext:
		a.SearchNextInHelp()
	case input.SearchPrev:
		a.SearchPrevInHelp()
	}
	return false
}

func (m *Model) dispatchCommand(action input.Action) bool {
	a := m.App
	switch action.Kind {
	case input.ExitMode:
		a.ExitCommandMode()
		m.completion = nil
	case input.InsertChar:
		a.CommandBuffer += string(action.Ch)
		m.completion = nil
	case input.DeleteChar:
		if a.CommandBuffer != "" {
			a.CommandBuffer = a.CommandBuffer[:len(a.CommandBuffer)-1]
		}
		m.completion = nil
	case input.ClearLine:
		a.CommandBuffer = ""
		m.completion = nil
	case input.DeleteWord:
		a.CommandBuffer = strings.TrimRight(a.CommandBuffer, " ")
		if idx := strings.LastIndex(a.CommandBuffer, " "); idx >= 0 {
			a.CommandBuffer = a.CommandBuffer[:idx+1]
		} else {
			a.CommandBuffer = ""
		}
		m.completion = nil
	case input.CompleteCommand, input.CompleteCommandReverse:
		res := input.Complete(a.CommandBuffer, m.completion, action.Kind == input.CompleteCommandReverse)
		a.CommandBuffer = res.Buffer
		m.completion = res.State
		if res.Message != "" {
			a.SetMessage(res.Message)
		}
	case input.SubmitInput:
		cmd := input.ParseCommand(a.CommandBuffer)
		a.ExitCommandMode()
		m.completion = nil
		return m.runCommand(cmd)
	}
	return false
}

func (m *Model) runCommand(cmd input.Command) bool {
	a := m.App
	switch cmd.Kind {
	case input.CmdQuit:
		// tuicr's dirty guard: unsaved comments block :q; reviewed-only
		// dirt just quits (the session was saved by toggles or discards).
		if a.Dirty && a.Session.HasComments() {
			a.SetError("No write since last change (add ! to override)")
			return false
		}
		return true
	case input.CmdForceQuit:
		return true
	case input.CmdWrite:
		if err := m.saveSession(); err != nil {
			a.SetError("Save failed: " + err.Error())
		} else {
			a.SetMessage("Session saved")
		}
	case input.CmdWriteQuit:
		if err := m.saveSession(); err != nil {
			a.SetError("Save failed: " + err.Error())
			return false
		}
		return true
	case input.CmdToggleWrap:
		a.ToggleDiffWrap()
	case input.CmdSetWrap:
		a.SetDiffWrap(true)
	case input.CmdDiff:
		a.ToggleDiffViewMode()
	case input.CmdHelp:
		a.ToggleHelp()
	case input.CmdFocus:
		a.ToggleSingleFileView()
	case input.CmdGotoLine:
		a.GoToSourceLine(cmd.N, "new")
	case input.CmdGotoLineOld:
		a.GoToSourceLine(cmd.N, "old")
	case input.CmdExport:
		if out := m.exportToClipboard(); out != "" {
			m.PendingStdout = out
		}
	case input.CmdClear:
		cleared, unreviewed := a.Session.ClearComments(model.ClearCommentsAndReviewed)
		a.Dirty = true
		a.RebuildAnnotations()
		a.SetMessage(fmt.Sprintf("Cleared %d comment(s), unreviewed %d file(s)", cleared, unreviewed))
	case input.CmdClearCommentsOnly:
		cleared, _ := a.Session.ClearComments(model.ClearCommentsOnly)
		a.Dirty = true
		a.RebuildAnnotations()
		a.SetMessage(fmt.Sprintf("Cleared %d comment(s)", cleared))
	case input.CmdStage:
		if !a.CanStage() {
			a.SetMessage("Staging is only available for unstaged reviews in git")
			break
		}
		staged := a.StageReviewedFiles()
		a.SetMessage(fmt.Sprintf("Staged %d reviewed file(s)", staged))
	case input.CmdSetCommitsVisible, input.CmdSetCommitsHidden, input.CmdToggleCommits,
		input.CmdReload, input.CmdEdit,
		input.CmdTargetsLocal, input.CmdTargetsPrs, input.CmdSubmitPicker,
		input.CmdSubmitComment, input.CmdSubmitApprove, input.CmdSubmitRequestChanges,
		input.CmdSubmitDraft, input.CmdCommentsUnresolved, input.CmdCommentsAll,
		input.CmdCommentsHide, input.CmdToggleVim, input.CmdSetVim, input.CmdSetNoVim,
		input.CmdVersion:
		a.SetMessage("Not available yet: :" + cmd.Raw)
	default:
		a.SetError("Unknown command: " + cmd.Raw)
	}
	return false
}

// dispatchCommitSelect handles the full-screen target selector.
func (m *Model) dispatchCommitSelect(action input.Action) bool {
	a := m.App
	switch action.Kind {
	case input.Quit:
		return true
	case input.CommitSelectDown:
		a.CommitSelectDown()
	case input.CommitSelectUp:
		a.CommitSelectUp()
	case input.ToggleCommitSelect:
		if a.IsOnExpandRow() {
			if err := a.ExpandCommit(); err != nil {
				a.SetError("Load more failed: " + err.Error())
			}
		} else {
			a.ToggleCommitSelectionAndAdvance()
		}
	case input.ConfirmCommitSelect:
		if a.IsOnExpandRow() {
			if err := a.ExpandCommit(); err != nil {
				a.SetError("Load more failed: " + err.Error())
			}
		} else {
			m.confirmSelection()
		}
	case input.ExitMode:
		switch a.ExitCommitSelectMode() {
		case app.ExitSelectorLoadWorkingTree, app.ExitSelectorReloadInline:
			// Nothing loaded yet (fresh start): keep whatever is shown.
		case app.ExitSelectorNone:
		}
	case input.TargetSelectorTabNext:
		a.CycleTargetTab(true)
	case input.TargetSelectorTabPrev:
		a.CycleTargetTab(false)
	}
	return false
}

// dispatchComment handles non-vim comment composition.
func (m *Model) dispatchComment(action input.Action) bool {
	a := m.App
	switch action.Kind {
	case input.ExitMode:
		a.ExitCommentMode()
	case input.SubmitInput:
		m.saveComment()
	case input.InsertChar:
		a.InsertCommentChar(action.Ch)
	case input.Paste:
		a.InsertCommentText(action.Text)
	case input.DeleteChar:
		a.DeleteCommentChar()
	case input.DeleteWord:
		a.DeleteCommentWord()
	case input.ClearLine:
		a.ClearCommentLine()
	case input.CycleCommentType:
		a.CycleCommentType()
	case input.CycleCommentTypeReverse:
		a.CycleCommentTypeReverse()
	case input.TextCursorLeft:
		a.CommentCursorLeft()
	case input.TextCursorRight:
		a.CommentCursorRight()
	case input.TextCursorLineStart:
		a.CommentCursorLineStart()
	case input.TextCursorLineEnd:
		a.CommentCursorLineEnd()
	case input.TextCursorWordLeft:
		a.CommentCursorWordLeft()
	case input.TextCursorWordRight:
		a.CommentCursorWordRight()
	}
	return false
}

// dispatchVisual handles visual selection mode.
func (m *Model) dispatchVisual(action input.Action) bool {
	a := m.App
	switch action.Kind {
	case input.Quit:
		return true
	case input.CursorDown:
		a.CursorDown(action.N)
		a.ExtendVisualToCursor()
	case input.CursorUp:
		a.CursorUp(action.N)
		a.ExtendVisualToCursor()
	case input.AddRangeComment:
		a.EnterCommentFromVisual()
		m.enterComposeMode()
	case input.ExportToClipboard:
		text, chars, err := a.CopyVisualSelection()
		a.ExitVisualMode()
		if err != nil {
			a.SetError("Copy failed: " + err.Error())
			break
		}
		if _, copyErr := output.CopyText(text); copyErr != nil {
			a.SetError("Clipboard failed: " + copyErr.Error())
		} else {
			a.SetMessage(fmt.Sprintf("Yanked %d character(s)", chars))
		}
	case input.ExitMode:
		a.ExitVisualMode()
	}
	return false
}

func (m *Model) dispatchSearch(action input.Action) bool {
	a := m.App
	switch action.Kind {
	case input.ExitMode:
		a.ExitSearchMode()
	case input.InsertChar:
		a.SearchBuffer += string(action.Ch)
	case input.DeleteChar:
		if a.SearchBuffer != "" {
			a.SearchBuffer = a.SearchBuffer[:len(a.SearchBuffer)-1]
		}
	case input.ClearLine:
		a.SearchBuffer = ""
	case input.SubmitInput:
		pattern := a.SearchBuffer
		// Search reads the live buffer, so it must run before exiting
		// search mode (which clears the buffer).
		var found bool
		if a.SearchingHelp() {
			found = a.SearchInHelpFromScroll()
		} else {
			found = a.SearchInDiffFromCursor()
		}
		a.ExitSearchMode()
		if !found {
			a.SetWarning("Pattern not found: " + pattern)
		}
	}
	return false
}

func (m *Model) dispatchFileList(action input.Action) bool {
	a := m.App
	switch action.Kind {
	case input.CursorDown:
		a.FileListDown(action.N)
	case input.CursorUp:
		a.FileListUp(action.N)
	case input.SelectFile, input.SelectFileFull:
		if item, ok := a.SelectedTreeItem(); ok {
			if item.IsDir {
				a.ToggleDirectory(item.Path)
			} else {
				a.JumpToFile(item.FileIdx)
				a.FocusedPanel = app.PanelDiff
			}
		}
	case input.ScrollLeft:
		a.FileListState.ScrollLeft(action.N)
	case input.ScrollRight:
		a.FileListState.ScrollRight(action.N)
	case input.ToggleExpand:
		if item, ok := a.SelectedTreeItem(); ok && item.IsDir {
			a.ToggleDirectory(item.Path)
		}
	case input.ExpandAll:
		a.ExpandAllDirs()
	case input.CollapseAll:
		a.CollapseAllDirs()
	default:
		return false
	}
	return true
}

func (m *Model) dispatchNormal(action input.Action) bool {
	a := m.App
	switch action.Kind {
	case input.Quit:
		return true
	case input.CursorDown:
		a.CursorDown(action.N)
	case input.CursorUp:
		a.CursorUp(action.N)
	case input.HalfPageDown:
		a.ScrollDown(a.DiffState.EffectiveVisibleLines() / 2)
	case input.HalfPageUp:
		a.ScrollUp(a.DiffState.EffectiveVisibleLines() / 2)
	case input.PageDown:
		a.ScrollDown(a.DiffState.EffectiveVisibleLines())
	case input.PageUp:
		a.ScrollUp(a.DiffState.EffectiveVisibleLines())
	case input.ScrollViewDown:
		a.ScrollViewDown(action.N)
	case input.ScrollViewUp:
		a.ScrollViewUp(action.N)
	case input.ScrollLeft:
		a.ScrollLeft(action.N)
	case input.ScrollRight:
		a.ScrollRight(action.N)
	case input.GoToTop:
		a.MoveCursorToAnnotation(0)
		a.DiffState.ScrollOffset = 0
	case input.GoToBottom:
		a.JumpToBottom()
	case input.NextFile:
		a.NextFile()
	case input.PrevFile:
		a.PrevFile()
	case input.NextHunk:
		a.NextHunk()
	case input.PrevHunk:
		a.PrevHunk()
	case input.ToggleFocus:
		m.cycleFocus(1)
	case input.ToggleFocusReverse:
		m.cycleFocus(-1)
	case input.ToggleExpand:
		m.expandGapAtCursor()
	case input.EnterCommandMode:
		a.EnterCommandMode()
	case input.EnterSearchMode:
		a.EnterSearchMode()
	case input.ToggleHelp:
		a.ToggleHelp()
	case input.SearchNext:
		if !a.SearchNextInDiff() {
			a.SetWarning("Pattern not found")
		}
	case input.SearchPrev:
		if !a.SearchPrevInDiff() {
			a.SetWarning("Pattern not found")
		}
	case input.ExportToClipboard:
		if out := m.exportToClipboard(); out != "" {
			m.PendingStdout = out
		}
	case input.ToggleReviewed:
		a.ToggleReviewed()
		m.autosave()
	case input.ToggleHunkReviewed:
		a.ToggleHunkReviewed()
		m.autosave()
	case input.AddLineComment:
		a.EnterCommentMode(false)
		m.enterComposeMode()
	case input.AddFileComment:
		a.EnterCommentMode(true)
		m.enterComposeMode()
	case input.EditComment:
		a.EnterEditMode(false)
		m.enterComposeMode()
	case input.EditCommentAtEnd:
		a.EnterEditMode(true)
		m.enterComposeMode()
	case input.EnterVisualMode:
		a.EnterVisualModeAtCursor()
	case input.NextComment:
		a.NextComment()
	case input.PrevComment:
		a.PrevComment()
	}
	return false
}

func (m *Model) cycleFocus(dir int) {
	a := m.App
	if !a.ShowFileList {
		a.FocusedPanel = app.PanelDiff
		return
	}
	if a.FocusedPanel == app.PanelDiff {
		a.FocusedPanel = app.PanelFileList
	} else {
		a.FocusedPanel = app.PanelDiff
	}
	_ = dir
}

func (m *Model) expandGapAtCursor() {
	a := m.App
	hit, ok := a.GapAtCursor()
	if !ok {
		return
	}
	switch hit.Kind {
	case app.GapHitExpander:
		limit := app.GapExpandBatch
		if err := a.ExpandGap(hit.GapID, hit.Direction, &limit); err != nil {
			a.SetError(err.Error())
		}
	case app.GapHitExpandedContent:
		a.CollapseGap(hit.GapID)
	}
}

// View composes the frame.
func (m *Model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return tea.NewView("")
	}
	a := m.App
	innerH := m.height - 4
	if innerH < 1 {
		innerH = 1
	}

	if a.InputMode == input.ModeHelp {
		return m.helpView()
	}
	if a.InputMode == input.ModeCommitSelect {
		view := tea.NewView(strings.Join(m.selectorView(), "\n"))
		view.AltScreen = true
		view.KeyboardEnhancements = tea.KeyboardEnhancements{ReportEventTypes: true}
		return view
	}

	var mainCols []string
	if a.ShowFileList {
		flWidth := m.fileListWidth()
		flInner := m.fileList.BuildLines(a, flWidth-2, innerH)
		mainCols = append(mainCols, m.titledPanel(
			strings.Join(flInner, "\n"), m.fileList.Title(a),
			a.FocusedPanel == app.PanelFileList, flWidth-2, innerH))
	}

	diffW := m.diffInnerWidth()
	diffInner := m.diffPane.BuildLines(a, diffW, innerH)
	if a.InputMode == input.ModeComment {
		overlay := m.diffPane.commentInputOverlay(a, m.vim, diffW)
		if len(overlay) > innerH {
			overlay = overlay[len(overlay)-innerH:]
		}
		copy(diffInner[innerH-len(overlay):], overlay)
	}
	mainCols = append(mainCols, m.titledPanel(
		strings.Join(diffInner, "\n"), m.diffTitle(),
		a.FocusedPanel == app.PanelDiff, diffW, innerH))

	frame := lipgloss.JoinVertical(lipgloss.Left,
		Header(a, m.Theme, m.width),
		lipgloss.JoinHorizontal(lipgloss.Top, mainCols...),
		StatusBar(a, m.Theme, m.width),
	)
	view := tea.NewView(frame)
	view.AltScreen = true
	view.KeyboardEnhancements = tea.KeyboardEnhancements{ReportEventTypes: true}
	return view
}

func (m *Model) diffTitle() string {
	a := m.App
	if a.IsCursorInOverview() {
		return " Overview "
	}
	if path, ok := a.CurrentFilePath(); ok {
		return " " + path + " "
	}
	return " Diff "
}

// titledPanel renders content in a border whose top edge embeds the title,
// built manually because splicing into an ANSI-styled border row is lossy.
func (m *Model) titledPanel(content, title string, focused bool, innerW, innerH int) string {
	borderStyle := m.Styles.Border(focused)
	body := borderStyle.BorderTop(false).
		Width(innerW).Height(innerH).
		Render(content)

	titleW := 0
	if title != "" {
		titleW = len([]rune(title))
	}
	fill := innerW - titleW
	if fill < 0 {
		title = string([]rune(title)[:innerW])
		fill = 0
	}
	topStyle := borderStyle.UnsetBorderStyle().UnsetWidth().UnsetHeight()
	top := topStyle.Render("╭"+title+strings.Repeat("─", fill)+"╮") + "\n"
	return top + body
}

func (m *Model) helpView() tea.View {
	content := helpContent(m.leader)
	m.App.HelpState.TotalLines = len(content)
	m.App.HelpState.ViewportHeight = m.height - 2
	start := m.App.HelpState.ScrollOffset
	if start > len(content) {
		start = len(content)
	}
	end := start + m.App.HelpState.ViewportHeight
	if end > len(content) {
		end = len(content)
	}
	body := strings.Join(content[start:end], "\n")
	frame := lipgloss.JoinVertical(lipgloss.Left,
		Header(m.App, m.Theme, m.width),
		m.Styles.Border(true).Width(m.width-2).Height(m.height-4).Render(body),
		StatusBar(m.App, m.Theme, m.width),
	)
	view := tea.NewView(frame)
	view.AltScreen = true
	return view
}
