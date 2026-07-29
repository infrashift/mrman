package input

import (
	"strconv"
	"strings"
)

// CommandKind enumerates the `:` commands, ported from tuicr's COMMAND_SPECS.
type CommandKind int

// Command kinds.
const (
	CmdUnknown CommandKind = iota
	CmdQuit
	CmdForceQuit
	CmdWrite
	CmdWriteQuit
	CmdReload
	CmdEdit
	CmdExport
	CmdClear
	CmdClearCommentsOnly
	CmdHelp
	CmdVersion
	CmdSetWrap
	CmdToggleWrap
	CmdToggleVim
	CmdSetVim
	CmdSetNoVim
	CmdSetCommitsVisible
	CmdSetCommitsHidden
	CmdToggleCommits
	CmdDiff
	CmdFocus
	CmdStage
	CmdTargetsLocal
	CmdTargetsPrs
	CmdSubmitPicker
	CmdSubmitComment
	CmdSubmitApprove
	CmdSubmitRequestChanges
	CmdSubmitDraft
	CmdCommentsUnresolved
	CmdCommentsAll
	CmdCommentsHide
	CmdGotoLine    // :{N} — new side
	CmdGotoLineOld // :o{N} — old side
)

// Command is a parsed command with an optional numeric argument.
type Command struct {
	Kind CommandKind
	N    uint32 // GotoLine target
	Raw  string // original input, for error messages
}

// commandSpecs maps every accepted spelling to its kind, in tuicr's order
// (order matters for completion listing).
var commandSpecs = []struct {
	name string
	kind CommandKind
}{
	{"q", CmdQuit}, {"quit", CmdQuit},
	{"q!", CmdForceQuit}, {"quit!", CmdForceQuit},
	{"w", CmdWrite}, {"write", CmdWrite},
	{"x", CmdWriteQuit}, {"wq", CmdWriteQuit},
	{"e", CmdReload}, {"reload", CmdReload},
	{"edit", CmdEdit},
	{"clip", CmdExport}, {"export", CmdExport},
	{"clear", CmdClear},
	{"clearc", CmdClearCommentsOnly},
	{"help", CmdHelp}, {"h", CmdHelp},
	{"version", CmdVersion},
	{"set wrap", CmdSetWrap},
	{"set wrap!", CmdToggleWrap}, {"wrap", CmdToggleWrap},
	{"vim", CmdToggleVim}, {"set vim!", CmdToggleVim},
	{"set vim", CmdSetVim},
	{"novim", CmdSetNoVim}, {"set novim", CmdSetNoVim},
	{"set commits", CmdSetCommitsVisible},
	{"set nocommits", CmdSetCommitsHidden},
	{"set commits!", CmdToggleCommits},
	{"diff", CmdDiff},
	{"focus", CmdFocus}, {"f", CmdFocus},
	{"stage", CmdStage},
	{"commits", CmdTargetsLocal}, {"targets", CmdTargetsLocal},
	{"prs", CmdTargetsPrs},
	{"submit", CmdSubmitPicker},
	{"submit comment", CmdSubmitComment},
	{"submit approve", CmdSubmitApprove},
	{"submit request-changes", CmdSubmitRequestChanges},
	{"submit draft", CmdSubmitDraft},
	{"comments unresolved", CmdCommentsUnresolved},
	{"comments all", CmdCommentsAll},
	{"comments hide", CmdCommentsHide},
}

// ParseCommand resolves a `:` command line. Unmatched input falls through
// to line-number parsing (`:{N}` new side, `:o{N}` old side), else
// CmdUnknown with Raw preserved.
func ParseCommand(inputStr string) Command {
	trimmed := strings.TrimSpace(inputStr)
	for _, spec := range commandSpecs {
		if trimmed == spec.name {
			return Command{Kind: spec.kind, Raw: trimmed}
		}
	}
	if n, ok := parseLineno(trimmed, ""); ok {
		return Command{Kind: CmdGotoLine, N: n, Raw: trimmed}
	}
	if n, ok := parseLineno(trimmed, "o"); ok {
		return Command{Kind: CmdGotoLineOld, N: n, Raw: trimmed}
	}
	return Command{Kind: CmdUnknown, Raw: trimmed}
}

func parseLineno(s, prefix string) (uint32, bool) {
	rest, ok := strings.CutPrefix(s, prefix)
	if !ok || rest == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(rest, 10, 32)
	if err != nil || n == 0 {
		return 0, false
	}
	return uint32(n), true
}

// CommandNames returns every command spelling in listing order (for
// completion candidates).
func CommandNames() []string {
	names := make([]string, len(commandSpecs))
	for i, spec := range commandSpecs {
		names[i] = spec.name
	}
	return names
}

// CompletionState tracks a Tab-completion cycle anchored on the prefix the
// user had typed before cycling began.
type CompletionState struct {
	Prefix   string
	Matches  []string
	Selected int
}

// CompletionResult is the outcome of one completion step.
type CompletionResult struct {
	Buffer  string           // new command buffer contents
	State   *CompletionState // non-nil when a cycle is active
	Message string           // "no matches" style feedback, empty otherwise
}

// Complete advances completion for buffer. When state is non-nil, an active
// cycle advances (reverse selects backwards). Otherwise: zero matches yields
// a message; one match replaces the buffer; several extend to the longest
// common prefix, and if that adds nothing, a cycle starts.
func Complete(buffer string, state *CompletionState, reverse bool) CompletionResult {
	if state != nil {
		n := len(state.Matches)
		if reverse {
			state.Selected = (state.Selected - 1 + n) % n
		} else {
			state.Selected = (state.Selected + 1) % n
		}
		return CompletionResult{Buffer: state.Matches[state.Selected], State: state}
	}

	var matches []string
	for _, name := range CommandNames() {
		if strings.HasPrefix(name, buffer) {
			matches = append(matches, name)
		}
	}
	switch len(matches) {
	case 0:
		return CompletionResult{Buffer: buffer, Message: "No matching command"}
	case 1:
		return CompletionResult{Buffer: matches[0]}
	}
	common := longestCommonPrefix(matches)
	if len(common) > len(buffer) {
		return CompletionResult{Buffer: common}
	}
	st := &CompletionState{Prefix: buffer, Matches: matches, Selected: 0}
	if reverse {
		st.Selected = len(matches) - 1
	}
	return CompletionResult{Buffer: st.Matches[st.Selected], State: st}
}

func longestCommonPrefix(items []string) string {
	if len(items) == 0 {
		return ""
	}
	prefix := items[0]
	for _, item := range items[1:] {
		for !strings.HasPrefix(item, prefix) {
			prefix = prefix[:len(prefix)-1]
			if prefix == "" {
				return ""
			}
		}
	}
	return prefix
}
