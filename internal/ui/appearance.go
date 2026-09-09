package ui

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"charm.land/lipgloss/v2"
)

// detectSystemDark implements tuicr's appearance-detection chain: query the
// terminal background via OSC 11 (lipgloss), then fall back to OS-level
// preferences, defaulting to dark. Runs before the Bubbletea program starts.
// Skipped (returns dark) when the TUI renders to /dev/tty for --stdout mode,
// since probe replies would pollute stdout.
func detectSystemDark(stdoutMode bool) bool {
	if stdoutMode {
		return true
	}
	if isTerminal(os.Stdin) && isTerminal(os.Stdout) {
		return lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
	}
	if dark, ok := osDarkPreference(); ok {
		return dark
	}
	return true
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// osDarkPreference ports tuicr's per-OS fallbacks.
func osDarkPreference() (dark, ok bool) {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.CommandContext(context.Background(), "defaults", "read", "-g", "AppleInterfaceStyle").Output()
		if err != nil {
			return false, true // key absent = light mode
		}
		return strings.TrimSpace(string(out)) == "Dark", true
	case "linux":
		out, err := exec.CommandContext(context.Background(), "gsettings", "get", "org.gnome.desktop.interface", "color-scheme").Output()
		if err == nil {
			scheme := strings.TrimSpace(string(out))
			if strings.Contains(scheme, "prefer-dark") {
				return true, true
			}
			if strings.Contains(scheme, "prefer-light") || strings.Contains(scheme, "default") {
				return false, true
			}
		}
		out, err = exec.CommandContext(context.Background(), "gsettings", "get", "org.gnome.desktop.interface", "gtk-theme").Output()
		if err == nil {
			return strings.Contains(strings.ToLower(string(out)), "dark"), true
		}
	}
	return false, false
}
