package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/theme"
)

// TestCommentNavMarkerEncodesScopeAndType pins the split: the glyph carries
// scope, the colour carries comment type.
//
// The navigator is width/5 — around 26 columns — and already truncates the
// author, so a "[ISSUE]" tag would push the filename out of a row whose whole
// job is identifying a location. Colour costs no width.
func TestCommentNavMarkerEncodesScopeAndType(t *testing.T) {
	th := theme.TokyoNightStorm()

	t.Run("glyph follows scope", func(t *testing.T) {
		for scope, want := range map[app.CommentNavigatorScope]string{
			app.NavScopeReview: "★ ",
			app.NavScopeFile:   "▣ ",
			app.NavScopeLine:   "● ",
		} {
			item := app.CommentNavigatorItem{Key: app.CommentNavigatorKey{Scope: scope}}
			if glyph, _ := commentNavMarker(th, &item); glyph != want {
				t.Errorf("scope %v: glyph = %q, want %q", scope, glyph, want)
			}
		}
	})

	t.Run("colour follows type", func(t *testing.T) {
		for id, want := range map[string]any{
			"note":       th.CommentNote,
			"issue":      th.CommentIssue,
			"suggestion": th.CommentSuggestion,
			"praise":     th.CommentPraise,
			// Untyped stays distinguishable from every typed row.
			model.CommentTypeNoneID: th.FgSecondary,
		} {
			item := app.CommentNavigatorItem{
				Key:         app.CommentNavigatorKey{Scope: app.NavScopeLine},
				CommentType: model.CommentTypeFromID(id),
			}
			_, style := commentNavMarker(th, &item)
			if style.Fg != want {
				t.Errorf("type %q: colour = %v, want %v", id, style.Fg, want)
			}
		}
	})

	t.Run("scope does not change the colour", func(t *testing.T) {
		// Regression guard: the marker colour used to be picked by scope from
		// the comment-type palette, so a file-scoped ISSUE rendered in the
		// SUGGESTION colour.
		issue := model.CommentTypeFromID("issue")
		for _, scope := range []app.CommentNavigatorScope{
			app.NavScopeReview, app.NavScopeFile, app.NavScopeLine,
		} {
			item := app.CommentNavigatorItem{
				Key:         app.CommentNavigatorKey{Scope: scope},
				CommentType: issue,
			}
			if _, style := commentNavMarker(th, &item); style.Fg != th.CommentIssue {
				t.Errorf("scope %v: an ISSUE must use the ISSUE colour, got %v", scope, style.Fg)
			}
		}
	})

	t.Run("remote threads stay muted and neutral", func(t *testing.T) {
		item := app.CommentNavigatorItem{IsRemote: true}
		glyph, style := commentNavMarker(th, &item)
		if glyph != "◇ " {
			t.Errorf("glyph = %q, want the remote diamond", glyph)
		}
		if style.Fg != th.FgDim {
			t.Errorf("colour = %v, want FgDim: a forge thread carries no local type", style.Fg)
		}
	})
}

// TestCommentPeekOverlayRendersContextAndComment covers the peek panel's
// rendering: the commented line with context around it, then the comment,
// inside a bordered box with its own key hint.
func TestCommentPeekOverlayRendersContextAndComment(t *testing.T) {
	_, m := testLifecycle(t)
	moveToDiffLine(t, m)
	pressRune(m, 'c')
	for _, r := range "needs a guard" {
		pressRune(m, r)
	}
	press(m, "", tea.KeyEnter, 0)

	// Fold the file, then peek the comment from the navigator.
	pressRune(m, 'r')
	items := m.App.BuildCommentNavigatorItems()
	if len(items) != 1 {
		t.Fatalf("expected the comment to survive folding, got %d items", len(items))
	}
	if items[0].TargetAnnotation != app.NoTargetAnnotation {
		t.Fatalf("setup: expected a collapsed comment, got %+v", items[0])
	}
	m.App.FocusPanel(app.PanelComments)
	m.App.CommentNav.Cursor = 0
	m.App.CommentNavSelect()
	if m.App.CommentPeek == nil {
		t.Fatal("setup: the peek panel did not open")
	}

	frame := ansiSequence.ReplaceAllString(viewString(m), "")
	for _, want := range []string{
		"needs a guard", // the comment body
		"j/k scroll",    // the panel's own hint
		"Esc close",     //
	} {
		if !strings.Contains(frame, want) {
			t.Errorf("peek frame missing %q:\n%s", want, frame)
		}
	}
	// The status bar names the mode, so the reviewer knows why keys changed.
	if !strings.Contains(frame, "PEEK") {
		t.Errorf("status bar must show PEEK:\n%s", frame)
	}
	// The panel is a peek, not a replacement diff: it stays bounded.
	rows := m.diffPane.commentPeekOverlay(m.App, 80, 40)
	if len(rows) > 18 {
		t.Errorf("panel rendered %d rows; it must stay bounded", len(rows))
	}

	// Esc closes it and leaves the file folded.
	press(m, "", tea.KeyEscape, 0)
	if m.App.CommentPeek != nil {
		t.Error("Esc must close the panel")
	}
	if !m.App.Session.IsFileReviewed(m.App.DiffFiles[0].DisplayPath()) {
		t.Error("peeking must not unmark the file as reviewed")
	}
}

// TestCommentPeekOnAShortTerminalDoesNotPanic: the peek panel is at least
// three rows (two borders and a line) however little room there is, and on
// a terminal of six rows or fewer that overran the diff pane and panicked
// on a negative slice index.
func TestCommentPeekOnAShortTerminalDoesNotPanic(t *testing.T) {
	_, m := testLifecycle(t)
	moveToDiffLine(t, m)
	pressRune(m, 'c')
	for _, r := range "short" {
		pressRune(m, r)
	}
	press(m, "", tea.KeyEnter, 0)
	pressRune(m, 'r') // fold the file: the peek is for comments it hides
	m.App.FocusPanel(app.PanelComments)
	m.App.CommentNav.Cursor = 0
	m.App.CommentNavSelect()
	if m.App.CommentPeek == nil {
		t.Fatal("setup: the peek panel did not open")
	}
	for h := 1; h <= 8; h++ {
		m.width, m.height = 100, h
		m.syncViewport()
		_ = viewString(m) // must not panic
	}
}
