package patch

import (
	"strings"
)

// Kind is how an artifact was recognised.
type Kind int

// Artifact kinds.
const (
	// KindUnknown is not a patch at all.
	KindUnknown Kind = iota
	// KindMbox is a Unix mailbox: one or more messages, each beginning with
	// a "From " line. `git format-patch --stdout` emits exactly this shape
	// for one patch as well as for a series, so both share a code path.
	KindMbox
	// KindMail is a single mail message with headers but no "From " line —
	// what you get by saving one message out of a mail client.
	KindMail
	// KindDiff is a bare diff with no mail around it: `git diff` output,
	// quilt, or `diff -u`.
	KindDiff
)

// String names the kind for error messages.
func (k Kind) String() string {
	switch k {
	case KindMbox:
		return "mbox"
	case KindMail:
		return "mail message"
	case KindDiff:
		return "diff"
	}
	return "unknown"
}

// Detect classifies an artifact from its text.
//
// Order matters: mbox is checked first because a format-patch file is both a
// mailbox and a mail message, and treating it as a mailbox is what makes a
// series work. The diff check is last and deliberately loose — anything
// carrying a recognisable diff is worth trying, even when its surroundings
// are unfamiliar.
func Detect(text string) Kind {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return KindUnknown
	}

	if isMboxFromLine(lines[0]) {
		return KindMbox
	}
	if looksLikeMailHeaders(lines) {
		return KindMail
	}
	if containsDiff(lines) {
		return KindDiff
	}
	return KindUnknown
}

// isMboxFromLine reports whether line opens an mbox message. The separator is
// "From " followed by an address and a date; a diff's "From" would not have
// the trailing space, and prose is excluded by requiring something after it.
func isMboxFromLine(line string) bool {
	rest, ok := strings.CutPrefix(line, "From ")
	return ok && strings.TrimSpace(rest) != ""
}

// looksLikeMailHeaders reports whether the text opens with a mail header
// block. It requires a syntactically valid header on the first line plus one
// of the headers that actually identifies a message, so that a diff whose
// first line happens to contain a colon is not mistaken for mail.
func looksLikeMailHeaders(lines []string) bool {
	if len(lines) == 0 || !isHeaderLine(lines[0]) {
		return false
	}
	for _, line := range lines {
		if line == "" {
			return false // end of the header block, nothing identifying found
		}
		name, _, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "subject", "from", "message-id":
			return true
		}
	}
	return false
}

// isHeaderLine reports whether line has the shape of an RFC 5322 header
// field: a name of printable ASCII excluding space and colon, then a colon.
func isHeaderLine(line string) bool {
	name, _, ok := strings.Cut(line, ":")
	if !ok || name == "" {
		return false
	}
	for _, r := range name {
		if r <= ' ' || r > '~' {
			return false
		}
	}
	return true
}

// containsDiff reports whether the lines carry something a diff parser could
// work with: a git header, or the ---/+++/@@ triple of a plain unified diff.
func containsDiff(lines []string) bool {
	for i, line := range lines {
		if strings.HasPrefix(line, "diff --git ") {
			return true
		}
		if isUnifiedFileStart(lines, i) {
			return true
		}
	}
	return false
}

// isUnifiedFileStart reports whether index i begins a plain unified diff file
// header: "--- x" then "+++ y" then an "@@" hunk. All three are required —
// "---" alone is also a mail signature separator and a Markdown rule.
func isUnifiedFileStart(lines []string, i int) bool {
	if i+2 >= len(lines) {
		return false
	}
	return strings.HasPrefix(lines[i], "--- ") &&
		strings.HasPrefix(lines[i+1], "+++ ") &&
		strings.HasPrefix(lines[i+2], "@@")
}

// normalizeNewlines converts CRLF and lone CR to LF.
//
// Mail transports rewrite line endings freely, so a patch that arrives by
// mail may carry any of the three. Everything downstream — the mbox splitter,
// the header parser, and diffparser.ParseLines, which unlike Parse does not
// trim \r — assumes LF, so this runs once on the way in.
func normalizeNewlines(text string) string {
	if !strings.ContainsRune(text, '\r') {
		return text
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.ReplaceAll(text, "\r", "\n")
}
