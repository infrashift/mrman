package patch

import (
	"encoding/base64"
	"io"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// message is one parsed mail message: its headers and its decoded body.
type message struct {
	headers mail.Header
	body    string
}

// splitMbox splits a mailbox into messages.
//
// A message begins at a "From " line that is either the first line or
// preceded by a blank one. The blank-line requirement is what keeps a body
// line that happens to begin with "From " — the reason mboxrd escaping exists
// in the first place — from being read as a new message.
func splitMbox(text string) []string {
	lines := strings.Split(text, "\n")
	var starts []int
	for i, line := range lines {
		if !isMboxFromLine(line) {
			continue
		}
		if i == 0 || lines[i-1] == "" {
			starts = append(starts, i)
		}
	}
	if len(starts) == 0 {
		return nil
	}

	messages := make([]string, 0, len(starts))
	for i, start := range starts {
		end := len(lines)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		// Drop the "From " separator itself; it is a mailbox delimiter, not
		// part of the message.
		body := strings.Join(lines[start+1:end], "\n")
		messages = append(messages, unescapeMboxrd(body))
	}
	return messages
}

// mboxrdEscape matches a body line quoted by an mbox writer: one or more ">"
// followed by "From ". Writers add a ">" so the line cannot be mistaken for a
// separator; readers take one back off.
var mboxrdEscape = regexp.MustCompile(`(?m)^>(>*From )`)

// unescapeMboxrd removes one level of mboxrd ">" quoting from "From " lines.
func unescapeMboxrd(body string) string {
	return mboxrdEscape.ReplaceAllString(body, "$1")
}

// parseMessage splits headers from body and decodes both.
func parseMessage(text string) message {
	head, body, found := strings.Cut(text, "\n\n")
	if !found {
		// A message with no blank line is all headers or all body; treat a
		// leading header-shaped line as headers, otherwise as body.
		if isHeaderLine(firstLine(text)) {
			head, body = text, ""
		} else {
			head, body = "", text
		}
	}

	headers := parseHeaders(head)
	return message{
		headers: headers,
		body:    decodeBody(body, headers),
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// parseHeaders parses a header block, unfolding continuation lines.
//
// It does not use net/mail.ReadMessage: that rejects a message whose headers
// are malformed in any way, and a patch harvested from a list archive is
// exactly where malformed headers turn up. Losing one header is better than
// refusing to review the patch.
func parseHeaders(head string) mail.Header {
	headers := mail.Header{}
	if head == "" {
		return headers
	}

	var name, value string
	flush := func() {
		if name != "" {
			headers[name] = append(headers[name], strings.TrimSpace(value))
		}
		name, value = "", ""
	}

	for _, line := range strings.Split(head, "\n") {
		if line == "" {
			continue
		}
		// A leading space or tab continues the previous header's value.
		if line[0] == ' ' || line[0] == '\t' {
			if name != "" {
				value += " " + strings.TrimSpace(line)
			}
			continue
		}
		rawName, rawValue, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		flush()
		// Canonical casing, so lookups need not guess how the sender spelled it.
		name = textprotoCanonical(strings.TrimSpace(rawName))
		value = strings.TrimSpace(rawValue)
	}
	flush()
	return headers
}

// textprotoCanonical renders a header name in X-Header-Case, matching what
// mail.Header lookups expect.
func textprotoCanonical(name string) string {
	parts := strings.Split(name, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
	}
	return strings.Join(parts, "-")
}

// decodeBody reverses the transfer encoding named in the headers. An encoding
// that fails to decode yields the raw body: a patch that renders oddly is
// more use than no patch.
func decodeBody(body string, headers mail.Header) string {
	switch strings.ToLower(strings.TrimSpace(header(headers, "Content-Transfer-Encoding"))) {
	case "quoted-printable":
		decoded, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(body)))
		if err != nil {
			return body
		}
		return string(decoded)
	case "base64":
		decoded, err := base64.StdEncoding.DecodeString(stripWhitespace(body))
		if err != nil {
			return body
		}
		return string(decoded)
	}
	return body
}

func stripWhitespace(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, s)
}

// header returns the first value of a header, or "".
func header(h mail.Header, name string) string {
	if vals := h[textprotoCanonical(name)]; len(vals) > 0 {
		return vals[0]
	}
	return ""
}

// decodeWords decodes RFC 2047 encoded-words ("=?UTF-8?q?...?="), which is how
// a non-ASCII author name or subject survives a mail transport. Undecodable
// input passes through unchanged.
func decodeWords(s string) string {
	decoded, err := (&mime.WordDecoder{}).DecodeHeader(s)
	if err != nil {
		return s
	}
	return decoded
}

// subjectPrefix matches a leading "[PATCH ...]" bracket, capturing its
// contents. List software and contributors add their own words inside the
// brackets ("[PATCH net-next v3 2/7]", "[RFC PATCH]"), so the contents are
// scanned rather than matched positionally.
var subjectPrefix = regexp.MustCompile(`^\s*\[([^\]]*)\]\s*`)

var (
	versionToken  = regexp.MustCompile(`^[vV](\d+)$`)
	positionToken = regexp.MustCompile(`^(\d+)/(\d+)$`)
)

// parseSubject splits a mail subject into its [PATCH ...] metadata and the
// commit summary. Version defaults to 1; position is zero when unstated.
//
// A cover letter is "0/N", which is a real position rather than a missing
// one — hence SeriesLen, not SeriesPos, being the test for "is in a series".
func parseSubject(raw string) (summary string, version, pos, total int) {
	version = 1
	summary = strings.TrimSpace(decodeWords(raw))

	// "Re:" comes off first, because the bracket pattern is anchored at the
	// start and a reply or resend carries it ahead of the [PATCH] prefix.
	summary = trimReplyPrefixes(summary)

	// Strip any number of leading brackets: "[RFC] [PATCH v2 1/3] title".
	for {
		m := subjectPrefix.FindStringSubmatch(summary)
		if m == nil {
			break
		}
		for _, tok := range strings.Fields(m[1]) {
			if v := versionToken.FindStringSubmatch(tok); v != nil {
				if n, err := strconv.Atoi(v[1]); err == nil && n > 0 {
					version = n
				}
				continue
			}
			if p := positionToken.FindStringSubmatch(tok); p != nil {
				pos, _ = strconv.Atoi(p[1])
				total, _ = strconv.Atoi(p[2])
			}
		}
		summary = subjectPrefix.ReplaceAllString(summary, "")
	}

	// And again afterwards, for the "[PATCH] Re: ..." ordering some clients
	// produce.
	return trimReplyPrefixes(summary), version, pos, total
}

// trimReplyPrefixes removes any run of leading "Re:" markers. On a patch
// subject they mean a resend or a reply; the summary is what follows either
// way.
func trimReplyPrefixes(s string) string {
	for {
		trimmed, ok := cutPrefixFold(strings.TrimSpace(s), "re:")
		if !ok {
			return strings.TrimSpace(s)
		}
		s = trimmed
	}
}

// cutPrefixFold is strings.CutPrefix with an ASCII case-insensitive match.
func cutPrefixFold(s, prefix string) (string, bool) {
	if len(s) < len(prefix) || !strings.EqualFold(s[:len(prefix)], prefix) {
		return s, false
	}
	return s[len(prefix):], true
}

// parseDate parses a mail Date: header, returning the zero time when absent
// or unparsable.
func parseDate(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := mail.ParseDate(s); err == nil {
		return t
	}
	return time.Time{}
}

// trimAngles removes the angle brackets around a Message-Id.
func trimAngles(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "<")
	return strings.TrimSuffix(s, ">")
}

// splitRefs splits a References header into bare message ids.
func splitRefs(s string) []string {
	var refs []string
	for _, f := range strings.Fields(s) {
		if id := trimAngles(f); id != "" {
			refs = append(refs, id)
		}
	}
	return refs
}
