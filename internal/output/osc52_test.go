package output

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestOSC52SequenceFormat(t *testing.T) {
	got := osc52Sequence("Hello, World!")
	if !strings.HasPrefix(got, "\x1b]52;c;") || !strings.HasSuffix(got, "\x07") {
		t.Fatalf("bad envelope: %q", got)
	}
	payload := got[len("\x1b]52;c;") : len(got)-1]
	if payload != base64.StdEncoding.EncodeToString([]byte("Hello, World!")) {
		t.Errorf("payload = %q", payload)
	}
}

func TestOSC52SequenceEmptyString(t *testing.T) {
	if got := osc52Sequence(""); got != "\x1b]52;c;\x07" {
		t.Errorf("empty = %q", got)
	}
}

func TestOSC52SequenceUnicodeRoundTrip(t *testing.T) {
	text := "こんにちは 🦀"
	got := osc52Sequence(text)
	payload := got[len("\x1b]52;c;") : len(got)-1]
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(decoded) != text {
		t.Errorf("round trip = %q, want %q", decoded, text)
	}
}

func TestOSC52SequenceMarkdownRoundTrip(t *testing.T) {
	markdown := renderDefault(t, newTestSession(), ScopeWorkingTree, nil, true, testLegend(), "")
	got := osc52Sequence(markdown)
	payload := got[len("\x1b]52;c;") : len(got)-1]
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(decoded) != markdown {
		t.Errorf("round trip mismatch:\n%q\nvs\n%q", decoded, markdown)
	}
}

func TestTmuxWrapDoublesEscapes(t *testing.T) {
	got := tmuxWrap("\x1b]52;c;AA\x07")
	want := "\x1bPtmux;\x1b\x1b]52;c;AA\x07\x1b\\"
	if got != want {
		t.Errorf("tmuxWrap = %q, want %q", got, want)
	}
}
