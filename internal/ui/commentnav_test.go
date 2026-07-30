package ui

import (
	"testing"

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
