package version

import "testing"

func TestString(t *testing.T) {
	if got, want := String(), "dev (none, unknown)"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
