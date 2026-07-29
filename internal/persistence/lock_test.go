package persistence

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/infrashift/mrman/internal/model"
)

// withDuration temporarily overrides a package duration knob.
func withDuration(t *testing.T, target *time.Duration, value time.Duration) {
	t.Helper()
	old := *target
	*target = value
	t.Cleanup(func() { *target = old })
}

// withProcessAlive temporarily overrides pid-liveness probing.
func withProcessAlive(t *testing.T, fake func(pid int) bool) {
	t.Helper()
	old := processAlive
	processAlive = fake
	t.Cleanup(func() { processAlive = old })
}

// newLockTestStore builds a store whose sessions/ dir already exists so the
// pre-created lock files below do not trigger the layout migration.
func newLockTestStore(t *testing.T) *Store {
	t.Helper()
	store := newTestStore(t)
	if err := os.MkdirAll(filepath.Join(store.ReviewsDir, SessionsDirname), 0o755); err != nil {
		t.Fatal(err)
	}
	return store
}

func lockPath(store *Store) string {
	return filepath.Join(store.ReviewsDir, lockFilename)
}

func TestLockReleasedAfterSave(t *testing.T) {
	store := newTestStore(t)
	mustSave(t, store, makeLocalSession(t, makeRepo(t), "abc1234", strp("main"), model.SourceWorkingTree, nil))

	if fileExists(t, lockPath(store)) {
		t.Fatal("lock must be released after save")
	}
}

func TestLockBodyRecordsPidAndTimestamp(t *testing.T) {
	store := newLockTestStore(t)
	release, err := store.acquireLock()
	if err != nil {
		t.Fatalf("acquireLock: %v", err)
	}
	data, err := os.ReadFile(lockPath(store))
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 {
		t.Fatalf("lock body = %q, want \"<pid> <timestamp>\"", data)
	}
	if fields[0] != fmt.Sprint(os.Getpid()) {
		t.Fatalf("lock pid = %q, want %d", fields[0], os.Getpid())
	}
	if _, err := time.Parse(time.RFC3339, fields[1]); err != nil {
		t.Fatalf("lock timestamp %q not RFC3339: %v", fields[1], err)
	}

	release()
	if fileExists(t, lockPath(store)) {
		t.Fatal("release must remove the lock file")
	}
}

func TestRecoverStaleLockWithGarbageContent(t *testing.T) {
	store := newLockTestStore(t)
	writeTestFile(t, lockPath(store), "stale lock")
	// An unparseable lock is only reclaimed after its grace period.
	withDuration(t, &lockUnparseableStaleAfter, 0)

	path := mustSave(t, store, makeLocalSession(t, makeRepo(t), "abc1234", strp("main"), model.SourceWorkingTree, nil))

	if !fileExists(t, path) {
		t.Fatal("save should succeed after reclaiming the stale lock")
	}
	if fileExists(t, lockPath(store)) {
		t.Fatal("stale lock should be gone")
	}
}

func TestKeepFreshGarbageLockUntilGracePeriod(t *testing.T) {
	store := newLockTestStore(t)
	writeTestFile(t, lockPath(store), "stale lock")
	withDuration(t, &lockTimeout, 60*time.Millisecond)
	withDuration(t, &lockRetryInterval, 5*time.Millisecond)

	_, err := store.SaveSession(makeLocalSession(t, makeRepo(t), "abc1234", strp("main"), model.SourceWorkingTree, nil))
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want lock timeout", err)
	}
}

func TestRecoverStaleLockWithDeadPid(t *testing.T) {
	store := newLockTestStore(t)
	writeTestFile(t, lockPath(store), "4194304 2026-01-01T00:00:00Z")
	withProcessAlive(t, func(int) bool { return false })

	path := mustSave(t, store, makeLocalSession(t, makeRepo(t), "abc1234", strp("main"), model.SourceWorkingTree, nil))

	if !fileExists(t, path) {
		t.Fatal("save should succeed after reclaiming a dead pid's lock")
	}
}

func TestRecoverStaleLockOlderThanReuseGuard(t *testing.T) {
	store := newLockTestStore(t)
	// The owner pid is alive (it is us), but the lock is so old the pid may
	// have been recycled: the reuse guard reclaims it.
	writeTestFile(t, lockPath(store), fmt.Sprintf("%d 2026-01-01T00:00:00Z", os.Getpid()))
	old := time.Now().Add(-13 * time.Hour)
	if err := os.Chtimes(lockPath(store), old, old); err != nil {
		t.Fatal(err)
	}

	path := mustSave(t, store, makeLocalSession(t, makeRepo(t), "abc1234", strp("main"), model.SourceWorkingTree, nil))

	if !fileExists(t, path) {
		t.Fatal("save should succeed after reclaiming an ancient lock")
	}
}

func TestLockTimeoutWhenHeldByLivePid(t *testing.T) {
	store := newLockTestStore(t)
	writeTestFile(t, lockPath(store), fmt.Sprintf("%d %s", os.Getpid(), time.Now().Format(time.RFC3339)))
	withDuration(t, &lockTimeout, 60*time.Millisecond)
	withDuration(t, &lockRetryInterval, 5*time.Millisecond)

	_, err := store.SaveSession(makeLocalSession(t, makeRepo(t), "abc1234", strp("main"), model.SourceWorkingTree, nil))
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want lock timeout", err)
	}
	if !fileExists(t, lockPath(store)) {
		t.Fatal("a live holder's lock must not be removed")
	}
}
