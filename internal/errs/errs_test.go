package errs

import (
	"errors"
	"fmt"
	"testing"
)

func TestUnsupportedfWrapsSentinel(t *testing.T) {
	err := Unsupportedf("commit range diff for %s", "gitlab")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatal("Unsupportedf must wrap ErrUnsupported")
	}
	want := "unsupported operation: commit range diff for gitlab"
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestSentinelsSurviveWrapping(t *testing.T) {
	for _, sentinel := range []error{ErrNotARepository, ErrNoChanges, ErrNoComments, ErrUnsupported} {
		wrapped := fmt.Errorf("context: %w", sentinel)
		if !errors.Is(wrapped, sentinel) {
			t.Errorf("wrapped %v not matched by errors.Is", sentinel)
		}
	}
}

func TestStructuredErrorMessages(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{&CorruptedSession{Detail: "bad json"}, "review session corrupted: bad json"},
		{&VcsCommand{Detail: "git failed"}, "VCS command failed: git failed"},
		{&InvalidInput{Detail: "line 0"}, "invalid input: line 0"},
		{&Clipboard{Detail: "no backend"}, "clipboard error: no backend"},
	}
	for _, c := range cases {
		if c.err.Error() != c.want {
			t.Errorf("Error() = %q, want %q", c.err.Error(), c.want)
		}
	}
}
