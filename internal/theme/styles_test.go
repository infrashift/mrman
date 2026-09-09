package theme

import (
	"image/color"
	"slices"
	"testing"

	lipgloss "charm.land/lipgloss/v2"
)

var noColor = lipgloss.NoColor{}

func TestNewStylesMapsThemeSlots(t *testing.T) {
	th := TokyoNightStorm()
	s := NewStyles(th)

	checks := []struct {
		name   string
		style  lipgloss.Style
		wantFg color.Color
		wantBg color.Color
		bold   bool
	}{
		{"Selected", s.Selected, th.FgPrimary, th.BgHighlight, false},
		{"Dim", s.Dim, th.FgDim, noColor, false},
		{"DiffAdd", s.DiffAdd, th.DiffAdd, th.DiffAddBg, false},
		{"DiffDel", s.DiffDel, th.DiffDel, th.DiffDelBg, false},
		{"DiffContext", s.DiffContext, th.DiffContext, noColor, false},
		{"ExpandedContext", s.ExpandedContext, th.ExpandedContextFg, noColor, false},
		{"HunkHeader", s.HunkHeader, th.FgDim, th.SectionHighlightBg(), false},
		{"FileHeader", s.FileHeader, th.FgPrimary, noColor, true},
		{"Reviewed", s.Reviewed, th.Reviewed, noColor, false},
		{"Pending", s.Pending, th.Pending, noColor, false},
		{"Panel", s.Panel, th.FgPrimary, th.PanelBg, false},
		{"Popup", s.Popup, th.FgPrimary, th.PanelBg, false},
		{"StatusBar", s.StatusBar, th.FgPrimary, th.StatusBarBg, false},
		{"Mode", s.Mode, th.ModeFg, th.ModeBg, true},
		{"CurrentLineIndicator", s.CurrentLineIndicator, th.BorderFocused, noColor, false},
		{"Hash", s.Hash, th.CursorColor, noColor, false},
		{"Branch", s.Branch, th.BranchName, noColor, false},
		{"DirIcon", s.DirIcon, th.DiffHunkHeader, noColor, false},
		{"VisualSelection", s.VisualSelection, noColor, th.BgHighlight, false},
		{"HelpIndicator", s.HelpIndicator, th.HelpIndicator, th.PanelBg, false},
		{"RangeBar", s.RangeBar, th.BorderFocused, noColor, false},
		{"ErrorInline", s.ErrorInline, th.MessageErrorFg, noColor, true},
		{"PseudoCommitTag", s.PseudoCommitTag, th.FileModified, noColor, false},
		{"BorderFocused", s.BorderFocused, th.BorderFocused, noColor, false},
		{"BorderUnfocused", s.BorderUnfocused, th.BorderUnfocused, noColor, false},
	}
	for _, tc := range checks {
		if got := tc.style.GetForeground(); got != tc.wantFg {
			t.Errorf("%s foreground = %v, want %v", tc.name, got, tc.wantFg)
		}
		if got := tc.style.GetBackground(); got != tc.wantBg {
			t.Errorf("%s background = %v, want %v", tc.name, got, tc.wantBg)
		}
		if got := tc.style.GetBold(); got != tc.bold {
			t.Errorf("%s bold = %v, want %v", tc.name, got, tc.bold)
		}
	}
}

func TestNewStylesTransparentBackground(t *testing.T) {
	th := TokyoNightStorm()
	th.ApplyTransparentBackground()
	s := NewStyles(th)

	for name, style := range map[string]lipgloss.Style{
		"Panel":         s.Panel,
		"Popup":         s.Popup,
		"HelpIndicator": s.HelpIndicator,
		"HunkHeader":    s.HunkHeader, // derived from PanelBg via SectionHighlightBg
	} {
		if got := style.GetBackground(); got != noColor {
			t.Errorf("%s background = %v with transparent theme, want unset", name, got)
		}
	}
	// Foregrounds survive.
	if s.Panel.GetForeground() != th.FgPrimary {
		t.Error("Panel foreground should still be FgPrimary")
	}
}

func TestBorder(t *testing.T) {
	th := TokyoNightDay()
	s := NewStyles(th)
	if got := s.Border(true).GetForeground(); got != th.BorderFocused {
		t.Errorf("Border(true) fg = %v, want %v", got, th.BorderFocused)
	}
	if got := s.Border(false).GetForeground(); got != th.BorderUnfocused {
		t.Errorf("Border(false) fg = %v, want %v", got, th.BorderUnfocused)
	}
}

func TestFileStatusColor(t *testing.T) {
	th := TokyoNightStorm()
	tests := []struct {
		status byte
		want   color.Color
	}{
		{'A', th.FileAdded},
		{'M', th.FileModified},
		{'D', th.FileDeleted},
		{'R', th.FileRenamed},
		{'C', th.FgSecondary},
		{'?', th.FgSecondary},
	}
	for _, tc := range tests {
		if got := FileStatusColor(th, tc.status); got != tc.want {
			t.Errorf("FileStatusColor(%q) = %v, want %v", tc.status, got, tc.want)
		}
	}
}

func TestAuthorColorForDeterminism(t *testing.T) {
	for _, author := range []string{"alice", "bob", "some-agent"} {
		first := AuthorColorFor(author)
		for range 3 {
			if got := AuthorColorFor(author); got != first {
				t.Fatalf("AuthorColorFor(%q) not deterministic: %v vs %v", author, got, first)
			}
		}
		found := slices.Contains(authorPalette, first)
		if !found {
			t.Errorf("AuthorColorFor(%q) = %v not in palette", author, first)
		}
	}
}

func TestAuthorPaletteAnsiIndices(t *testing.T) {
	want := []color.Color{
		lipgloss.Cyan, lipgloss.Yellow, lipgloss.Magenta,
		lipgloss.Blue, lipgloss.BrightMagenta, lipgloss.BrightCyan,
	}
	if len(authorPalette) != len(want) {
		t.Fatalf("palette has %d entries, want %d", len(authorPalette), len(want))
	}
	for i, c := range want {
		if authorPalette[i] != c {
			t.Errorf("authorPalette[%d] = %v, want %v", i, authorPalette[i], c)
		}
	}
}

func TestAuthorAccent(t *testing.T) {
	if c, ok := AuthorAccent("alice", "alice"); ok || c != nil {
		t.Error("viewer's own comment should have no accent")
	}
	if c, ok := AuthorAccent("alice", ""); ok || c != nil {
		t.Error("empty author should have no accent")
	}
	c, ok := AuthorAccent("alice", "bob")
	if !ok {
		t.Fatal("different author should have an accent")
	}
	if c != AuthorColorFor("bob") {
		t.Errorf("accent = %v, want AuthorColorFor(bob) = %v", c, AuthorColorFor("bob"))
	}
}
