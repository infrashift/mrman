package app

// single_file_view_test.go ports tuicr's
// src/app/tests/single_file_view_tests.rs. Skipped: the six editor-target
// tests (queue_editor_for_focused_item and the forge-checkout fallbacks
// belong to the editor/PR milestones).

import (
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

func sfvApp(files ...model.DiffFile) *App {
	return buildAppWithFiles(files, 0)
}

func sfvFile(path string, hunks ...model.DiffHunk) model.DiffFile {
	return makeFileWithHunks(path, hunks)
}

func TestTogglePreservesFilePosition(t *testing.T) {
	a := sfvApp(
		sfvFile("a.rs", makeHunk(1, 3)),
		sfvFile("b.rs", makeHunk(1, 3)),
		sfvFile("c.rs", makeHunk(1, 3)),
	)
	a.DiffState.CurrentFileIdx = 1
	expectedMulti := a.calculateFileScrollOffset(1)
	a.DiffState.ScrollOffset = expectedMulti

	a.ToggleSingleFileView()
	if !a.IsSingleFileView {
		t.Fatal("single-file view should be on")
	}
	expectedSingle := a.calculateFileScrollOffset(1)
	assertEq(t, a.DiffState.ScrollOffset, expectedSingle, "scroll after toggle on")
	assertEq(t, a.DiffState.CursorLine, expectedSingle, "cursor after toggle on")

	a.ToggleSingleFileView()
	if a.IsSingleFileView {
		t.Fatal("single-file view should be off")
	}
	expectedBack := a.calculateFileScrollOffset(1)
	assertEq(t, a.DiffState.ScrollOffset, expectedBack, "scroll after toggle off")
	assertEq(t, a.DiffState.CursorLine, expectedBack, "cursor after toggle off")
}

func TestCursorDownRequiresTwoPressesToWalkToNextFile(t *testing.T) {
	a := sfvApp(sfvFile("a.rs", makeHunk(1, 3)), sfvFile("b.rs", makeHunk(1, 3)))
	a.IsSingleFileView = true
	a.DiffState.CurrentFileIdx = 0
	a.RebuildAnnotations()
	a.DiffState.CursorLine = a.MaxCursorLine()
	maxA := a.MaxCursorLine()

	// First press at file end arms PrimedWalkNext and stays on max.
	a.CursorDown(1)
	assertEq(t, a.DiffState.CurrentFileIdx, 0, "file after first press")
	assertEq(t, a.DiffState.CursorLine, maxA, "cursor parked on max")
	assertEq(t, a.PrimedWalkNext, true, "primed after first press")

	// Second press consumes the prime and walks.
	a.CursorDown(1)
	assertEq(t, a.DiffState.CurrentFileIdx, 1, "file after second press")
	assertEq(t, a.PrimedWalkNext, false, "prime consumed")
}

func TestCursorUpRequiresTwoPressesToWalkToPrevFile(t *testing.T) {
	a := sfvApp(sfvFile("a.rs", makeHunk(1, 3)), sfvFile("b.rs", makeHunk(1, 3)))
	a.IsSingleFileView = true
	a.DiffState.CurrentFileIdx = 1
	a.RebuildAnnotations()
	fileTop := a.calculateFileScrollOffset(1)
	a.DiffState.CursorLine = fileTop

	// First press at file top arms PrimedWalkPrev and stays.
	a.CursorUp(1)
	assertEq(t, a.DiffState.CurrentFileIdx, 1, "file after first press")
	assertEq(t, a.DiffState.CursorLine, fileTop, "cursor stays at file top")
	assertEq(t, a.PrimedWalkPrev, true, "primed after first press")

	// Second press walks to the previous file.
	a.CursorUp(1)
	assertEq(t, a.DiffState.CurrentFileIdx, 0, "file after second press")
	assertEq(t, a.PrimedWalkPrev, false, "prime consumed")
}

func TestPrimedWalkClearsOnNonOverflowCursorMove(t *testing.T) {
	a := sfvApp(sfvFile("a.rs", makeHunk(1, 5)), sfvFile("b.rs", makeHunk(1, 5)))
	a.IsSingleFileView = true
	a.DiffState.CurrentFileIdx = 0
	a.RebuildAnnotations()
	a.DiffState.CursorLine = a.MaxCursorLine()
	a.CursorDown(1) // arms
	assertEq(t, a.PrimedWalkNext, true, "armed")

	// A non-overflow move (cursor up within file) clears the next prime.
	a.CursorUp(1)
	assertEq(t, a.PrimedWalkNext, false, "prime cleared")
	assertEq(t, a.DiffState.CurrentFileIdx, 0, "still on first file")
}

func TestNextHunkCrossesIntoNextFileInSingleFileView(t *testing.T) {
	a := sfvApp(
		sfvFile("a.rs", makeHunk(1, 3), makeHunk(10, 3)),
		sfvFile("b.rs", makeHunk(1, 3), makeHunk(10, 3)),
	)
	a.IsSingleFileView = true
	a.DiffState.CurrentFileIdx = 0
	a.RebuildAnnotations()
	positions := a.HunkPositions()
	lastHunk := positions[len(positions)-1]
	a.DiffState.CursorLine = lastHunk

	// From a.rs's last hunk, ] should land on b.rs's first hunk.
	a.NextHunk()
	assertEq(t, a.DiffState.CurrentFileIdx, 1, "crossed into next file")
	newFirst := a.HunkPositions()[0]
	assertEq(t, a.DiffState.CursorLine, newFirst, "cursor on next file's first hunk")
}

func TestPrevHunkCrossesIntoPrevFileInSingleFileView(t *testing.T) {
	a := sfvApp(
		sfvFile("a.rs", makeHunk(1, 3), makeHunk(10, 3)),
		sfvFile("b.rs", makeHunk(1, 3), makeHunk(10, 3)),
	)
	a.IsSingleFileView = true
	a.DiffState.CurrentFileIdx = 1
	a.RebuildAnnotations()
	firstHunk := a.HunkPositions()[0]
	a.DiffState.CursorLine = firstHunk

	// From b.rs's first hunk, [ should land on a.rs's last hunk.
	a.PrevHunk()
	assertEq(t, a.DiffState.CurrentFileIdx, 0, "crossed into previous file")
	positions := a.HunkPositions()
	newLast := positions[len(positions)-1]
	assertEq(t, a.DiffState.CursorLine, newLast, "cursor on prev file's last hunk")
}

func TestHeldKeyDoesNotWalkWhenKeyboardEnhancementSupported(t *testing.T) {
	// Simulates kitty REPORT_EVENT_TYPES: held-j auto-repeats arm the prime
	// but the release flag never trips, so consecutive CursorDown calls
	// park on max forever.
	a := sfvApp(sfvFile("a.rs", makeHunk(1, 3)), sfvFile("b.rs", makeHunk(1, 3)))
	a.SupportsKeyboardEnhancement = true
	a.IsSingleFileView = true
	a.DiffState.CurrentFileIdx = 0
	a.RebuildAnnotations()
	a.DiffState.CursorLine = a.MaxCursorLine()
	maxA := a.MaxCursorLine()

	// 10 consecutive presses (no release) stay parked on max.
	for range 10 {
		a.CursorDown(1)
	}
	assertEq(t, a.DiffState.CurrentFileIdx, 0, "no walk while held")
	assertEq(t, a.DiffState.CursorLine, maxA, "parked on max")
	assertEq(t, a.PrimedWalkNext, true, "still primed")
	assertEq(t, a.DownReleasedSinceArm, false, "release flag never tripped")
}

func TestReleaseThenPressWalksWhenKeyboardEnhancementSupported(t *testing.T) {
	a := sfvApp(sfvFile("a.rs", makeHunk(1, 3)), sfvFile("b.rs", makeHunk(1, 3)))
	a.SupportsKeyboardEnhancement = true
	a.IsSingleFileView = true
	a.DiffState.CurrentFileIdx = 0
	a.RebuildAnnotations()
	a.DiffState.CursorLine = a.MaxCursorLine()

	// First press: arm.
	a.CursorDown(1)
	assertEq(t, a.PrimedWalkNext, true, "primed")
	assertEq(t, a.DiffState.CurrentFileIdx, 0, "no walk yet")

	// Simulate a Down-Release event from the main loop.
	a.DownReleasedSinceArm = true

	// Second press: walks.
	a.CursorDown(1)
	assertEq(t, a.DiffState.CurrentFileIdx, 1, "walked")
	assertEq(t, a.PrimedWalkNext, false, "prime consumed")
	assertEq(t, a.DownReleasedSinceArm, false, "release flag reset")
}

func TestEffectiveFileHeightIsZeroForNonCurrentInSingleFileView(t *testing.T) {
	a := sfvApp(sfvFile("a.rs", makeHunk(1, 3)), sfvFile("b.rs", makeHunk(1, 3)))
	a.IsSingleFileView = true
	a.DiffState.CurrentFileIdx = 0

	other := a.DiffFiles[1]
	assertEq(t, a.EffectiveFileHeight(1, &other), 0, "non-current file height")
	current := a.DiffFiles[0]
	if a.EffectiveFileHeight(0, &current) <= 0 {
		t.Error("current file height should be positive")
	}
}
