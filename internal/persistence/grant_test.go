package persistence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

// grantStore returns a store with one saved session, plus its path.
func grantStore(t *testing.T) (*Store, *model.ReviewSession, string) {
	t.Helper()
	store := &Store{ReviewsDir: filepath.Join(t.TempDir(), "reviews")}
	session := model.NewReviewSession("/repo", "abc1234", nil, model.SourceWorkingTree)
	path, err := store.SaveSession(session)
	if err != nil {
		t.Fatalf("save session: %v", err)
	}
	return store, session, path
}

func TestNoGrantByDefault(t *testing.T) {
	store, session, path := grantStore(t)
	// An ordinary active session — the TUI without --auto — grants nothing.
	if err := store.MarkSessionActive(session, path); err != nil {
		t.Fatal(err)
	}
	granted, reason, err := store.AgentSubmitGrant(path, "comment")
	if err != nil {
		t.Fatal(err)
	}
	if reason != GrantNone || len(granted) != 0 {
		t.Errorf("an un-granted session must report no_grant, got %q %v", reason, granted)
	}
}

func TestGrantAuthorizesOnlyItsEvents(t *testing.T) {
	store, session, path := grantStore(t)
	if err := store.MarkSessionActiveWithGrant(session, path, []string{"comment", "draft"}); err != nil {
		t.Fatal(err)
	}

	for _, event := range []string{"comment", "draft"} {
		if _, reason, _ := store.AgentSubmitGrant(path, event); reason != GrantOK {
			t.Errorf("%s must be authorized, got %q", event, reason)
		}
	}
	for _, event := range []string{"approve", "request-changes"} {
		granted, reason, _ := store.AgentSubmitGrant(path, event)
		if reason != GrantEventNotAllowed {
			t.Errorf("%s must be refused, got %q", event, reason)
		}
		if len(granted) != 2 {
			t.Errorf("the refusal must still report what is granted, got %v", granted)
		}
	}
}

// TestGrantDiesWithItsProcess is the property the whole interlock rests on:
// authority is held against a live pid, so closing the TUI revokes it with
// no teardown step that could be skipped or crash.
func TestGrantDiesWithItsProcess(t *testing.T) {
	store, session, path := grantStore(t)
	if err := store.MarkSessionActiveWithGrant(session, path, []string{"comment"}); err != nil {
		t.Fatal(err)
	}
	if _, reason, _ := store.AgentSubmitGrant(path, "comment"); reason != GrantOK {
		t.Fatalf("expected a live grant, got %q", reason)
	}

	orphanGrant(t, store)

	_, reason, err := store.AgentSubmitGrant(path, "comment")
	if err != nil {
		t.Fatal(err)
	}
	if reason != GrantExpired {
		t.Errorf("a grant whose process is gone must read as expired, got %q", reason)
	}
}

func TestExpiredIsDistinctFromNeverGranted(t *testing.T) {
	store, session, path := grantStore(t)

	// Never granted.
	if _, reason, _ := store.AgentSubmitGrant(path, "comment"); reason != GrantNone {
		t.Errorf("want no_grant, got %q", reason)
	}

	// Granted, then orphaned. The agent deserves to know the difference:
	// one means "ask the user to enable it", the other "your session ended".
	if err := store.MarkSessionActiveWithGrant(session, path, []string{"comment"}); err != nil {
		t.Fatal(err)
	}
	orphanGrant(t, store)
	if _, reason, _ := store.AgentSubmitGrant(path, "comment"); reason != GrantExpired {
		t.Errorf("want grant_expired, got %q", reason)
	}
}

// TestOrphanedEntryWithoutAGrantIsNotExpired keeps the two states honest:
// a dead session that never carried a grant is still "no grant", not
// "expired", or every stale entry would masquerade as revoked authority.
func TestOrphanedEntryWithoutAGrantIsNotExpired(t *testing.T) {
	store, session, path := grantStore(t)
	if err := store.MarkSessionActive(session, path); err != nil {
		t.Fatal(err)
	}
	orphanGrant(t, store)

	if _, reason, _ := store.AgentSubmitGrant(path, "comment"); reason != GrantNone {
		t.Errorf("want no_grant for a dead un-granted session, got %q", reason)
	}
}

func TestRevokeGrantKeepsTheSessionActive(t *testing.T) {
	store, session, path := grantStore(t)
	if err := store.MarkSessionActiveWithGrant(session, path, []string{"comment"}); err != nil {
		t.Fatal(err)
	}

	if err := store.RevokeGrantForPid(); err != nil {
		t.Fatal(err)
	}
	if _, reason, _ := store.AgentSubmitGrant(path, "comment"); reason != GrantNone {
		t.Errorf("revoking must remove authority, got %q", reason)
	}
	// The review is still open; only the grant went away.
	paths, err := store.ActiveSessionPaths()
	if err != nil {
		t.Fatal(err)
	}
	if !paths[normalizeActivePath(path)] {
		t.Error("revoking a grant must not close the session")
	}
}

func TestRevokeIsSafeWithNothingToRevoke(t *testing.T) {
	store, _, _ := grantStore(t)
	if err := store.RevokeGrantForPid(); err != nil {
		t.Errorf("revoking nothing must not fail: %v", err)
	}
}

func TestGrantedEventsForPath(t *testing.T) {
	store, session, path := grantStore(t)
	if got := store.GrantedEventsForPath(path); len(got) != 0 {
		t.Errorf("no session, no grant: %v", got)
	}
	if err := store.MarkSessionActiveWithGrant(session, path, []string{"comment"}); err != nil {
		t.Fatal(err)
	}
	if got := store.GrantedEventsForPath(path); len(got) != 1 || got[0] != "comment" {
		t.Errorf("granted = %v, want [comment]", got)
	}
}

func TestGrantDoesNotLeakToOtherSessions(t *testing.T) {
	store, session, path := grantStore(t)
	if err := store.MarkSessionActiveWithGrant(session, path, []string{"approve"}); err != nil {
		t.Fatal(err)
	}
	other := model.NewReviewSession("/other", "def5678", nil, model.SourceWorkingTree)
	otherPath, err := store.SaveSession(other)
	if err != nil {
		t.Fatal(err)
	}

	if _, reason, _ := store.AgentSubmitGrant(otherPath, "approve"); reason != GrantNone {
		t.Errorf("a grant is per-session; the other session reported %q", reason)
	}
}

// TestEntriesWrittenBeforeGrantsExistedLoadCleanly covers the upgrade path:
// an active_sessions.json from an older mrman has no granted_events, and
// must read as "no grant" rather than failing to parse.
func TestEntriesWrittenBeforeGrantsExistedLoadCleanly(t *testing.T) {
	store, session, path := grantStore(t)
	if err := store.MarkSessionActive(session, path); err != nil {
		t.Fatal(err)
	}
	stripGrantField(t, store)

	if _, reason, err := store.AgentSubmitGrant(path, "comment"); err != nil {
		t.Fatalf("an older file must still load: %v", err)
	} else if reason != GrantNone {
		t.Errorf("want no_grant, got %q", reason)
	}
}

// orphanGrant rewrites every entry's pid to one that cannot be running.
func orphanGrant(t *testing.T, store *Store) {
	t.Helper()
	rewriteActive(t, store, func(entry map[string]any) { entry["pid"] = 0 })
}

// stripGrantField removes granted_events, as an older mrman would have.
func stripGrantField(t *testing.T, store *Store) {
	t.Helper()
	rewriteActive(t, store, func(entry map[string]any) { delete(entry, "granted_events") })
}

func rewriteActive(t *testing.T, store *Store, edit func(map[string]any)) {
	t.Helper()
	path := store.activeSessionsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read active sessions: %v", err)
	}
	var file struct {
		Version  string           `json:"version"`
		Sessions []map[string]any `json:"sessions"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("parse active sessions: %v", err)
	}
	for i := range file.Sessions {
		edit(file.Sessions[i])
	}
	out, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}
