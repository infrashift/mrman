package output

import (
	"encoding/base64"
	"strings"
)

// osc52Sequence builds the OSC 52 clipboard escape for text:
// ESC ] 52 ; c ; <base64> BEL.
func osc52Sequence(text string) string {
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07"
}

// tmuxWrap wraps an escape sequence in tmux's DCS passthrough envelope
// (ESC P tmux ; <seq with every ESC doubled> ESC \) so tmux forwards it to
// the outer terminal instead of consuming it.
func tmuxWrap(seq string) string {
	return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
}
