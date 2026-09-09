package vimtext

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// runeLenAt returns the byte length of the rune starting at i.
func runeLenAt(s string, i int) int {
	_, sz := utf8.DecodeRuneInString(s[i:])
	return sz
}

// prevRuneStart returns the byte offset of the rune ending at i (i must be a
// rune boundary > 0).
func prevRuneStart(s string, i int) int {
	_, sz := utf8.DecodeLastRuneInString(s[:i])
	return i - sz
}

// lastRuneLen returns the byte length of the final rune of s.
func lastRuneLen(s string) int {
	_, sz := utf8.DecodeLastRuneInString(s)
	return sz
}

// lineStart returns the byte offset of the first character of the line
// containing pos.
func (e *Editor) lineStart(pos int) int {
	return strings.LastIndexByte(e.text[:pos], '\n') + 1
}

// lineEnd returns the byte offset of the '\n' terminating the line containing
// pos, or len(text) for the last line.
func (e *Editor) lineEnd(pos int) int {
	if j := strings.IndexByte(e.text[pos:], '\n'); j >= 0 {
		return pos + j
	}
	return len(e.text)
}

// lineSpan returns the [start, end) byte block of whole lines covering
// positions a and b, including the trailing newline when present.
func (e *Editor) lineSpan(a, b int) (int, int) {
	if a > b {
		a, b = b, a
	}
	ls := e.lineStart(a)
	le := e.lineEnd(b)
	if le < len(e.text) {
		le++ // include the terminating '\n'
	}
	return ls, le
}

// firstNonBlank returns the offset of the first non-blank character of the
// line containing pos, or the line end when the line is blank.
func (e *Editor) firstNonBlank(pos int) int {
	ls := e.lineStart(pos)
	le := e.lineEnd(ls)
	for i := ls; i < le; {
		r, sz := utf8.DecodeRuneInString(e.text[i:])
		if !unicode.IsSpace(r) {
			return i
		}
		i += sz
	}
	return le
}

// lastLine returns the zero-based index of the buffer's last line.
func (e *Editor) lastLine() int { return strings.Count(e.text, "\n") }

// lineStartOf returns the byte offset of the start of the given zero-based
// line, clamped to the last line.
func (e *Editor) lineStartOf(line int) int {
	ls := 0
	for range line {
		j := strings.IndexByte(e.text[ls:], '\n')
		if j < 0 {
			break
		}
		ls += j + 1
	}
	return ls
}

// clampNormal pulls the cursor into range, onto a rune boundary, and off the
// one-past-line-end column (Normal-mode rule: the cursor sits on the last
// character of the line; empty lines allow column 0).
func (e *Editor) clampNormal() {
	if e.cursor > len(e.text) {
		e.cursor = len(e.text)
	}
	for e.cursor > 0 && e.cursor < len(e.text) && !utf8.RuneStart(e.text[e.cursor]) {
		e.cursor--
	}
	ls := e.lineStart(e.cursor)
	le := e.lineEnd(e.cursor)
	if e.cursor == le && le > ls {
		e.cursor = prevRuneStart(e.text, le)
	}
}

// vertTarget returns the cursor position n lines down (negative = up),
// preserving the character column where the target line allows.
func (e *Editor) vertTarget(n int) int {
	col := utf8.RuneCountInString(e.text[e.lineStart(e.cursor):e.cursor])
	pos := e.cursor
	for ; n > 0; n-- {
		le := e.lineEnd(pos)
		if le >= len(e.text) {
			break
		}
		pos = le + 1
	}
	for ; n < 0; n++ {
		ls := e.lineStart(pos)
		if ls == 0 {
			break
		}
		pos = e.lineStart(ls - 1)
	}
	ls := e.lineStart(pos)
	le := e.lineEnd(ls)
	for i := 0; i < col && ls < le; i++ {
		ls += runeLenAt(e.text, ls)
	}
	return ls
}

// charClass groups runes for word motions: 0 whitespace, 1 word characters
// (letters, digits, '_'), 2 other punctuation/symbols. Runs of class 1 or 2
// form words, vim-style.
func charClass(r rune) int {
	switch {
	case unicode.IsSpace(r):
		return 0
	case r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
		return 1
	default:
		return 2
	}
}

// nextWordStart returns the position of the next word start after pos ("w"),
// stopping on empty lines like vim. May return len(s).
func nextWordStart(s string, pos int) int {
	n := len(s)
	if pos >= n {
		return pos
	}
	r, _ := utf8.DecodeRuneInString(s[pos:])
	c := charClass(r)
	i := pos
	if c != 0 {
		for i < n {
			r, sz := utf8.DecodeRuneInString(s[i:])
			if charClass(r) != c {
				break
			}
			i += sz
		}
	}
	for i < n {
		r, sz := utf8.DecodeRuneInString(s[i:])
		if charClass(r) != 0 {
			break
		}
		if r == '\n' && i+sz < n && s[i+sz] == '\n' {
			return i + sz // empty line counts as a word
		}
		i += sz
	}
	return i
}

// prevWordStart returns the position of the previous word start before pos
// ("b"), stopping on empty lines like vim.
func prevWordStart(s string, pos int) int {
	if pos <= 0 {
		return 0
	}
	i := prevRuneStart(s, pos)
	for i > 0 {
		r, _ := utf8.DecodeRuneInString(s[i:])
		if charClass(r) != 0 {
			break
		}
		if r == '\n' && s[i-1] == '\n' {
			return i // empty line counts as a word
		}
		i = prevRuneStart(s, i)
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	c := charClass(r)
	if c == 0 {
		return i // hit buffer start inside whitespace
	}
	for i > 0 {
		p := prevRuneStart(s, i)
		pr, _ := utf8.DecodeRuneInString(s[p:])
		if charClass(pr) != c {
			break
		}
		i = p
	}
	return i
}

// wordEnd returns the position of the last character of the next word end at
// or after pos ("e").
func wordEnd(s string, pos int) int {
	n := len(s)
	if n == 0 {
		return 0
	}
	i := pos
	if i < n {
		i += runeLenAt(s, i)
	}
	for i < n {
		r, sz := utf8.DecodeRuneInString(s[i:])
		if charClass(r) != 0 {
			break
		}
		i += sz
	}
	if i >= n {
		return prevRuneStart(s, n)
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	c := charClass(r)
	for {
		nx := i + runeLenAt(s, i)
		if nx >= n {
			return i
		}
		nr, _ := utf8.DecodeRuneInString(s[nx:])
		if charClass(nr) != c {
			return i
		}
		i = nx
	}
}
