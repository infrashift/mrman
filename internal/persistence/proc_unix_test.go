//go:build unix

package persistence

import (
	"os"
	"testing"
)

func TestDefaultProcessAliveProbesRealPids(t *testing.T) {
	if !defaultProcessAlive(os.Getpid()) {
		t.Fatal("our own pid must be alive")
	}
	if defaultProcessAlive(0) {
		t.Fatal("pid 0 must not count as alive")
	}
	if defaultProcessAlive(-42) {
		t.Fatal("negative pids must not count as alive")
	}
}
