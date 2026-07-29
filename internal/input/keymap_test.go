package input

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func key(text string, code rune, mod tea.KeyMod) tea.Key {
	return tea.Key{Text: text, Code: code, Mod: mod}
}

func pr(r rune) tea.Key { return key(string(r), r, 0) } // printable, no mods

func TestNormalModeBindings(t *testing.T) {
	cases := []struct {
		k    tea.Key
		want Kind
		n    int
	}{
		{pr('j'), CursorDown, 1},
		{pr('k'), CursorUp, 1},
		{key("", tea.KeyDown, 0), CursorDown, 1},
		{key("", tea.KeyUp, 0), CursorUp, 1},
		{key("", 'e', tea.ModCtrl), ScrollViewDown, 1},
		{key("", 'y', tea.ModCtrl), ScrollViewUp, 1},
		{key("", 'd', tea.ModCtrl), HalfPageDown, 0},
		{key("", 'u', tea.ModCtrl), HalfPageUp, 0},
		{key("", 'f', tea.ModCtrl), PageDown, 0},
		{key("", 'b', tea.ModCtrl), PageUp, 0},
		{key("", tea.KeyPgDown, 0), PageDown, 0},
		{key("", tea.KeyPgUp, 0), PageUp, 0},
		{pr('g'), GoToTop, 0},
		{key("G", 'g', tea.ModShift), GoToBottom, 0},
		{pr('z'), PendingZCommand, 0},
		{key("Z", 'z', tea.ModShift), PendingShiftZCommand, 0},
		{key("}", '}', 0), NextFile, 0},
		{key("{", '{', 0), PrevFile, 0},
		{key("]", ']', 0), NextHunk, 0},
		{key("[", '[', 0), PrevHunk, 0},
		{pr('m'), NextComment, 0},
		{key("M", 'm', tea.ModShift), PrevComment, 0},
		{key(")", ')', 0), CycleCommitNext, 0},
		{key("(", '(', 0), CycleCommitPrev, 0},
		{key("", tea.KeyTab, 0), ToggleFocus, 0},
		{key("", tea.KeyTab, tea.ModShift), ToggleFocusReverse, 0},
		{key("", tea.KeyEnter, 0), SelectFile, 0},
		{key("", tea.KeyEnter, tea.ModShift), SelectFileFull, 0},
		{pr('h'), ScrollLeft, 4},
		{pr('l'), ScrollRight, 4},
		{key("", tea.KeyLeft, 0), ScrollLeft, 4},
		{key("", tea.KeyRight, 0), ScrollRight, 4},
		{pr('r'), ToggleReviewed, 0},
		{key("R", 'r', tea.ModShift), ToggleHunkReviewed, 0},
		{pr('c'), AddLineComment, 0},
		{key("C", 'c', tea.ModShift), AddFileComment, 0},
		{pr('i'), EditComment, 0},
		{key("A", 'a', tea.ModShift), EditCommentAtEnd, 0},
		{pr('d'), PendingDCommand, 0},
		{pr('v'), EnterVisualMode, 0},
		{key("V", 'v', tea.ModShift), EnterVisualMode, 0},
		{pr('y'), ExportToClipboard, 0},
		{pr('n'), SearchNext, 0},
		{key("N", 'n', tea.ModShift), SearchPrev, 0},
		{key(":", ':', tea.ModShift), EnterCommandMode, 0},
		{key("/", '/', 0), EnterSearchMode, 0},
		{key("?", '?', tea.ModShift), ToggleHelp, 0},
		{key("", tea.KeyEscape, 0), ExitMode, 0},
		{pr('q'), Quit, 0},
		{key(" ", tea.KeySpace, 0), ToggleExpand, 0},
		{pr('o'), ExpandAll, 0},
		{key("O", 'o', tea.ModShift), CollapseAll, 0},
		{pr('5'), Digit, 5},
		{pr('0'), Digit, 0},
		{pr(';'), PendingLeaderCommand, 0},
	}
	for _, c := range cases {
		got := MapKey(c.k, ModeNormal, ';')
		if got.Kind != c.want || got.N != c.n {
			t.Errorf("key %+v: got %v/%d, want %v/%d", c.k, got.Kind, got.N, c.want, c.n)
		}
	}
}

func TestNormalModeDeliberateNonBindings(t *testing.T) {
	// Bare `e` is unbound (verified by tuicr unit test).
	if got := MapKey(pr('e'), ModeNormal, ';'); got.Kind != None {
		t.Errorf("bare e must be unbound, got %v", got.Kind)
	}
	// Shifted digits never produce Digit.
	if got := MapKey(key("%", '5', tea.ModShift), ModeNormal, ';'); got.Kind == Digit {
		t.Error("shifted digit must not be Digit")
	}
}

func TestLeaderIsConfigurable(t *testing.T) {
	// With leader ',' the comma binds and ';' falls through to nothing.
	if got := MapKey(pr(','), ModeNormal, ','); got.Kind != PendingLeaderCommand {
		t.Errorf("got %v", got.Kind)
	}
	if got := MapKey(pr(';'), ModeNormal, ','); got.Kind != None {
		t.Errorf("got %v", got.Kind)
	}
	// Leader wins over an existing binding when remapped onto one.
	if got := MapKey(pr('m'), ModeNormal, 'm'); got.Kind != PendingLeaderCommand {
		t.Errorf("leader must be matched first, got %v", got.Kind)
	}
}

func TestCommandAndSearchModes(t *testing.T) {
	if got := MapKey(key("", tea.KeyTab, 0), ModeCommand, ';'); got.Kind != CompleteCommand {
		t.Errorf("command Tab = %v", got.Kind)
	}
	if got := MapKey(key("", tea.KeyTab, tea.ModShift), ModeCommand, ';'); got.Kind != CompleteCommandReverse {
		t.Errorf("command BackTab = %v", got.Kind)
	}
	// Search mode has no completion.
	if got := MapKey(key("", tea.KeyTab, 0), ModeSearch, ';'); got.Kind != None {
		t.Errorf("search Tab = %v", got.Kind)
	}
	for _, mode := range []Mode{ModeCommand, ModeSearch} {
		if got := MapKey(key("", 'w', tea.ModCtrl), mode, ';'); got.Kind != DeleteWord {
			t.Errorf("mode %v Ctrl-W = %v", mode, got.Kind)
		}
		if got := MapKey(key("", 'u', tea.ModCtrl), mode, ';'); got.Kind != ClearLine {
			t.Errorf("mode %v Ctrl-U = %v", mode, got.Kind)
		}
		if got := MapKey(key("", tea.KeyEnter, 0), mode, ';'); got.Kind != SubmitInput {
			t.Errorf("mode %v Enter = %v", mode, got.Kind)
		}
		if got := MapKey(pr('x'), mode, ';'); got.Kind != InsertChar || got.Ch != 'x' {
			t.Errorf("mode %v printable = %v", mode, got)
		}
	}
}

func TestCommentModeBindings(t *testing.T) {
	cases := []struct {
		k    tea.Key
		want Kind
	}{
		{key("", tea.KeyEscape, 0), ExitMode},
		{key("", tea.KeyEnter, 0), SubmitInput},
		{key("", tea.KeyEnter, tea.ModCtrl), SubmitInput},
		{key("", 's', tea.ModCtrl), SubmitInput},
		{key("", tea.KeyEnter, tea.ModShift), InsertChar},
		{key("", tea.KeyEnter, tea.ModAlt), InsertChar},
		{key("", 'j', tea.ModCtrl), InsertChar},
		{key("", 'k', tea.ModCtrl), InsertChar},
		{key("", tea.KeyTab, 0), CycleCommentType},
		{key("", tea.KeyTab, tea.ModShift), CycleCommentTypeReverse},
		{key("", 'a', tea.ModCtrl), TextCursorLineStart},
		{key("", 'e', tea.ModCtrl), TextCursorLineEnd},
		{key("", tea.KeyHome, 0), TextCursorLineStart},
		{key("", tea.KeyEnd, 0), TextCursorLineEnd},
		{key("", tea.KeyLeft, tea.ModAlt), TextCursorWordLeft},
		{key("", tea.KeyRight, tea.ModCtrl), TextCursorWordRight},
		{key("", tea.KeyLeft, tea.ModSuper), TextCursorLineStart},
		{key("", tea.KeyRight, tea.ModMeta), TextCursorLineEnd},
		{key("", tea.KeyLeft, 0), TextCursorLeft},
		{key("", tea.KeyRight, 0), TextCursorRight},
		{key("", tea.KeyBackspace, tea.ModAlt), DeleteWord},
		{key("", tea.KeyBackspace, tea.ModSuper), DeleteWord},
		{key("", tea.KeyBackspace, 0), DeleteChar},
		{key("", 'w', tea.ModCtrl), DeleteWord},
		{key("", 'u', tea.ModCtrl), ClearLine},
		{key("", 'b', tea.ModAlt), TextCursorWordLeft},
		{key("", 'f', tea.ModAlt), TextCursorWordRight},
	}
	for _, c := range cases {
		if got := MapKey(c.k, ModeComment, ';'); got.Kind != c.want {
			t.Errorf("key %+v: got %v, want %v", c.k, got.Kind, c.want)
		}
	}
	// Newline payload on Shift-Enter.
	if got := MapKey(key("", tea.KeyEnter, tea.ModShift), ModeComment, ';'); got.Ch != '\n' {
		t.Errorf("Shift-Enter payload = %q", got.Ch)
	}
	if got := MapKey(pr('x'), ModeComment, ';'); got.Kind != InsertChar || got.Ch != 'x' {
		t.Errorf("printable = %v", got)
	}
}

func TestHelpConfirmVisualModes(t *testing.T) {
	for _, k := range []tea.Key{key("", tea.KeyEscape, 0), pr('q'), key("?", '?', tea.ModShift)} {
		if got := MapKey(k, ModeHelp, ';'); got.Kind != ToggleHelp {
			t.Errorf("help close %+v = %v", k, got.Kind)
		}
	}
	if got := MapKey(key("/", '/', 0), ModeHelp, ';'); got.Kind != EnterSearchMode {
		t.Errorf("help / = %v", got.Kind)
	}

	for _, k := range []tea.Key{pr('y'), key("Y", 'y', tea.ModShift), key("", tea.KeyEnter, 0)} {
		if got := MapKey(k, ModeConfirm, ';'); got.Kind != ConfirmYes {
			t.Errorf("confirm yes %+v = %v", k, got.Kind)
		}
	}
	for _, k := range []tea.Key{pr('n'), key("", tea.KeyEscape, 0)} {
		if got := MapKey(k, ModeConfirm, ';'); got.Kind != ConfirmNo {
			t.Errorf("confirm no %+v = %v", k, got.Kind)
		}
	}
	// SubmitConfirm adds reload.
	if got := MapKey(pr('r'), ModeSubmitConfirm, ';'); got.Kind != SubmitReloadPr {
		t.Errorf("submit confirm r = %v", got.Kind)
	}
	if got := MapKey(pr('r'), ModeConfirm, ';'); got.Kind != None {
		t.Errorf("plain confirm r = %v", got.Kind)
	}

	cases := map[rune]Kind{'j': CursorDown, 'c': AddRangeComment, 'y': ExportToClipboard, 'v': ExitMode, 'q': Quit}
	for r, want := range cases {
		if got := MapKey(pr(r), ModeVisualSelect, ';'); got.Kind != want {
			t.Errorf("visual %c = %v, want %v", r, got.Kind, want)
		}
	}
	if got := MapKey(key("", tea.KeyEnter, 0), ModeVisualSelect, ';'); got.Kind != AddRangeComment {
		t.Errorf("visual Enter = %v", got.Kind)
	}
}

func TestCommitSelectAndSubmitModes(t *testing.T) {
	cs := map[rune]Kind{'j': CommitSelectDown, 'k': CommitSelectUp, 'q': Quit,
		'/': BeginTargetFilter, 'r': TogglePrReviewRequestedFilter}
	for r, want := range cs {
		if got := MapKey(pr(r), ModeCommitSelect, ';'); got.Kind != want {
			t.Errorf("commitselect %c = %v, want %v", r, got.Kind, want)
		}
	}
	if got := MapKey(key(" ", tea.KeySpace, 0), ModeCommitSelect, ';'); got.Kind != ToggleCommitSelect {
		t.Errorf("space = %v", got.Kind)
	}
	if got := MapKey(key("", tea.KeyTab, 0), ModeCommitSelect, ';'); got.Kind != TargetSelectorTabNext {
		t.Errorf("tab = %v", got.Kind)
	}
	if got := MapKey(key("", tea.KeyTab, tea.ModShift), ModeCommitSelect, ';'); got.Kind != TargetSelectorTabPrev {
		t.Errorf("backtab = %v", got.Kind)
	}

	if got := MapKey(key(" ", tea.KeySpace, 0), ModeSubmitResolver, ';'); got.Kind != SubmitResolverToggle {
		t.Errorf("resolver space = %v", got.Kind)
	}
	if got := MapKey(pr('s'), ModeSubmitResolver, ';'); got.Kind != SubmitResolverAdvance {
		t.Errorf("resolver s = %v", got.Kind)
	}
	if got := MapKey(key("", tea.KeyEnter, 0), ModeSubmitActionPicker, ';'); got.Kind != SubmitPickerConfirm {
		t.Errorf("picker enter = %v", got.Kind)
	}
}

func TestTargetFilterMode(t *testing.T) {
	if got := MapTargetFilter(key("", tea.KeyEscape, 0)); got.Kind != ExitMode {
		t.Errorf("esc = %v", got.Kind)
	}
	if got := MapTargetFilter(key("", tea.KeyEnter, 0)); got.Kind != SubmitInput {
		t.Errorf("enter = %v", got.Kind)
	}
	if got := MapTargetFilter(key("", 'u', tea.ModCtrl)); got.Kind != ClearLine {
		t.Errorf("ctrl-u = %v", got.Kind)
	}
	if got := MapTargetFilter(pr('a')); got.Kind != InsertChar || got.Ch != 'a' {
		t.Errorf("printable = %v", got)
	}
}
