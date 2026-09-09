package persistence

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// lockFilename is the advisory lock file guarding mutations of the reviews
// directory (session writes, manifest updates, active-session bookkeeping).
const lockFilename = ".mrman.lock"

// Lock tuning knobs. Vars rather than consts so tests can shrink the
// timeouts; production code never mutates them.
var (
	// lockTimeout bounds how long acquisition retries before giving up.
	lockTimeout = 10 * time.Second
	// lockRetryInterval is the pause between acquisition attempts.
	lockRetryInterval = 25 * time.Millisecond
	// lockUnparseableStaleAfter is when a lock whose owner pid cannot be
	// parsed is considered abandoned.
	lockUnparseableStaleAfter = 60 * time.Second
	// lockReuseGuardAfter is when even a lock with a live owner pid is
	// considered abandoned — the pid may have been recycled.
	lockReuseGuardAfter = 12 * time.Hour
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

// acquireLock creates the lock file with O_CREATE|O_EXCL, retrying every
// lockRetryInterval up to lockTimeout and clearing stale locks along the way.
// The lock body records "<pid> <RFC3339 timestamp>".
func (s *Store) acquireLock() (release func(), err error) {
	if err := os.MkdirAll(s.ReviewsDir, dirMode); err != nil {
		return nil, err
	}
	path := filepath.Join(s.ReviewsDir, lockFilename)
	started := nowFn()
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fileMode)
		if err == nil {
			_, _ = fmt.Fprintf(f, "%d %s\n", os.Getpid(), nowFn().Format(time.RFC3339))
			_ = f.Sync()
			_ = f.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		removed, rerr := removeStaleLock(path)
		if rerr != nil {
			return nil, rerr
		}
		if removed {
			continue
		}
		if nowFn().Sub(started) >= lockTimeout {
			return nil, fmt.Errorf("timed out waiting for review storage lock %s", path)
		}
		time.Sleep(lockRetryInterval)
	}
}

// removeStaleLock deletes the lock file when its owner is provably gone: the
// recorded pid is not alive, the lock is older than the pid-reuse guard, or
// (when no pid can be parsed) the file is older than the unparseable-lock
// grace period. It reports whether a stale lock was removed.
func removeStaleLock(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	lockAge := nowFn().Sub(info.ModTime())

	var stale bool
	if pid, ok := readLockOwnerPid(path); ok {
		stale = !processAlive(pid) || lockAge >= lockReuseGuardAfter
	} else {
		stale = lockAge >= lockUnparseableStaleAfter
	}
	if !stale {
		return false, nil
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// readLockOwnerPid parses the pid from the lock body's first token.
func readLockOwnerPid(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, false
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, false
	}
	return pid, true
}
