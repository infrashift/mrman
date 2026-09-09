package input

import (
	tea "charm.land/bubbletea/v2"
)

// ch returns the printable rune of a key press when it carries exactly one
// character and no modifiers beyond Shift (shifted chars arrive with their
// final text, e.g. "G"). ok=false otherwise.
func ch(k tea.Key) (rune, bool) {
	if k.Mod&^tea.ModShift != 0 {
		return 0, false
	}
	runes := []rune(k.Text)
	if len(runes) != 1 {
		return 0, false
	}
	return runes[0], true
}

// ctrl reports whether the key is Ctrl+<letter> (no other modifiers).
func ctrl(k tea.Key, letter rune) bool {
	return k.Mod == tea.ModCtrl && k.Code == letter
}

// alt reports whether the key is Alt+<code> (no other modifiers).
func alt(k tea.Key, code rune) bool {
	return k.Mod == tea.ModAlt && k.Code == code
}

func isBackTab(k tea.Key) bool {
	return k.Code == tea.KeyTab && k.Mod&tea.ModShift != 0
}

// MapKey resolves a key press to an Action for the given mode. leader is the
// configurable leader character (tuicr default ';').
func MapKey(k tea.Key, mode Mode, leader rune) Action {
	switch mode {
	case ModeNormal:
		return mapNormal(k, leader)
	case ModeCommand:
		return mapCommand(k, true)
	case ModeSearch:
		return mapCommand(k, false)
	case ModeComment:
		return mapComment(k)
	case ModeHelp:
		return mapHelp(k)
	case ModeConfirm:
		return mapConfirm(k, false)
	case ModeCommitSelect:
		return mapCommitSelect(k)
	case ModeVisualSelect:
		return mapVisualSelect(k)
	case ModeSubmitResolver:
		return mapSubmitResolver(k)
	case ModeSubmitConfirm:
		return mapConfirm(k, true)
	case ModeSubmitActionPicker:
		return mapSubmitPicker(k)
	case ModeCommentPeek:
		return mapCommentPeek(k)
	}
	return act(None)
}

// binding is one keymap row: the keys it matches and the action they map
// to. Rows are tried in order, so a more specific row (Shift-Enter) goes
// before the general one (Enter).
type binding struct {
	match  func(tea.Key) bool
	action Action
}

// lookup returns the first row that matches k.
func lookup(rows []binding, k tea.Key) (Action, bool) {
	for _, row := range rows {
		if row.match(k) {
			return row.action, true
		}
	}
	return Action{}, false
}

// Row constructors, named for how the key reads in the keybindings table.
func ctrlKey(letter rune, a Action) binding {
	return binding{func(k tea.Key) bool { return ctrl(k, letter) }, a}
}

func altKey(code rune, a Action) binding {
	return binding{func(k tea.Key) bool { return alt(k, code) }, a}
}

// plainKey matches a key code with no modifier at all.
func plainKey(code rune, a Action) binding {
	return binding{func(k tea.Key) bool { return k.Code == code && k.Mod == 0 }, a}
}

// anyModKey matches a key code under any modifier.
func anyModKey(code rune, a Action) binding {
	return binding{func(k tea.Key) bool { return k.Code == code }, a}
}

// modKey matches a key code when any of the given modifier bits is set.
func modKey(code rune, mods tea.KeyMod, a Action) binding {
	return binding{func(k tea.Key) bool { return k.Code == code && k.Mod&mods != 0 }, a}
}

var backTab = binding{isBackTab, act(ToggleFocusReverse)}

// normalBindings are the modifier and special-key rows of normal mode;
// normalRunes are its plain printable keys.
var normalBindings = []binding{
	ctrlKey('e', actN(ScrollViewDown, 1)),
	ctrlKey('y', actN(ScrollViewUp, 1)),
	ctrlKey('d', act(HalfPageDown)),
	ctrlKey('u', act(HalfPageUp)),
	ctrlKey('f', act(PageDown)),
	ctrlKey('b', act(PageUp)),
	backTab,
	plainKey(tea.KeyDown, actN(CursorDown, 1)),
	plainKey(tea.KeyUp, actN(CursorUp, 1)),
	anyModKey(tea.KeyPgDown, act(PageDown)),
	anyModKey(tea.KeyPgUp, act(PageUp)),
	plainKey(tea.KeyTab, act(ToggleFocus)),
	modKey(tea.KeyEnter, tea.ModShift, act(SelectFileFull)),
	plainKey(tea.KeyEnter, act(SelectFile)),
	plainKey(tea.KeyLeft, actN(ScrollLeft, 4)),
	plainKey(tea.KeyRight, actN(ScrollRight, 4)),
	plainKey(tea.KeyEscape, act(ExitMode)),
	plainKey(tea.KeySpace, act(ToggleExpand)),
}

var normalRunes = map[rune]Action{
	'j': actN(CursorDown, 1),
	'k': actN(CursorUp, 1),
	'g': act(GoToTop),
	'G': act(GoToBottom),
	'z': act(PendingZCommand),
	'Z': act(PendingShiftZCommand),
	'}': act(NextFile),
	'{': act(PrevFile),
	']': act(NextHunk),
	'[': act(PrevHunk),
	'm': act(NextComment),
	'M': act(PrevComment),
	')': act(CycleCommitNext),
	'(': act(CycleCommitPrev),
	'h': actN(ScrollLeft, 4),
	'l': actN(ScrollRight, 4),
	'r': act(ToggleReviewed),
	'R': act(ToggleHunkReviewed),
	'c': act(AddLineComment),
	'C': act(AddFileComment),
	'i': act(EditComment),
	'A': act(EditCommentAtEnd),
	'd': act(PendingDCommand),
	'v': act(EnterVisualMode),
	'V': act(EnterVisualMode),
	'y': act(ExportToClipboard),
	'n': act(SearchNext),
	'N': act(SearchPrev),
	':': act(EnterCommandMode),
	'/': act(EnterSearchMode),
	'?': act(ToggleHelp),
	'q': act(Quit),
	'o': act(ExpandAll),
	'O': act(CollapseAll),
}

func mapNormal(k tea.Key, leader rune) Action {
	// Leader is matched first so a remapped leader beats every other arm.
	if r, ok := ch(k); ok && r == leader {
		return act(PendingLeaderCommand)
	}
	if a, ok := lookup(normalBindings, k); ok {
		return a
	}
	r, ok := ch(k)
	if !ok {
		return act(None)
	}
	if a, ok := normalRunes[r]; ok {
		return a
	}
	// Digits only unshifted (shifted digits produce symbols and never reach
	// here as digits).
	if r >= '0' && r <= '9' && k.Mod == 0 {
		return actN(Digit, int(r-'0'))
	}
	return act(None)
}

// mapCommand covers Command mode; withCompletion=false yields Search mode
// (identical minus Tab completion).
func mapCommand(k tea.Key, withCompletion bool) Action {
	switch {
	case ctrl(k, 'w'):
		return act(DeleteWord)
	case ctrl(k, 'u'):
		return act(ClearLine)
	case alt(k, tea.KeyBackspace):
		return act(DeleteWord)
	}
	if withCompletion && isBackTab(k) {
		return act(CompleteCommandReverse)
	}
	switch k.Code {
	case tea.KeyEscape:
		return act(ExitMode)
	case tea.KeyEnter:
		return act(SubmitInput)
	case tea.KeyTab:
		if withCompletion && k.Mod == 0 {
			return act(CompleteCommand)
		}
	case tea.KeyBackspace:
		if k.Mod == 0 {
			return act(DeleteChar)
		}
	}
	if r, ok := ch(k); ok {
		return Action{Kind: InsertChar, Ch: r}
	}
	return act(None)
}

// commentBindings are the comment editor's rows. The modified Enter family
// inserts a newline; Ctrl-J/Ctrl-K are aliases that survive terminals
// without the kitty protocol.
var commentBindings = []binding{
	ctrlKey('s', act(SubmitInput)),
	ctrlKey('j', Action{Kind: InsertChar, Ch: '\n'}),
	ctrlKey('k', Action{Kind: InsertChar, Ch: '\n'}),
	ctrlKey('a', act(TextCursorLineStart)),
	ctrlKey('e', act(TextCursorLineEnd)),
	ctrlKey('w', act(DeleteWord)),
	ctrlKey('u', act(ClearLine)),
	altKey('b', act(TextCursorWordLeft)),
	altKey('f', act(TextCursorWordRight)),
	altKey(tea.KeyBackspace, act(DeleteWord)),
	altKey(tea.KeyLeft, act(TextCursorWordLeft)),
	altKey(tea.KeyRight, act(TextCursorWordRight)),
	modKey(tea.KeyLeft, tea.ModCtrl, act(TextCursorWordLeft)),
	modKey(tea.KeyRight, tea.ModCtrl, act(TextCursorWordRight)),
	modKey(tea.KeyLeft, tea.ModSuper|tea.ModMeta, act(TextCursorLineStart)),
	modKey(tea.KeyRight, tea.ModSuper|tea.ModMeta, act(TextCursorLineEnd)),
	modKey(tea.KeyBackspace, tea.ModSuper|tea.ModMeta, act(DeleteWord)),
	{isBackTab, act(CycleCommentTypeReverse)},
	anyModKey(tea.KeyEscape, act(ExitMode)),
	modKey(tea.KeyEnter, tea.ModShift|tea.ModAlt, Action{Kind: InsertChar, Ch: '\n'}),
	anyModKey(tea.KeyEnter, act(SubmitInput)),
	anyModKey(tea.KeyTab, act(CycleCommentType)),
	anyModKey(tea.KeyHome, act(TextCursorLineStart)),
	anyModKey(tea.KeyEnd, act(TextCursorLineEnd)),
	plainKey(tea.KeyLeft, act(TextCursorLeft)),
	plainKey(tea.KeyRight, act(TextCursorRight)),
	plainKey(tea.KeyBackspace, act(DeleteChar)),
}

func mapComment(k tea.Key) Action {
	if a, ok := lookup(commentBindings, k); ok {
		return a
	}
	if r, ok := ch(k); ok {
		return Action{Kind: InsertChar, Ch: r}
	}
	return act(None)
}

// mapCommentPeek maps keys for the read-only comment peek panel: scroll with
// the usual vertical motions, dismiss with Esc, q or Enter.
func mapCommentPeek(k tea.Key) Action {
	switch {
	case ctrl(k, 'd'):
		return act(HalfPageDown)
	case ctrl(k, 'u'):
		return act(HalfPageUp)
	}
	switch k.Code {
	case tea.KeyEscape, tea.KeyEnter:
		return act(ExitMode)
	case tea.KeyDown:
		return actN(CursorDown, 1)
	case tea.KeyUp:
		return actN(CursorUp, 1)
	}
	if r, ok := ch(k); ok {
		switch r {
		case 'q':
			return act(ExitMode)
		case 'j':
			return actN(CursorDown, 1)
		case 'k':
			return actN(CursorUp, 1)
		}
	}
	return act(None)
}

func mapHelp(k tea.Key) Action {
	switch {
	case ctrl(k, 'd'):
		return act(HalfPageDown)
	case ctrl(k, 'u'):
		return act(HalfPageUp)
	case ctrl(k, 'f'):
		return act(PageDown)
	case ctrl(k, 'b'):
		return act(PageUp)
	}
	switch k.Code {
	case tea.KeyEscape:
		return act(ToggleHelp)
	case tea.KeyDown:
		return actN(CursorDown, 1)
	case tea.KeyUp:
		return actN(CursorUp, 1)
	case tea.KeyPgDown:
		return act(PageDown)
	case tea.KeyPgUp:
		return act(PageUp)
	}
	if r, ok := ch(k); ok {
		switch r {
		case 'q', '?':
			return act(ToggleHelp)
		case 'j':
			return actN(CursorDown, 1)
		case 'k':
			return actN(CursorUp, 1)
		case 'g':
			return act(GoToTop)
		case 'G':
			return act(GoToBottom)
		case '/':
			return act(EnterSearchMode)
		case 'n':
			return act(SearchNext)
		case 'N':
			return act(SearchPrev)
		}
	}
	// Digits accumulate a count, as in Normal mode: the popup is a
	// scrollable document, and "20j" doing nothing was the one place a
	// count was silently dropped.
	if r, ok := ch(k); ok && r >= '0' && r <= '9' && k.Mod == 0 {
		return actN(Digit, int(r-'0'))
	}
	return act(None)
}

// mapConfirm covers Confirm mode and, with reload=true, SubmitConfirm
// (which adds r/R for reload).
func mapConfirm(k tea.Key, reload bool) Action {
	switch k.Code {
	case tea.KeyEnter:
		return act(ConfirmYes)
	case tea.KeyEscape:
		return act(ConfirmNo)
	}
	if r, ok := ch(k); ok {
		switch r {
		case 'y', 'Y':
			return act(ConfirmYes)
		case 'n', 'N':
			return act(ConfirmNo)
		case 'r', 'R':
			if reload {
				return act(SubmitReloadPr)
			}
		}
	}
	return act(None)
}

func mapCommitSelect(k tea.Key) Action {
	if isBackTab(k) {
		return act(TargetSelectorTabPrev)
	}
	switch k.Code {
	case tea.KeyDown:
		return act(CommitSelectDown)
	case tea.KeyUp:
		return act(CommitSelectUp)
	case tea.KeySpace:
		return act(ToggleCommitSelect)
	case tea.KeyEnter:
		return act(ConfirmCommitSelect)
	case tea.KeyEscape:
		return act(ExitMode)
	case tea.KeyTab:
		if k.Mod == 0 {
			return act(TargetSelectorTabNext)
		}
	}
	if r, ok := ch(k); ok {
		switch r {
		case 'j':
			return act(CommitSelectDown)
		case 'k':
			return act(CommitSelectUp)
		case 'q':
			return act(Quit)
		case '/':
			return act(BeginTargetFilter)
		case 'r':
			return act(TogglePrReviewRequestedFilter)
		}
	}
	return act(None)
}

func mapVisualSelect(k tea.Key) Action {
	switch k.Code {
	case tea.KeyDown:
		return actN(CursorDown, 1)
	case tea.KeyUp:
		return actN(CursorUp, 1)
	case tea.KeyEnter:
		return act(AddRangeComment)
	case tea.KeyEscape:
		return act(ExitMode)
	}
	if r, ok := ch(k); ok {
		switch r {
		case 'j':
			return actN(CursorDown, 1)
		case 'k':
			return actN(CursorUp, 1)
		case 'c':
			return act(AddRangeComment)
		case ']':
			return act(NextHunk)
		case '[':
			return act(PrevHunk)
		case 'y':
			return act(ExportToClipboard)
		case 'v', 'V':
			return act(ExitMode)
		case 'q':
			return act(Quit)
		}
	}
	return act(None)
}

func mapSubmitResolver(k tea.Key) Action {
	switch k.Code {
	case tea.KeyDown:
		return act(SubmitResolverDown)
	case tea.KeyUp:
		return act(SubmitResolverUp)
	case tea.KeyEnter, tea.KeySpace:
		return act(SubmitResolverToggle)
	case tea.KeyEscape:
		return act(ExitMode)
	}
	if r, ok := ch(k); ok {
		switch r {
		case 'j':
			return act(SubmitResolverDown)
		case 'k':
			return act(SubmitResolverUp)
		case 's':
			return act(SubmitResolverAdvance)
		}
	}
	return act(None)
}

func mapSubmitPicker(k tea.Key) Action {
	switch k.Code {
	case tea.KeyDown:
		return act(SubmitPickerDown)
	case tea.KeyUp:
		return act(SubmitPickerUp)
	case tea.KeyEnter:
		return act(SubmitPickerConfirm)
	case tea.KeyEscape:
		return act(ExitMode)
	}
	if r, ok := ch(k); ok {
		switch r {
		case 'j':
			return act(SubmitPickerDown)
		case 'k':
			return act(SubmitPickerUp)
		case 'q':
			return act(Quit)
		}
	}
	return act(None)
}

// MapTargetFilter handles the PR-list filter sub-state of the target
// selector.
func MapTargetFilter(k tea.Key) Action {
	switch {
	case ctrl(k, 'u'):
		return act(ClearLine)
	case ctrl(k, 'w'):
		return act(DeleteWord)
	case alt(k, tea.KeyBackspace):
		return act(DeleteWord)
	}
	switch k.Code {
	case tea.KeyEscape:
		return act(ExitMode)
	case tea.KeyEnter:
		return act(SubmitInput)
	case tea.KeyBackspace:
		if k.Mod == 0 {
			return act(DeleteChar)
		}
	}
	if r, ok := ch(k); ok {
		return Action{Kind: InsertChar, Ch: r}
	}
	return act(None)
}
