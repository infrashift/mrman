package ui

import (
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/theme"
)

// commentedModel adds one line comment to the shared test app with the given
// snapshot content, validates the anchors, and renders a frame.
func commentedModel(t *testing.T, snapshot string) string {
	t.Helper()
	a := testApp(t)
	review := a.Session.File("src/x.go")
	if review == nil {
		t.Fatal("test file missing from session")
	}
	side := model.LineSideNew
	c := model.NewComment("needs work", model.CommentTypeFromID("issue"), &side)
	c.LineContext = &model.LineContext{NewLine: lineno(3), Content: snapshot}
	review.AddLineComment(3, c)

	a.ValidateCommentAnchors()
	a.RebuildAnnotations()

	m := NewModel(a, theme.TokyoNightStorm())
	m.width, m.height = 100, 30
	m.syncViewport()
	return plainView(m)
}

// TestCommentBoxFlagsOutdatedAnchor is what makes a carried-over session safe
// to read: without the badge the stale comment's line number renders exactly
// as confidently as a good one.
func TestCommentBoxFlagsOutdatedAnchor(t *testing.T) {
	view := commentedModel(t, "a line that no longer exists")
	if !strings.Contains(view, "(outdated)") {
		t.Errorf("comment box must carry an (outdated) badge, got:\n%s", view)
	}
}

// TestCommentBoxFlagsMovedAnchor shows the re-anchoring rather than performing
// it silently — the reviewer wrote the comment against a line number and is
// entitled to know it changed.
func TestCommentBoxFlagsMovedAnchor(t *testing.T) {
	// "var A = 10" lives at new line 2, so a comment keyed at 3 gets moved.
	view := commentedModel(t, "var A = 10")
	if !strings.Contains(view, "(moved)") {
		t.Errorf("comment box must carry a (moved) badge, got:\n%s", view)
	}
}

// TestCommentBoxQuietWhenAnchorIntact keeps the badge from becoming noise on
// every comment in an untouched review.
func TestCommentBoxQuietWhenAnchorIntact(t *testing.T) {
	view := commentedModel(t, "var C = 3")
	if strings.Contains(view, "(outdated)") || strings.Contains(view, "(moved)") {
		t.Errorf("an intact anchor must render no badge, got:\n%s", view)
	}
}

// TestReportAnchorValidationSeverity pins how the open path speaks: a
// re-anchoring is news, an unplaceable anchor is a warning.
func TestReportAnchorValidationSeverity(t *testing.T) {
	a := testApp(t)
	a.AnchorStats = app.AnchorStats{Checked: 2, Moved: 1}
	reportAnchorValidation(a)
	if a.Message == nil || a.Message.Type != app.MessageInfo {
		t.Errorf("a re-anchoring should be informational, got %+v", a.Message)
	}

	a = testApp(t)
	a.AnchorStats = app.AnchorStats{Checked: 2, Outdated: 1}
	reportAnchorValidation(a)
	if a.Message == nil || a.Message.Type != app.MessageWarning {
		t.Errorf("an unplaceable anchor should warn, got %+v", a.Message)
	}

	a = testApp(t)
	a.AnchorStats = app.AnchorStats{Checked: 2}
	reportAnchorValidation(a)
	if a.Message != nil {
		t.Errorf("a clean pass must say nothing, got %+v", a.Message)
	}
}
