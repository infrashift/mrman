// modes.go ports tuicr's src/app/modes.rs: status-bar messages with TTLs
// and the enter/exit transitions for the help, search, and command shells.
package app

import (
	"time"

	"github.com/infrashift/mrman/internal/input"
)

// MessageType classifies a status-bar message.
type MessageType int

// Message types.
const (
	MessageInfo MessageType = iota
	MessageWarning
	MessageError
)

// Message is a status-bar message. A nil ExpiresAt means sticky.
type Message struct {
	Content   string
	Type      MessageType
	ExpiresAt *time.Time
}

// Message TTLs: info messages clear after 3s, warnings after 5s; errors are
// sticky until overwritten.
const (
	MessageTTLInfo    = 3 * time.Second
	MessageTTLWarning = 5 * time.Second
)

// now is the clock seam; tests may override it to control message expiry.
var now = time.Now

// SetMessage shows an info message with the info TTL.
func (a *App) SetMessage(msg string) {
	a.setMessageInner(msg, MessageInfo, MessageTTLInfo)
}

// SetWarning shows a warning with the warning TTL.
func (a *App) SetWarning(msg string) {
	a.setMessageInner(msg, MessageWarning, MessageTTLWarning)
}

// SetError shows a sticky error message.
func (a *App) SetError(msg string) {
	a.setMessageInner(msg, MessageError, 0)
}

// SetStickyWarning shows a warning that stays until something else
// overwrites it. Used for state-tied messages like the dirty-quit prompt
// where the visual must outlive any TTL.
func (a *App) SetStickyWarning(msg string) {
	a.setMessageInner(msg, MessageWarning, 0)
}

func (a *App) setMessageInner(msg string, messageType MessageType, ttl time.Duration) {
	var expiresAt *time.Time
	if ttl > 0 {
		t := now().Add(ttl)
		expiresAt = &t
	}
	a.Message = &Message{Content: msg, Type: messageType, ExpiresAt: expiresAt}
}

// ClearExpiredMessage clears a TTL-expired message and reports true so the
// main loop can schedule a redraw.
func (a *App) ClearExpiredMessage() bool {
	expired := a.Message != nil && a.Message.ExpiresAt != nil && !now().Before(*a.Message.ExpiresAt)
	if expired {
		a.Message = nil
	}
	return expired
}

// EnterCommandMode opens the : command shell with an empty buffer.
func (a *App) EnterCommandMode() {
	a.InputMode = input.ModeCommand
	a.CommandBuffer = ""
}

// ExitCommandMode returns to normal mode and clears the command buffer.
func (a *App) ExitCommandMode() {
	a.InputMode = input.ModeNormal
	a.CommandBuffer = ""
}

// EnterSearchMode opens the / search shell, remembering which mode to
// return to on exit.
func (a *App) EnterSearchMode() {
	a.SearchReturnMode = a.InputMode
	a.InputMode = input.ModeSearch
	a.SearchBuffer = ""
}

// ExitSearchMode returns to the mode active when search was entered.
func (a *App) ExitSearchMode() {
	a.InputMode = a.SearchReturnMode
	a.SearchBuffer = ""
}

// SearchingHelp reports whether the active search targets the help overlay.
func (a *App) SearchingHelp() bool {
	return a.InputMode == input.ModeSearch && a.SearchReturnMode == input.ModeHelp
}

// ToggleHelp opens or closes the help overlay, resetting its scroll on
// open.
func (a *App) ToggleHelp() {
	if a.InputMode == input.ModeHelp {
		a.InputMode = input.ModeNormal
	} else {
		a.InputMode = input.ModeHelp
		a.HelpState.ScrollOffset = 0
	}
}

// HelpScrollDown scrolls the help overlay down, clamped to the content.
func (a *App) HelpScrollDown(lines int) {
	maxOffset := satSub(a.HelpState.TotalLines, a.HelpState.ViewportHeight)
	a.HelpState.ScrollOffset = min(a.HelpState.ScrollOffset+lines, maxOffset)
}

// HelpScrollUp scrolls the help overlay up.
func (a *App) HelpScrollUp(lines int) {
	a.HelpState.ScrollOffset = satSub(a.HelpState.ScrollOffset, lines)
}

// HelpScrollToTop jumps the help overlay to the top.
func (a *App) HelpScrollToTop() {
	a.HelpState.ScrollOffset = 0
}

// HelpScrollToBottom jumps the help overlay to the bottom.
func (a *App) HelpScrollToBottom() {
	a.HelpState.ScrollOffset = satSub(a.HelpState.TotalLines, a.HelpState.ViewportHeight)
}
