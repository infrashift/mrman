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

func mapNormal(k tea.Key, leader rune) Action {
	// Leader is matched first so a remapped leader beats every other arm.
	if r, ok := ch(k); ok && r == leader {
		return act(PendingLeaderCommand)
	}

	switch {
	case ctrl(k, 'e'):
		return actN(ScrollViewDown, 1)
	case ctrl(k, 'y'):
		return actN(ScrollViewUp, 1)
	case ctrl(k, 'd'):
		return act(HalfPageDown)
	case ctrl(k, 'u'):
		return act(HalfPageUp)
	case ctrl(k, 'f'):
		return act(PageDown)
	case ctrl(k, 'b'):
		return act(PageUp)
	}

	if isBackTab(k) {
		return act(ToggleFocusReverse)
	}
	switch k.Code {
	case tea.KeyDown:
		if k.Mod == 0 {
			return actN(CursorDown, 1)
		}
	case tea.KeyUp:
		if k.Mod == 0 {
			return actN(CursorUp, 1)
		}
	case tea.KeyPgDown:
		return act(PageDown)
	case tea.KeyPgUp:
		return act(PageUp)
	case tea.KeyTab:
		if k.Mod == 0 {
			return act(ToggleFocus)
		}
	case tea.KeyEnter:
		if k.Mod&tea.ModShift != 0 {
			return act(SelectFileFull)
		}
		if k.Mod == 0 {
			return act(SelectFile)
		}
	case tea.KeyLeft:
		if k.Mod == 0 {
			return actN(ScrollLeft, 4)
		}
	case tea.KeyRight:
		if k.Mod == 0 {
			return actN(ScrollRight, 4)
		}
	case tea.KeyEscape:
		if k.Mod == 0 {
			return act(ExitMode)
		}
	case tea.KeySpace:
		if k.Mod == 0 {
			return act(ToggleExpand)
		}
	}

	r, ok := ch(k)
	if !ok {
		return act(None)
	}
	switch r {
	case 'j':
		return actN(CursorDown, 1)
	case 'k':
		return actN(CursorUp, 1)
	case 'g':
		return act(GoToTop)
	case 'G':
		return act(GoToBottom)
	case 'z':
		return act(PendingZCommand)
	case 'Z':
		return act(PendingShiftZCommand)
	case '}':
		return act(NextFile)
	case '{':
		return act(PrevFile)
	case ']':
		return act(NextHunk)
	case '[':
		return act(PrevHunk)
	case 'm':
		return act(NextComment)
	case 'M':
		return act(PrevComment)
	case ')':
		return act(CycleCommitNext)
	case '(':
		return act(CycleCommitPrev)
	case 'h':
		return actN(ScrollLeft, 4)
	case 'l':
		return actN(ScrollRight, 4)
	case 'r':
		return act(ToggleReviewed)
	case 'R':
		return act(ToggleHunkReviewed)
	case 'c':
		return act(AddLineComment)
	case 'C':
		return act(AddFileComment)
	case 'i':
		return act(EditComment)
	case 'A':
		return act(EditCommentAtEnd)
	case 'd':
		return act(PendingDCommand)
	case 'v', 'V':
		return act(EnterVisualMode)
	case 'y':
		return act(ExportToClipboard)
	case 'n':
		return act(SearchNext)
	case 'N':
		return act(SearchPrev)
	case ':':
		return act(EnterCommandMode)
	case '/':
		return act(EnterSearchMode)
	case '?':
		return act(ToggleHelp)
	case 'q':
		return act(Quit)
	case 'o':
		return act(ExpandAll)
	case 'O':
		return act(CollapseAll)
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

func mapComment(k tea.Key) Action {
	// Modified Enter family: newline inserts. Ctrl-J/Ctrl-K are aliases
	// that survive terminals without the kitty protocol.
	switch {
	case ctrl(k, 's'):
		return act(SubmitInput)
	case ctrl(k, 'j'), ctrl(k, 'k'):
		return Action{Kind: InsertChar, Ch: '\n'}
	case ctrl(k, 'a'):
		return act(TextCursorLineStart)
	case ctrl(k, 'e'):
		return act(TextCursorLineEnd)
	case ctrl(k, 'w'):
		return act(DeleteWord)
	case ctrl(k, 'u'):
		return act(ClearLine)
	case alt(k, 'b'):
		return act(TextCursorWordLeft)
	case alt(k, 'f'):
		return act(TextCursorWordRight)
	case alt(k, tea.KeyBackspace):
		return act(DeleteWord)
	case alt(k, tea.KeyLeft):
		return act(TextCursorWordLeft)
	case alt(k, tea.KeyRight):
		return act(TextCursorWordRight)
	case k.Mod&tea.ModCtrl != 0 && k.Code == tea.KeyLeft:
		return act(TextCursorWordLeft)
	case k.Mod&tea.ModCtrl != 0 && k.Code == tea.KeyRight:
		return act(TextCursorWordRight)
	case k.Mod&(tea.ModSuper|tea.ModMeta) != 0 && k.Code == tea.KeyLeft:
		return act(TextCursorLineStart)
	case k.Mod&(tea.ModSuper|tea.ModMeta) != 0 && k.Code == tea.KeyRight:
		return act(TextCursorLineEnd)
	case k.Mod&(tea.ModSuper|tea.ModMeta) != 0 && k.Code == tea.KeyBackspace:
		return act(DeleteWord)
	}
	if isBackTab(k) {
		return act(CycleCommentTypeReverse)
	}
	switch k.Code {
	case tea.KeyEscape:
		return act(ExitMode)
	case tea.KeyEnter:
		switch {
		case k.Mod&tea.ModShift != 0, k.Mod&tea.ModAlt != 0:
			return Action{Kind: InsertChar, Ch: '\n'}
		case k.Mod&tea.ModCtrl != 0:
			return act(SubmitInput)
		default:
			return act(SubmitInput)
		}
	case tea.KeyTab:
		return act(CycleCommentType)
	case tea.KeyHome:
		return act(TextCursorLineStart)
	case tea.KeyEnd:
		return act(TextCursorLineEnd)
	case tea.KeyLeft:
		if k.Mod == 0 {
			return act(TextCursorLeft)
		}
	case tea.KeyRight:
		if k.Mod == 0 {
			return act(TextCursorRight)
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
