package vimtext

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestVisualModeSwitching(t *testing.T) {
	e := normal(t, "abc", 0)
	press(e, "v")
	if e.Mode() != ModeVisual {
		t.Fatalf("v: mode = %v", e.Mode())
	}
	e.HandleKey(sp(tea.KeyEscape))
	if e.Mode() != ModeNormal {
		t.Fatalf("esc: mode = %v", e.Mode())
	}

	press(e, "vv") // v again exits
	if e.Mode() != ModeNormal {
		t.Fatalf("vv: mode = %v", e.Mode())
	}
	press(e, "vV") // switch charwise -> linewise
	if e.Mode() != ModeVisualLine {
		t.Fatalf("vV: mode = %v", e.Mode())
	}
	press(e, "v") // switch back to charwise
	if e.Mode() != ModeVisual {
		t.Fatalf("Vv: mode = %v", e.Mode())
	}
	press(e, "v")
	press(e, "VV") // V twice exits
	if e.Mode() != ModeNormal {
		t.Fatalf("VV: mode = %v", e.Mode())
	}

	// Esc in visual is consumed (it only reaches the app from Normal).
	press(e, "v")
	if !e.HandleKey(sp(tea.KeyEscape)) {
		t.Fatal("esc in visual not consumed")
	}
	// Enter in visual is consumed as a no-op.
	press(e, "v")
	if !e.HandleKey(sp(tea.KeyEnter)) {
		t.Fatal("enter in visual not consumed")
	}
	e.HandleKey(sp(tea.KeyEscape))
}

func TestVisualCharOps(t *testing.T) {
	// Yank: selection is inclusive of the cursor rune.
	e := normal(t, "hello", 0)
	press(e, "vlly")
	wantState(t, e, "hello", 0)
	wantReg(t, e, "hel", false)
	if e.Mode() != ModeNormal {
		t.Fatalf("mode = %v", e.Mode())
	}

	// Delete.
	e = normal(t, "hello", 0)
	press(e, "vlld")
	wantState(t, e, "lo", 0)
	wantReg(t, e, "hel", false)

	// x acts like d in visual.
	e = normal(t, "abc", 0)
	press(e, "vx")
	wantState(t, e, "bc", 0)
	wantReg(t, e, "a", false)

	// Backward selection: anchor after cursor.
	e = normal(t, "hello", 3)
	press(e, "vhhd")
	wantState(t, e, "ho", 1)
	wantReg(t, e, "ell", false)

	// Change enters Insert.
	e = normal(t, "hello", 0)
	press(e, "vc")
	if e.Mode() != ModeInsert {
		t.Fatalf("vc: mode = %v", e.Mode())
	}
	wantState(t, e, "ello", 0)
	wantReg(t, e, "h", false)

	// Word-motion extension.
	e = normal(t, "foo bar", 4)
	press(e, "vey")
	wantReg(t, e, "bar", false)

	// Count applies to visual motions.
	e = normal(t, "foo bar baz", 0)
	press(e, "v2wy")
	wantReg(t, e, "foo bar b", false)

	// $ extends through the last character.
	e = normal(t, "hello", 1)
	press(e, "v$d")
	wantState(t, e, "h", 0)
	wantReg(t, e, "ello", false)

	// G extends across lines charwise.
	e = normal(t, "a\nb\nc", 0)
	press(e, "vGy")
	wantReg(t, e, "a\nb\nc", false)
}

func TestVisualCharPaste(t *testing.T) {
	// p replaces the selection; the replaced text takes over the register.
	e := normal(t, "abc", 0)
	press(e, "x") // reg "a", text "bc"
	press(e, "vlp")
	wantState(t, e, "a", 0)
	wantReg(t, e, "bc", false)

	// Linewise register over a charwise selection splits the line.
	e = normal(t, "Y\naxb", 0)
	press(e, "yy") // reg "Y" linewise
	press(e, "jl") // cursor on 'x'
	press(e, "vp") // replace 'x'
	wantState(t, e, "Y\na\nY\nb", 4)
	wantReg(t, e, "x", false)

	// Empty register: exits visual, no edit.
	e = normal(t, "ab", 0)
	e.reg = ""
	press(e, "vp")
	wantState(t, e, "ab", 0)
	if e.Mode() != ModeNormal {
		t.Fatalf("mode = %v", e.Mode())
	}
}

func TestVisualLineOps(t *testing.T) {
	// Delete two lines.
	e := normal(t, "l1\nl2\nl3", 0)
	press(e, "Vjd")
	wantState(t, e, "l3", 0)
	wantReg(t, e, "l1\nl2", true)
	if e.Mode() != ModeNormal {
		t.Fatalf("mode = %v", e.Mode())
	}

	// Yank a single line.
	e = normal(t, "l1\nl2", 3)
	press(e, "Vy")
	wantState(t, e, "l1\nl2", 3)
	wantReg(t, e, "l2", true)

	// Yank upward: cursor moves to the selection start.
	e = normal(t, "l1\nl2", 3)
	press(e, "Vky")
	wantState(t, e, "l1\nl2", 0)
	wantReg(t, e, "l1\nl2", true)

	// Change collapses the lines to one blank line and enters Insert.
	e = normal(t, "a\nb", 0)
	press(e, "Vc")
	if e.Mode() != ModeInsert {
		t.Fatalf("Vc: mode = %v", e.Mode())
	}
	wantState(t, e, "\nb", 0)
	wantReg(t, e, "a", true)

	// Middle line delete.
	e = normal(t, "a\nb\nc", 2)
	press(e, "Vd")
	wantState(t, e, "a\nc", 2)
	wantReg(t, e, "b", true)

	// Replace a line via p; the old line takes over the register.
	e = normal(t, "a\nb", 0)
	press(e, "yyjVp")
	wantState(t, e, "a\na", 2)
	wantReg(t, e, "b", true)

	// Motions extend the linewise selection by whole lines.
	e = normal(t, "l1\nl2\nl3", 0)
	press(e, "VGy")
	wantReg(t, e, "l1\nl2\nl3", true)
}
