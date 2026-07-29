package app

// modes_test.go covers modes.go: message TTLs through the clock seam, and
// the help/search/command shell transitions.

import (
	"testing"
	"time"

	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
)

func modeApp(t *testing.T) *App {
	t.Helper()
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	return buildAppWithFiles([]model.DiffFile{file}, 0)
}

func withFakeClock(t *testing.T) func(time.Duration) {
	t.Helper()
	current := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	orig := now
	now = func() time.Time { return current }
	t.Cleanup(func() { now = orig })
	return func(d time.Duration) { current = current.Add(d) }
}

func TestMessageTTLs(t *testing.T) {
	advance := withFakeClock(t)
	a := modeApp(t)

	a.SetMessage("info")
	assertEq(t, a.Message.Type, MessageInfo, "info type")
	if a.Message.ExpiresAt == nil {
		t.Fatal("info messages must carry a TTL")
	}
	advance(MessageTTLInfo - time.Millisecond)
	if a.ClearExpiredMessage() {
		t.Error("info message should not expire before its TTL")
	}
	advance(time.Millisecond)
	if !a.ClearExpiredMessage() {
		t.Error("info message should expire after 3s")
	}
	if a.Message != nil {
		t.Error("expired message should be cleared")
	}

	a.SetWarning("warn")
	assertEq(t, a.Message.Type, MessageWarning, "warning type")
	advance(MessageTTLWarning)
	if !a.ClearExpiredMessage() {
		t.Error("warning should expire after 5s")
	}

	a.SetError("boom")
	assertEq(t, a.Message.Type, MessageError, "error type")
	if a.Message.ExpiresAt != nil {
		t.Error("errors are sticky")
	}
	advance(time.Hour)
	if a.ClearExpiredMessage() {
		t.Error("sticky error should never auto-clear")
	}

	a.SetStickyWarning("dirty")
	assertEq(t, a.Message.Type, MessageWarning, "sticky warning type")
	if a.Message.ExpiresAt != nil {
		t.Error("sticky warnings carry no TTL")
	}
}

func TestClearExpiredMessageWithNoMessage(t *testing.T) {
	a := modeApp(t)
	if a.ClearExpiredMessage() {
		t.Error("nothing to clear")
	}
}

func TestCommandModeTransitions(t *testing.T) {
	a := modeApp(t)
	a.CommandBuffer = "stale"

	a.EnterCommandMode()
	assertEq(t, a.InputMode, input.ModeCommand, "enter command mode")
	assertEq(t, a.CommandBuffer, "", "buffer cleared on enter")

	a.CommandBuffer = "quit"
	a.ExitCommandMode()
	assertEq(t, a.InputMode, input.ModeNormal, "exit command mode")
	assertEq(t, a.CommandBuffer, "", "buffer cleared on exit")
}

func TestSearchModeRemembersReturnMode(t *testing.T) {
	a := modeApp(t)

	a.EnterSearchMode()
	assertEq(t, a.InputMode, input.ModeSearch, "enter search mode")
	assertEq(t, a.SearchingHelp(), false, "not searching help from normal")
	a.ExitSearchMode()
	assertEq(t, a.InputMode, input.ModeNormal, "return to normal")

	a.ToggleHelp()
	assertEq(t, a.InputMode, input.ModeHelp, "help open")
	a.EnterSearchMode()
	assertEq(t, a.SearchingHelp(), true, "searching help")
	a.ExitSearchMode()
	assertEq(t, a.InputMode, input.ModeHelp, "return to help")
	a.ToggleHelp()
	assertEq(t, a.InputMode, input.ModeNormal, "help closed")
}

func TestHelpScrolling(t *testing.T) {
	a := modeApp(t)
	a.HelpState.TotalLines = 50
	a.HelpState.ViewportHeight = 10
	a.HelpState.ScrollOffset = 5

	a.ToggleHelp()
	assertEq(t, a.HelpState.ScrollOffset, 0, "scroll reset on open")

	a.HelpScrollDown(100)
	assertEq(t, a.HelpState.ScrollOffset, 40, "scroll down clamps to bottom")

	a.HelpScrollUp(5)
	assertEq(t, a.HelpState.ScrollOffset, 35, "scroll up")

	a.HelpScrollToTop()
	assertEq(t, a.HelpState.ScrollOffset, 0, "scroll to top")

	a.HelpScrollToBottom()
	assertEq(t, a.HelpState.ScrollOffset, 40, "scroll to bottom")
}
