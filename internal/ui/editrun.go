package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/infrashift/mrman/internal/editor"
)

// editorFinishedMsg reports that $EDITOR exited and the terminal is ours
// again.
type editorFinishedMsg struct {
	Err error
}

// openInEditor implements `:edit`: hand the terminal to $EDITOR on the
// focused file, then take it back.
//
// The app layer resolves *which* file and line (QueueEditorForFocusedItem);
// it cannot run the editor itself because that means suspending the TUI,
// which only the bubbletea program can do.
func (m *Model) openInEditor() tea.Cmd {
	a := m.App
	a.QueueEditorForFocusedItem()
	target, ok := a.TakePendingEditorTarget()
	if !ok {
		return nil // the app already explained why
	}

	// Save first: an editor session can be long, and a crash in between
	// should not cost the review.
	if err := m.saveSession(); err != nil {
		a.SetError("Save failed: " + err.Error())
		return nil
	}

	cmd := editor.FromEnv(target).ExecCommand()
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorFinishedMsg{Err: err}
	})
}

// handleEditorFinished reloads after an editor session when the review
// target can actually have changed underneath it.
//
// Reviewing a commit range or a pull request reads immutable history, so
// there is nothing to pick up and a reload would only throw away the
// reviewer's position for no reason.
func (m *Model) handleEditorFinished(msg editorFinishedMsg) {
	a := m.App
	if msg.Err != nil {
		a.SetError("Editor failed: " + msg.Err.Error())
		return
	}
	if !a.DiffSource.IncludesWorktreeChanges() {
		a.SetMessage("Editor closed")
		return
	}
	m.reloadLocalDiff()
}
