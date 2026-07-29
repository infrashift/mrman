// Package input maps terminal key events to editor actions, porting tuicr's
// per-mode keybinding tables (src/input/keybindings.rs). Bindings are
// hardcoded by design; the only configurable knob is the single-character
// leader key.
package input

// Kind enumerates every action the TUI can dispatch.
type Kind uint16

// Actions, one per tuicr Action variant.
const (
	None Kind = iota
	CursorDown
	CursorUp
	HalfPageDown
	HalfPageUp
	PageDown
	PageUp
	GoToTop
	GoToBottom
	Digit
	NextFile
	PrevFile
	NextHunk
	PrevHunk
	NextComment
	PrevComment
	PendingZCommand
	PendingShiftZCommand
	PendingLeaderCommand
	PendingDCommand
	ScrollLeft
	ScrollRight
	ScrollViewDown
	ScrollViewUp
	ToggleFocus
	ToggleFocusReverse
	SelectFile
	SelectFileFull
	ToggleReviewed
	ToggleHunkReviewed
	AddLineComment
	AddFileComment
	EditComment
	EditCommentAtEnd
	SearchNext
	SearchPrev
	EnterVisualMode
	AddRangeComment
	Quit
	ExportToClipboard
	EnterCommandMode
	EnterSearchMode
	ExitMode
	ToggleHelp
	InsertChar
	Paste
	DeleteChar
	DeleteWord
	ClearLine
	SubmitInput
	CompleteCommand
	CompleteCommandReverse
	TextCursorLeft
	TextCursorRight
	TextCursorLineStart
	TextCursorLineEnd
	TextCursorWordLeft
	TextCursorWordRight
	CycleCommentType
	CycleCommentTypeReverse
	ConfirmYes
	ConfirmNo
	CommitSelectUp
	CommitSelectDown
	ToggleCommitSelect
	ConfirmCommitSelect
	CycleCommitNext
	CycleCommitPrev
	TargetSelectorTabNext
	TargetSelectorTabPrev
	BeginTargetFilter
	TogglePrReviewRequestedFilter
	SubmitResolverDown
	SubmitResolverUp
	SubmitResolverToggle
	SubmitResolverAdvance
	SubmitReloadPr
	SubmitPickerDown
	SubmitPickerUp
	SubmitPickerConfirm
	ToggleExpand
	ExpandAll
	CollapseAll
)

// Action is a dispatched action with its parameters.
type Action struct {
	Kind Kind
	N    int    // CursorDown/Up, Scroll*, Digit magnitude
	Ch   rune   // InsertChar payload
	Text string // Paste payload
}

// Mode mirrors the application's input modes for keymap selection.
type Mode int

// Input modes.
const (
	ModeNormal Mode = iota
	ModeComment
	ModeCommand
	ModeSearch
	ModeHelp
	ModeConfirm
	ModeCommitSelect
	ModeVisualSelect
	ModeSubmitResolver
	ModeSubmitConfirm
	ModeSubmitActionPicker
)

func act(kind Kind) Action         { return Action{Kind: kind} }
func actN(kind Kind, n int) Action { return Action{Kind: kind, N: n} }
