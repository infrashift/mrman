package app

// search_test.go ports the embedded HelpState tests from tuicr's
// src/app/search.rs and adds diff-search coverage for the same engine.

import (
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

func helpState() HelpState {
	return HelpState{
		ViewportHeight: 5,
		SearchableLines: []string{
			"Navigation",
			"Scroll down/up",
			"Review actions",
			"Add line comment",
			"Commands",
			"Reload comments",
			"Toggle this help",
		},
	}
}

func TestShouldFindHelpTextCaseInsensitivelyAndCenterItInTheViewport(t *testing.T) {
	state := helpState()

	if !state.search("COMMENT", true, true) {
		t.Fatal("expected match")
	}
	assertEq(t, *state.CurrentMatchLine, 3, "match line")
	assertEq(t, state.ScrollOffset, 1, "centered scroll offset")
}

func TestShouldMoveToNextAndPreviousHelpMatches(t *testing.T) {
	state := helpState()
	if !state.search("comment", true, true) {
		t.Fatal("expected first match")
	}

	if !state.search("comment", true, false) {
		t.Fatal("expected next match")
	}
	assertEq(t, *state.CurrentMatchLine, 5, "next match line")

	if !state.search("comment", false, false) {
		t.Fatal("expected previous match")
	}
	assertEq(t, *state.CurrentMatchLine, 3, "previous match line")
}

func TestShouldKeepTheCurrentHelpPositionWhenNoMatchExists(t *testing.T) {
	state := helpState()
	state.ScrollOffset = 2

	if state.search("missing", true, true) {
		t.Fatal("expected no match")
	}
	if state.CurrentMatchLine != nil {
		t.Errorf("current match line = %v, want nil", *state.CurrentMatchLine)
	}
	assertEq(t, state.ScrollOffset, 2, "scroll offset unchanged")
}

// --- App-level help search wrappers ---

func TestHelpSearchWrappers(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 0)
	a.HelpState = helpState()

	a.SearchBuffer = "  "
	if a.SearchInHelpFromScroll() {
		t.Error("blank pattern should not match")
	}
	assertEq(t, a.Message.Content, "Search pattern is empty", "empty pattern message")

	a.SearchBuffer = "comment"
	if !a.SearchInHelpFromScroll() {
		t.Fatal("expected help match")
	}
	if !a.SearchNextInHelp() {
		t.Fatal("expected next help match")
	}
	if a.SearchNextInHelp() {
		t.Error("no further matches expected")
	}
	assertEq(t, a.Message.Content, `No further help matches for "comment"`, "exhausted message")
	if !a.SearchPrevInHelp() {
		t.Fatal("expected previous help match")
	}

	a.HelpState.LastSearchPattern = nil
	if a.SearchNextInHelp() || a.SearchPrevInHelp() {
		t.Error("n/N without a previous help search should fail")
	}
	assertEq(t, a.Message.Content, "No previous help search", "no previous search message")
}

// --- Diff search ---

func TestSearchInDiffFromCursorFindsAndCentersMatch(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 10)})
	a := buildAppWithFiles([]model.DiffFile{file}, 0)
	a.DiffState.ViewportHeight = 5
	a.DiffState.VisibleLineCount = 5

	a.SearchBuffer = "HUNK LINE 7"
	if !a.SearchInDiffFromCursor() {
		t.Fatal("expected diff match")
	}
	ann := a.LineAnnotations[a.DiffState.CursorLine]
	assertEq(t, ann.Kind, AnnDiffLine, "cursor on diff line")
	assertLineno(t, ann.NewLineno, 7, "match line number")
}

func TestSearchNextAndPrevInDiff(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 10)})
	a := buildAppWithFiles([]model.DiffFile{file}, 0)
	a.DiffState.ViewportHeight = 5
	a.DiffState.VisibleLineCount = 5

	a.SearchBuffer = "hunk line 1" // matches lines 1 and 10
	if !a.SearchInDiffFromCursor() {
		t.Fatal("expected first match")
	}
	first := a.DiffState.CursorLine

	if !a.SearchNextInDiff() {
		t.Fatal("expected next match")
	}
	assertLineno(t, a.LineAnnotations[a.DiffState.CursorLine].NewLineno, 10, "n lands on line 10")

	// No wrap-around: a further n fails and stays put.
	pos := a.DiffState.CursorLine
	if a.SearchNextInDiff() {
		t.Error("no wrap-around expected")
	}
	assertEq(t, a.DiffState.CursorLine, pos, "cursor unchanged on failed n")
	assertEq(t, a.Message.Content, `No matches for "hunk line 1"`, "failed n message")

	if !a.SearchPrevInDiff() {
		t.Fatal("expected previous match")
	}
	assertEq(t, a.DiffState.CursorLine, first, "N returns to first match")
}

func TestSearchInDiffWithoutPreviousPattern(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 0)

	if a.SearchNextInDiff() || a.SearchPrevInDiff() {
		t.Error("n/N without a previous search should fail")
	}
	assertEq(t, a.Message.Content, "No previous search", "no previous search message")
}

func TestLineTextForSearchCoversAnnotationKinds(t *testing.T) {
	// Two hunks with a large gap (down + hidden + up expanders) plus a
	// binary file; partially expand the gap for ExpandedContext coverage.
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 2), makeHunk(40, 2)})
	binary := model.DiffFile{NewPath: strPtr("bin.dat"), Status: model.StatusModified, IsBinary: true}
	a := buildAppWithFiles([]model.DiffFile{file, binary}, 50)
	gapID := GapID{FileIdx: 0, HunkIdx: 1}
	if err := a.ExpandGap(gapID, ExpandDown, intPtr(5)); err != nil {
		t.Fatal(err)
	}

	texts := map[AnnKind]string{}
	for i := range a.LineAnnotations {
		if text, ok := a.lineTextForSearch(i); ok {
			if _, seen := texts[a.LineAnnotations[i].Kind]; !seen {
				texts[a.LineAnnotations[i].Kind] = text
			}
		}
	}

	assertEq(t, texts[AnnReviewCommentsHeader], "Review comments", "header text")
	assertEq(t, texts[AnnFileHeader], "test.rs [M]", "file header text")
	assertEq(t, texts[AnnHunkHeader], "@@ -1,2 +1,2 @@", "hunk header text")
	assertEq(t, texts[AnnDiffLine], "hunk line 1", "diff line text")
	assertEq(t, texts[AnnExpandedContext], "line 3", "expanded context text")
	assertEq(t, texts[AnnBinaryOrEmpty], "(binary file)", "binary text")
	assertEq(t, texts[AnnExpander], "... ↓ expand (20 lines) ...", "expander text")
	assertEq(t, texts[AnnHiddenLines], "... 32 lines hidden ...", "hidden lines text")

	// Side-by-side text joins both panes.
	a.ToggleDiffViewMode()
	found := false
	for i := range a.LineAnnotations {
		if a.LineAnnotations[i].Kind == AnnSideBySideLine {
			text, ok := a.lineTextForSearch(i)
			if ok && text == "hunk line 1 hunk line 1" {
				found = true
			}
			break
		}
	}
	if !found {
		t.Error("side-by-side search text should join both panes")
	}
}

func TestLineTextForSearchTooLargeAndEmpty(t *testing.T) {
	tooLarge := model.DiffFile{NewPath: strPtr("big.bin"), Status: model.StatusModified, IsTooLarge: true, IsBinary: true}
	empty := model.DiffFile{NewPath: strPtr("same.txt"), Status: model.StatusModified}
	a := buildAppWithFiles([]model.DiffFile{tooLarge, empty}, 0)

	var got []string
	for i := range a.LineAnnotations {
		if a.LineAnnotations[i].Kind == AnnBinaryOrEmpty {
			text, _ := a.lineTextForSearch(i)
			got = append(got, text)
		}
	}
	assertEq(t, len(got), 2, "two markers")
	assertEq(t, got[0], "(file too large to display)", "too-large marker")
	assertEq(t, got[1], "(no changes)", "empty marker")
}
