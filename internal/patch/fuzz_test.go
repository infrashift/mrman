package patch

import "testing"

// FuzzLoad runs the mbox/patch splitter, header parser and diff normalizer
// over arbitrary input. --patch reads files nobody in the repository
// wrote; none of them may crash the review.
func FuzzLoad(f *testing.F) {
	f.Add("diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+b\n", 1)
	f.Add("From 0123456789abcdef Mon Sep 17 00:00:00 2001\nFrom: A <a@x>\nSubject: [PATCH 1/2] one\nMessage-Id: <1@x>\n\nbody\n---\n x | 1 +\n\ndiff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+b\n-- \n2.0\n", 1)
	f.Add("--- \"a/quoted\\303\\251\"\n+++ \"b/quoted\\303\\251\"\n@@ -1 +1 @@\n-a\n+b\n", 0)
	f.Add("--- x\n+++ y\n@@ -1 +1 @@\n+\\\n", 3)
	f.Add("", 1)
	f.Fuzz(func(_ *testing.T, text string, strip int) {
		if strip < 0 || strip > 8 {
			strip = 1
		}
		_ = Detect(text)
		_ = Normalize(text, strip)
		_, _ = Load(text, Options{StripLevel: strip})
	})
}
