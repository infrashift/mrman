package persistence

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
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

// lockIsFree reports whether the store lock can be taken right now.
func lockIsFree(t *testing.T, store *Store) bool {
	t.Helper()
	withDuration(t, &lockTimeout, 0)
	release, err := store.acquireLock()
	if err != nil {
		return false
	}
	release()
	return true
}

func TestLockReleasedAfterSave(t *testing.T) {
	store := newTestStore(t)
	mustSave(t, store, makeLocalSession(t, makeRepo(t), "abc1234", new("main"), model.SourceWorkingTree, nil))

	if !lockIsFree(t, store) {
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
	if !lockIsFree(t, store) {
		t.Fatal("release must free the lock")
	}
}

// TestLockTimesOutWhileHeld: a second writer waits for the holder and gives
// up after lockTimeout, leaving the holder's lock in place.
func TestLockTimesOutWhileHeld(t *testing.T) {
	store := newLockTestStore(t)
	release, err := store.acquireLock()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	withDuration(t, &lockTimeout, 60*time.Millisecond)
	withDuration(t, &lockRetryInterval, 5*time.Millisecond)

	_, err = store.SaveSession(makeLocalSession(t, makeRepo(t), "abc1234", new("main"), model.SourceWorkingTree, nil))
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want lock timeout", err)
	}
}

// TestLegacyPidLockDoesNotBlock: a .mrman.lock pid file left by an older
// release, even one naming a live pid, is not this lock.
func TestLegacyPidLockDoesNotBlock(t *testing.T) {
	store := newLockTestStore(t)
	writeTestFile(t, filepath.Join(store.ReviewsDir, ".mrman.lock"),
		fmt.Sprintf("%d %s", os.Getpid(), time.Now().Format(time.RFC3339)))

	if !lockIsFree(t, store) {
		t.Fatal("a legacy pid file must not hold the lock")
	}
}

// lockHolderEnv makes the test binary, re-executed by
// TestLockFreedWhenHolderDies, take the lock in the given reviews dir and
// hold it until killed.
const lockHolderEnv = "MRMAN_TEST_HOLD_LOCK_IN"

func TestMain(m *testing.M) {
	if dir := os.Getenv(lockHolderEnv); dir != "" {
		if _, err := (&Store{ReviewsDir: dir}).acquireLock(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		fmt.Println("held")
		select {} // hold until killed
	}
	os.Exit(m.Run())
}

// TestLockFreedWhenHolderDies is the case the pid file needed reaping for:
// a writer killed while holding the lock. The kernel releases it with the
// process, so the next writer gets it with no staleness rule involved.
func TestLockFreedWhenHolderDies(t *testing.T) {
	store := newLockTestStore(t)
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), lockHolderEnv+"="+store.ReviewsDir)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "held" {
		_ = cmd.Process.Kill()
		t.Fatalf("holder did not take the lock: %q, %v", line, err)
	}
	if lockIsFree(t, store) {
		_ = cmd.Process.Kill()
		t.Fatal("another process holds the lock, yet it was free")
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()

	withDuration(t, &lockTimeout, 5*time.Second)
	release, err := store.acquireLock()
	if err != nil {
		t.Fatalf("lock not freed after its holder died: %v", err)
	}
	release()
}
