package persistence

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// lockFilename is the file whose kernel lock guards mutations of the reviews
// directory (session writes, manifest updates, active-session bookkeeping).
//
// The file is permanent; holding the lock means holding an exclusive flock
// (LockFileEx on Windows) on it. The kernel drops the lock when its holder
// exits, however it exits, so there is no stale lock to detect or reap.
// Older releases used a pid file named .mrman.lock created with O_EXCL and
// deleted on release; reaping a stale one let two writers each delete the
// other's lock and proceed together. The new name keeps an older binary
// from mistaking this permanent file for one of its own stale locks.
const lockFilename = ".mrman.flock"

// Lock tuning knobs. Vars rather than consts so tests can shrink the
// timeouts; production code never mutates them.
var (
	// lockTimeout bounds how long acquisition retries before giving up.
	lockTimeout = 10 * time.Second
	// lockRetryInterval is the pause between acquisition attempts.
	lockRetryInterval = 25 * time.Millisecond
)

// processAlive reports whether pid refers to a running process. It is a var
// so tests can fake liveness; the default is platform-specific (see
// proc_unix.go / proc_other.go).
var processAlive = defaultProcessAlive

// withLock runs fn while holding the reviews-dir lock, releasing it on
// return. All mutating store operations funnel through this.
func (s *Store) withLock(fn func() error) error {
	release, err := s.acquireLock()
	if err != nil {
		return err
	}
	defer release()
	return fn()
}

// acquireLock takes the exclusive kernel lock on the lock file, retrying
// every lockRetryInterval up to lockTimeout. Each acquisition opens the file
// afresh, so two acquisitions in one process exclude each other as well.
//
// The body records "<pid> <RFC3339 timestamp>" of the current holder, for a
// person wondering what a timed-out writer was waiting on; the lock itself
// never reads it.
func (s *Store) acquireLock() (release func(), err error) {
	if err := os.MkdirAll(s.ReviewsDir, dirMode); err != nil {
		return nil, err
	}
	path := filepath.Join(s.ReviewsDir, lockFilename)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, fileMode)
	if err != nil {
		return nil, err
	}
	started := nowFn()
	for {
		locked, err := tryLockFile(f)
		if err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("locking review storage %s: %w", path, err)
		}
		if locked {
			break
		}
		if nowFn().Sub(started) >= lockTimeout {
			_ = f.Close()
			return nil, fmt.Errorf("timed out waiting for review storage lock %s", path)
		}
		time.Sleep(lockRetryInterval)
	}
	if err := f.Truncate(0); err == nil {
		_, _ = fmt.Fprintf(f, "%d %s\n", os.Getpid(), nowFn().Format(time.RFC3339))
	}
	return func() {
		_ = unlockFile(f)
		_ = f.Close()
	}, nil
}
