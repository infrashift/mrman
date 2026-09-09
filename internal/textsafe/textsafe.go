// Package textsafe scrubs text that arrives from outside mrman — forge
// payloads, patch files, agent-written comments — before it can reach a
// terminal.
//
// The renderer is not a filter. A pull request title, a review comment or
// a diff line is written straight into the screen buffer, and Bubble Tea's
// buffer keeps SGR styling, OSC 8 hyperlinks and BEL intact. So a comment
// on a forge could restyle or hide parts of the review, attach a link to
// text that reads as something else, or ring the bell on every redraw.
// Terminal emulators differ in what else they honour. Rather than track
// that, mrman strips everything that is not text at the point of ingress,
// so the same clean string feeds layout, hashing, exports and the screen.
//
// Kept: printable text, tabs, and (for Sanitize) newlines. Removed: every
// escape sequence, the other C0 controls, DEL, the C1 range, the Unicode
// line and paragraph separators, and bytes that are not UTF-8. The bidi
// override and isolate controls become U+FFFD instead of vanishing — text
// that tried to reorder itself is worth a visible mark.
package textsafe

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// Sanitize returns s with every terminal-controlling or invisible-but-
// consequential character removed, keeping tabs and newlines.
func Sanitize(s string) string {
	if clean(s, true) {
		return s
	}
	return scrub(s, true)
}

// SanitizeLine is Sanitize for single-line values — titles, authors, file
// paths, diff lines — where a newline would break layout; newlines become
// spaces.
func SanitizeLine(s string) string {
	if clean(s, false) {
		return s
	}
	return scrub(s, false)
}

// clean is the fast path: a byte scan that is exact for ASCII and
// conservative for the two UTF-8 lead bytes under which every non-ASCII
// character this package touches lives. Most text never leaves it.
func clean(s string, keepNewline bool) bool {
	for i := 0; i < len(s); i++ {
		switch b := s[i]; {
		case b == '\t':
		case b == '\n':
			if !keepNewline {
				return false
			}
		case b < 0x20, b == 0x7f:
			return false
		case b == 0xc2, b == 0xe2:
			// C1 (U+0080–U+009F) is C2 80–C2 9F; U+2028/2029 and the bidi
			// controls are E2 80 xx / E2 81 xx. Let scrub decide.
			return false
		}
	}
	return utf8.ValidString(s)
}

func scrub(s string, keepNewline bool) string {
	s = ansi.Strip(s)
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch {
		case r == utf8.RuneError && size == 1:
			// Not UTF-8; drop the byte.
		case r == '\t':
			b.WriteRune(r)
		case r == '\n':
			if keepNewline {
				b.WriteRune(r)
			} else {
				b.WriteByte(' ')
			}
		case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
			// C0, DEL, C1.
		case r == '\u2028', r == '\u2029':
			// Line and paragraph separators: invisible line breaks.
		case r >= '\u202a' && r <= '\u202e', r >= '\u2066' && r <= '\u2069':
			// Bidi embeddings, overrides and isolates.
			b.WriteRune('\ufffd')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
