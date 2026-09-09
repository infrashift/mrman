package output

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/charmbracelet/x/term"

	"github.com/infrashift/mrman/internal/errs"
)

// Injection seams for tests: platform, environment, subprocess execution and
// terminal output are all swappable.
var (
	goosFn          = func() string { return runtime.GOOS }
	lookupEnv       = os.LookupEnv
	runClipboardCmd = execClipboardCmd
	openTTY         = defaultOpenTTY
)

// CopyText copies text to the system clipboard, porting tuicr's
// copy_text_to_clipboard chain:
//
//  1. macOS: pbcopy unconditionally — it reaches the system pasteboard even
//     inside tmux/SSH, where OSC 52 depends on the outer terminal honoring
//     the escape (Terminal.app does not).
//  2. tmux / SSH / Zellij sessions (TMUX, SSH_TTY or ZELLIJ set): OSC 52,
//     because desktop clipboards may "succeed" into an inaccessible display.
//  3. Desktop Linux by XDG_SESSION_TYPE: wl-copy (wayland) or
//     xclip -selection clipboard (x11).
//  4. Last resort (unset/unknown session type, missing binaries, other
//     platforms — including Windows): OSC 52.
//
// viaTerminal reports whether the OSC 52 terminal path was used, so callers
// can phrase the status message like tuicr ("via terminal").
func CopyText(text string) (viaTerminal bool, err error) {
	if goosFn() == "darwin" && runClipboardCmd("pbcopy", nil, text) == nil {
		return false, nil
	}
	if envSet("TMUX") || envSet("SSH_TTY") || envSet("ZELLIJ") {
		return true, copyOSC52(text)
	}
	if tryDesktopClipboard(text) {
		return false, nil
	}
	return true, copyOSC52(text)
}

// envSet reports whether the environment variable is present (any value,
// including empty, matching Rust's env::var().is_ok()).
func envSet(key string) bool {
	_, ok := lookupEnv(key)
	return ok
}

// tryDesktopClipboard shells out to the desktop clipboard tool selected by
// XDG_SESSION_TYPE. It reports false — falling through to OSC 52 — when the
// session type is unset or unsupported, or the tool is missing or fails.
func tryDesktopClipboard(text string) bool {
	session, ok := lookupEnv("XDG_SESSION_TYPE")
	if !ok {
		return false
	}
	switch session {
	case "wayland":
		return runClipboardCmd("wl-copy", nil, text) == nil
	case "x11":
		return runClipboardCmd("xclip", []string{"-selection", "clipboard"}, text) == nil
	default:
		return false
	}
}

// copyOSC52 copies text via the terminal. Inside tmux it prefers
// `tmux load-buffer -w -`, which stores a tmux buffer and forwards to the
// outer terminal's clipboard; if tmux cannot be run it writes a
// tmux-passthrough-wrapped OSC 52 sequence to the tty instead. Outside tmux
// it writes the raw OSC 52 sequence.
func copyOSC52(text string) error {
	if envSet("TMUX") {
		if err := runClipboardCmd("tmux", []string{"load-buffer", "-w", "-"}, text); err == nil {
			return nil
		}
		return writeToTTY(tmuxWrap(osc52Sequence(text)))
	}
	return writeToTTY(osc52Sequence(text))
}

// writeToTTY writes seq to the controlling terminal.
func writeToTTY(seq string) error {
	w, err := openTTY()
	if err != nil {
		return &errs.Clipboard{Detail: "failed to open terminal: " + err.Error()}
	}
	defer func() { _ = w.Close() }()
	if _, err := io.WriteString(w, seq); err != nil {
		return &errs.Clipboard{Detail: "failed to write OSC 52: " + err.Error()}
	}
	return nil
}

// execClipboardCmd runs a clipboard CLI with text on stdin, discarding
// output. A non-nil error means the binary is missing or exited non-zero,
// which callers treat as "try the next strategy".
func execClipboardCmd(name string, args []string, stdin string) error {
	cmd := exec.CommandContext(context.Background(), name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}

// defaultOpenTTY opens /dev/tty for writing. Without a controlling
// terminal it falls back to stdout only when stdout *is* a terminal; an
// OSC 52 sequence written into a pipe or a file is not a clipboard copy,
// it is an escape sequence handed to whatever reads that stream.
func defaultOpenTTY() (io.WriteCloser, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err == nil {
		return tty, nil
	}
	if term.IsTerminal(os.Stdout.Fd()) {
		return nopCloser{os.Stdout}, nil
	}
	return nil, fmt.Errorf("no controlling terminal for OSC 52: %w", err)
}

// nopCloser adapts a Writer we must not close (stdout) to WriteCloser.
type nopCloser struct{ io.Writer }

// Close is a no-op.
func (nopCloser) Close() error { return nil }
