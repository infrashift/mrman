package ui

import (
	"os"
	"testing"
)

// TestMain stubs the clipboard for every test in the package. The real
// copy path needs a terminal to talk to, and refusing to write escape
// sequences into the test runner's stdout is the behaviour we want.
func TestMain(m *testing.M) {
	copyText = func(string) (bool, error) { return false, nil }
	os.Exit(m.Run())
}
