package render

import (
	"image/color"
	"testing"
)

func TestEmitterPlainTextEmitsNoEscapes(t *testing.T) {
	var e Emitter
	if got := e.Line([]Span{{Text: "hello"}}); got != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestEmitterEmptySpansEmitNothing(t *testing.T) {
	var e Emitter
	if got := e.Line(nil); got != "" {
		t.Fatalf("got %q", got)
	}
	if got := e.Line([]Span{{Text: "", Style: red}}); got != "" {
		t.Fatalf("empty-text span emitted %q", got)
	}
}

func TestEmitterBoldWithReset(t *testing.T) {
	var e Emitter
	got := e.Line([]Span{{Text: "hi", Style: Style{Bold: true}}})
	if got != "\x1b[1mhi\x1b[0m" {
		t.Fatalf("got %q", got)
	}
}

func TestEmitterItalicUnderlineCombined(t *testing.T) {
	var e Emitter
	got := e.Line([]Span{{Text: "x", Style: Style{Italic: true, Underline: true}}})
	if got != "\x1b[3;4mx\x1b[0m" {
		t.Fatalf("got %q", got)
	}
}

func TestEmitterTruecolorForegroundAndBackground(t *testing.T) {
	var e Emitter
	st := Style{Fg: color.RGBA{R: 255, A: 255}, Bg: color.RGBA{B: 255, A: 255}}
	got := e.Line([]Span{{Text: "x", Style: st}})
	if got != "\x1b[38;2;255;0;0;48;2;0;0;255mx\x1b[0m" {
		t.Fatalf("got %q", got)
	}
}

func TestEmitterNilColorsEmitNoColorCodes(t *testing.T) {
	var e Emitter
	got := e.Line([]Span{{Text: "x", Style: Style{Bold: true, Fg: nil, Bg: nil}}})
	if got != "\x1b[1mx\x1b[0m" {
		t.Fatalf("got %q", got)
	}
}

func TestEmitterCoalescesAdjacentEqualStyles(t *testing.T) {
	var e Emitter
	got := e.Line([]Span{{Text: "ab", Style: red}, {Text: "cd", Style: red}})
	if got != "\x1b[38;2;255;0;0mabcd\x1b[0m" {
		t.Fatalf("got %q", got)
	}
}

func TestEmitterResetsBetweenDifferentStyles(t *testing.T) {
	var e Emitter
	got := e.Line([]Span{{Text: "a", Style: red}, {Text: "b", Style: blue}})
	if got != "\x1b[38;2;255;0;0ma\x1b[0;38;2;0;0;255mb\x1b[0m" {
		t.Fatalf("got %q", got)
	}
}

func TestEmitterReturnToDefaultStyleMidLineEmitsBareReset(t *testing.T) {
	var e Emitter
	got := e.Line([]Span{{Text: "a", Style: red}, {Text: "b"}})
	if got != "\x1b[38;2;255;0;0ma\x1b[0mb" {
		t.Fatalf("got %q", got)
	}
}

func TestEmitterDefaultThenStyledNeedsNoLeadingReset(t *testing.T) {
	var e Emitter
	got := e.Line([]Span{{Text: "a"}, {Text: "b", Style: Style{Bold: true}}})
	if got != "a\x1b[1mb\x1b[0m" {
		t.Fatalf("got %q", got)
	}
}

func TestEmitterBufferIsReusableAcrossLines(t *testing.T) {
	var e Emitter
	first := e.Line([]Span{{Text: "one", Style: red}})
	second := e.Line([]Span{{Text: "two"}})
	if first != "\x1b[38;2;255;0;0mone\x1b[0m" {
		t.Fatalf("first got %q", first)
	}
	if second != "two" {
		t.Fatalf("second got %q", second)
	}
}
