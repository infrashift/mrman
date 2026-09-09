//go:build windows

package persistence

import (
	"os"
	"os/exec"
	"testing"
)

func TestDefaultProcessAliveWindows(t *testing.T) {
	if !defaultProcessAlive(os.Getpid()) {
		t.Fatal("our own pid must be alive")
	}
	if defaultProcessAlive(0) || defaultProcessAlive(-1) {
		t.Fatal("non-positive pids are never alive")
	}
	cmd := exec.Command("cmd", "/C", "exit 0")
	if err := cmd.Run(); err != nil {
		t.Skipf("cannot spawn a child: %v", err)
	}
	if defaultProcessAlive(cmd.Process.Pid) {
		t.Fatal("an exited child must not read as alive")
	}
}
