package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/infrashift/mrman/charmkit/vimtext"
	"github.com/infrashift/mrman/internal/app"
)

// vimState wraps the vimtext editor with the app-level chrome tuicr layers
// on top: the `:` command line, double-press Enter/Esc arm-confirm, and
// save/cancel routing.
type vimState struct {
	editor  *vimtext.Editor
	cmdline *string // non-nil while a `:` command line is open
	pending vimPending
}

type vimPending int

const (
	vimPendingNone vimPending = iota
	vimPendingSave
	vimPendingCancel
)

func newVimState(a *app.App, tabWidth int) *vimState {
	editor := vimtext.New(a.CommentBuffer, a.CommentCursor)
	if tabWidth > 0 {
		editor.TabWidth = tabWidth
	}
	return &vimState{editor: editor}
}

// label is the [..] tag shown in the input box header.
// cyclesTypeOnTab reports whether Tab currently cycles the comment type
// rather than inserting spaces. Only Normal mode reaches the app-level
// binding: in Insert mode the editor consumes Tab to insert TabWidth spaces,
// and while a `:` command line is open the keystroke belongs to it.
func (v *vimState) cyclesTypeOnTab() bool {
	return v.cmdline == nil && v.editor.Mode() == vimtext.ModeNormal
}

func (v *vimState) label() string {
	switch {
	case v.cmdline != nil:
		return ":" + *v.cmdline
	case v.pending == vimPendingSave:
		return "Enter again to save"
	case v.pending == vimPendingCancel:
		return "Esc/q again to cancel"
	}
	return v.editor.ModeLabel()
}

// vimOutcome is what a key press resolved to.
type vimOutcome int

const (
	vimContinue vimOutcome = iota
	vimSave
	vimCancel
	vimCycleType
	vimCycleTypeReverse
)

// handleKey feeds a key through the wrapper layers, syncing the app buffer.
func (v *vimState) handleKey(a *app.App, k tea.Key) vimOutcome {
	defer func() {
		a.CommentBuffer = v.editor.Text()
		a.CommentCursor = v.editor.Cursor()
	}()

	// Open command line captures everything.
	if v.cmdline != nil {
		switch k.Code {
		case tea.KeyEnter:
			cmd := *v.cmdline
			v.cmdline = nil
			switch cmd {
			case "w", "wq", "x":
				return vimSave
			case "q", "q!":
				return vimCancel
			case "":
				return vimContinue
			}
			return vimContinue
		case tea.KeyEscape:
			v.cmdline = nil
			return vimContinue
		case tea.KeyBackspace:
			if *v.cmdline == "" {
				v.cmdline = nil
			} else {
				trimmed := dropLastRune(*v.cmdline)
				v.cmdline = &trimmed
			}
			return vimContinue
		}
		if len(k.Text) > 0 && k.Mod&^tea.ModShift == 0 {
			appended := *v.cmdline + k.Text
			v.cmdline = &appended
			return vimContinue
		}
		return vimContinue
	}

	// Alt-Enter saves, Alt-Esc cancels in any mode (survives all terminals).
	if k.Mod == tea.ModAlt {
		switch k.Code {
		case tea.KeyEnter:
			return vimSave
		case tea.KeyEscape:
			return vimCancel
		}
	}
	// Ctrl-S / Ctrl-Enter save from any mode.
	if (k.Mod == tea.ModCtrl && k.Code == 's') ||
		(k.Mod&tea.ModCtrl != 0 && k.Code == tea.KeyEnter) {
		return vimSave
	}

	consumed := v.editor.HandleKey(k)
	if consumed {
		v.pending = vimPendingNone
		return vimContinue
	}

	// Unconsumed keys are the app-level chrome.
	if v.editor.Mode() == vimtext.ModeNormal && k.Mod&^tea.ModShift == 0 {
		switch {
		case k.Code == tea.KeyEnter:
			if v.pending == vimPendingSave {
				return vimSave
			}
			v.pending = vimPendingSave
			return vimContinue
		case k.Code == tea.KeyEscape || k.Text == "q":
			if v.pending == vimPendingCancel {
				return vimCancel
			}
			v.pending = vimPendingCancel
			return vimContinue
		case k.Code == tea.KeyTab && k.Mod&tea.ModShift != 0:
			return vimCycleTypeReverse
		case k.Code == tea.KeyTab:
			return vimCycleType
		case k.Text == ":":
			empty := ""
			v.cmdline = &empty
			return vimContinue
		}
	}
	v.pending = vimPendingNone
	return vimContinue
}
