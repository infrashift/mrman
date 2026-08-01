package input

import "testing"

func TestParseCommandAliases(t *testing.T) {
	cases := map[string]CommandKind{
		"q": CmdQuit, "quit": CmdQuit, "q!": CmdForceQuit, "quit!": CmdForceQuit,
		"w": CmdWrite, "write": CmdWrite, "x": CmdWriteQuit, "wq": CmdWriteQuit,
		"e": CmdReload, "reload": CmdReload, "edit": CmdEdit,
		"clip": CmdExport, "export": CmdExport,
		"clear": CmdClear, "clearc": CmdClearCommentsOnly,
		"help": CmdHelp, "h": CmdHelp, "version": CmdVersion,
		"set wrap": CmdSetWrap, "set wrap!": CmdToggleWrap, "wrap": CmdToggleWrap,
		"vim": CmdToggleVim, "set vim!": CmdToggleVim, "set vim": CmdSetVim,
		"novim": CmdSetNoVim, "set novim": CmdSetNoVim,
		"set commits": CmdSetCommitsVisible, "set nocommits": CmdSetCommitsHidden,
		"set commits!": CmdToggleCommits,
		"diff":         CmdDiff, "focus": CmdFocus, "f": CmdFocus, "stage": CmdStage,
		"commits": CmdTargetsLocal, "targets": CmdTargetsLocal,
		"mrs":    CmdTargetsPrs,
		"submit": CmdSubmitPicker, "submit comment": CmdSubmitComment,
		"submit approve": CmdSubmitApprove, "submit request-changes": CmdSubmitRequestChanges,
		"submit draft":        CmdSubmitDraft,
		"comments unresolved": CmdCommentsUnresolved, "comments all": CmdCommentsAll,
		"comments hide": CmdCommentsHide,
	}
	for name, want := range cases {
		if got := ParseCommand(name); got.Kind != want {
			t.Errorf("ParseCommand(%q) = %v, want %v", name, got.Kind, want)
		}
	}
}

func TestParseCommandLineJumps(t *testing.T) {
	if got := ParseCommand("42"); got.Kind != CmdGotoLine || got.N != 42 {
		t.Errorf("got %+v", got)
	}
	if got := ParseCommand("o17"); got.Kind != CmdGotoLineOld || got.N != 17 {
		t.Errorf("got %+v", got)
	}
	for _, s := range []string{"0", "o0", "abc", "o", "4x2", ""} {
		if got := ParseCommand(s); got.Kind == CmdGotoLine || got.Kind == CmdGotoLineOld {
			t.Errorf("ParseCommand(%q) must not be a line jump: %+v", s, got)
		}
	}
	if got := ParseCommand("bogus"); got.Kind != CmdUnknown || got.Raw != "bogus" {
		t.Errorf("got %+v", got)
	}
	// Whitespace trimmed.
	if got := ParseCommand("  wq  "); got.Kind != CmdWriteQuit {
		t.Errorf("got %+v", got)
	}
}

func TestCompleteSingleMatch(t *testing.T) {
	res := Complete("versi", nil, false)
	if res.Buffer != "version" || res.State != nil || res.Message != "" {
		t.Fatalf("got %+v", res)
	}
}

func TestCompleteNoMatch(t *testing.T) {
	res := Complete("zzz", nil, false)
	if res.Message == "" || res.Buffer != "zzz" {
		t.Fatalf("got %+v", res)
	}
}

func TestCompleteExtendsToCommonPrefix(t *testing.T) {
	// "cl" → clip, clear, clearc share "cl"; extending gives... clip vs clear
	// diverge at 3rd char, so no extension — a cycle starts instead.
	res := Complete("cle", nil, false)
	// clear, clearc share "clear".
	if res.Buffer != "clear" || res.State != nil {
		t.Fatalf("got %+v", res)
	}
}

func TestCompleteCycles(t *testing.T) {
	// "submit " prefixes: submit comment/approve/request-changes/draft share
	// "submit " exactly, so a cycle starts immediately.
	res := Complete("submit ", nil, false)
	if res.State == nil {
		t.Fatalf("expected a cycle: %+v", res)
	}
	if res.Buffer != "submit comment" {
		t.Fatalf("first candidate = %q", res.Buffer)
	}
	// Advance.
	res2 := Complete(res.Buffer, res.State, false)
	if res2.Buffer != "submit approve" {
		t.Fatalf("second = %q", res2.Buffer)
	}
	// Reverse goes back.
	res3 := Complete(res2.Buffer, res2.State, true)
	if res3.Buffer != "submit comment" {
		t.Fatalf("reversed = %q", res3.Buffer)
	}
	// Reverse from a fresh cycle starts at the end.
	resR := Complete("submit ", nil, true)
	if resR.State == nil || resR.Buffer != "submit draft" {
		t.Fatalf("reverse start = %+v", resR)
	}
}

func TestCompleteCycleWrapsAround(t *testing.T) {
	res := Complete("submit ", nil, false)
	buf, st := res.Buffer, res.State
	for i := 0; i < len(st.Matches); i++ {
		r := Complete(buf, st, false)
		buf, st = r.Buffer, r.State
	}
	if buf != "submit comment" {
		t.Fatalf("wrap-around = %q", buf)
	}
}

func TestCommandNamesStable(t *testing.T) {
	names := CommandNames()
	if len(names) == 0 || names[0] != "q" {
		t.Fatalf("names[0] = %v", names[:3])
	}
}
