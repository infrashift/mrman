package vimtext

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestCharMotions(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		cursor int
		keys   string
		want   int
	}{
		{"l", "hello", 0, "ll", 2},
		{"l count", "hello", 0, "3l", 3},
		{"l clamps at last char", "hello", 4, "l", 4},
		{"l multi-digit count", "abcdefghijklmno", 0, "12l", 12},
		{"h", "hello", 2, "2h", 0},
		{"h at line start", "hello", 0, "h", 0},
		{"h stops at line start", "ab\ncd", 3, "5h", 3},
		{"dollar", "hello", 2, "$", 4},
		{"zero", "hello", 3, "0", 0},
		{"caret", "  ab", 3, "^", 2},
		{"caret blank line", "   \nx", 1, "^", 2}, // all-blank: clamps to last char
		{"empty buffer l", "", 0, "l", 0},
		{"j", "ab\ncd", 0, "j", 3},
		{"j clamps column", "abcde\nab", 4, "j", 7},
		{"j at last line", "ab\ncd", 4, "j", 4},
		{"k", "ab\ncd", 4, "k", 1},
		{"k count", "a\n\nb", 3, "2k", 0},
		{"k at first line", "ab\ncd", 1, "k", 1},
		{"j onto empty line", "a\n\nb", 0, "j", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := normal(t, tt.text, tt.cursor)
			press(e, tt.keys)
			if e.Cursor() != tt.want {
				t.Errorf("cursor = %d, want %d", e.Cursor(), tt.want)
			}
			if e.Text() != tt.text {
				t.Errorf("text changed: %q", e.Text())
			}
		})
	}
}

func TestArrowMotionsInNormal(t *testing.T) {
	e := normal(t, "ab\ncd", 0)
	e.HandleKey(sp(tea.KeyRight))
	if e.Cursor() != 1 {
		t.Fatalf("right: cursor = %d, want 1", e.Cursor())
	}
	e.HandleKey(sp(tea.KeyDown))
	if e.Cursor() != 4 {
		t.Fatalf("down: cursor = %d, want 4", e.Cursor())
	}
	e.HandleKey(sp(tea.KeyLeft))
	if e.Cursor() != 3 {
		t.Fatalf("left: cursor = %d, want 3", e.Cursor())
	}
	e.HandleKey(sp(tea.KeyUp))
	if e.Cursor() != 0 {
		t.Fatalf("up: cursor = %d, want 0", e.Cursor())
	}
	e.HandleKey(sp(tea.KeyBackspace)) // backspace acts like h
	if e.Cursor() != 0 {
		t.Fatalf("backspace: cursor = %d, want 0", e.Cursor())
	}
}

func TestWordMotions(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		cursor int
		keys   string
		want   int
	}{
		{"w", "foo bar baz", 0, "w", 4},
		{"w count", "foo bar baz", 0, "2w", 8},
		{"w at last word clamps", "foo bar baz", 8, "w", 10},
		{"w punctuation is a word", "foo.bar", 0, "w", 3},
		{"w from punctuation", "foo.bar", 3, "w", 4},
		{"w across newline", "foo\nbar", 0, "w", 4},
		{"w stops on empty line", "a\n\nb", 0, "w", 2},
		{"w leaves empty line", "a\n\nb", 2, "w", 3},
		{"w from whitespace", "a   b", 1, "w", 4},
		{"b to word start", "foo bar", 4, "b", 0},
		{"b within word", "foo bar", 6, "b", 4},
		{"b over punctuation", "foo.bar", 4, "b", 3},
		{"b count", "foo bar", 4, "2b", 0},
		{"b stops on empty line", "a\n\nb", 3, "b", 2},
		{"b at buffer start", "foo", 0, "b", 0},
		{"e to word end", "foo bar", 0, "e", 2},
		{"e to next word end", "foo bar", 2, "e", 6},
		{"e at buffer end", "foo", 2, "e", 2},
		{"e count", "foo bar baz", 0, "2e", 6},
		{"e skips empty lines", "a\n\nfoo", 0, "e", 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := normal(t, tt.text, tt.cursor)
			press(e, tt.keys)
			if e.Cursor() != tt.want {
				t.Errorf("cursor = %d, want %d", e.Cursor(), tt.want)
			}
		})
	}
}

func TestGotoLineMotions(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		cursor int
		keys   string
		want   int
	}{
		{"gg", "a\nb\nc", 4, "gg", 0},
		{"G", "a\nb\nc", 0, "G", 4},
		{"count G", "a\nb\nc", 0, "2G", 2},
		{"count gg", "a\nb\nc", 4, "2gg", 2},
		{"G past last line clamps", "a\nb", 0, "9G", 2},
		{"gg to first non-blank", "  x\ny", 4, "gg", 2},
		{"g then other key aborts", "a\nb", 2, "gx", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := normal(t, tt.text, tt.cursor)
			press(e, tt.keys)
			if e.Cursor() != tt.want {
				t.Errorf("cursor = %d, want %d", e.Cursor(), tt.want)
			}
		})
	}
}

func TestInsertEntryCommands(t *testing.T) {
	e := normal(t, "ab", 1)
	press(e, "i")
	if e.Mode() != ModeInsert || e.Cursor() != 1 {
		t.Errorf("i: mode = %v cursor = %d", e.Mode(), e.Cursor())
	}

	e = normal(t, "ab", 0)
	press(e, "a")
	if e.Mode() != ModeInsert || e.Cursor() != 1 {
		t.Errorf("a: mode = %v cursor = %d", e.Mode(), e.Cursor())
	}

	e = normal(t, "ab", 1)
	press(e, "a") // append after the last character: one past line content
	if e.Cursor() != 2 {
		t.Errorf("a at line end: cursor = %d, want 2", e.Cursor())
	}

	e = normal(t, "  ab", 3)
	press(e, "I")
	if e.Mode() != ModeInsert || e.Cursor() != 2 {
		t.Errorf("I: mode = %v cursor = %d, want 2", e.Mode(), e.Cursor())
	}

	e = normal(t, "ab", 0)
	press(e, "A")
	if e.Mode() != ModeInsert || e.Cursor() != 2 {
		t.Errorf("A: mode = %v cursor = %d, want 2", e.Mode(), e.Cursor())
	}

	e = normal(t, "ab\ncd", 0)
	press(e, "o")
	if e.Mode() != ModeInsert {
		t.Fatalf("o: mode = %v", e.Mode())
	}
	wantState(t, e, "ab\n\ncd", 3)
	press(e, "x")
	wantState(t, e, "ab\nx\ncd", 4)

	e = normal(t, "ab", 1)
	press(e, "O")
	if e.Mode() != ModeInsert {
		t.Fatalf("O: mode = %v", e.Mode())
	}
	wantState(t, e, "\nab", 0)
	press(e, "y")
	wantState(t, e, "y\nab", 1)
}
