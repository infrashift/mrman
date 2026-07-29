package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/infrashift/mrman/internal/input"
)

// helpText is the help popup as one searchable blob.
func helpText() string { return strings.Join(helpContent(';'), "\n") }

// TestHelpDocumentsEveryNormalModeKey walks the normal-mode keymap and
// fails when a key it dispatches has no help row.
//
// The help popup went three milestones out of date before this existed —
// it still advertised itself as a "read-only subset". Pinning it to the
// keymap is the only thing that keeps the two together.
func TestHelpDocumentsEveryNormalModeKey(t *testing.T) {
	help := helpText()

	// Each entry is a key the user can press and a string the help must
	// contain for it. Keys whose help row spells them differently (ranges
	// like "j/k", chords like "zz/zt/zb") map to that row's text.
	for _, tc := range []struct {
		key  rune
		want string
	}{
		{'j', "j/k"}, {'k', "j/k"},
		{'g', "g/G"}, {'G', "g/G"},
		{'}', "}/{"}, {'{', "}/{"},
		{']', "]/["}, {'[', "]/["},
		{'m', "m/M"}, {'M', "m/M"},
		{'(', "(/)"}, {')', "(/)"},
		{'h', "h/l"}, {'l', "h/l"},
		{'r', "toggle file reviewed"},
		{'R', "toggle hunk reviewed"},
		{'c', "comment on the line"},
		{'C', "comment on the file"},
		{'v', "v/V"}, {'V', "v/V"},
		{'i', "i/A"}, {'A', "i/A"},
		{'y', "yank"},
		{'o', "o/O"}, {'O', "o/O"},
		{'n', "n/N"}, {'N', "n/N"},
		{'/', "search"},
		{'q', "quit"},
		{'?', "Help"},
	} {
		// The key must actually be bound, or the help documents a fiction.
		if action := input.MapKey(
			runeKey(tc.key), input.ModeNormal, ';'); action.Kind == input.None {
			t.Errorf("help documents %q but the keymap does not bind it", tc.key)
			continue
		}
		if !strings.Contains(help, tc.want) {
			t.Errorf("key %q is bound but the help never mentions %q", tc.key, tc.want)
		}
	}
}

// TestHelpDocumentsEveryLeaderChord pins the leader chords, which are
// dispatched by hand rather than through the keymap and so are especially
// easy to add without documenting.
func TestHelpDocumentsEveryLeaderChord(t *testing.T) {
	help := helpText()
	for _, chord := range []string{";e", ";h", ";l", ";j", ";k", ";s", ";c", ";f"} {
		if !strings.Contains(help, chord) {
			t.Errorf("leader chord %q is dispatched but undocumented", chord)
		}
	}
}

// TestHelpUsesTheConfiguredLeader keeps the popup honest for users who
// rebound it.
func TestHelpUsesTheConfiguredLeader(t *testing.T) {
	help := strings.Join(helpContent(','), "\n")
	if !strings.Contains(help, ",e") {
		t.Error("the help must render the configured leader, not a hardcoded one")
	}
	if strings.Contains(help, ";e") {
		t.Error("the default leader leaked into a rebound help popup")
	}
}

// TestNoCommandReportsUnavailable asserts every command in the registry has
// a real implementation. "Not available yet" was the marker for eleven
// unimplemented commands; none should remain.
func TestNoCommandReportsUnavailable(t *testing.T) {
	for _, name := range input.CommandNames() {
		m := testModel(t)
		m.runCommand(input.ParseCommand(name))
		if msg := m.App.Message; msg != nil && strings.Contains(msg.Content, "Not available yet") {
			t.Errorf(":%s still reports %q", name, msg.Content)
		}
	}
}

// TestEveryCommandIsDocumented pins the command list to the help popup.
func TestEveryCommandIsDocumented(t *testing.T) {
	help := helpText()
	// Aliases and argument forms share a help row with their primary name.
	documentedBy := map[string]string{
		"quit": ":q", "quit!": ":q!", "q!": ":q!", "write": ":w", "wq": ":wq", "x": ":wq",
		"reload": ":reload", "e": ":e", "export": ":export", "clip": ":clip",
		"clearc": ":clearc", "clear": ":clear", "h": ":q", "help": "Help",
		"f": ":focus", "focus": ":focus", "targets": ":targets", "commits": ":commits",
		"wrap": ":wrap", "novim": ":vim", "vim": ":vim",
	}
	for _, name := range input.CommandNames() {
		want, ok := documentedBy[name]
		if !ok {
			want = ":" + strings.Fields(name)[0]
		}
		if !strings.Contains(help, want) {
			t.Errorf("command %q is not documented (looked for %q)", name, want)
		}
	}
}

// runeKey builds the key event a printable rune produces.
func runeKey(r rune) tea.Key {
	return tea.Key{Text: string(r), Code: r}
}
