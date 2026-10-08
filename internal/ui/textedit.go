package ui

import (
	"unicode/utf8"

	render "github.com/infrashift/mrman/charmkit/cellrender"
)

// dropLastRune removes the last character of s. Backspace in the command,
// search and vim command-line buffers used to drop the last byte, leaving
// invalid UTF-8 behind when the last character was multibyte.
func dropLastRune(s string) string {
	_, size := utf8.DecodeLastRuneInString(s)
	return s[:len(s)-size]
}

// truncateToWidth cuts s to at most width display columns, never splitting
// a character, and reports the width of what it kept.
func truncateToWidth(s string, width int) (string, int) {
	used := 0
	for i, r := range s {
		w := render.StringWidth(string(r))
		if used+w > width {
			return s[:i], used
		}
		used += w
	}
	return s, used
}
