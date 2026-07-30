package patch

import (
	"encoding/base64"
	"strings"
	"testing"
)

// patchBody is a minimal message body carrying one file diff.
const patchBody = "Prose.\n---\ndiff --git a/x.c b/x.c\n--- a/x.c\n+++ b/x.c\n@@ -1,1 +1,2 @@\n a\n+b\n"

func TestKindString(t *testing.T) {
	cases := map[Kind]string{
		KindMbox:    "mbox",
		KindMail:    "mail message",
		KindDiff:    "diff",
		KindUnknown: "unknown",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", k, got, want)
		}
	}
}

// TestBase64Body covers the other transfer encoding list mail uses. A patch
// that arrives base64-encoded is unreadable without this.
func TestBase64Body(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte(patchBody))
	// Real mailers wrap base64 at 76 columns, so feed it wrapped.
	var wrapped strings.Builder
	for i := 0; i < len(encoded); i += 76 {
		end := min(i+76, len(encoded))
		wrapped.WriteString(encoded[i:end] + "\n")
	}

	s, err := Load("Subject: [PATCH] b64\nFrom: a@b.c\n"+
		"Content-Transfer-Encoding: base64\n\n"+wrapped.String(), Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(s.Patches[0].DiffText, "+b") {
		t.Errorf("base64 body was not decoded:\n%s", s.Patches[0].DiffText)
	}
	if !strings.Contains(s.Patches[0].Changelog, "Prose.") {
		t.Errorf("changelog = %q", s.Patches[0].Changelog)
	}
}

// TestUndecodableBodyFallsBackToRaw keeps a broken encoding from costing the
// review entirely — a patch that renders oddly beats no patch.
func TestUndecodableBodyFallsBackToRaw(t *testing.T) {
	s, err := Load("Subject: [PATCH] bad\nFrom: a@b.c\n"+
		"Content-Transfer-Encoding: base64\n\n!!!not base64!!!\n"+patchBody, Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(s.Patches[0].DiffText, "+b") {
		t.Error("an undecodable body should fall back to the raw text")
	}
}

// TestFoldedHeaders covers RFC 5322 continuation lines, which real mailers
// produce for any long header — References on a deep thread especially.
func TestFoldedHeaders(t *testing.T) {
	s, err := Load("Subject: [PATCH v2 1/2] a very long subject that a mailer\n"+
		"\twould certainly fold\n"+
		"From: A <a@b.c>\n"+
		"References: <one@x>\n <two@x>\n <three@x>\n\n"+patchBody, Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	p := s.Patches[0]
	if !strings.Contains(p.Subject, "would certainly fold") {
		t.Errorf("folded subject was not unfolded: %q", p.Subject)
	}
	if p.SeriesPos != 1 || p.SeriesLen != 2 || p.Version != 2 {
		t.Errorf("got v%d %d/%d, want v2 1/2", p.Version, p.SeriesPos, p.SeriesLen)
	}
	if len(p.References) != 3 {
		t.Errorf("References = %v, want 3 entries", p.References)
	}
}

// TestMalformedHeadersDoNotLoseTheMessage is why this package does not use
// net/mail.ReadMessage: that rejects the whole message over one bad line, and
// list archives are exactly where bad lines turn up.
func TestMalformedHeadersDoNotLoseTheMessage(t *testing.T) {
	s, err := Load("Subject: [PATCH] survives\nthis line has no colon\n"+
		"From: A <a@b.c>\n\n"+patchBody, Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Patches[0].Subject != "survives" {
		t.Errorf("Subject = %q", s.Patches[0].Subject)
	}
	if s.Patches[0].Author != "A <a@b.c>" {
		t.Errorf("Author = %q", s.Patches[0].Author)
	}
}

// TestBodyWithNoBlankLine covers a message whose headers and body were never
// separated — a bare diff pasted into a file, or a truncated save.
func TestBodyWithNoBlankLine(t *testing.T) {
	// All body, no headers: a bare diff.
	s, err := Load("diff --git a/x.c b/x.c\n--- a/x.c\n+++ b/x.c\n@@ -1,1 +1,2 @@\n a\n+b\n", Options{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Kind != KindDiff || s.Len() != 1 {
		t.Errorf("Kind = %v, patches = %d", s.Kind, s.Len())
	}
	if s.Patches[0].Subject != "" {
		t.Errorf("a bare diff has no subject, got %q", s.Patches[0].Subject)
	}
}

func TestLoadReader(t *testing.T) {
	s, err := LoadReader(strings.NewReader("Subject: [PATCH] r\nFrom: a@b.c\n\n"+patchBody), Options{})
	if err != nil {
		t.Fatalf("LoadReader: %v", err)
	}
	if s.Len() != 1 || s.Patches[0].Subject != "r" {
		t.Errorf("got %d patches, subject %q", s.Len(), s.Patches[0].Subject)
	}
}

// TestCoverLetterOnlyIsRejected covers a 0/N posted on its own: prose with no
// diff is not a review target, and saying so beats an empty review.
func TestCoverLetterOnlyIsRejected(t *testing.T) {
	_, err := Load("From x y\nSubject: [PATCH 0/3] a series\nFrom: a@b.c\n\n"+
		"Here is what the series does.\n", Options{})
	if err == nil {
		t.Fatal("a cover letter alone should not open a review")
	}
	if !strings.Contains(err.Error(), "no diff") {
		t.Errorf("error should say the artifact carries no diff, got %q", err)
	}
}

func TestStripLevelResolution(t *testing.T) {
	cases := map[int]int{
		0:  DefaultStripLevel, // unset means the -p1 convention
		-1: 0,                 // explicit "strip nothing"
		2:  2,
	}
	for in, want := range cases {
		if got := (Options{StripLevel: in}).stripLevel(); got != want {
			t.Errorf("Options{StripLevel: %d}.stripLevel() = %d, want %d", in, got, want)
		}
	}
}

func TestSeriesHelpers(t *testing.T) {
	s := &Series{Patches: []Patch{
		{SeriesPos: 1, SeriesLen: 2, DiffText: "x"},
		{SeriesPos: 2, SeriesLen: 2, DiffText: "y"},
	}}
	if s.Len() != 2 {
		t.Errorf("Len = %d, want 2", s.Len())
	}
	if _, ok := s.CoverLetter(); ok {
		t.Error("this series has no cover letter")
	}
	if !s.Patches[0].IsSeriesMember() {
		t.Error("a patch with an n/m position is a series member")
	}
	if (&Patch{}).IsSeriesMember() {
		t.Error("a lone patch is not a series member")
	}
}
