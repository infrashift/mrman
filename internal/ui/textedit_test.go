package ui

import (
	"strings"
	"testing"
	"unicode/utf8"

	render "github.com/infrashift/mrman/charmkit/cellrender"
	"github.com/infrashift/mrman/internal/input"
)

func TestBackspaceRemovesWholeCharacters(t *testing.T) {
	m := testModel(t)
	m.App.InputMode = input.ModeCommand
	m.App.CommandBuffer = "e café"
	m.dispatchCommand(input.Action{Kind: input.DeleteChar})
	if m.App.CommandBuffer != "e caf" {
		t.Errorf("command buffer = %q, want %q", m.App.CommandBuffer, "e caf")
	}
	m.App.SearchBuffer = "日本"
	m.dispatchSearch(input.Action{Kind: input.DeleteChar})
	if m.App.SearchBuffer != "日" || !utf8.ValidString(m.App.SearchBuffer) {
		t.Errorf("search buffer = %q, want %q", m.App.SearchBuffer, "日")
	}
}

// TestPanelTitleFitsByDisplayWidth: a title with wide characters was
// measured in runes, came out wider than the panel, and wrapped the frame.
func TestPanelTitleFitsByDisplayWidth(t *testing.T) {
	m := testModel(t)
	for _, title := range []string{" 日本語のファイル.go ", " " + strings.Repeat("界", 40) + " "} {
		top, _, _ := strings.Cut(m.titledPanel("body", title, false, 30, 1), "\n")
		if w := render.StringWidth(ansiSequence.ReplaceAllString(top, "")); w != 32 {
			t.Errorf("title row %q is %d columns, want 32", ansiSequence.ReplaceAllString(top, ""), w)
		}
	}
}

func TestTruncateToWidthNeverSplitsACharacter(t *testing.T) {
	got, w := truncateToWidth("ab日本", 3)
	if got != "ab" || w != 2 {
		t.Errorf("truncateToWidth = %q, %d; want %q, 2", got, w, "ab")
	}
}
