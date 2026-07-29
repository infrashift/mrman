package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/input"
)

func TestZChordsMoveTheViewport(t *testing.T) {
	for _, tc := range []struct {
		second rune
		name   string
	}{{'z', "center"}, {'t', "top"}, {'b', "bottom"}} {
		m := testModel(t)
		m.App.DiffState.CursorLine = 3
		pressRune(m, 'z')
		if m.chords.Pending() != "z" {
			t.Fatalf("%s: z must be held as a prefix, pending = %q", tc.name, m.chords.Pending())
		}
		pressRune(m, tc.second)
		if m.chords.Pending() != "" {
			t.Errorf("%s: the chord must clear the prefix", tc.name)
		}
		if m.App.DiffState.CursorLine != 3 {
			t.Errorf("%s: z%c scrolls the view, not the cursor", tc.name, tc.second)
		}
	}
}

func TestZZSavesAndQuits(t *testing.T) {
	m := testModel(t)
	pressRune(m, 'Z')
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Text: "Z", Code: 'Z'}))
	if cmd == nil {
		t.Fatal("ZZ must quit")
	}
}

func TestZQQuitsWithoutSaving(t *testing.T) {
	m := testModel(t)
	pressRune(m, 'Z')
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Text: "Q", Code: 'Q'}))
	if cmd == nil {
		t.Fatal("ZQ must quit")
	}
}

func TestAnUnrecognizedChordFallsThroughToItsSecondKey(t *testing.T) {
	// "z:" is not a chord, so the ":" acts on its own — the same as it did
	// before the stray z. This matches tuicr.
	m := testModel(t)
	pressRune(m, 'z')
	pressRune(m, ':')

	if m.chords.Pending() != "" {
		t.Error("an unknown chord must clear the prefix")
	}
	if m.App.InputMode != input.ModeCommand {
		t.Errorf("the second key must still act, got mode %v", m.App.InputMode)
	}
}

func TestDDDeletesTheCommentAtTheCursor(t *testing.T) {
	m := testModel(t)
	addReviewComment(m, "delete me")
	for i := range m.App.LineAnnotations {
		if m.App.LineAnnotations[i].Kind == app.AnnReviewComment {
			m.App.DiffState.CursorLine = i
			break
		}
	}
	pressRune(m, 'd')
	pressRune(m, 'd')
	if len(m.App.Session.ReviewComments) != 0 {
		t.Error("dd must delete the comment under the cursor")
	}
}

func TestLeaderChordDispatches(t *testing.T) {
	m := testModel(t)
	before := m.App.ShowFileList
	pressRune(m, ';')
	if m.chords.Pending() != ";" {
		t.Fatalf("the leader must be held as a prefix, got %q", m.chords.Pending())
	}
	pressRune(m, 'e')
	if m.App.ShowFileList == before {
		t.Error(";e must toggle the file list")
	}
}

func TestRebindingTheLeaderRebuildsTheResolver(t *testing.T) {
	m := testModel(t)
	m.SetLeader(',')

	// The old leader is now an ordinary key.
	pressRune(m, ';')
	if m.chords.Pending() != "" {
		t.Error("the old leader must stop acting as a prefix")
	}
	before := m.App.ShowFileList
	pressRune(m, ',')
	pressRune(m, 'e')
	if m.App.ShowFileList == before {
		t.Error(",e must toggle the file list after rebinding")
	}
}

func TestANonPrintableKeyAbandonsAPendingChord(t *testing.T) {
	m := testModel(t)
	pressRune(m, 'z')
	press(m, "", tea.KeyDown, 0)
	if m.chords.Pending() != "" {
		t.Error("a key that cannot complete a chord must abandon it")
	}
}

func TestChordsAreAbandonedOutsideNormalMode(t *testing.T) {
	m := testModel(t)
	pressRune(m, 'z')
	if m.chords.Pending() == "" {
		t.Fatal("expected a pending prefix")
	}
	// Opening the command line must not leave a chord armed against it.
	pressRune(m, ':')
	if m.App.InputMode != input.ModeCommand {
		t.Fatalf("expected command mode, got %v", m.App.InputMode)
	}
	pressRune(m, 'z')
	if m.chords.Pending() != "" {
		t.Error("a text field must never accumulate a chord prefix")
	}
	if m.App.CommandBuffer != "z" {
		t.Errorf("the key must reach the command line, buffer = %q", m.App.CommandBuffer)
	}
}

func TestStartingAChordClearsAPendingCount(t *testing.T) {
	m := testModel(t)
	pressRune(m, '5')
	if m.App.PendingCount == nil || *m.App.PendingCount != 5 {
		t.Fatal("expected a pending count of 5")
	}
	pressRune(m, 'z')
	if m.App.PendingCount != nil {
		t.Error("5zz centers once; the 5 must not survive to the next action")
	}
}
