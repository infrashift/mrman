package app

// textedit_test.go ports the test suite embedded in tuicr's
// src/text_edit.rs and adds coverage for the comment-buffer operations.

import (
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

func TestPrevCharBoundary(t *testing.T) {
	assertEq(t, PrevCharBoundary("hello", 0), 0, "at start")
	assertEq(t, PrevCharBoundary("hello", 5), 4, "ascii end")
	assertEq(t, PrevCharBoundary("hello", 3), 2, "ascii middle")
	assertEq(t, PrevCharBoundary("hello", 1), 0, "ascii first")

	// Each Hangul syllable is 3 bytes.
	s := "좋아"
	assertEq(t, len(s), 6, "multibyte len")
	assertEq(t, PrevCharBoundary(s, 6), 3, "end -> start of second char")
	assertEq(t, PrevCharBoundary(s, 3), 0, "second char -> start")

	// The crab emoji is 4 bytes.
	assertEq(t, PrevCharBoundary("🦀", 4), 0, "emoji")

	mixed := "a좋b" // 1 + 3 + 1 = 5 bytes
	assertEq(t, PrevCharBoundary(mixed, 5), 4, "after b")
	assertEq(t, PrevCharBoundary(mixed, 4), 1, "after multibyte")
	assertEq(t, PrevCharBoundary(mixed, 1), 0, "after a")
}

func TestNextCharBoundary(t *testing.T) {
	assertEq(t, NextCharBoundary("hello", 5), 5, "at end")
	assertEq(t, NextCharBoundary("hello", 0), 1, "ascii start")
	assertEq(t, NextCharBoundary("hello", 2), 3, "ascii middle")
	assertEq(t, NextCharBoundary("hello", 4), 5, "ascii last")

	s := "좋아"
	assertEq(t, NextCharBoundary(s, 0), 3, "start -> after first")
	assertEq(t, NextCharBoundary(s, 3), 6, "after first -> end")

	assertEq(t, NextCharBoundary("🦀", 0), 4, "emoji")
}

func TestDeleteCharBefore(t *testing.T) {
	s, cursor := DeleteCharBefore("hello", 5)
	assertEq(t, s, "hell", "ascii buffer")
	assertEq(t, cursor, 4, "ascii cursor")

	s, cursor = DeleteCharBefore("좋아", 6)
	assertEq(t, s, "좋", "multibyte buffer")
	assertEq(t, cursor, 3, "multibyte cursor")

	s, cursor = DeleteCharBefore("좋아요", 6)
	assertEq(t, s, "좋요", "middle buffer")
	assertEq(t, cursor, 3, "middle cursor")

	s, cursor = DeleteCharBefore("hello", 0)
	assertEq(t, s, "hello", "at-start buffer unchanged")
	assertEq(t, cursor, 0, "at-start cursor")

	s, cursor = DeleteCharBefore("hi🦀", 6)
	assertEq(t, s, "hi", "emoji buffer")
	assertEq(t, cursor, 2, "emoji cursor")
}

func TestDeleteWordBefore(t *testing.T) {
	s, cursor := DeleteWordBefore("hello world", 11)
	assertEq(t, s, "hello ", "ascii buffer")
	assertEq(t, cursor, 6, "ascii cursor")

	s, cursor = DeleteWordBefore("안녕 아가브라", 19)
	assertEq(t, s, "안녕 ", "multibyte buffer")
	assertEq(t, cursor, 7, "multibyte cursor")

	s, cursor = DeleteWordBefore("hello   ", 8)
	assertEq(t, s, "", "trailing whitespace buffer")
	assertEq(t, cursor, 0, "trailing whitespace cursor")

	s, cursor = DeleteWordBefore("hello", 0)
	assertEq(t, s, "hello", "at-start buffer unchanged")
	assertEq(t, cursor, 0, "at-start cursor")
}

func TestNavigateMultibyteStringCorrectly(t *testing.T) {
	s := "좋아요" // 9 bytes, 3 chars
	cursor := 0
	cursor = NextCharBoundary(s, cursor)
	assertEq(t, cursor, 3, "right 1")
	cursor = NextCharBoundary(s, cursor)
	assertEq(t, cursor, 6, "right 2")
	cursor = NextCharBoundary(s, cursor)
	assertEq(t, cursor, 9, "right 3")
	cursor = PrevCharBoundary(s, cursor)
	assertEq(t, cursor, 6, "left 1")
	cursor = PrevCharBoundary(s, cursor)
	assertEq(t, cursor, 3, "left 2")
	cursor = PrevCharBoundary(s, cursor)
	assertEq(t, cursor, 0, "left 3")
}

func TestInsertDeleteRoundtrip(t *testing.T) {
	a := buildAppWithFiles([]model.DiffFile{makeFileWithHunks("t.rs", []model.DiffHunk{makeHunk(1, 3)})}, 20)
	for _, c := range "좋아" {
		a.InsertCommentChar(c)
	}
	assertEq(t, a.CommentBuffer, "좋아", "inserted")
	assertEq(t, a.CommentCursor, 6, "cursor after insert")

	a.DeleteCommentChar()
	assertEq(t, a.CommentBuffer, "좋", "first delete")
	a.DeleteCommentChar()
	assertEq(t, a.CommentBuffer, "", "second delete")
	assertEq(t, a.CommentCursor, 0, "cursor after deletes")
}

func TestCommentBufferOps(t *testing.T) {
	a := buildAppWithFiles([]model.DiffFile{makeFileWithHunks("t.rs", []model.DiffHunk{makeHunk(1, 3)})}, 20)
	a.InsertCommentText("first line\r\nsecond word")
	assertEq(t, a.CommentBuffer, "first line\nsecond word", "paste normalizes CRLF")
	assertEq(t, a.CommentCursor, len(a.CommentBuffer), "cursor at end")

	a.CommentCursorLineStart()
	assertEq(t, a.CommentCursor, len("first line\n"), "line start of second line")
	a.CommentCursorLineEnd()
	assertEq(t, a.CommentCursor, len(a.CommentBuffer), "line end")

	a.CommentCursorWordLeft()
	assertEq(t, a.CommentBuffer[a.CommentCursor:], "word", "word left lands on last word")
	a.CommentCursorWordLeft()
	assertEq(t, a.CommentBuffer[a.CommentCursor:], "second word", "word left again")
	a.CommentCursorWordRight()
	assertEq(t, a.CommentBuffer[a.CommentCursor:], "word", "word right skips to next word")
	a.CommentCursorWordRight()
	assertEq(t, a.CommentCursor, len(a.CommentBuffer), "word right at end")

	a.CommentCursorLeft()
	a.CommentCursorRight()
	assertEq(t, a.CommentCursor, len(a.CommentBuffer), "left/right roundtrip")

	a.DeleteCommentWord()
	assertEq(t, a.CommentBuffer, "first line\nsecond ", "delete word")

	a.ClearCommentLine()
	assertEq(t, a.CommentBuffer, "", "clear")
	assertEq(t, a.CommentCursor, 0, "clear cursor")
}
