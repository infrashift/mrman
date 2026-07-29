//go:build unix

package persistence

import (
	"errors"
	"syscall"
)

// defaultProcessAlive probes pid liveness with signal 0. EPERM means the
// process exists but belongs to another user, so it counts as alive.
func defaultProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM)
}
