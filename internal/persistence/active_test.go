package persistence

import (
	"os"
	"testing"
	"time"

	"github.com/infrashift/mrman/internal/model"
)

// withNow temporarily overrides the package clock.
func withNow(t *testing.T, fake func() time.Time) {
	t.Helper()
	old := nowFn
	nowFn = fake
	t.Cleanup(func() { nowFn = old })
}

func TestMarkAndClearActiveSession(t *testing.T) {
	store := newTestStore(t)
	sess := makeLocalSession(t, makeRepo(t), "abc1234", new("main"), model.SourceWorkingTree, nil)
	path := mustSave(t, store, sess)

	if err := store.MarkSessionActive(sess, path); err != nil {
		t.Fatalf("MarkSessionActive: %v", err)
	}
	active, err := store.ActiveSessionPaths()
	if err != nil {
		t.Fatal(err)
	}
	if !active[normalizeActivePath(path)] {
		t.Fatal("marked session should be active")
	}

	if err := store.ClearActiveSessionForPid(); err != nil {
		t.Fatalf("ClearActiveSessionForPid: %v", err)
	}
	active, err = store.ActiveSessionPaths()
	if err != nil {
		t.Fatal(err)
	}
	if active[normalizeActivePath(path)] {
		t.Fatal("cleared session should not be active")
	}
}

func TestMarkSessionActiveSupersedesSamePathEntry(t *testing.T) {
	store := newTestStore(t)
	sess := makeLocalSession(t, makeRepo(t), "abc1234", new("main"), model.SourceWorkingTree, nil)
	path := mustSave(t, store, sess)

	// A previous (fresh, live) entry from another pid for the same path is
	// superseded by our mark.
	otherPid := os.Getpid() + 1
	seed := activeSessionsFile{
		Version: activeSessionsVersion,
		Sessions: []activeSessionEntry{{
			Pid:        otherPid,
			Slug:       "stale-slug",
			Path:       normalizeActivePath(path),
			LastSeenAt: time.Now(),
		}},
	}
	data, err := marshalPretty(seed)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, store.activeSessionsPath(), string(data))
	withProcessAlive(t, func(int) bool { return true })

	if err := store.MarkSessionActive(sess, path); err != nil {
		t.Fatalf("MarkSessionActive: %v", err)
	}

	active := store.loadActiveSessionsOrDefault()
	if len(active.Sessions) != 1 {
		t.Fatalf("have %d entries, want the superseded single entry", len(active.Sessions))
	}
	if active.Sessions[0].Pid != os.Getpid() {
		t.Fatalf("entry pid = %d, want %d", active.Sessions[0].Pid, os.Getpid())
	}
}

func TestActiveSessionStaleAfterAge(t *testing.T) {
	store := newTestStore(t)
	sess := makeLocalSession(t, makeRepo(t), "abc1234", new("main"), model.SourceWorkingTree, nil)
	path := mustSave(t, store, sess)
	if err := store.MarkSessionActive(sess, path); err != nil {
		t.Fatal(err)
	}

	withNow(t, func() time.Time { return time.Now().Add(13 * time.Hour) })

	active, err := store.ActiveSessionPaths()
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("aged-out entries must not be active, got %v", active)
	}
}

func TestActiveSessionStaleWhenHeartbeatInFuture(t *testing.T) {
	store := newTestStore(t)
	sess := makeLocalSession(t, makeRepo(t), "abc1234", new("main"), model.SourceWorkingTree, nil)
	path := mustSave(t, store, sess)

	seed := activeSessionsFile{
		Version: activeSessionsVersion,
		Sessions: []activeSessionEntry{{
			Pid:        os.Getpid(),
			Slug:       "s",
			Path:       normalizeActivePath(path),
			LastSeenAt: time.Now().Add(time.Hour),
		}},
	}
	data, err := marshalPretty(seed)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, store.activeSessionsPath(), string(data))

	active, err := store.ActiveSessionPaths()
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("future heartbeats must not be fresh, got %v", active)
	}
}

func TestActiveSessionStaleWhenProcessDead(t *testing.T) {
	store := newTestStore(t)
	sess := makeLocalSession(t, makeRepo(t), "abc1234", new("main"), model.SourceWorkingTree, nil)
	path := mustSave(t, store, sess)
	if err := store.MarkSessionActive(sess, path); err != nil {
		t.Fatal(err)
	}

	withProcessAlive(t, func(int) bool { return false })

	active, err := store.ActiveSessionPaths()
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("dead owners must not be active, got %v", active)
	}
}

func TestActiveSessionStaleWhenPathMissing(t *testing.T) {
	store := newTestStore(t)
	sess := makeLocalSession(t, makeRepo(t), "abc1234", new("main"), model.SourceWorkingTree, nil)
	path := mustSave(t, store, sess)
	if err := store.MarkSessionActive(sess, path); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteSessionIfEmpty(path); err != nil {
		t.Fatal(err)
	}

	active, err := store.ActiveSessionPaths()
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("entries whose session file is gone must not be active, got %v", active)
	}
}

func TestActiveSessionsFileCorruptRecovers(t *testing.T) {
	store := newTestStore(t)
	sess := makeLocalSession(t, makeRepo(t), "abc1234", new("main"), model.SourceWorkingTree, nil)
	path := mustSave(t, store, sess)
	writeTestFile(t, store.activeSessionsPath(), "not json {")

	if err := store.MarkSessionActive(sess, path); err != nil {
		t.Fatalf("MarkSessionActive should recover from corruption: %v", err)
	}
	active, err := store.ActiveSessionPaths()
	if err != nil {
		t.Fatal(err)
	}
	if !active[normalizeActivePath(path)] {
		t.Fatal("marked session should be active after recovery")
	}
}

// TestTouchKeepsALongRunningSessionFresh: a TUI open for longer than the
// staleness window keeps its entry, and its grant, by heartbeating.
func TestTouchKeepsALongRunningSessionFresh(t *testing.T) {
	store := newTestStore(t)
	sess := makeLocalSession(t, makeRepo(t), "abc1234", new("main"), model.SourceWorkingTree, nil)
	path := mustSave(t, store, sess)

	now := time.Now()
	withNow(t, func() time.Time { return now })
	if err := store.MarkSessionActiveWithGrant(sess, path, []string{"comment"}); err != nil {
		t.Fatal(err)
	}

	// Two thirds of the way through the window: heartbeat.
	now = now.Add(activeSessionStaleAfter * 2 / 3)
	if err := store.TouchActiveSession(); err != nil {
		t.Fatal(err)
	}
	// Past the original expiry, but inside the refreshed one.
	now = now.Add(activeSessionStaleAfter * 2 / 3)
	if _, reason, err := store.AgentSubmitGrant(path, "comment"); err != nil || reason != GrantOK {
		t.Fatalf("grant after heartbeat: reason=%q err=%v", reason, err)
	}
	active, err := store.ActiveSessionPaths()
	if err != nil || !active[normalizeActivePath(path)] {
		t.Fatalf("session should still be active after a heartbeat (err=%v)", err)
	}

	// Without a further heartbeat the window does expire.
	now = now.Add(activeSessionStaleAfter)
	if _, reason, _ := store.AgentSubmitGrant(path, "comment"); reason != GrantExpired {
		t.Fatalf("reason = %q, want expired once heartbeats stop", reason)
	}
}
