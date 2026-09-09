package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/infrashift/mrman/charmkit/keychord"
	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/input"
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

	diffPane    DiffPane
	fileList    FileListPane
	commentNav  CommentNavPane
	commitStrip CommitStripPane

	// chords resolves the two-key prefixes (z, Z, d and the leader).
	// Counts stay out of it: mrman's count applies per-action semantics —
	// scaling j/k, repeating }/{ — that keychord deliberately does not
	// model, so the keymap keeps emitting Digit and PendingCount.
	chords       *keychord.Resolver
	pendingCtrlC time.Time
	leader       rune

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
	// commentTabWidth is the soft-tab width Tab inserts in the vim comment
	// box (config `comment_tab_width`); 0 keeps vimtext's own default.
	commentTabWidth int

	// forge lazily resolves the driver backing the selector's Pull
	// Requests tab; nil in tests and when no forge remote exists.
	forge *forgeResolver

	// inflight holds the cancel for each replaceable async family. A new
	// request in a family cancels the previous one, and shutdown cancels
	// them all, so an abandoned forge call stops instead of running to
	// completion behind a generation check that will discard it anyway.
	inflight inflightRequests
	// queuedCmd holds async work started deep in the dispatch tree — a ":"
	// command that reaches the forge, say. handleKey drains it after
	// dispatching, so every dispatch handler need not return a tea.Cmd.
	queuedCmd tea.Cmd
	// localCheckout is the working copy a PR review may read blobs from as
	// an optimization, "" when reviewing outside a checkout.
	localCheckout string

	// grantedEvents is the agent-submit authorization this TUI was launched
	// with (--auto). It follows the process, not the pull request: opening
	// a different PR from the selector, or advancing to a new head, stays
	// within the session the human authorized.
	grantedEvents []string

	// layout is the frame's hit-test map, rebuilt by every View.
	layout layoutRects
	// mouseEnabled mirrors the `mouse` config; when false no mouse mode is
	// requested at all, so the terminal keeps its own selection.
	mouseEnabled bool
	// dragging is true between a diff press and its release.
	dragging bool
	// dragAnchor is where a press landed, held until the pointer actually
	// moves. The selection is only created then, so a plain click leaves none
	// — a zero-width selection would make y copy nothing instead of
	// exporting the review.
	dragAnchor *app.SelPoint

	width, height int
}

// enterComposeMode initializes the vim wrapper when enabled.
func (m *Model) enterComposeMode() {
	if m.CommentVimMode {
		m.vim = newVimState(m.App, m.commentTabWidth)
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
		App:         a,
		Theme:       t,
		Styles:      theme.NewStyles(t),
		diffPane:    DiffPane{Theme: t},
		fileList:    FileListPane{Theme: t},
		commentNav:  CommentNavPane{Theme: t},
		commitStrip: CommitStripPane{Theme: t},
		leader:      ';',
		chords:      newChordResolver(';'),
	}
}

// newChordResolver builds the chord state machine for a leader key.
func newChordResolver(leader rune) *keychord.Resolver {
	return keychord.New(keychord.Config{
		Prefixes: []string{"z", "Z", "d"},
		Leader:   string(leader),
	})
}

// SetLeader rebinds the leader and rebuilds the chord resolver around it.
func (m *Model) SetLeader(leader rune) {
	m.leader = leader
	m.chords = newChordResolver(leader)
}

// Init requests the first tick.
func (m *Model) Init() tea.Cmd {
	// `mrman pr <target>` arrives already in PR mode, so its existing
	// discussions are fetched on the first frame rather than on open.
	return tea.Batch(tick(), m.loadRemoteCommentsOnOpen())
}

func tick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// Update is the event pipeline, transliterating tuicr's main loop ordering.
//
// Every path funnels through takeQueued: a handler that queues async work —
// the review-metadata reload, a ":" command reaching the forge — must have
// it issued no matter which message triggered it. Draining only on
// keypresses stranded that work and left the spinner it set running.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := m.update(msg)
	if queued := m.takeQueued(); queued != nil {
		if cmd == nil {
			return model, queued
		}
		return model, tea.Batch(cmd, queued)
	}
	return model, cmd
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if cmd, handled := m.handleTerminalEvent(msg); handled {
		return m, cmd
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(tea.Key(msg))
	case tea.PasteMsg:
		m.handlePaste(msg.Content)
	case prSubmitResultMsg:
		m.handleSubmitResult(msg)
	case prListResultMsg:
		m.handlePrListResult(msg)
	case patchListResultMsg:
		m.handlePatchListResult(msg)
	case prOpenResultMsg:
		return m, m.handlePrOpenResult(msg)
	case remoteCommentsResultMsg:
		m.handleRemoteCommentsResult(msg)
	case prContextResultMsg:
		m.handlePrContextResult(msg)
	case prReloadResultMsg:
		return m, m.handlePrReloadResult(msg)
	case prRangeDiffResultMsg:
		m.handlePrRangeDiffResult(msg)
	case tea.MouseMsg:
		return m, m.handleMouse(msg)
	case editorFinishedMsg:
		m.handleEditorFinished(msg)
	}
	return m, nil
}

// handleTerminalEvent takes the messages that come from the terminal and
// the clock rather than from the user's keys or an async result: resize,
// keyboard-protocol negotiation, key release, and the tick.
func (m *Model) handleTerminalEvent(msg tea.Msg) (tea.Cmd, bool) {
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
		return tick(), true
	default:
		return nil, false
	}
	return nil, true
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
	innerH := max(
		// header + status + diff borders
		m.height-4, 1)
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

// handleKey is the key pipeline: Ctrl-C guard, comment-vim routing, chord
// feeding, keymap lookup, count prefix, modal dispatch, then the normal
// dispatch. Each stage that consumes the key returns early.
func (m *Model) handleKey(k tea.Key) (tea.Model, tea.Cmd) {
	a := m.App

	if done, cmd := m.handleCtrlC(k); done {
		return m, cmd
	}
	if a.InputMode == input.ModeComment && m.vim != nil {
		m.routeVimComment(k)
		return m, nil
	}
	if done, quit := m.feedChord(k); done {
		if quit {
			return m, tea.Quit
		}
		return m, nil
	}

	action := input.MapKey(k, a.InputMode, m.leader)
	action, consumed := m.applyCountPrefix(action)
	if consumed {
		return m, nil
	}

	if cmd, quit, handled := m.dispatchModal(k, action); handled {
		if quit {
			return m, tea.Quit
		}
		return m, cmd
	}

	if quit := m.dispatch(action); quit {
		return m, tea.Quit
	}
	// The jump planner can arm a forge fetch from deep inside the state
	// machine; Update drains it with everything else.
	m.queue(m.drainPrContextRequest())
	return m, nil
}

// handleCtrlC implements Ctrl-C twice to exit. done is true when the key
// was Ctrl-C, whether it quit or only armed the second press.
func (m *Model) handleCtrlC(k tea.Key) (done bool, cmd tea.Cmd) {
	if k.Mod != tea.ModCtrl || k.Code != 'c' {
		m.pendingCtrlC = time.Time{}
		return false, nil
	}
	if !m.pendingCtrlC.IsZero() && time.Since(m.pendingCtrlC) <= ctrlCWindow {
		return true, tea.Quit
	}
	m.pendingCtrlC = time.Now()
	m.App.SetWarning("Press Ctrl+C again to exit")
	return true, nil
}

// routeVimComment hands a key to the comment editor's vim layer, which
// intercepts everything while composing.
func (m *Model) routeVimComment(k tea.Key) {
	a := m.App
	m.chords.Reset()
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
}

// feedChord runs the chord recognizer. Every prefix comes from the
// normal-mode keymap, so a half-typed chord is abandoned the moment input
// goes anywhere else — otherwise it would complete against a text field.
// done is true when the key was consumed (as a prefix or a chord).
func (m *Model) feedChord(k tea.Key) (done, quit bool) {
	a := m.App
	if a.InputMode != input.ModeNormal {
		m.chords.Reset()
		return false, false
	}
	r, ok := printable(k)
	if !ok {
		// A non-printable key cannot complete a chord; drop the prefix and
		// let the keymap have the key.
		m.chords.Reset()
		return false, false
	}
	ev := m.chords.Feed(string(r))
	if ev.Consumed {
		// Starting a chord abandons any count typed before it: "5zz"
		// centers the view, it does not center it five times and then
		// apply the 5 to whatever comes next.
		a.PendingCount = nil
		return true, false
	}
	if ev.IsChord() {
		quit, handled := m.dispatchChord(ev)
		if quit || handled {
			return true, quit
		}
		// An unrecognized chord is a mistyped prefix, not a mistyped key:
		// let the second key act on its own, as it did before the prefix
		// was pressed.
	}
	return false, false
}

// applyCountPrefix accumulates a typed count and applies it to the action
// it precedes. Normal mode and the help popup both take one — the popup is
// a scrollable document. consumed is true when the key was a digit.
func (m *Model) applyCountPrefix(action input.Action) (input.Action, bool) {
	a := m.App
	if a.InputMode != input.ModeNormal && a.InputMode != input.ModeHelp {
		return action, false
	}
	if action.Kind == input.Digit {
		n := 0
		if a.PendingCount != nil {
			n = *a.PendingCount
		}
		n = min(n*10+action.N, 999_999)
		a.PendingCount = &n
		return action, true
	}
	if a.PendingCount == nil {
		return action, false
	}
	count := *a.PendingCount
	a.PendingCount = nil
	switch action.Kind {
	case input.GoToBottom:
		// {N}G jumps to a source line, which the help popup does not
		// have; there a count leaves G meaning "to the end".
		if a.InputMode == input.ModeNormal {
			a.GoToSourceLine(uint32(count), "new") //nolint:gosec // G115: line numbers fit uint32
			return action, true
		}
	case input.CursorDown, input.CursorUp, input.ScrollLeft, input.ScrollRight,
		input.ScrollViewDown, input.ScrollViewUp:
		action.N *= count
	case input.NextFile, input.PrevFile, input.NextHunk, input.PrevHunk:
		for range count - 1 {
			m.dispatch(action)
		}
	}
	return action, false
}

// dispatchModal routes the modes whose handlers may spawn async work: the
// submit modals and the target selector. handled is false for every other
// mode.
func (m *Model) dispatchModal(k tea.Key, action input.Action) (cmd tea.Cmd, quit, handled bool) {
	switch m.App.InputMode {
	case input.ModeSubmitActionPicker:
		quit, cmd = m.dispatchSubmitPicker(action)
		return cmd, quit, true
	case input.ModeSubmitResolver:
		return m.dispatchSubmitResolver(action), false, true
	case input.ModeSubmitConfirm:
		return m.dispatchSubmitConfirm(action), false, true
	case input.ModeCommitSelect:
		// The target selector reaches the forge: listing, paging and
		// opening a PR all spawn async work.
		quit, cmd = m.dispatchSelector(k, action)
		return cmd, quit, true
	}
	return nil, false, false
}

// queue records async work for handleKey to return once dispatch unwinds.
func (m *Model) queue(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if m.queuedCmd == nil {
		m.queuedCmd = cmd
		return
	}
	m.queuedCmd = tea.Batch(m.queuedCmd, cmd)
}

// takeQueued drains the queued async work.
func (m *Model) takeQueued() tea.Cmd {
	cmd := m.queuedCmd
	m.queuedCmd = nil
	return cmd
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
		a.FocusPanel(app.PanelFileList)
	case 'l':
		a.FocusPanel(app.PanelDiff)
	case 'j':
		a.MovePanelFocus(1)
	case 'k':
		a.MovePanelFocus(-1)
	case 's':
		a.ToggleCommitSelector()
	case 'c':
		a.EnterReviewCommentMode()
		m.enterComposeMode()
	case 'f':
		a.ToggleSingleFileView()
	case 't':
		m.openTargetSelector(app.TargetTabLocal)
	case 'p':
		m.openTargetSelector(app.TargetTabPullRequests)
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
	case input.ModeCommentPeek:
		return m.dispatchCommentPeek(action)
	case input.ModeVisualSelect:
		return m.dispatchVisual(action)
	case input.ModeNormal:
		// Focused-pane overlays get first refusal; anything they do not
		// claim falls through to the normal-mode bindings.
		switch a.FocusedPanel {
		case app.PanelFileList:
			if handled := m.dispatchFileList(action); handled {
				return false
			}
		case app.PanelComments:
			if handled := m.dispatchCommentNav(action); handled {
				return false
			}
		case app.PanelCommitSelector:
			if handled := m.dispatchCommitStrip(action); handled {
				return false
			}
		case app.PanelDiff:
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

// runCommand executes a parsed `:` command; true means quit.
func (m *Model) runCommand(cmd input.Command) bool {
	handler, ok := commandTable[cmd.Kind]
	if !ok {
		m.App.SetError("Unknown command: " + cmd.Raw)
		return false
	}
	return handler(m, cmd)
}

// dispatchCommitSelect handles the full-screen target selector.
// dispatchSelector routes a key in the review target selector. It returns
// true to quit, plus any async forge work the action started (listing,
// paging, or opening a pull request).
func (m *Model) dispatchSelector(k tea.Key, action input.Action) (bool, tea.Cmd) {
	a := m.App

	// The "/" filter prompt is a sub-state of whichever tab owns one, not a
	// top-level input mode, so it needs its own key mapping.
	switch {
	case a.PrTabFilterEditing():
		m.dispatchPrTabFilter(input.MapTargetFilter(k))
		return false, nil
	case a.PatchTabFilterEditing():
		m.dispatchPatchTabFilter(input.MapTargetFilter(k))
		return false, nil
	}

	switch a.TargetTab {
	case app.TargetTabPullRequests:
		return m.dispatchPrTab(action)
	case app.TargetTabPatches:
		quit, cmd := m.dispatchPatchTab(action)
		if cmd == nil {
			cmd = m.drainPatchTabLoad()
		}
		if !quit {
			if tabCmd := m.selectorTabSwitch(action); tabCmd != nil {
				cmd = tea.Batch(cmd, tabCmd)
			}
		}
		return quit, cmd
	}
	return m.dispatchLocalTab(action), tea.Batch(m.drainPrTabLoad(), m.drainPatchTabLoad())
}

// selectorTabSwitch handles the keys that move between tabs, which every tab
// shares. Esc steps back to Local explicitly rather than cycling: with three
// tabs "the previous one" is no longer the same thing as "back to the start".
func (m *Model) selectorTabSwitch(action input.Action) tea.Cmd {
	a := m.App
	switch action.Kind {
	case input.ExitMode:
		a.SetTargetTab(app.TargetTabLocal)
	case input.TargetSelectorTabNext:
		a.CycleTargetTab(true)
	case input.TargetSelectorTabPrev:
		a.CycleTargetTab(false)
	default:
		return nil
	}
	return tea.Batch(m.drainPrTabLoad(), m.drainPatchTabLoad())
}

// dispatchLocalTab handles the selector's Local (commits) tab.
func (m *Model) dispatchLocalTab(action input.Action) bool {
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

// dispatchPrTab handles the selector's Pull Requests tab.
func (m *Model) dispatchPrTab(action input.Action) (bool, tea.Cmd) {
	a := m.App
	switch action.Kind {
	case input.Quit:
		return true, nil
	case input.CommitSelectDown:
		a.PrTabDown()
	case input.CommitSelectUp:
		a.PrTabUp()
	case input.ConfirmCommitSelect, input.ToggleCommitSelect:
		if a.IsOnPrLoadMoreRow() {
			a.LoadMorePrs()
			break
		}
		if cmd := m.openSelectedPr(); cmd != nil {
			return false, cmd
		}
	case input.BeginTargetFilter:
		a.BeginPrTabFilter()
	case input.TogglePrReviewRequestedFilter:
		a.TogglePrTabScope()
	case input.ExitMode:
		// Esc steps back to Local rather than closing the selector outright:
		// the tabs are peers, and leaving from here would strand a user who
		// only wanted to switch back.
		a.SetTargetTab(app.TargetTabLocal)
	case input.TargetSelectorTabNext:
		a.CycleTargetTab(true)
	case input.TargetSelectorTabPrev:
		a.CycleTargetTab(false)
	}
	return false, tea.Batch(m.drainPrTabLoad(), m.drainPatchTabLoad())
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
	case input.NextHunk:
		a.ExtendVisualToNextHunk()
	case input.PrevHunk:
		a.ExtendVisualToPrevHunk()
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
		if _, copyErr := copyText(text); copyErr != nil {
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

// dispatchCommentNav handles keys while the comment navigator has focus.
// dispatchCommentPeek handles the read-only comment panel: scroll or dismiss.
func (m *Model) dispatchCommentPeek(action input.Action) bool {
	a := m.App
	switch action.Kind {
	case input.ExitMode:
		a.CloseCommentPeek()
	case input.CursorDown:
		a.PeekScroll(max(action.N, 1))
	case input.CursorUp:
		a.PeekScroll(-max(action.N, 1))
	case input.HalfPageDown:
		a.PeekScroll(max(a.CommentPeek.ViewportHeight/2, 1))
	case input.HalfPageUp:
		a.PeekScroll(-max(a.CommentPeek.ViewportHeight/2, 1))
	}
	return false
}

func (m *Model) dispatchCommentNav(action input.Action) bool {
	a := m.App
	switch action.Kind {
	case input.CursorDown:
		a.CommentNavDown()
	case input.CursorUp:
		a.CommentNavUp()
	case input.SelectFile, input.SelectFileFull, input.ToggleExpand:
		a.CommentNavSelect()
	default:
		return false
	}
	return true
}

// dispatchCommitStrip handles keys while the inline commit selector has
// focus. Toggling a commit renarrows the review, so the diff reloads.
func (m *Model) dispatchCommitStrip(action input.Action) bool {
	a := m.App
	switch action.Kind {
	case input.CursorDown:
		a.CommitSelectDown()
	case input.CursorUp:
		a.CommitSelectUp()
	case input.ToggleExpand, input.SelectFile, input.SelectFileFull:
		a.ToggleCommitSelectionAndAdvance()
		m.queue(m.reloadInlineSelection())
	case input.ExitMode:
		a.FocusPanel(app.PanelDiff)
	default:
		return false
	}
	return true
}

// dispatchNormal runs a normal-mode action; true means quit. Esc with
// nothing typed and nothing loaded reopens the selector the review came
// from rather than stranding the user on an empty pane.
func (m *Model) dispatchNormal(action input.Action) bool {
	if action.Kind == input.ExitMode && m.App.PendingCount == nil && len(m.App.DiffFiles) == 0 {
		m.openTargetSelector(m.App.SelectorTabForReview())
		return false
	}
	if handler, ok := normalActions[action.Kind]; ok {
		return handler(m, action)
	}
	return false
}

func (m *Model) cycleFocus(dir int) {
	m.App.CyclePanelFocus(dir)
}

// View composes the frame.
func (m *Model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return tea.NewView("")
	}
	a := m.App
	innerH := max(m.height-4, 1)

	if a.InputMode == input.ModeHelp {
		return m.helpView()
	}
	if a.InputMode == input.ModeCommitSelect {
		view := tea.NewView(strings.Join(m.selectorView(), "\n"))
		view.AltScreen = true
		view.KeyboardEnhancements = tea.KeyboardEnhancements{ReportEventTypes: true}
		return view
	}

	// The inline commit selector is a full-width strip above the columns,
	// so it eats into the height everything below gets.
	stripH := commitStripHeight(a)
	if stripH > innerH-4 {
		stripH = 0 // too short to be worth the space
	}
	colH := innerH - stripH

	// Screen geometry, recorded for mouse hit-testing as the frame is
	// composed. Row 0 is the header; the strip (when shown) occupies
	// rows 1..stripH; the columns start below it. Each titledPanel spends
	// its first row on the title border, so bodies start one row in.
	m.layout.reset()
	colTop := 1 + stripH
	if stripH > 0 {
		m.layout.add(paneRect{
			Panel: app.PanelCommitSelector, X: 1, Y: 2,
			W: m.width - 2, H: stripH - 2, Offset: a.CommitListScrollOffset,
		})
	}

	var mainCols []string
	if a.ShowFileList {
		flWidth := m.fileListWidth()
		m.recordLeftColumnRects(flWidth, colTop, colH)
		mainCols = append(mainCols, m.leftColumn(flWidth-2, colH))
	}

	diffW := m.diffInnerWidth()
	diffX := 1
	if a.ShowFileList {
		diffX += m.fileListWidth()
	}
	m.layout.add(paneRect{
		Panel: app.PanelDiff, X: diffX, Y: colTop + 1,
		W: diffW, H: colH, Offset: a.DiffState.ScrollOffset,
	})
	diffInner := m.diffPane.BuildLines(a, diffW, colH)
	if a.InputMode == input.ModeComment {
		overlay := m.diffPane.commentInputOverlay(a, m.vim, diffW)
		if len(overlay) > colH {
			overlay = overlay[len(overlay)-colH:]
		}
		copy(diffInner[colH-len(overlay):], overlay)
	}
	if modal := m.submitModalView(diffW); len(modal) > 0 {
		if len(modal) > colH {
			modal = modal[:colH]
		}
		copy(diffInner[colH-len(modal):], modal)
	}
	if peek := m.diffPane.commentPeekOverlay(a, diffW, colH); len(peek) > 0 {
		copy(diffInner[colH-len(peek):], peek)
	}
	mainCols = append(mainCols, m.titledPanel(
		strings.Join(diffInner, "\n"), m.diffTitle(),
		a.FocusedPanel == app.PanelDiff, diffW, colH))

	body := lipgloss.JoinHorizontal(lipgloss.Top, mainCols...)
	if stripH > 0 {
		stripW := m.width - 2
		a.CommitListViewportHeight = stripH - 2
		strip := m.commitStrip.BuildLines(a, stripW, stripH-2)
		body = lipgloss.JoinVertical(lipgloss.Left,
			m.titledPanel(strings.Join(strip, "\n"), m.commitStrip.Title(a),
				a.FocusedPanel == app.PanelCommitSelector, stripW, stripH-2),
			body)
	}

	frame := lipgloss.JoinVertical(lipgloss.Left,
		HeaderWithGrant(a, m.Theme, m.width, m.grantedEvents),
		body,
		StatusBar(a, m.Theme, m.width),
	)
	view := tea.NewView(frame)
	view.AltScreen = true
	view.KeyboardEnhancements = tea.KeyboardEnhancements{ReportEventTypes: true}
	view.MouseMode = m.mouseMode()
	return view
}

// mouseMode requests cell-motion tracking only when the config wants mouse
// support. Leaving it off keeps the terminal's native selection, which is
// what a user who disabled mouse is asking for.
func (m *Model) mouseMode() tea.MouseMode {
	if m.mouseEnabled {
		return tea.MouseModeCellMotion
	}
	return tea.MouseModeNone
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
// leftColumn renders the file tree, optionally splitting the bottom of the
// column off for the comment navigator. The navigator is dropped entirely
// when the column is too short to give both panes a usable body, since a
// two-row tree is worse than no navigator.
func (m *Model) leftColumn(innerW, innerH int) string {
	a := m.App
	navH := commentNavHeight(a, innerH)
	if navH == 0 {
		a.FileListState.ViewportHeight = innerH
		tree := m.fileList.BuildLines(a, innerW, innerH)
		return m.titledPanel(strings.Join(tree, "\n"), m.fileList.Title(a),
			a.FocusedPanel == app.PanelFileList, innerW, innerH)
	}

	// Each titledPanel costs two rows of border, and the unsplit column
	// occupies innerH+2 screen rows; the split must total the same or the
	// left column ends short of the diff beside it.
	treeBody := innerH - navH
	a.FileListState.ViewportHeight = treeBody
	a.CommentNav.ViewportHeight = navH - 2

	tree := m.fileList.BuildLines(a, innerW, treeBody)
	nav := m.commentNav.BuildLines(a, innerW, navH-2)
	return lipgloss.JoinVertical(lipgloss.Left,
		m.titledPanel(strings.Join(tree, "\n"), m.fileList.Title(a),
			a.FocusedPanel == app.PanelFileList, innerW, treeBody),
		m.titledPanel(strings.Join(nav, "\n"), m.commentNav.Title(a),
			a.FocusedPanel == app.PanelComments, innerW, navH-2),
	)
}

// commentNavHeight is the navigator's height inside the left column,
// borders included, or 0 when it is not shown.
func commentNavHeight(a *app.App, columnH int) int {
	items := len(a.BuildCommentNavigatorItems())
	if items == 0 || columnH < app.CommentNavMinTotalHeight {
		return 0
	}
	navH := min(items+2, app.CommentNavMaxHeight)
	navH = max(navH, app.CommentNavMinHeight)
	// The tree keeps its floor; if honoring it leaves the navigator below
	// its own minimum, the split is not worth making.
	if columnH-navH < app.FileTreeMinHeight {
		navH = columnH - app.FileTreeMinHeight
	}
	if navH < app.CommentNavMinHeight {
		return 0
	}
	return navH
}

// titledPanel draws a bordered pane with a title in its top rule, occupying
// innerH+2 screen rows: the title, innerH rows of content, and the bottom
// border.
//
// lipgloss Height() counts the border, so the body is rendered at innerH+1
// to leave innerH rows for content. Without the +1 every panel came out a
// row short, the frame never filled the terminal, and whatever the previous
// view had drawn on the last rows stayed on screen.
func (m *Model) titledPanel(content, title string, focused bool, innerW, innerH int) string {
	borderStyle := m.Styles.Border(focused)
	body := borderStyle.BorderTop(false).
		Width(innerW).Height(innerH + 1).
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
	start := min(m.App.HelpState.ScrollOffset, len(content))
	end := min(start+m.App.HelpState.ViewportHeight, len(content))
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

// dispatchChord runs a completed two-key chord, reporting whether to quit
// and whether the chord was recognized at all.
//
// keychord hands over the whole chord rather than deciding what an unknown
// one means, because only the caller knows its own bindings. mrman lets an
// unrecognized chord fall through to its second key, matching tuicr: after
// a stray "z", ":" should still open the command line.
func (m *Model) dispatchChord(ev keychord.Event) (quit, handled bool) {
	a := m.App
	switch ev.Prefix {
	case "z":
		switch ev.Key {
		case "z":
			a.CenterCursor()
		case "t":
			a.CursorToTop()
		case "b":
			a.CursorToBottom()
		default:
			return false, false
		}
	case "Z":
		switch ev.Key {
		case "Z":
			// ZZ saves before quitting; ZQ quits without saving.
			if err := m.saveSession(); err != nil {
				a.SetError("Save failed: " + err.Error())
				return false, true
			}
			return true, true
		case "Q":
			return true, true
		default:
			return false, false
		}
	case "d":
		if ev.Key != "d" {
			return false, false
		}
		if a.DeleteCommentAtCursor() {
			m.autosave()
		}
	case string(m.leader):
		m.handleLeader([]rune(ev.Key)[0])
	default:
		return false, false
	}
	return false, true
}
