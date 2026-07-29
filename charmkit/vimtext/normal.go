package vimtext

import (
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// normalKey handles one key press in Normal, Visual or Visual-Line mode. It
// returns false for the app-level keys (Enter/Esc with nothing pending,
// Tab/Shift-Tab, ':') so the caller can run arm-confirm save/cancel,
// comment-type cycling and the command line.
func (e *Editor) normalKey(k tea.Key) bool {
	visual := e.mode == ModeVisual || e.mode == ModeVisualLine
	pending := e.op != 0 || e.count > 0 || e.opCount > 0 || e.pendingG || e.pendingObj

	switch k.Code {
	case tea.KeyEscape:
		if visual {
			e.resetPending()
			e.mode = ModeNormal
			return true
		}
		if pending {
			e.resetPending()
			return true
		}
		return false
	case tea.KeyEnter, tea.KeyTab:
		if visual || pending {
			e.resetPending()
			return true
		}
		return false
	case tea.KeyLeft:
		e.motion('h')
		return true
	case tea.KeyRight:
		e.motion('l')
		return true
	case tea.KeyUp:
		e.motion('k')
		return true
	case tea.KeyDown:
		e.motion('j')
		return true
	case tea.KeyBackspace:
		e.motion('h')
		return true
	}

	if k.Mod&tea.ModCtrl != 0 {
		if k.Code == 'r' && e.mode == ModeNormal {
			e.resetPending()
			e.redoOp()
		}
		return true
	}

	r, ok := printableRune(k)
	if !ok {
		return true // unrecognized special key: swallow
	}

	if e.pendingObj {
		op := e.op
		e.pendingObj = false
		if r == 'w' {
			e.innerWord(op)
		}
		e.resetPending()
		return true
	}
	if e.pendingG {
		e.pendingG = false
		if r == 'g' {
			e.motion('g')
		} else {
			e.resetPending()
		}
		return true
	}

	if r >= '0' && r <= '9' && (r != '0' || e.count > 0) {
		e.count = e.count*10 + int(r-'0')
		return true
	}
	if r == ':' && !visual && e.op == 0 {
		e.resetPending()
		return false
	}

	switch r {
	case 'h', 'l', 'j', 'k', '0', '^', '$', 'w', 'b', 'e', 'G':
		e.motion(r)
		return true
	case 'g':
		e.pendingG = true
		return true
	}

	if visual {
		e.visualCommand(r)
	} else {
		e.normalCommand(r)
	}
	return true
}

// motion executes motion m, applying any pending count and operator. m is
// one of h l j k 0 ^ $ w b e G, or 'g' meaning gg.
func (e *Editor) motion(m rune) {
	op := e.op
	n := e.effCount()
	num := e.count
	if num == 0 {
		num = e.opCount
	}
	e.resetPending()

	// Linewise motions: j k gg G.
	if m == 'j' || m == 'k' || m == 'g' || m == 'G' {
		var target int
		switch m {
		case 'j':
			target = e.vertTarget(n)
		case 'k':
			target = e.vertTarget(-n)
		default: // gg / G: absolute line, count = line number
			line := 0
			if num > 0 {
				line = num - 1
			} else if m == 'G' {
				line = e.lastLine()
			}
			target = e.lineStartOf(line)
		}
		if op == 0 {
			e.cursor = target
			if m == 'g' || m == 'G' {
				e.cursor = e.firstNonBlank(e.cursor)
			}
			e.clampNormal()
			return
		}
		if (m == 'j' || m == 'k') && e.lineStart(target) == e.lineStart(e.cursor) {
			return // motion failed (already at first/last line)
		}
		switch op {
		case 'd':
			if e.text != "" {
				e.pushUndo()
				e.deleteLines(e.cursor, target)
			}
		case 'c':
			e.pushUndo()
			e.changeLines(e.cursor, target)
		case 'y':
			e.yankLines(e.cursor, target)
		}
		return
	}

	// Vim special case: cw on a non-blank acts like ce.
	if op == 'c' && m == 'w' && e.cursor < len(e.text) {
		if r, _ := utf8.DecodeRuneInString(e.text[e.cursor:]); !unicode.IsSpace(r) {
			m = 'e'
		}
	}

	// Charwise motions.
	inclusive := false
	target := e.cursor
	switch m {
	case 'h':
		ls := e.lineStart(e.cursor)
		for i := 0; i < n && target > ls; i++ {
			target = prevRuneStart(e.text, target)
		}
	case 'l':
		le := e.lineEnd(e.cursor)
		for i := 0; i < n && target < le; i++ {
			target += runeLenAt(e.text, target)
		}
	case '0':
		target = e.lineStart(e.cursor)
	case '^':
		target = e.firstNonBlank(e.cursor)
	case '$':
		target = e.lineEnd(e.cursor)
	case 'w':
		prev := target
		for i := 0; i < n; i++ {
			prev = target
			target = nextWordStart(e.text, target)
		}
		if op != 0 {
			// Vim special case: the operated text stops at the end
			// of the line holding the last word moved over.
			if le := e.lineEnd(prev); target > le && le > prev {
				target = le
			}
		}
	case 'b':
		for i := 0; i < n; i++ {
			target = prevWordStart(e.text, target)
		}
	case 'e':
		for i := 0; i < n; i++ {
			target = wordEnd(e.text, target)
		}
		inclusive = true
	}

	if op == 0 {
		e.cursor = target
		e.clampNormal()
		return
	}
	lo, hi := e.cursor, target
	if lo > hi {
		lo, hi = hi, lo
	}
	if inclusive && hi < len(e.text) {
		hi += runeLenAt(e.text, hi)
	}
	if lo == hi {
		return // empty range: operator aborts
	}
	seg := e.text[lo:hi]
	switch op {
	case 'y':
		e.reg, e.regLine = seg, false
		e.cursor = lo
	case 'd':
		e.pushUndo()
		e.reg, e.regLine = seg, false
		e.text = e.text[:lo] + e.text[hi:]
		e.cursor = lo
		e.clampNormal()
	case 'c':
		e.pushUndo()
		e.reg, e.regLine = seg, false
		e.text = e.text[:lo] + e.text[hi:]
		e.cursor = lo
		e.startInsert(true)
	}
}

// normalCommand handles a printable Normal-mode key that is not a motion,
// digit or ':'.
func (e *Editor) normalCommand(r rune) {
	if e.op != 0 {
		op := e.op
		switch r {
		case 'i': // text object prefix: diw / ciw / yiw
			e.pendingObj = true
			return
		case op: // doubled operator: dd / cc / yy
			e.lineOp(op)
		}
		e.resetPending()
		return
	}
	switch r {
	case 'd', 'c', 'y':
		e.op = r
		e.opCount = e.count
		e.count = 0
		return
	}
	n := e.effCount()
	e.resetPending()
	switch r {
	case 'i':
		e.startInsert(false)
	case 'a':
		if e.cursor < e.lineEnd(e.cursor) {
			e.cursor += runeLenAt(e.text, e.cursor)
		}
		e.startInsert(false)
	case 'I':
		e.cursor = e.firstNonBlank(e.cursor)
		e.startInsert(false)
	case 'A':
		e.cursor = e.lineEnd(e.cursor)
		e.startInsert(false)
	case 'o':
		e.openLine(true)
	case 'O':
		e.openLine(false)
	case 'x':
		e.deleteChars(n)
	case 's':
		e.substitute(n)
	case 'D':
		e.deleteToLineEnd(false)
	case 'C':
		e.deleteToLineEnd(true)
	case 'p':
		e.paste(true)
	case 'P':
		e.paste(false)
	case 'u':
		e.undoOp()
	case 'v':
		e.visualAnchor = e.cursor
		e.mode = ModeVisual
	case 'V':
		e.visualAnchor = e.cursor
		e.mode = ModeVisualLine
	}
}

// lineOp runs a doubled operator (dd / cc / yy) over count lines.
func (e *Editor) lineOp(op rune) {
	n := e.effCount()
	b := e.cursor
	if n > 1 {
		b = e.vertTarget(n - 1)
	}
	switch op {
	case 'd':
		if e.text == "" {
			return
		}
		e.pushUndo()
		e.deleteLines(e.cursor, b)
	case 'c':
		e.pushUndo()
		e.changeLines(e.cursor, b)
	case 'y':
		e.yankLines(e.cursor, b)
	}
}

// deleteLines removes the whole lines covering positions a and b into the
// register (linewise) and lands the cursor on the first non-blank of the
// following line. Ends in Normal mode (shared with Visual-Line d).
func (e *Editor) deleteLines(a, b int) {
	ls, le := e.lineSpan(a, b)
	e.reg, e.regLine = strings.TrimSuffix(e.text[ls:le], "\n"), true
	start := ls
	if le >= len(e.text) && ls > 0 {
		start = ls - 1 // deleting the trailing block: eat the preceding '\n'
	}
	e.text = e.text[:start] + e.text[le:]
	e.mode = ModeNormal
	e.cursor = min(ls, len(e.text))
	e.cursor = e.firstNonBlank(e.cursor)
	e.clampNormal()
}

// changeLines empties the whole lines covering a and b (register linewise),
// leaving one blank line, and enters Insert mode (cc / cj / Visual-Line c).
func (e *Editor) changeLines(a, b int) {
	if a > b {
		a, b = b, a
	}
	ls := e.lineStart(a)
	le := e.lineEnd(b)
	e.reg, e.regLine = e.text[ls:le], true
	e.text = e.text[:ls] + e.text[le:]
	e.cursor = ls
	e.startInsert(true)
}

// yankLines copies the whole lines covering a and b into the register
// (linewise); a backward target moves the cursor to the range start.
func (e *Editor) yankLines(a, b int) {
	ls, le := e.lineSpan(a, b)
	e.reg, e.regLine = strings.TrimSuffix(e.text[ls:le], "\n"), true
	if b < a {
		e.cursor = e.firstNonBlank(ls)
		e.clampNormal()
	}
}

// innerWord applies operator op to the "iw" text object: the run of same
// class characters (word, punctuation or blanks) under the cursor.
func (e *Editor) innerWord(op rune) {
	if e.cursor >= len(e.text) || e.text[e.cursor] == '\n' {
		if op == 'c' { // ciw on an empty line still enters Insert
			e.pushUndo()
			e.startInsert(true)
		}
		return
	}
	r, _ := utf8.DecodeRuneInString(e.text[e.cursor:])
	c := charClass(r)
	lo := e.cursor
	for lo > 0 {
		p := prevRuneStart(e.text, lo)
		pr, _ := utf8.DecodeRuneInString(e.text[p:])
		if pr == '\n' || charClass(pr) != c {
			break
		}
		lo = p
	}
	hi := e.cursor
	for hi < len(e.text) {
		hr, sz := utf8.DecodeRuneInString(e.text[hi:])
		if hr == '\n' || charClass(hr) != c {
			break
		}
		hi += sz
	}
	seg := e.text[lo:hi]
	switch op {
	case 'y':
		e.reg, e.regLine = seg, false
		e.cursor = lo
	case 'd':
		e.pushUndo()
		e.reg, e.regLine = seg, false
		e.text = e.text[:lo] + e.text[hi:]
		e.cursor = lo
		e.clampNormal()
	case 'c':
		e.pushUndo()
		e.reg, e.regLine = seg, false
		e.text = e.text[:lo] + e.text[hi:]
		e.cursor = lo
		e.startInsert(true)
	}
}

// deleteChars deletes n runes under and after the cursor within the line
// ("x"), into the register.
func (e *Editor) deleteChars(n int) {
	le := e.lineEnd(e.cursor)
	if e.cursor >= le {
		return
	}
	end := e.cursor
	for i := 0; i < n && end < le; i++ {
		end += runeLenAt(e.text, end)
	}
	e.pushUndo()
	e.reg, e.regLine = e.text[e.cursor:end], false
	e.text = e.text[:e.cursor] + e.text[end:]
	e.clampNormal()
}

// substitute deletes n runes within the line into the register and enters
// Insert mode ("s").
func (e *Editor) substitute(n int) {
	e.pushUndo()
	le := e.lineEnd(e.cursor)
	end := e.cursor
	for i := 0; i < n && end < le; i++ {
		end += runeLenAt(e.text, end)
	}
	if end > e.cursor {
		e.reg, e.regLine = e.text[e.cursor:end], false
		e.text = e.text[:e.cursor] + e.text[end:]
	}
	e.startInsert(true)
}

// deleteToLineEnd deletes from the cursor to the end of line into the
// register ("D"); with change it enters Insert mode afterwards ("C").
func (e *Editor) deleteToLineEnd(change bool) {
	le := e.lineEnd(e.cursor)
	if e.cursor < le {
		e.pushUndo()
		e.reg, e.regLine = e.text[e.cursor:le], false
		e.text = e.text[:e.cursor] + e.text[le:]
		if change {
			e.startInsert(true)
		} else {
			e.clampNormal()
		}
		return
	}
	if change {
		e.startInsert(false)
	}
}

// openLine opens a new empty line below or above the current one and enters
// Insert mode ("o" / "O").
func (e *Editor) openLine(below bool) {
	e.pushUndo()
	if below {
		le := e.lineEnd(e.cursor)
		e.text = e.text[:le] + "\n" + e.text[le:]
		e.cursor = le + 1
	} else {
		ls := e.lineStart(e.cursor)
		e.text = e.text[:ls] + "\n" + e.text[ls:]
		e.cursor = ls
	}
	e.startInsert(true)
}

// paste puts the register after (p) or before (P) the cursor: charwise
// content goes after/before the cursor character, linewise content on a new
// line below/above.
func (e *Editor) paste(after bool) {
	if e.reg == "" {
		return
	}
	e.pushUndo()
	switch {
	case e.regLine && after:
		if le := e.lineEnd(e.cursor); le >= len(e.text) {
			at := len(e.text)
			e.text += "\n" + e.reg
			e.cursor = e.firstNonBlank(at + 1)
		} else {
			at := le + 1
			e.text = e.text[:at] + e.reg + "\n" + e.text[at:]
			e.cursor = e.firstNonBlank(at)
		}
	case e.regLine:
		ls := e.lineStart(e.cursor)
		e.text = e.text[:ls] + e.reg + "\n" + e.text[ls:]
		e.cursor = e.firstNonBlank(ls)
	default:
		at := e.cursor
		if after && at < e.lineEnd(at) {
			at += runeLenAt(e.text, at)
		}
		e.text = e.text[:at] + e.reg + e.text[at:]
		e.cursor = at + len(e.reg) - lastRuneLen(e.reg)
	}
	e.clampNormal()
}

// visualCommand handles a printable non-motion key in Visual or Visual-Line
// mode.
func (e *Editor) visualCommand(r rune) {
	e.resetPending()
	switch r {
	case 'v': // toggle charwise visual / switch from linewise
		if e.mode == ModeVisual {
			e.mode = ModeNormal
		} else {
			e.mode = ModeVisual
		}
	case 'V': // toggle linewise visual / switch from charwise
		if e.mode == ModeVisualLine {
			e.mode = ModeNormal
		} else {
			e.mode = ModeVisualLine
		}
	case 'y':
		e.visualYank()
	case 'd', 'x':
		e.visualDelete(false)
	case 'c', 's':
		e.visualDelete(true)
	case 'p':
		e.visualPaste()
	}
}

// visualRange returns the [lo, hi) byte range of the charwise selection,
// inclusive of the rune under the selection end.
func (e *Editor) visualRange() (int, int) {
	lo, hi := e.visualAnchor, e.cursor
	if lo > hi {
		lo, hi = hi, lo
	}
	if hi < len(e.text) {
		hi += runeLenAt(e.text, hi)
	}
	return lo, hi
}

// visualYank copies the selection into the register and returns to Normal
// mode with the cursor at the selection start.
func (e *Editor) visualYank() {
	if e.mode == ModeVisualLine {
		ls, le := e.lineSpan(e.visualAnchor, e.cursor)
		e.reg, e.regLine = strings.TrimSuffix(e.text[ls:le], "\n"), true
		e.cursor = min(e.cursor, e.visualAnchor)
	} else {
		lo, hi := e.visualRange()
		e.reg, e.regLine = e.text[lo:hi], false
		e.cursor = lo
	}
	e.mode = ModeNormal
	e.clampNormal()
}

// visualDelete removes the selection into the register; with change it
// enters Insert mode (visual c/s), otherwise Normal (visual d/x).
func (e *Editor) visualDelete(change bool) {
	if e.mode == ModeVisualLine {
		if e.text == "" && !change {
			e.mode = ModeNormal
			return
		}
		e.pushUndo()
		if change {
			e.changeLines(e.visualAnchor, e.cursor)
		} else {
			e.deleteLines(e.visualAnchor, e.cursor)
		}
		return
	}
	lo, hi := e.visualRange()
	if lo == hi && !change {
		e.mode = ModeNormal
		return
	}
	e.pushUndo()
	e.reg, e.regLine = e.text[lo:hi], false
	e.text = e.text[:lo] + e.text[hi:]
	e.cursor = lo
	if change {
		e.startInsert(true)
	} else {
		e.mode = ModeNormal
		e.clampNormal()
	}
}

// visualPaste replaces the selection with the register content; the replaced
// text becomes the new register content, vim-style.
func (e *Editor) visualPaste() {
	if e.reg == "" {
		e.mode = ModeNormal
		return
	}
	saved, savedLine := e.reg, e.regLine
	e.pushUndo()
	if e.mode == ModeVisualLine {
		ls, le := e.lineSpan(e.visualAnchor, e.cursor)
		old := strings.TrimSuffix(e.text[ls:le], "\n")
		ins := saved
		if le < len(e.text) || strings.HasSuffix(e.text[ls:le], "\n") {
			ins += "\n"
		}
		e.text = e.text[:ls] + ins + e.text[le:]
		e.reg, e.regLine = old, true
		e.cursor = e.firstNonBlank(ls)
	} else {
		lo, hi := e.visualRange()
		old := e.text[lo:hi]
		if savedLine {
			// Linewise put over a charwise selection: the register
			// lands on its own line(s), splitting the line.
			pre, post := "\n", "\n"
			if lo == e.lineStart(lo) {
				pre = ""
			}
			if hi >= len(e.text) || e.text[hi] == '\n' {
				post = ""
			}
			e.text = e.text[:lo] + pre + saved + post + e.text[hi:]
			e.cursor = e.firstNonBlank(lo + len(pre))
		} else {
			e.text = e.text[:lo] + saved + e.text[hi:]
			e.cursor = lo + len(saved) - lastRuneLen(saved)
		}
		e.reg, e.regLine = old, false
	}
	e.mode = ModeNormal
	e.clampNormal()
}
