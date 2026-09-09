package output

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/errs"
)

// cmdCall records one clipboard subprocess invocation.
type cmdCall struct {
	name  string
	args  []string
	stdin string
}

// ttyBuffer is an in-memory stand-in for /dev/tty.
type ttyBuffer struct {
	strings.Builder
	closed bool
}

func (b *ttyBuffer) Close() error {
	b.closed = true
	return nil
}

// stubClipboard swaps every clipboard seam for the test's duration.
// failCmds maps command names that should report failure.
func stubClipboard(t *testing.T, goos string, env map[string]string,
	failCmds map[string]bool) (*[]cmdCall, *ttyBuffer) {
	t.Helper()
	origGoos, origEnv, origRun, origTTY := goosFn, lookupEnv, runClipboardCmd, openTTY
	t.Cleanup(func() {
		goosFn, lookupEnv, runClipboardCmd, openTTY = origGoos, origEnv, origRun, origTTY
	})

	calls := &[]cmdCall{}
	tty := &ttyBuffer{}
	goosFn = func() string { return goos }
	lookupEnv = func(key string) (string, bool) {
		v, ok := env[key]
		return v, ok
	}
	runClipboardCmd = func(name string, args []string, stdin string) error {
		*calls = append(*calls, cmdCall{name: name, args: args, stdin: stdin})
		if failCmds[name] {
			return fmt.Errorf("%s: command failed", name)
		}
		return nil
	}
	openTTY = func() (io.WriteCloser, error) { return tty, nil }
	return calls, tty
}

func TestCopyTextDarwinUsesPbcopy(t *testing.T) {
	calls, tty := stubClipboard(t, "darwin", map[string]string{"TMUX": "1"}, nil)

	viaTerminal, err := CopyText("hello")
	if err != nil || viaTerminal {
		t.Fatalf("CopyText = (%v, %v), want (false, nil)", viaTerminal, err)
	}
	want := []cmdCall{{name: "pbcopy", stdin: "hello"}}
	if !reflect.DeepEqual(*calls, want) {
		t.Errorf("calls = %+v, want %+v", *calls, want)
	}
	if tty.Len() != 0 {
		t.Errorf("tty must be untouched, got %q", tty.String())
	}
}

func TestCopyTextDarwinPbcopyFailureFallsThrough(t *testing.T) {
	calls, _ := stubClipboard(t, "darwin",
		map[string]string{"XDG_SESSION_TYPE": "wayland"}, map[string]bool{"pbcopy": true})

	viaTerminal, err := CopyText("hello")
	if err != nil || viaTerminal {
		t.Fatalf("CopyText = (%v, %v), want (false, nil)", viaTerminal, err)
	}
	want := []cmdCall{
		{name: "pbcopy", stdin: "hello"},
		{name: "wl-copy", stdin: "hello"},
	}
	if !reflect.DeepEqual(*calls, want) {
		t.Errorf("calls = %+v, want %+v", *calls, want)
	}
}

func TestCopyTextTmuxUsesLoadBuffer(t *testing.T) {
	calls, tty := stubClipboard(t, "linux", map[string]string{"TMUX": "/tmp/tmux-1000/default,42,0"}, nil)

	viaTerminal, err := CopyText("hello")
	if err != nil || !viaTerminal {
		t.Fatalf("CopyText = (%v, %v), want (true, nil)", viaTerminal, err)
	}
	want := []cmdCall{{name: "tmux", args: []string{"load-buffer", "-w", "-"}, stdin: "hello"}}
	if !reflect.DeepEqual(*calls, want) {
		t.Errorf("calls = %+v, want %+v", *calls, want)
	}
	if tty.Len() != 0 {
		t.Errorf("tty must be untouched, got %q", tty.String())
	}
}

func TestCopyTextTmuxMissingBinaryWritesPassthroughEscape(t *testing.T) {
	_, tty := stubClipboard(t, "linux",
		map[string]string{"TMUX": "1"}, map[string]bool{"tmux": true})

	viaTerminal, err := CopyText("hello")
	if err != nil || !viaTerminal {
		t.Fatalf("CopyText = (%v, %v), want (true, nil)", viaTerminal, err)
	}
	want := tmuxWrap(osc52Sequence("hello"))
	if tty.String() != want {
		t.Errorf("tty = %q, want %q", tty.String(), want)
	}
	if !tty.closed {
		t.Error("tty must be closed after writing")
	}
}

func TestCopyTextSSHUsesRawOSC52(t *testing.T) {
	calls, tty := stubClipboard(t, "linux", map[string]string{"SSH_TTY": "/dev/pts/0"}, nil)

	viaTerminal, err := CopyText("hello")
	if err != nil || !viaTerminal {
		t.Fatalf("CopyText = (%v, %v), want (true, nil)", viaTerminal, err)
	}
	if len(*calls) != 0 {
		t.Errorf("no subprocess expected, got %+v", *calls)
	}
	if tty.String() != osc52Sequence("hello") {
		t.Errorf("tty = %q", tty.String())
	}
}

func TestCopyTextZellijUsesRawOSC52(t *testing.T) {
	_, tty := stubClipboard(t, "linux", map[string]string{"ZELLIJ": "0"}, nil)

	viaTerminal, err := CopyText("hello")
	if err != nil || !viaTerminal {
		t.Fatalf("CopyText = (%v, %v), want (true, nil)", viaTerminal, err)
	}
	if tty.String() != osc52Sequence("hello") {
		t.Errorf("tty = %q", tty.String())
	}
}

func TestCopyTextWaylandUsesWlCopy(t *testing.T) {
	calls, _ := stubClipboard(t, "linux", map[string]string{"XDG_SESSION_TYPE": "wayland"}, nil)

	viaTerminal, err := CopyText("hello")
	if err != nil || viaTerminal {
		t.Fatalf("CopyText = (%v, %v), want (false, nil)", viaTerminal, err)
	}
	want := []cmdCall{{name: "wl-copy", stdin: "hello"}}
	if !reflect.DeepEqual(*calls, want) {
		t.Errorf("calls = %+v, want %+v", *calls, want)
	}
}

func TestCopyTextX11UsesXclip(t *testing.T) {
	calls, _ := stubClipboard(t, "linux", map[string]string{"XDG_SESSION_TYPE": "x11"}, nil)

	viaTerminal, err := CopyText("hello")
	if err != nil || viaTerminal {
		t.Fatalf("CopyText = (%v, %v), want (false, nil)", viaTerminal, err)
	}
	want := []cmdCall{{name: "xclip", args: []string{"-selection", "clipboard"}, stdin: "hello"}}
	if !reflect.DeepEqual(*calls, want) {
		t.Errorf("calls = %+v, want %+v", *calls, want)
	}
}

func TestCopyTextMissingWlCopyFallsBackToOSC52(t *testing.T) {
	_, tty := stubClipboard(t, "linux",
		map[string]string{"XDG_SESSION_TYPE": "wayland"}, map[string]bool{"wl-copy": true})

	viaTerminal, err := CopyText("hello")
	if err != nil || !viaTerminal {
		t.Fatalf("CopyText = (%v, %v), want (true, nil)", viaTerminal, err)
	}
	if tty.String() != osc52Sequence("hello") {
		t.Errorf("tty = %q", tty.String())
	}
}

func TestCopyTextNoSessionTypeFallsBackToOSC52(t *testing.T) {
	calls, tty := stubClipboard(t, "linux", nil, nil)

	viaTerminal, err := CopyText("hello")
	if err != nil || !viaTerminal {
		t.Fatalf("CopyText = (%v, %v), want (true, nil)", viaTerminal, err)
	}
	if len(*calls) != 0 {
		t.Errorf("no subprocess expected, got %+v", *calls)
	}
	if tty.String() != osc52Sequence("hello") {
		t.Errorf("tty = %q", tty.String())
	}
}

func TestCopyTextUnsupportedSessionTypeFallsBackToOSC52(t *testing.T) {
	_, tty := stubClipboard(t, "linux", map[string]string{"XDG_SESSION_TYPE": "tty"}, nil)

	viaTerminal, err := CopyText("hello")
	if err != nil || !viaTerminal {
		t.Fatalf("CopyText = (%v, %v), want (true, nil)", viaTerminal, err)
	}
	if tty.String() != osc52Sequence("hello") {
		t.Errorf("tty = %q", tty.String())
	}
}

func TestCopyTextTTYOpenFailureIsClipboardError(t *testing.T) {
	stubClipboard(t, "linux", map[string]string{"SSH_TTY": "/dev/pts/0"}, nil)
	openTTY = func() (io.WriteCloser, error) { return nil, errors.New("no tty") }

	_, err := CopyText("hello")
	var clipErr *errs.Clipboard
	if !errors.As(err, &clipErr) {
		t.Fatalf("want *errs.Clipboard, got %v", err)
	}
}

// failingWriter always errors on write.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }
func (failingWriter) Close() error              { return nil }

func TestCopyTextTTYWriteFailureIsClipboardError(t *testing.T) {
	stubClipboard(t, "linux", map[string]string{"SSH_TTY": "/dev/pts/0"}, nil)
	openTTY = func() (io.WriteCloser, error) { return failingWriter{}, nil }

	_, err := CopyText("hello")
	var clipErr *errs.Clipboard
	if !errors.As(err, &clipErr) {
		t.Fatalf("want *errs.Clipboard, got %v", err)
	}
}

func TestEnvSetTreatsEmptyValueAsSet(t *testing.T) {
	stubClipboard(t, "linux", map[string]string{"TMUX": ""}, map[string]bool{"tmux": true})

	viaTerminal, err := CopyText("x")
	if err != nil || !viaTerminal {
		t.Fatalf("CopyText = (%v, %v), want (true, nil)", viaTerminal, err)
	}
}

func TestExecClipboardCmdRunsRealCommand(t *testing.T) {
	if err := execClipboardCmd("cat", nil, "hello"); err != nil {
		t.Errorf("cat should succeed, got %v", err)
	}
	if err := execClipboardCmd("mrman-no-such-binary-a1b2c3", nil, "x"); err == nil {
		t.Error("missing binary must error")
	}
	if err := execClipboardCmd("false", nil, ""); err == nil {
		t.Error("non-zero exit must error")
	}
}

// TestDefaultOpenTTYNeverWritesEscapesIntoAPipe pins the contract: with a
// controlling terminal the tty is used; without one, stdout is used only
// when it is itself a terminal, and otherwise the copy fails rather than
// leaking an OSC 52 sequence into whatever stdout is redirected to. Under
// `go test` stdout is a pipe, so the fallback must be an error whenever
// /dev/tty cannot be opened.
func TestDefaultOpenTTYNeverWritesEscapesIntoAPipe(t *testing.T) {
	w, err := defaultOpenTTY()
	if err != nil {
		if w != nil {
			t.Fatalf("error and writer both returned: %v", err)
		}
		if !strings.Contains(err.Error(), "no controlling terminal") {
			t.Fatalf("err = %v, want it to explain the missing terminal", err)
		}
		return
	}
	// /dev/tty opened: this is a real terminal session. It must not be
	// the stdout shim, because stdout is a pipe here.
	if _, isStdout := w.(nopCloser); isStdout {
		t.Fatal("fell back to a non-terminal stdout")
	}
	if err := w.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestNopCloserClose(t *testing.T) {
	if err := (nopCloser{io.Discard}).Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}
