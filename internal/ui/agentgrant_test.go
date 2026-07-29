package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/persistence"
	"github.com/infrashift/mrman/internal/theme"
)

func TestAgentCommandReportsNoGrant(t *testing.T) {
	m := testModel(t)
	m.runCommand(input.ParseCommand("agent"))
	if m.App.Message == nil || !strings.Contains(m.App.Message.Content, "off") {
		t.Errorf(":agent with no grant must say so, got %+v", m.App.Message)
	}
}

func TestAgentCommandReportsAndRevokesAGrant(t *testing.T) {
	m := testModel(t)
	store := &persistence.Store{ReviewsDir: filepath.Join(t.TempDir(), "reviews")}
	session := model.NewReviewSession("/repo", "abc", nil, model.SourceWorkingTree)
	path, err := store.SaveSession(session)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkSessionActiveWithGrant(session, path, []string{"comment", "draft"}); err != nil {
		t.Fatal(err)
	}
	m.store = store
	m.grantedEvents = []string{"comment", "draft"}

	m.runCommand(input.ParseCommand("agent"))
	if m.App.Message == nil || !strings.Contains(m.App.Message.Content, "comment, draft") {
		t.Errorf(":agent must name the granted events, got %+v", m.App.Message)
	}

	m.runCommand(input.ParseCommand("agent off"))
	if len(m.grantedEvents) != 0 {
		t.Error(":agent off must drop the grant in this process")
	}
	if _, reason, _ := store.AgentSubmitGrant(path, "comment"); reason != persistence.GrantNone {
		t.Errorf(":agent off must revoke in the registry too, got %q", reason)
	}

	// Revoking twice is harmless.
	m.runCommand(input.ParseCommand("agent off"))
	if m.App.Message == nil || !strings.Contains(m.App.Message.Content, "already off") {
		t.Errorf("a second revoke should say it is already off, got %+v", m.App.Message)
	}
}

// TestHeaderShowsALiveGrant pins the visibility half of the interlock: a
// human must be able to see, at a glance and without it expiring, that an
// agent may submit on their behalf.
func TestHeaderShowsALiveGrant(t *testing.T) {
	a := testApp(t)
	tm := theme.TokyoNightStorm()

	plain := ansiSequence.ReplaceAllString(HeaderWithGrant(a, tm, 160, nil), "")
	if strings.Contains(plain, "AGENT SUBMIT") {
		t.Error("no grant must show no chip")
	}

	plain = ansiSequence.ReplaceAllString(
		HeaderWithGrant(a, tm, 160, []string{"comment", "draft"}), "")
	if !strings.Contains(plain, "AGENT SUBMIT: comment,draft") {
		t.Errorf("a live grant must be visible in the header, got %q", plain)
	}
}
