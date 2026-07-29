// textedit.go ports tuicr's src/text_edit.rs (UTF-8 aware byte-offset
// cursor helpers) plus the comment-buffer operations from the comment-mode
// half of tuicr's handler.rs, operating on App.CommentBuffer and
// App.CommentCursor.
package app

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// PrevCharBoundary finds the byte position of the previous character
// boundary, or 0 when already at the start.
func PrevCharBoundary(buffer string, cursor int) int {
	if cursor == 0 {
		return 0
	}
	cursor = min(cursor, len(buffer))
	pos := cursor - 1
	for pos > 0 && !utf8.RuneStart(buffer[pos]) {
		pos--
	}
	return pos
}

// NextCharBoundary finds the byte position of the next character boundary,
// or len(buffer) when already at the end.
func NextCharBoundary(buffer string, cursor int) int {
	if cursor >= len(buffer) {
		return len(buffer)
	}
	pos := cursor + 1
	for pos < len(buffer) && !utf8.RuneStart(buffer[pos]) {
		pos++
	}
	return pos
}

// DeleteCharBefore deletes the character before cursor, returning the new
// buffer and cursor position.
func DeleteCharBefore(buffer string, cursor int) (string, int) {
	if cursor == 0 {
		return buffer, 0
	}
	cursor = min(cursor, len(buffer))
	prev := PrevCharBoundary(buffer, cursor)
	return buffer[:prev] + buffer[cursor:], prev
}

// DeleteWordBefore deletes the word before cursor (trailing whitespace plus
// the word itself), returning the new buffer and cursor position.
func DeleteWordBefore(buffer string, cursor int) (string, int) {
	if cursor == 0 {
		return buffer, 0
	}
	cursor = min(cursor, len(buffer))
	pos := cursor

	// Skip whitespace backwards.
	for pos > 0 {
		r, size := utf8.DecodeLastRuneInString(buffer[:pos])
		if !unicode.IsSpace(r) {
			break
		}
		pos -= size
	}
	// Skip non-whitespace backwards (the word itself).
	for pos > 0 {
		r, size := utf8.DecodeLastRuneInString(buffer[:pos])
		if unicode.IsSpace(r) {
			break
		}
		pos -= size
	}

	return buffer[:pos] + buffer[cursor:], pos
}

// commentLineStart is the byte offset of the start of the line containing
// cursor.
func commentLineStart(buffer string, cursor int) int {
	cursor = min(cursor, len(buffer))
	if pos := strings.LastIndexByte(buffer[:cursor], '\n'); pos >= 0 {
		return pos + 1
	}
	return 0
}

// commentLineEnd is the byte offset of the end of the line containing
// cursor.
func commentLineEnd(buffer string, cursor int) int {
	cursor = min(cursor, len(buffer))
	if pos := strings.IndexByte(buffer[cursor:], '\n'); pos >= 0 {
		return cursor + pos
	}
	return len(buffer)
}

// commentWordLeft is the byte offset of the start of the word before cursor.
func commentWordLeft(buffer string, cursor int) int {
	cursor = min(cursor, len(buffer))
	if cursor == 0 {
		return 0
	}
	// Find the last non-whitespace character before the cursor.
	pos := cursor
	for pos > 0 {
		r, size := utf8.DecodeLastRuneInString(buffer[:pos])
		if !unicode.IsSpace(r) {
			break
		}
		pos -= size
	}
	if pos == 0 {
		return 0
	}
	// Walk back to the start of that word.
	for pos > 0 {
		r, size := utf8.DecodeLastRuneInString(buffer[:pos])
		if unicode.IsSpace(r) {
			return pos
		}
		pos -= size
	}
	return pos
}

// commentWordRight is the byte offset of the start of the next word after
// cursor.
func commentWordRight(buffer string, cursor int) int {
	cursor = min(cursor, len(buffer))
	if cursor >= len(buffer) {
		return len(buffer)
	}
	pos := cursor
	if r, _ := utf8.DecodeRuneInString(buffer[pos:]); unicode.IsSpace(r) {
		// On whitespace: skip forward to the next word start.
		for pos < len(buffer) {
			r, size := utf8.DecodeRuneInString(buffer[pos:])
			if !unicode.IsSpace(r) {
				return pos
			}
			pos += size
		}
		return len(buffer)
	}
	// Inside a word: skip to its end, then over whitespace.
	for pos < len(buffer) {
		r, size := utf8.DecodeRuneInString(buffer[pos:])
		if unicode.IsSpace(r) {
			break
		}
		pos += size
	}
	for pos < len(buffer) {
		r, size := utf8.DecodeRuneInString(buffer[pos:])
		if !unicode.IsSpace(r) {
			return pos
		}
		pos += size
	}
	return len(buffer)
}

// InsertCommentChar inserts r at the comment cursor and advances it.
func (a *App) InsertCommentChar(r rune) {
	a.CommentCursor = min(a.CommentCursor, len(a.CommentBuffer))
	s := string(r)
	a.CommentBuffer = a.CommentBuffer[:a.CommentCursor] + s + a.CommentBuffer[a.CommentCursor:]
	a.CommentCursor += len(s)
}

// InsertCommentText inserts pasted text at the comment cursor, normalizing
// CRLF/CR line endings to LF.
func (a *App) InsertCommentText(text string) {
	normalized := strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	a.CommentCursor = min(a.CommentCursor, len(a.CommentBuffer))
	a.CommentBuffer = a.CommentBuffer[:a.CommentCursor] + normalized + a.CommentBuffer[a.CommentCursor:]
	a.CommentCursor += len(normalized)
}

// DeleteCommentChar deletes the character before the comment cursor.
func (a *App) DeleteCommentChar() {
	a.CommentBuffer, a.CommentCursor = DeleteCharBefore(a.CommentBuffer, a.CommentCursor)
}

// DeleteCommentWord deletes the word before the comment cursor.
func (a *App) DeleteCommentWord() {
	a.CommentBuffer, a.CommentCursor = DeleteWordBefore(a.CommentBuffer, a.CommentCursor)
}

// ClearCommentLine clears the whole comment buffer (Ctrl-U).
func (a *App) ClearCommentLine() {
	a.CommentBuffer = ""
	a.CommentCursor = 0
}

// CommentCursorLeft moves the comment cursor one character left.
func (a *App) CommentCursorLeft() {
	a.CommentCursor = PrevCharBoundary(a.CommentBuffer, a.CommentCursor)
}

// CommentCursorRight moves the comment cursor one character right.
func (a *App) CommentCursorRight() {
	a.CommentCursor = NextCharBoundary(a.CommentBuffer, a.CommentCursor)
}

// CommentCursorLineStart moves the comment cursor to the start of its line.
func (a *App) CommentCursorLineStart() {
	a.CommentCursor = commentLineStart(a.CommentBuffer, a.CommentCursor)
}

// CommentCursorLineEnd moves the comment cursor to the end of its line.
func (a *App) CommentCursorLineEnd() {
	a.CommentCursor = commentLineEnd(a.CommentBuffer, a.CommentCursor)
}

// CommentCursorWordLeft moves the comment cursor to the previous word start.
func (a *App) CommentCursorWordLeft() {
	a.CommentCursor = commentWordLeft(a.CommentBuffer, a.CommentCursor)
}

// CommentCursorWordRight moves the comment cursor to the next word start.
func (a *App) CommentCursorWordRight() {
	a.CommentCursor = commentWordRight(a.CommentBuffer, a.CommentCursor)
}
