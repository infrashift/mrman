package vimtext

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// pr builds a printable key press (shifted characters arrive as their final
// text, so Code == the rune works for tests).
func pr(r rune) tea.Key { return tea.Key{Text: string(r), Code: r} }

// sp builds a special (non-printable) key press.
func sp(code rune) tea.Key { return tea.Key{Code: code} }

// ctrlKey builds a Ctrl-modified key press.
func ctrlKey(code rune) tea.Key { return tea.Key{Code: code, Mod: tea.ModCtrl} }

// press feeds each rune of keys as a printable key press.
func press(e *Editor, keys string) {
	for _, r := range keys {
		e.HandleKey(pr(r))
	}
}

// normal builds an editor seeded in Normal mode.
func normal(t *testing.T, text string, cursor int) *Editor {
	t.Helper()
	e := New(text, cursor)
	e.EnterNormal()
	return e
}

func wantState(t *testing.T, e *Editor, text string, cursor int) {
	t.Helper()
	if e.Text() != text {
		t.Errorf("text = %q, want %q", e.Text(), text)
	}
	if e.Cursor() != cursor {
		t.Errorf("cursor = %d, want %d", e.Cursor(), cursor)
	}
}

func wantReg(t *testing.T, e *Editor, reg string, linewise bool) {
	t.Helper()
	if e.reg != reg {
		t.Errorf("register = %q, want %q", e.reg, reg)
	}
	if e.regLine != linewise {
		t.Errorf("register linewise = %v, want %v", e.regLine, linewise)
	}
}

func TestNewClampsCursor(t *testing.T) {
	if e := New("abc", -3); e.Cursor() != 0 {
		t.Errorf("negative cursor = %d, want 0", e.Cursor())
	}
	if e := New("abc", 99); e.Cursor() != 3 {
		t.Errorf("past-end cursor = %d, want 3", e.Cursor())
	}
	// Byte 2 is inside the 'é' (2 bytes at offset 1): snap back to 1.
	if e := New("héllo", 2); e.Cursor() != 1 {
		t.Errorf("mid-rune cursor = %d, want 1", e.Cursor())
	}
	if e := New("abc", 1); e.Mode() != ModeInsert {
		t.Errorf("mode = %v, want ModeInsert", e.Mode())
	}
}

func TestModeLabels(t *testing.T) {
	e := New("abc", 0)
	if got := e.ModeLabel(); got != "INSERT" {
		t.Errorf("insert label = %q", got)
	}
	e.EnterNormal()
	if got := e.ModeLabel(); got != "NORMAL" {
		t.Errorf("normal label = %q", got)
	}
	press(e, "v")
	if got := e.ModeLabel(); got != "VISUAL" {
		t.Errorf("visual label = %q", got)
	}
	e.HandleKey(sp(tea.KeyEscape))
	press(e, "V")
	if got, m := e.ModeLabel(), e.Mode(); got != "VISUAL" || m != ModeVisualLine {
		t.Errorf("visual-line label = %q mode = %v", got, m)
	}
}

func TestInsertTypingAndEditing(t *testing.T) {
	e := New("", 0)
	press(e, "hi")
	wantState(t, e, "hi", 2)

	e.HandleKey(sp(tea.KeyEnter))
	wantState(t, e, "hi\n", 3)
	press(e, "yo")
	wantState(t, e, "hi\nyo", 5)

	e.HandleKey(sp(tea.KeyBackspace))
	wantState(t, e, "hi\ny", 4)

	// Backspace across the newline, then at offset 0 is a no-op.
	e.HandleKey(sp(tea.KeyBackspace))
	e.HandleKey(sp(tea.KeyBackspace))
	e.HandleKey(sp(tea.KeyBackspace))
	e.HandleKey(sp(tea.KeyBackspace))
	wantState(t, e, "", 0)
	e.HandleKey(sp(tea.KeyBackspace))
	wantState(t, e, "", 0)
}

func TestInsertSoftTabs(t *testing.T) {
	e := New("", 0)
	e.HandleKey(sp(tea.KeyTab))
	wantState(t, e, "    ", 4)

	e = New("", 0)
	e.TabWidth = 2
	e.HandleKey(sp(tea.KeyTab))
	wantState(t, e, "  ", 2)

	e = New("", 0)
	e.TabWidth = -1 // invalid: falls back to default
	e.HandleKey(sp(tea.KeyTab))
	wantState(t, e, "    ", 4)
}

func TestInsertArrows(t *testing.T) {
	e := New("ab\ncd", 0)
	e.HandleKey(sp(tea.KeyRight))
	if e.Cursor() != 1 {
		t.Fatalf("right: cursor = %d, want 1", e.Cursor())
	}
	e.HandleKey(sp(tea.KeyDown))
	if e.Cursor() != 4 {
		t.Fatalf("down: cursor = %d, want 4", e.Cursor())
	}
	e.HandleKey(sp(tea.KeyUp))
	if e.Cursor() != 1 {
		t.Fatalf("up: cursor = %d, want 1", e.Cursor())
	}
	e.HandleKey(sp(tea.KeyLeft))
	e.HandleKey(sp(tea.KeyLeft)) // second left at line start: no-op
	if e.Cursor() != 0 {
		t.Fatalf("left: cursor = %d, want 0", e.Cursor())
	}
	// Right stops at the line end (one past the last character is legal in
	// Insert mode, but not beyond).
	e = New("ab", 2)
	e.HandleKey(sp(tea.KeyRight))
	if e.Cursor() != 2 {
		t.Fatalf("right at end: cursor = %d, want 2", e.Cursor())
	}
}

func TestEscCursorAdjustment(t *testing.T) {
	// Past the line content end: pulled onto the last character.
	e := New("abc", 3)
	e.HandleKey(sp(tea.KeyEscape))
	if e.Mode() != ModeNormal || e.Cursor() != 2 {
		t.Errorf("mode = %v cursor = %d, want ModeNormal 2", e.Mode(), e.Cursor())
	}
	// Mid-line: stays put.
	e = New("abc", 1)
	e.HandleKey(sp(tea.KeyEscape))
	if e.Cursor() != 1 {
		t.Errorf("cursor = %d, want 1", e.Cursor())
	}
	// Empty buffer and empty line: column 0 is allowed.
	e = New("", 0)
	e.HandleKey(sp(tea.KeyEscape))
	if e.Cursor() != 0 {
		t.Errorf("cursor = %d, want 0", e.Cursor())
	}
	e = New("a\n\nb", 2)
	e.HandleKey(sp(tea.KeyEscape))
	if e.Cursor() != 2 {
		t.Errorf("empty-line cursor = %d, want 2", e.Cursor())
	}
}

func TestUnconsumedKeysInNormal(t *testing.T) {
	for name, k := range map[string]tea.Key{
		"enter":      sp(tea.KeyEnter),
		"esc":        sp(tea.KeyEscape),
		"tab":        sp(tea.KeyTab),
		"shift-tab":  {Code: tea.KeyTab, Mod: tea.ModShift},
		"colon":      pr(':'),
		"ctrl-s":     ctrlKey('s'),
		"ctrl-enter": {Code: tea.KeyEnter, Mod: tea.ModCtrl},
		"alt-x":      {Code: 'x', Mod: tea.ModAlt},
		"alt-enter":  {Code: tea.KeyEnter, Mod: tea.ModAlt},
	} {
		e := normal(t, "hello", 0)
		if e.HandleKey(k) {
			t.Errorf("%s: consumed in Normal, want unconsumed", name)
		}
	}
}

func TestPendingStateConsumesEnterAndEsc(t *testing.T) {
	// Count pending: Esc consumes and clears; the next Esc is unconsumed.
	e := normal(t, "hello", 0)
	press(e, "2")
	if !e.HandleKey(sp(tea.KeyEscape)) {
		t.Fatal("esc with pending count not consumed")
	}
	if e.HandleKey(sp(tea.KeyEscape)) {
		t.Fatal("esc after clearing still consumed")
	}
	press(e, "l") // count was cleared: moves one, not two
	if e.Cursor() != 1 {
		t.Fatalf("cursor = %d, want 1", e.Cursor())
	}

	// Operator pending: Enter consumes and clears.
	e = normal(t, "hello", 0)
	press(e, "d")
	if !e.HandleKey(sp(tea.KeyEnter)) {
		t.Fatal("enter with pending operator not consumed")
	}
	press(e, "w")
	wantState(t, e, "hello", 4) // plain motion: operator was cancelled

	// Tab with pending state consumes too.
	e = normal(t, "hello", 0)
	press(e, "3")
	if !e.HandleKey(sp(tea.KeyTab)) {
		t.Fatal("tab with pending count not consumed")
	}
}

func TestColonClearsCount(t *testing.T) {
	e := normal(t, "hello", 0)
	press(e, "3")
	if e.HandleKey(pr(':')) {
		t.Fatal("colon consumed in Normal")
	}
	press(e, "l")
	if e.Cursor() != 1 {
		t.Fatalf("cursor = %d, want 1 (count cleared)", e.Cursor())
	}
}

func TestConsumedKeys(t *testing.T) {
	e := normal(t, "hello", 0)
	for _, k := range []tea.Key{pr('q'), pr('h'), pr('Z'), sp(tea.KeyDown), ctrlKey('r'), sp(tea.KeyHome)} {
		if !e.HandleKey(k) {
			t.Errorf("key %v: unconsumed in Normal, want consumed", k)
		}
	}
	// Insert mode consumes Esc/Enter/Tab and ':'.
	for _, k := range []tea.Key{sp(tea.KeyEscape), sp(tea.KeyEnter), sp(tea.KeyTab), pr(':')} {
		e = New("", 0)
		if !e.HandleKey(k) {
			t.Errorf("key %v: unconsumed in Insert, want consumed", k)
		}
	}
	// Ctrl-S stays unconsumed even in Insert (app-level save).
	e = New("", 0)
	if e.HandleKey(ctrlKey('s')) {
		t.Error("ctrl-s consumed in Insert")
	}
	// Other Ctrl chords in Insert are swallowed without inserting.
	e = New("", 0)
	if !e.HandleKey(ctrlKey('w')) {
		t.Error("ctrl-w unconsumed in Insert")
	}
	wantState(t, e, "", 0)
}

func TestUndoRedo(t *testing.T) {
	e := normal(t, "a\nb", 0)
	press(e, "dd")
	wantState(t, e, "b", 0)
	press(e, "u")
	wantState(t, e, "a\nb", 0)
	e.HandleKey(ctrlKey('r'))
	wantState(t, e, "b", 0)

	// Undo/redo with empty stacks: no-ops.
	e = normal(t, "x", 0)
	press(e, "u")
	wantState(t, e, "x", 0)
	e.HandleKey(ctrlKey('r'))
	wantState(t, e, "x", 0)

	// A new edit clears the redo stack.
	e = normal(t, "ab", 0)
	press(e, "x")
	press(e, "u")
	press(e, "x")
	e.HandleKey(ctrlKey('r'))
	wantState(t, e, "b", 0)
}

func TestInsertCoalescing(t *testing.T) {
	// Everything typed in one insert session is a single undo step.
	e := New("", 0)
	press(e, "abc")
	e.HandleKey(sp(tea.KeyEscape))
	wantState(t, e, "abc", 2)
	press(e, "u")
	wantState(t, e, "", 0)
	e.HandleKey(ctrlKey('r'))
	wantState(t, e, "abc", 2)

	// Separate insert sessions are separate undo steps.
	e = normal(t, "", 0)
	press(e, "i")
	press(e, "ab")
	e.HandleKey(sp(tea.KeyEscape))
	press(e, "A")
	press(e, "cd")
	e.HandleKey(sp(tea.KeyEscape))
	wantState(t, e, "abcd", 3)
	press(e, "u")
	wantState(t, e, "ab", 1)
	press(e, "u")
	wantState(t, e, "", 0)

	// o plus typed text coalesces into one step including the newline.
	e = normal(t, "ab", 0)
	press(e, "o")
	press(e, "xy")
	e.HandleKey(sp(tea.KeyEscape))
	wantState(t, e, "ab\nxy", 4)
	press(e, "u")
	wantState(t, e, "ab", 0)
}

func TestInsertText(t *testing.T) {
	// Insert-mode paste joins the current insert session's undo step.
	e := New("", 0)
	press(e, "a")
	e.InsertText("bc")
	wantState(t, e, "abc", 3)
	e.HandleKey(sp(tea.KeyEscape))
	press(e, "u")
	wantState(t, e, "", 0)

	// Normal-mode paste is its own undo step; cursor follows the insertion.
	e = normal(t, "ab", 0)
	e.InsertText("Z\n")
	wantState(t, e, "Z\nab", 2)
	press(e, "u")
	wantState(t, e, "ab", 0)

	// Empty paste is a no-op.
	e.InsertText("")
	wantState(t, e, "ab", 0)
}

func TestUTF8(t *testing.T) {
	// Rune-wise l over CJK.
	e := normal(t, "世界", 0)
	press(e, "l")
	if e.Cursor() != 3 {
		t.Fatalf("l over CJK: cursor = %d, want 3", e.Cursor())
	}
	press(e, "$")
	if e.Cursor() != 3 {
		t.Fatalf("$ over CJK: cursor = %d, want 3", e.Cursor())
	}

	// x deletes one CJK rune.
	e = normal(t, "世界", 0)
	press(e, "x")
	wantState(t, e, "界", 0)
	wantReg(t, e, "世", false)

	// w treats a CJK run as one word; emoji (with modifier) as another.
	e = normal(t, "世界 hello", 0)
	press(e, "w")
	if e.Cursor() != 7 {
		t.Fatalf("w after CJK: cursor = %d, want 7", e.Cursor())
	}
	e = normal(t, "\U0001F44B\U0001F3FD hi", 0)
	press(e, "w")
	if e.Cursor() != 9 {
		t.Fatalf("w after emoji: cursor = %d, want 9", e.Cursor())
	}

	// e lands on the start byte of the last rune of the word.
	e = normal(t, "世界 x", 0)
	press(e, "e")
	if e.Cursor() != 3 {
		t.Fatalf("e over CJK: cursor = %d, want 3", e.Cursor())
	}

	// dd of a CJK line.
	e = normal(t, "世界\nabc", 0)
	press(e, "dd")
	wantState(t, e, "abc", 0)
	wantReg(t, e, "世界", true)

	// j keeps the character column across multibyte lines.
	e = normal(t, "世界\nab", 3)
	press(e, "j")
	if e.Cursor() != 8 {
		t.Fatalf("j column: cursor = %d, want 8", e.Cursor())
	}

	// Insert-mode backspace removes one full rune.
	e = New("é", 2)
	e.HandleKey(sp(tea.KeyBackspace))
	wantState(t, e, "", 0)
}
