package textsafe

import "testing"

func TestSanitize(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"plain", "hello world", "hello world"},
		{"tabs and newlines kept", "a\tb\nc\n", "a\tb\nc\n"},
		{"unicode kept", "naïve — 日本語 🎉", "naïve — 日本語 🎉"},
		{"SGR", "\x1b[31mred\x1b[0m", "red"},
		{"OSC 8 hyperlink", "\x1b]8;;https://x\x07link\x1b]8;;\x07", "link"},
		{"OSC 8 ST-terminated", "\x1b]8;;https://x\x1b\\link\x1b]8;;\x1b\\", "link"},
		{"BEL", "a\x07b", "ab"},
		{"OSC 52 clipboard", "\x1b]52;c;Zm9v\x07", ""},
		{"CSI clear screen", "\x1b[2J", ""},
		{"DCS and APC", "a\x1bPq...\x1b\\b\x1b_x\x1b\\c", "abc"},
		{"charset designation", "a\x1b(Bb", "ab"},
		{"truncated escape", "a\x1b[", "a"},
		{"C1 CSI as UTF-8", "\xc2\x9b31mX", "31mX"},
		{"C1 range", "a\u0085b\u009fc", "abc"},
		{"DEL", "a\x7fb", "ab"},
		{"other C0", "a\x00b\x08c\x1fd", "abcd"},
		{"line separator", "a\u2028b\u2029c", "abc"},
		{"bidi override marked", "a\u202eb", "a\ufffdb"},
		{"bidi isolate marked", "a\u2066b\u2069c", "a\ufffdb\ufffdc"},
		{"CRLF", "a\r\nb", "a\nb"},
		{"invalid UTF-8", "\xffz\xc3", "z"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Sanitize(tc.in); got != tc.want {
				t.Fatalf("Sanitize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSanitizeLine(t *testing.T) {
	for in, want := range map[string]string{
		"title":                   "title",
		"two\nlines":              "two lines",
		"tab\tkept":               "tab\tkept",
		"a\r\nb":                  "a b",
		"\x1b[1mbold\x1b[m title": "bold title",
	} {
		if got := SanitizeLine(in); got != want {
			t.Errorf("SanitizeLine(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestFastPathReturnsInputUnchanged pins the property the fast path is
// for: clean strings come back as the same string, not a copy.
func TestFastPathReturnsInputUnchanged(t *testing.T) {
	in := "ordinary text — with a dash, ütf-8 and\ttabs"
	if got := Sanitize(in); got != in {
		t.Fatalf("clean input changed: %q", got)
	}
	if SanitizeLine(in) != in {
		t.Fatal("clean single-line input changed")
	}
}
