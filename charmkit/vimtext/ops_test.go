package vimtext

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestDeleteChar(t *testing.T) {
	e := normal(t, "abc", 0)
	press(e, "x")
	wantState(t, e, "bc", 0)
	wantReg(t, e, "a", false)

	e = normal(t, "abc", 0)
	press(e, "2x")
	wantState(t, e, "c", 0)
	wantReg(t, e, "ab", false)

	// Count stops at the line end.
	e = normal(t, "ab\ncd", 0)
	press(e, "9x")
	wantState(t, e, "\ncd", 0)
	wantReg(t, e, "ab", false)

	// Deleting the last character clamps the cursor back.
	e = normal(t, "ab", 1)
	press(e, "x")
	wantState(t, e, "a", 0)
	wantReg(t, e, "b", false)

	// x on an empty line: no-op, register untouched.
	e = normal(t, "a\n\nb", 2)
	e.reg, e.regLine = "keep", true
	press(e, "x")
	wantState(t, e, "a\n\nb", 2)
	wantReg(t, e, "keep", true)
}

func TestDeleteLine(t *testing.T) {
	e := normal(t, "l1\nl2\nl3", 0)
	press(e, "dd")
	wantState(t, e, "l2\nl3", 0)
	wantReg(t, e, "l1", true)

	e = normal(t, "l1\nl2\nl3", 0)
	press(e, "2dd")
	wantState(t, e, "l3", 0)
	wantReg(t, e, "l1\nl2", true)

	// Middle line: cursor lands on the following line.
	e = normal(t, "l1\nl2\nl3", 3)
	press(e, "dd")
	wantState(t, e, "l1\nl3", 3)
	wantReg(t, e, "l2", true)

	// Last line: the preceding newline goes too.
	e = normal(t, "a\nb", 2)
	press(e, "dd")
	wantState(t, e, "a", 0)
	wantReg(t, e, "b", true)

	// Only line: buffer empties.
	e = normal(t, "abc", 1)
	press(e, "dd")
	wantState(t, e, "", 0)
	wantReg(t, e, "abc", true)

	// dd on an empty buffer: no-op, no undo entry.
	e = normal(t, "", 0)
	press(e, "dd")
	wantState(t, e, "", 0)
	if len(e.undo) != 0 {
		t.Errorf("undo stack = %d entries, want 0", len(e.undo))
	}
}

func TestDeleteMotions(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		cursor   int
		keys     string
		wantText string
		wantCur  int
		wantReg  string
		linewise bool
	}{
		{"dw", "foo bar", 0, "dw", "bar", 0, "foo ", false},
		{"dw last word on line", "foo\nbar", 0, "dw", "\nbar", 0, "foo", false},
		{"d2w capped at line end", "foo bar\nbaz", 0, "d2w", "\nbaz", 0, "foo bar", false},
		{"de", "foo bar", 0, "de", " bar", 0, "foo", false},
		{"d dollar", "hello", 1, "d$", "h", 0, "ello", false},
		{"d0", "hello", 3, "d0", "lo", 0, "hel", false},
		{"db", "foo bar", 4, "db", "bar", 0, "foo ", false},
		{"dh", "abc", 2, "dh", "ac", 1, "b", false},
		{"dl", "abc", 0, "dl", "bc", 0, "a", false},
		{"D", "hello", 2, "D", "he", 1, "llo", false},
		{"dj", "a\nb\nc\nd", 0, "dj", "c\nd", 0, "a\nb", true},
		{"dk", "a\nb\nc", 4, "dk", "a", 0, "b\nc", true},
		{"dG", "a\nb\nc", 2, "dG", "a", 0, "b\nc", true},
		{"dgg", "a\nb\nc", 4, "dgg", "", 0, "a\nb\nc", true},
		{"diw mid-word", "hello world", 8, "diw", "hello ", 5, "world", false},
		{"diw punctuation", "foo.bar", 3, "diw", "foobar", 3, ".", false},
		{"diw whitespace", "a b", 1, "diw", "ab", 1, " ", false},
		{"diw word start", "foo bar", 4, "diw", "foo ", 3, "bar", false},
		{"diw underscore word", "a_b c", 0, "diw", " c", 0, "a_b", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := normal(t, tt.text, tt.cursor)
			press(e, tt.keys)
			wantState(t, e, tt.wantText, tt.wantCur)
			wantReg(t, e, tt.wantReg, tt.linewise)
		})
	}

	// dj on the last line fails the motion: nothing happens.
	e := normal(t, "a\nb", 2)
	e.reg = "keep"
	press(e, "dj")
	wantState(t, e, "a\nb", 2)
	if e.reg != "keep" {
		t.Errorf("register = %q, want untouched", e.reg)
	}

	// dh at column zero: empty range aborts.
	e = normal(t, "abc", 0)
	press(e, "dh")
	wantState(t, e, "abc", 0)
}

func TestChangeOps(t *testing.T) {
	// cw acts like ce on a non-blank: the trailing space survives.
	e := normal(t, "foo bar", 0)
	press(e, "cw")
	if e.Mode() != ModeInsert {
		t.Fatalf("cw: mode = %v", e.Mode())
	}
	wantState(t, e, " bar", 0)
	wantReg(t, e, "foo", false)
	press(e, "qux")
	wantState(t, e, "qux bar", 3)

	// cw on whitespace deletes the run like dw.
	e = normal(t, "a  b", 1)
	press(e, "cw")
	wantState(t, e, "ab", 1)
	wantReg(t, e, "  ", false)

	// cc keeps a blank line and enters Insert.
	e = normal(t, "a\nb", 0)
	press(e, "cc")
	if e.Mode() != ModeInsert {
		t.Fatalf("cc: mode = %v", e.Mode())
	}
	wantState(t, e, "\nb", 0)
	wantReg(t, e, "a", true)

	e = normal(t, "abc", 1)
	press(e, "cc")
	wantState(t, e, "", 0)
	wantReg(t, e, "abc", true)

	// C changes to end of line.
	e = normal(t, "hello", 2)
	press(e, "C")
	if e.Mode() != ModeInsert {
		t.Fatalf("C: mode = %v", e.Mode())
	}
	wantState(t, e, "he", 2)
	wantReg(t, e, "llo", false)

	// C on an empty line still enters Insert.
	e = normal(t, "a\n\nb", 2)
	press(e, "C")
	if e.Mode() != ModeInsert {
		t.Fatalf("C empty: mode = %v", e.Mode())
	}
	wantState(t, e, "a\n\nb", 2)

	// cj is linewise: both lines collapse to one blank line.
	e = normal(t, "a\nb\nc", 0)
	press(e, "cj")
	wantState(t, e, "\nc", 0)
	wantReg(t, e, "a\nb", true)

	// ciw at a word boundary.
	e = normal(t, "foo.bar", 5)
	press(e, "ciw")
	if e.Mode() != ModeInsert {
		t.Fatalf("ciw: mode = %v", e.Mode())
	}
	wantState(t, e, "foo.", 4)
	wantReg(t, e, "bar", false)

	// ciw on an empty line: nothing to delete, still enters Insert.
	e = normal(t, "a\n\nb", 2)
	press(e, "ciw")
	if e.Mode() != ModeInsert {
		t.Fatalf("ciw empty: mode = %v", e.Mode())
	}
	wantState(t, e, "a\n\nb", 2)

	// The whole change (delete + typed text) undoes as one step.
	e = normal(t, "foo bar", 0)
	press(e, "cw")
	press(e, "new")
	e.HandleKey(sp(tea.KeyEscape))
	wantState(t, e, "new bar", 3)
	press(e, "u")
	wantState(t, e, "foo bar", 0)
}

func TestSubstitute(t *testing.T) {
	e := normal(t, "abc", 1)
	press(e, "s")
	if e.Mode() != ModeInsert {
		t.Fatalf("s: mode = %v", e.Mode())
	}
	wantState(t, e, "ac", 1)
	wantReg(t, e, "b", false)
	press(e, "X")
	e.HandleKey(sp(tea.KeyEscape))
	wantState(t, e, "aXc", 2) // Esc clamps only past line end

	e = normal(t, "abcd", 1)
	press(e, "2s")
	wantState(t, e, "ad", 1)
	wantReg(t, e, "bc", false)
}

func TestYank(t *testing.T) {
	// yy yanks linewise and keeps the cursor.
	e := normal(t, "l1\nl2", 3)
	press(e, "yy")
	wantState(t, e, "l1\nl2", 3)
	wantReg(t, e, "l2", true)

	e = normal(t, "l1\nl2\nl3", 0)
	press(e, "2yy")
	wantReg(t, e, "l1\nl2", true)

	// yw yanks charwise without moving.
	e = normal(t, "foo bar", 0)
	press(e, "yw")
	wantState(t, e, "foo bar", 0)
	wantReg(t, e, "foo ", false)

	// y$ to end of line.
	e = normal(t, "hello", 2)
	press(e, "y$")
	wantState(t, e, "hello", 2)
	wantReg(t, e, "llo", false)

	// yb moves the cursor to the range start.
	e = normal(t, "foo bar", 4)
	press(e, "yb")
	wantState(t, e, "foo bar", 0)
	wantReg(t, e, "foo ", false)

	// ygg: backward linewise yank moves to the first line.
	e = normal(t, "a\nb", 2)
	press(e, "ygg")
	wantState(t, e, "a\nb", 0)
	wantReg(t, e, "a\nb", true)

	// yj: forward linewise yank stays put.
	e = normal(t, "a\nb", 0)
	press(e, "yj")
	wantState(t, e, "a\nb", 0)
	wantReg(t, e, "a\nb", true)

	// yiw.
	e = normal(t, "foo bar", 5)
	press(e, "yiw")
	wantState(t, e, "foo bar", 4)
	wantReg(t, e, "bar", false)

	// Yank does not create an undo entry.
	e = normal(t, "ab", 0)
	press(e, "yy")
	if len(e.undo) != 0 {
		t.Errorf("undo stack = %d entries, want 0", len(e.undo))
	}
}

func TestPasteCharwise(t *testing.T) {
	// Classic xp swap.
	e := normal(t, "abc", 0)
	press(e, "xp")
	wantState(t, e, "bac", 1)

	// P puts before the cursor.
	e = normal(t, "abc", 0)
	press(e, "xP")
	wantState(t, e, "abc", 0)

	// p at the end of a line appends.
	e = normal(t, "ab", 1)
	press(e, "xp")
	wantState(t, e, "ab", 1)

	// Multibyte register: cursor lands on the start of the last rune.
	e = normal(t, "世界", 0)
	press(e, "xp")
	wantState(t, e, "界世", 3)

	// Empty register: no-op.
	e = normal(t, "ab", 0)
	press(e, "p")
	wantState(t, e, "ab", 0)
	if len(e.undo) != 0 {
		t.Errorf("undo stack = %d entries, want 0", len(e.undo))
	}
}

func TestPasteLinewise(t *testing.T) {
	// p puts the register on a new line below.
	e := normal(t, "l1\nl2", 0)
	press(e, "yyp")
	wantState(t, e, "l1\nl1\nl2", 3)

	// p below the last line (no trailing newline).
	e = normal(t, "a\nb", 2)
	press(e, "yyp")
	wantState(t, e, "a\nb\nb", 4)

	// P puts above the current line.
	e = normal(t, "a\nb", 2)
	press(e, "yyggP")
	wantState(t, e, "b\na\nb", 0)

	// Cursor lands on the first non-blank of the pasted line.
	e = normal(t, "  x\ny", 2)
	press(e, "yyGp")
	wantState(t, e, "  x\ny\n  x", 8)

	// dd + p reorders lines.
	e = normal(t, "l1\nl2", 0)
	press(e, "ddp")
	wantState(t, e, "l2\nl1", 3)
}
