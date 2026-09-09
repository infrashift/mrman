//go:build windows

package persistence

import (
	"errors"

	"golang.org/x/sys/windows"
)

// defaultProcessAlive asks the kernel whether pid still runs. An agent
// submit grant and the store lock are both keyed to a live process, so
// "always alive" — the previous behaviour here — meant a crashed TUI left
// its grant and its lock in force for the twelve-hour age guard.
//
// Only a failure of the probe itself counts as alive: refusing to open a
// process we lack rights to still proves it exists.
func defaultProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid)) //nolint:gosec // pid is a process id, not attacker-controlled
	if err != nil {
		// ERROR_INVALID_PARAMETER is what a pid that no longer exists gets.
		return !errors.Is(err, windows.ERROR_INVALID_PARAMETER)
	}
	defer func() { _ = windows.CloseHandle(h) }()
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return true
	}
	return code == uint32(windows.STATUS_PENDING) // STILL_ACTIVE
}
