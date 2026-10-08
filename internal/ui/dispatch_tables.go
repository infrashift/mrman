package ui

import (
	"fmt"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/version"
)

// commandHandler runs one `:` command; true means quit.
type commandHandler func(m *Model, cmd input.Command) bool

// actionHandler runs one normal-mode action; true means quit.
type actionHandler func(m *Model, action input.Action) bool

// commandTable maps every `:` command to what it does. A table rather than
// a switch so that adding a command is one entry, and so the list of
// commands the model understands can be read in one place.
var commandTable = map[input.CommandKind]commandHandler{
	input.CmdQuit: func(m *Model, _ input.Command) bool {
		// tuicr's dirty guard: unsaved comments block :q; reviewed-only
		// dirt just quits (the session was saved by toggles or discards).
		if m.App.Dirty && m.App.Session.HasComments() {
			m.App.SetError("No write since last change (add ! to override)")
			return false
		}
		return true
	},
	input.CmdForceQuit: func(*Model, input.Command) bool { return true },
	input.CmdWrite: func(m *Model, _ input.Command) bool {
		if err := m.saveSession(); err != nil {
			m.App.SetError("Save failed: " + err.Error())
		} else {
			m.App.SetMessage("Session saved")
		}
		return false
	},
	input.CmdWriteQuit: func(m *Model, _ input.Command) bool {
		if err := m.saveSession(); err != nil {
			m.App.SetError("Save failed: " + err.Error())
			return false
		}
		return true
	},
	input.CmdToggleWrap:  appCmd(func(a *app.App) { a.ToggleDiffWrap() }),
	input.CmdSetWrap:     appCmd(func(a *app.App) { a.SetDiffWrap(true) }),
	input.CmdSetNoWrap:   appCmd(func(a *app.App) { a.SetDiffWrap(false) }),
	input.CmdDiff:        appCmd(func(a *app.App) { a.ToggleDiffViewMode() }),
	input.CmdHelp:        appCmd(func(a *app.App) { a.ToggleHelp() }),
	input.CmdFocus:       appCmd(func(a *app.App) { a.ToggleSingleFileView() }),
	input.CmdGotoLine:    func(m *Model, cmd input.Command) bool { m.App.GoToSourceLine(cmd.N, "new"); return false },
	input.CmdGotoLineOld: func(m *Model, cmd input.Command) bool { m.App.GoToSourceLine(cmd.N, "old"); return false },
	input.CmdExport: func(m *Model, _ input.Command) bool {
		if out := m.exportToClipboard(); out != "" {
			m.PendingStdout = out
		}
		return false
	},
	input.CmdExportPatch: func(m *Model, _ input.Command) bool {
		if out := m.patchReplyToClipboard(); out != "" {
			m.PendingStdout = out
		}
		return false
	},
	input.CmdClear: appCmd(func(a *app.App) {
		cleared, unreviewed := a.Session.ClearComments(model.ClearCommentsAndReviewed)
		a.Dirty = true
		a.RebuildAnnotations()
		a.SetMessage(fmt.Sprintf("Cleared %d comment(s), unreviewed %d file(s)", cleared, unreviewed))
	}),
	input.CmdClearCommentsOnly: appCmd(func(a *app.App) {
		cleared, _ := a.Session.ClearComments(model.ClearCommentsOnly)
		a.Dirty = true
		a.RebuildAnnotations()
		a.SetMessage(fmt.Sprintf("Cleared %d comment(s)", cleared))
	}),
	input.CmdStage: appCmd(func(a *app.App) {
		if !a.CanStage() {
			a.SetMessage("Staging is only available for unstaged reviews in git")
			return
		}
		a.SetMessage(fmt.Sprintf("Staged %d reviewed file(s)", a.StageReviewedFiles()))
	}),
	input.CmdSubmitPicker:         appCmd(func(a *app.App) { a.StartSubmitPicker() }),
	input.CmdSubmitComment:        appCmd(func(a *app.App) { a.StartSubmitWith(forge.SubmitComment, false) }),
	input.CmdSubmitApprove:        appCmd(func(a *app.App) { a.StartSubmitWith(forge.SubmitApprove, false) }),
	input.CmdSubmitRequestChanges: appCmd(func(a *app.App) { a.StartSubmitWith(forge.SubmitRequestChanges, false) }),
	input.CmdSubmitDraft:          appCmd(func(a *app.App) { a.StartSubmitWith(forge.SubmitDraft, false) }),
	input.CmdToggleVim: func(m *Model, _ input.Command) bool {
		m.CommentVimMode = !m.CommentVimMode
		m.App.SetMessage(fmt.Sprintf("Comment vim mode: %v (next comment)", m.CommentVimMode))
		return false
	},
	input.CmdSetVim: func(m *Model, _ input.Command) bool {
		m.CommentVimMode = true
		m.App.SetMessage("Comment vim mode: on (next comment)")
		return false
	},
	input.CmdSetNoVim: func(m *Model, _ input.Command) bool {
		m.CommentVimMode = false
		m.App.SetMessage("Comment vim mode: off")
		return false
	},
	input.CmdTargetsLocal:       modelCmd(func(m *Model) { m.openTargetSelector(app.TargetTabLocal) }),
	input.CmdTargetsPatches:     modelCmd(func(m *Model) { m.openTargetSelector(app.TargetTabPatches) }),
	input.CmdTargetsPrs:         modelCmd(func(m *Model) { m.openTargetSelector(app.TargetTabPullRequests) }),
	input.CmdCommentsUnresolved: modelCmd(func(m *Model) { m.setRemoteCommentsVisibility(forgetypes.VisibilityUnresolved) }),
	input.CmdCommentsAll:        modelCmd(func(m *Model) { m.setRemoteCommentsVisibility(forgetypes.VisibilityAll) }),
	input.CmdCommentsHide:       modelCmd(func(m *Model) { m.setRemoteCommentsVisibility(forgetypes.VisibilityHide) }),
	input.CmdReload:             modelCmd(func(m *Model) { m.queue(m.reloadDiff()) }),
	input.CmdSetCommitsVisible:  modelCmd(func(m *Model) { m.setCommitSelectorVisible(true) }),
	input.CmdSetCommitsHidden:   modelCmd(func(m *Model) { m.setCommitSelectorVisible(false) }),
	input.CmdToggleCommits:      appCmd(func(a *app.App) { a.ToggleCommitSelector() }),
	input.CmdSetMouse:           modelCmd(func(m *Model) { m.setMouseEnabled(true) }),
	input.CmdSetNoMouse:         modelCmd(func(m *Model) { m.setMouseEnabled(false) }),
	input.CmdToggleMouse:        modelCmd(func(m *Model) { m.setMouseEnabled(!m.mouseEnabled) }),
	input.CmdEdit:               modelCmd(func(m *Model) { m.queue(m.openInEditor()) }),
	input.CmdVersion:            appCmd(func(a *app.App) { a.SetMessage("mrman " + version.String()) }),
	input.CmdAgentStatus:        modelCmd(func(m *Model) { m.reportAgentGrant() }),
	input.CmdAgentOff:           modelCmd(func(m *Model) { m.revokeAgentGrant() }),
}

// appCmd adapts a command that only touches the app state.
func appCmd(f func(a *app.App)) commandHandler {
	return func(m *Model, _ input.Command) bool { f(m.App); return false }
}

// modelCmd adapts a command that touches the model but takes no argument.
func modelCmd(f func(m *Model)) commandHandler {
	return func(m *Model, _ input.Command) bool { f(m); return false }
}

// normalActions maps every normal-mode action to what it does.
var normalActions = map[input.Kind]actionHandler{
	input.Quit: func(*Model, input.Action) bool { return true },
	input.ExitMode: appAct(func(a *app.App, _ input.Action) {
		// Esc discards a half-typed count, as in vim. With nothing typed
		// and nothing loaded, the model reopens the target selector before
		// this runs (see the ExitMode check in model.go), so Esc out of the
		// startup selector never strands the user on an empty pane.
		if a.PendingCount != nil {
			a.PendingCount = nil
		}
	}),
	input.CursorDown:     appAct(func(a *app.App, ac input.Action) { a.CursorDown(ac.N) }),
	input.CursorUp:       appAct(func(a *app.App, ac input.Action) { a.CursorUp(ac.N) }),
	input.HalfPageDown:   appAct(func(a *app.App, _ input.Action) { a.ScrollDown(a.DiffState.EffectiveVisibleLines() / 2) }),
	input.HalfPageUp:     appAct(func(a *app.App, _ input.Action) { a.ScrollUp(a.DiffState.EffectiveVisibleLines() / 2) }),
	input.PageDown:       appAct(func(a *app.App, _ input.Action) { a.ScrollDown(a.DiffState.EffectiveVisibleLines()) }),
	input.PageUp:         appAct(func(a *app.App, _ input.Action) { a.ScrollUp(a.DiffState.EffectiveVisibleLines()) }),
	input.ScrollViewDown: appAct(func(a *app.App, ac input.Action) { a.ScrollViewDown(ac.N) }),
	input.ScrollViewUp:   appAct(func(a *app.App, ac input.Action) { a.ScrollViewUp(ac.N) }),
	input.ScrollLeft:     appAct(func(a *app.App, ac input.Action) { a.ScrollLeft(ac.N) }),
	input.ScrollRight:    appAct(func(a *app.App, ac input.Action) { a.ScrollRight(ac.N) }),
	input.GoToTop: appAct(func(a *app.App, _ input.Action) {
		a.MoveCursorToAnnotation(0)
		a.DiffState.ScrollOffset = 0
	}),
	input.GoToBottom:         appAct(func(a *app.App, _ input.Action) { a.JumpToBottom() }),
	input.NextFile:           appAct(func(a *app.App, _ input.Action) { a.NextFile() }),
	input.PrevFile:           appAct(func(a *app.App, _ input.Action) { a.PrevFile() }),
	input.NextHunk:           appAct(func(a *app.App, _ input.Action) { a.NextHunk() }),
	input.PrevHunk:           appAct(func(a *app.App, _ input.Action) { a.PrevHunk() }),
	input.ToggleFocus:        modelAct(func(m *Model) { m.cycleFocus(1) }),
	input.ToggleFocusReverse: modelAct(func(m *Model) { m.cycleFocus(-1) }),
	// tuicr expands context gaps with Enter; mrman shipped Space. Both
	// work: Enter for muscle memory, Space because it is already
	// documented. They never collide — Enter in the file tree is dispatched
	// by that pane's overlay before reaching here.
	input.ToggleExpand:     modelAct(func(m *Model) { m.queue(m.expandGapAtCursor()) }),
	input.SelectFile:       modelAct(func(m *Model) { m.queue(m.expandGapAtCursor()) }),
	input.SelectFileFull:   modelAct(func(m *Model) { m.queue(m.expandGapAtCursor()) }),
	input.CycleCommitNext:  modelAct(func(m *Model) { m.queue(m.cycleCommit(true)) }),
	input.CycleCommitPrev:  modelAct(func(m *Model) { m.queue(m.cycleCommit(false)) }),
	input.EnterCommandMode: appAct(func(a *app.App, _ input.Action) { a.EnterCommandMode() }),
	input.EnterSearchMode:  appAct(func(a *app.App, _ input.Action) { a.EnterSearchMode() }),
	input.ToggleHelp:       appAct(func(a *app.App, _ input.Action) { a.ToggleHelp() }),
	input.SearchNext: appAct(func(a *app.App, _ input.Action) {
		if !a.SearchNextInDiff() {
			a.SetWarning("Pattern not found")
		}
	}),
	input.SearchPrev: appAct(func(a *app.App, _ input.Action) {
		if !a.SearchPrevInDiff() {
			a.SetWarning("Pattern not found")
		}
	}),
	input.ExportToClipboard: modelAct(func(m *Model) {
		// A live mouse drag makes y mean "copy what I highlighted", the
		// same as in visual mode; with nothing highlighted it exports the
		// whole review.
		if m.yankMouseSelection() {
			return
		}
		if out := m.exportToClipboard(); out != "" {
			m.PendingStdout = out
		}
	}),
	input.ToggleReviewed: modelAct(func(m *Model) {
		m.App.ToggleReviewed()
		m.autosave()
	}),
	input.ToggleHunkReviewed: modelAct(func(m *Model) {
		m.App.ToggleHunkReviewed()
		m.autosave()
	}),
	input.AddLineComment: modelAct(func(m *Model) {
		m.App.EnterCommentMode(false)
		m.enterComposeMode()
	}),
	input.AddFileComment: modelAct(func(m *Model) {
		m.App.EnterCommentMode(true)
		m.enterComposeMode()
	}),
	input.EditComment: modelAct(func(m *Model) {
		m.App.EnterEditMode(false)
		m.enterComposeMode()
	}),
	input.EditCommentAtEnd: modelAct(func(m *Model) {
		m.App.EnterEditMode(true)
		m.enterComposeMode()
	}),
	input.EnterVisualMode: appAct(func(a *app.App, _ input.Action) { a.EnterVisualModeAtCursor() }),
	input.NextComment:     appAct(func(a *app.App, _ input.Action) { a.NextComment() }),
	input.PrevComment:     appAct(func(a *app.App, _ input.Action) { a.PrevComment() }),
}

// appAct adapts an action that only touches the app state.
func appAct(f func(a *app.App, action input.Action)) actionHandler {
	return func(m *Model, action input.Action) bool { f(m.App, action); return false }
}

// modelAct adapts an action that touches the model and ignores its count.
func modelAct(f func(m *Model)) actionHandler {
	return func(m *Model, _ input.Action) bool { f(m); return false }
}
