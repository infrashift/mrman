// Package vimtext implements a minimal vim-style modal editing engine for the
// comment editor: Normal/Insert/Visual/Visual-Line modes over a plain string
// buffer with a byte-offset cursor. The buffer stays canonical in the app
// layer; this package only interprets keys and mutates text/cursor.
//
// Scope is the tuicr comment-vim parity subset: hjkl/arrow/word/line motions
// with counts, i a I A o O insert entry, x s dd D C cc d/c/y{motion} ciw diw
// edits, yy p P with a single linewise-aware unnamed register, visual and
// visual-line y/d/c/p, and u / Ctrl-r snapshot undo. Keys outside the subset
// are consumed as no-ops; a small set of app-level keys (Enter/Esc/Tab/':'
// in Normal, Ctrl-S/Ctrl-Enter, Alt-modified keys) is left unconsumed for
// the caller (arm-confirm save/cancel, comment-type cycling, command line).
package vimtext

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// Mode identifies the editor's modal state.
type Mode int

// Editor modes. New starts in ModeInsert (tuicr parity: typing works
// immediately; Esc drops to Normal).
const (
	ModeInsert Mode = iota
	ModeNormal
	ModeVisual
	ModeVisualLine
)

// defaultTabWidth is the soft-tab width used when TabWidth is unset.
const defaultTabWidth = 4

// snapshot is one undo/redo step: full text plus cursor.
type snapshot struct {
	text   string
	cursor int
}

// Editor is a modal text-editing engine over a string buffer. The cursor is
// a byte offset that always sits on a rune boundary. The zero value is not
// usable; construct with New.
type Editor struct {
	text   string
	cursor int
	mode   Mode

	op         rune // pending operator: 'd', 'c' or 'y' (0 = none)
	opCount    int  // count typed before the operator ("2dw")
	count      int  // count typed standalone or after the operator ("d2w")
	pendingG   bool // 'g' seen, waiting for the second 'g'
	pendingObj bool // operator + 'i' seen, waiting for the object key

	reg     string // unnamed register
	regLine bool   // register content is linewise

	undo         []snapshot
	redo         []snapshot
	insertActive bool // a snapshot already covers the current insert session

	visualAnchor int // selection anchor (byte offset) for visual modes

	// TabWidth is the number of spaces Tab inserts in Insert mode (soft
	// tab). Values < 1 fall back to the default of 4.
	TabWidth int
}

// New builds an editor seeded with text and a byte-offset cursor, starting in
// Insert mode (tuicr parity: typing works immediately; Esc drops to Normal).
// The cursor is clamped into range and onto a rune boundary.
func New(text string, cursor int) *Editor {
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(text) {
		cursor = len(text)
	}
	for cursor > 0 && cursor < len(text) && !utf8.RuneStart(text[cursor]) {
		cursor--
	}
	return &Editor{text: text, cursor: cursor, mode: ModeInsert, TabWidth: defaultTabWidth}
}

// Text returns the current buffer contents.
func (e *Editor) Text() string { return e.text }

// Cursor returns the byte-offset cursor (always on a rune boundary).
func (e *Editor) Cursor() int { return e.cursor }

// Mode returns the current editor mode.
func (e *Editor) Mode() Mode { return e.mode }

// ModeLabel returns the status label for the current mode: "NORMAL",
// "INSERT" or "VISUAL" (Visual-Line also reads "VISUAL", tuicr parity).
func (e *Editor) ModeLabel() string {
	switch e.mode {
	case ModeInsert:
		return "INSERT"
	case ModeVisual, ModeVisualLine:
		return "VISUAL"
	default:
		return "NORMAL"
	}
}

// EnterNormal switches to Normal mode from the app layer (e.g. an Esc the
// app intercepted), ending any insert session, clearing pending operator
// state, and clamping the cursor onto line content vim-style.
func (e *Editor) EnterNormal() {
	e.insertActive = false
	e.resetPending()
	e.mode = ModeNormal
	e.clampNormal()
}

// InsertText inserts s at the cursor (bracketed-paste path). In Insert mode
// it joins the current insert session's undo step; elsewhere it is one undo
// step of its own.
func (e *Editor) InsertText(s string) {
	if s == "" {
		return
	}
	if e.mode == ModeInsert {
		e.insertString(s)
		return
	}
	e.pushUndo()
	e.text = e.text[:e.cursor] + s + e.text[e.cursor:]
	e.cursor += len(s)
	e.clampNormal()
}

// HandleKey feeds one key press to the editor. It returns true when the key
// was consumed; false hands the key back to the caller (Enter/Esc in Normal
// with nothing pending, Tab/Shift-Tab and ':' in Normal, Ctrl-S/Ctrl-Enter,
// and any Alt-modified key).
func (e *Editor) HandleKey(k tea.Key) bool {
	if k.Mod&tea.ModAlt != 0 {
		return false
	}
	if k.Mod == tea.ModCtrl && (k.Code == 's' || k.Code == tea.KeyEnter) {
		return false
	}
	if e.mode == ModeInsert {
		e.insertKey(k)
		return true
	}
	return e.normalKey(k)
}

// insertKey handles one key press in Insert mode (always consumed).
func (e *Editor) insertKey(k tea.Key) {
	switch k.Code {
	case tea.KeyEscape:
		e.EnterNormal()
		return
	case tea.KeyEnter:
		e.insertString("\n")
		return
	case tea.KeyTab:
		e.insertString(strings.Repeat(" ", e.tabWidth()))
		return
	case tea.KeyBackspace:
		e.backspace()
		return
	case tea.KeyLeft:
		if e.cursor > e.lineStart(e.cursor) {
			e.cursor = prevRuneStart(e.text, e.cursor)
		}
		return
	case tea.KeyRight:
		if e.cursor < e.lineEnd(e.cursor) {
			e.cursor += runeLenAt(e.text, e.cursor)
		}
		return
	case tea.KeyUp:
		e.cursor = e.vertTarget(-1)
		return
	case tea.KeyDown:
		e.cursor = e.vertTarget(1)
		return
	}
	if k.Mod&^tea.ModShift != 0 {
		return // other modified keys: swallow
	}
	if k.Text != "" {
		e.insertString(k.Text)
	}
}

// tabWidth returns the effective soft-tab width.
func (e *Editor) tabWidth() int {
	if e.TabWidth > 0 {
		return e.TabWidth
	}
	return defaultTabWidth
}

// ensureInsertUndo pushes one snapshot covering the whole current insert
// session, so consecutive Insert-mode edits coalesce into a single undo step.
func (e *Editor) ensureInsertUndo() {
	if !e.insertActive {
		e.pushUndo()
		e.insertActive = true
	}
}

// insertString inserts s at the cursor as part of the insert session.
func (e *Editor) insertString(s string) {
	e.ensureInsertUndo()
	e.text = e.text[:e.cursor] + s + e.text[e.cursor:]
	e.cursor += len(s)
}

// backspace removes the rune before the cursor.
func (e *Editor) backspace() {
	if e.cursor == 0 {
		return
	}
	e.ensureInsertUndo()
	p := prevRuneStart(e.text, e.cursor)
	e.text = e.text[:p] + e.text[e.cursor:]
	e.cursor = p
}

// startInsert enters Insert mode. covered says whether the caller already
// pushed an undo snapshot for this session (change/open commands); when
// false the first Insert-mode edit pushes it.
func (e *Editor) startInsert(covered bool) {
	e.mode = ModeInsert
	e.insertActive = covered
}

// pushUndo records the current state as an undo step and clears redo.
func (e *Editor) pushUndo() {
	e.undo = append(e.undo, snapshot{e.text, e.cursor})
	e.redo = nil
}

// undoOp restores the most recent undo snapshot (Normal-mode "u").
func (e *Editor) undoOp() {
	if len(e.undo) == 0 {
		return
	}
	s := e.undo[len(e.undo)-1]
	e.undo = e.undo[:len(e.undo)-1]
	e.redo = append(e.redo, snapshot{e.text, e.cursor})
	e.text, e.cursor = s.text, s.cursor
	e.clampNormal()
}

// redoOp re-applies the most recently undone step (Ctrl-r).
func (e *Editor) redoOp() {
	if len(e.redo) == 0 {
		return
	}
	s := e.redo[len(e.redo)-1]
	e.redo = e.redo[:len(e.redo)-1]
	e.undo = append(e.undo, snapshot{e.text, e.cursor})
	e.text, e.cursor = s.text, s.cursor
	e.clampNormal()
}

// resetPending clears operator, count and multi-key pending state.
func (e *Editor) resetPending() {
	e.op, e.opCount, e.count = 0, 0, 0
	e.pendingG, e.pendingObj = false, false
}

// effCount combines the counts typed before and after an operator ("2d3w"
// deletes six words); missing counts default to one.
func (e *Editor) effCount() int {
	a, b := e.opCount, e.count
	if a <= 0 {
		a = 1
	}
	if b <= 0 {
		b = 1
	}
	return a * b
}

// printableRune returns the key's single printable rune when it carries
// exactly one character and no modifiers beyond Shift (shifted characters
// arrive with their final text, e.g. "G").
func printableRune(k tea.Key) (rune, bool) {
	if k.Mod&^tea.ModShift != 0 {
		return 0, false
	}
	rs := []rune(k.Text)
	if len(rs) != 1 {
		return 0, false
	}
	return rs[0], true
}
