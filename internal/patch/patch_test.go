package patch

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs/diffparser"
)

// loadFixture reads a checked-in artifact. The fixtures are real files rather
// than string literals on purpose: this package's entire risk is byte-level
// shapes — tabs, CRLF, trailing signatures, escaped From lines — that a Go
// literal quietly normalises away.
func loadFixture(t *testing.T, name string, opts Options) *Series {
	t.Helper()
	s, err := LoadFile(filepath.Join("testdata", name), opts)
	if err != nil {
		t.Fatalf("LoadFile(%s): %v", name, err)
	}
	return s
}

// parseDiff runs a patch's normalised diff through the real parser, which is
// the only thing that proves normalisation produced something usable.
func parseDiff(t *testing.T, p Patch) []string {
	t.Helper()
	files, err := diffparser.Parse(p.DiffText, diffparser.GitStyle, nil)
	if err != nil {
		t.Fatalf("diffparser.Parse: %v\n--- diff ---\n%s", err, p.DiffText)
	}
	paths := make([]string, 0, len(files))
	for i := range files {
		paths = append(paths, files[i].DisplayPath())
	}
	return paths
}

// ---- Detection ----

func TestDetectKinds(t *testing.T) {
	cases := map[string]Kind{
		"format-patch-single.patch":  KindMbox, // format-patch output is a mailbox
		"format-patch-series-3.mbox": KindMbox,
		"lore-qp.mbox":               KindMbox,
		"mboxrd-escaped.mbox":        KindMbox,
		"quilt-p1.diff":              KindDiff,
		"noprefix.patch":             KindDiff,
		"rename.patch":               KindDiff,
		"binary.patch":               KindDiff,
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			if got := Detect(normalizeNewlines(string(data))); got != want {
				t.Errorf("Detect = %v, want %v", got, want)
			}
		})
	}
}

// TestLoadRejectsNonPatch covers the failure a user is most likely to hit:
// pointing mrman at an ordinary text file. "no changes to review" would send
// them hunting for an empty diff.
func TestLoadRejectsNonPatch(t *testing.T) {
	_, err := LoadFile(filepath.Join("testdata", "notapatch.txt"), Options{})
	if _, ok := errors.AsType[*errs.InvalidInput](err); !ok {
		t.Fatalf("err = %v, want *errs.InvalidInput", err)
	}
	if !strings.Contains(err.Error(), "not a patch") {
		t.Errorf("error should say what is wrong, got %q", err)
	}
}

// TestDetectMailWithoutFromLine covers a message saved out of a mail client,
// which has headers but no mbox separator.
func TestDetectMailWithoutFromLine(t *testing.T) {
	text := "Subject: [PATCH] x\nFrom: a@b.c\n\nbody\n---\n" +
		"diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1,1 +1,2 @@\n a\n+b\n"
	if got := Detect(text); got != KindMail {
		t.Errorf("Detect = %v, want KindMail", got)
	}
	s, err := Load(text, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if s.Len() != 1 || s.Patches[0].Subject != "x" {
		t.Errorf("got %d patches, subject %q", s.Len(), s.Patches[0].Subject)
	}
}

// ---- Single format-patch file ----

func TestLoadFormatPatchSingle(t *testing.T) {
	s := loadFixture(t, "format-patch-single.patch", Options{})

	if s.Len() != 1 {
		t.Fatalf("got %d patches, want 1", s.Len())
	}
	p := s.Patches[0]

	if p.Subject != "drivers/foo: guard the probe" {
		t.Errorf("Subject = %q", p.Subject)
	}
	if p.Author != "Dev Eloper <dev@example.org>" {
		t.Errorf("Author = %q", p.Author)
	}
	if p.MessageID != "" {
		t.Errorf("MessageID = %q, want empty (this fixture has no Message-Id)", p.MessageID)
	}
	if p.Date.IsZero() {
		t.Error("Date was not parsed")
	}

	// The changelog keeps the prose and the trailer, and drops the generated
	// diffstat and the "---" separator.
	for _, want := range []string{"dereferences bar", "Changes since v1", "Signed-off-by"} {
		if !strings.Contains(p.Changelog, want) {
			t.Errorf("changelog missing %q:\n%s", want, p.Changelog)
		}
	}
	for _, unwanted := range []string{"1 file changed", "drivers/foo.c | 3"} {
		if strings.Contains(p.Changelog, unwanted) {
			t.Errorf("changelog should not carry the diffstat line %q:\n%s", unwanted, p.Changelog)
		}
	}
	if strings.HasSuffix(strings.TrimSpace(p.Changelog), "---") {
		t.Errorf("changelog should not end with the --- separator:\n%s", p.Changelog)
	}

	if got := parseDiff(t, p); len(got) != 1 || got[0] != "drivers/foo.c" {
		t.Errorf("parsed paths = %v, want [drivers/foo.c]", got)
	}
}

// TestSignatureStaysOutOfTheDiff pairs with the hunk-budget fix in
// diffparser: neither layer may let "-- \n2.43.0" become diff content.
func TestSignatureStaysOutOfTheDiff(t *testing.T) {
	s := loadFixture(t, "format-patch-single.patch", Options{})
	files, err := diffparser.Parse(s.Patches[0].DiffText, diffparser.GitStyle, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range files[0].Hunks {
		for _, l := range h.Lines {
			if strings.HasPrefix(l.Content, "- ") || l.Content == "2.43.0" {
				t.Errorf("signature leaked into the diff as %q", l.Content)
			}
		}
	}
}

// TestTabsSurviveNormalization is the property the export half depends on.
// Normalisation must not touch hunk bodies at all.
func TestTabsSurviveNormalization(t *testing.T) {
	s := loadFixture(t, "format-patch-single.patch", Options{})
	if !strings.Contains(s.Patches[0].DiffText, "\t") {
		t.Error("tabs were lost during normalisation")
	}
	if !strings.Contains(s.Patches[0].DiffText, "+\t\treturn -EINVAL;") {
		t.Errorf("expected a tab-indented added line:\n%s", s.Patches[0].DiffText)
	}
}

// ---- Series ----

func TestLoadSeries(t *testing.T) {
	s := loadFixture(t, "format-patch-series-3.mbox", Options{})

	if s.Len() != 3 {
		t.Fatalf("got %d patches, want 3 (cover letter + 2)", s.Len())
	}

	cover, ok := s.CoverLetter()
	if !ok {
		t.Fatal("cover letter not identified")
	}
	if cover.SeriesPos != 0 || cover.SeriesLen != 2 {
		t.Errorf("cover position = %d/%d, want 0/2", cover.SeriesPos, cover.SeriesLen)
	}
	if cover.DiffText != "" {
		t.Error("a cover letter carries no diff")
	}

	for i, want := range []struct{ pos, total, version int }{{0, 2, 2}, {1, 2, 2}, {2, 2, 2}} {
		p := s.Patches[i]
		if p.SeriesPos != want.pos || p.SeriesLen != want.total || p.Version != want.version {
			t.Errorf("patch %d: got v%d %d/%d, want v%d %d/%d",
				i, p.Version, p.SeriesPos, p.SeriesLen, want.version, want.pos, want.total)
		}
	}

	// Threading metadata is what a review reply needs to land in the thread.
	if s.Patches[1].InReplyTo != "20260101120000.1-1-dev@example.org" {
		t.Errorf("InReplyTo = %q", s.Patches[1].InReplyTo)
	}
	if len(s.Patches[1].References) != 1 {
		t.Errorf("References = %v, want one entry", s.Patches[1].References)
	}
	if s.Patches[2].MessageID != "20260101120000.1-3-dev@example.org" {
		t.Errorf("MessageID = %q", s.Patches[2].MessageID)
	}
}

// TestSeriesPatchesTouchingOneFileStaySeparate is why each patch is its own
// review target: flattened, these two would collide on net/foo.c.
func TestSeriesPatchesTouchingOneFileStaySeparate(t *testing.T) {
	s := loadFixture(t, "format-patch-series-3.mbox", Options{})

	for _, i := range []int{1, 2} {
		got := parseDiff(t, s.Patches[i])
		if len(got) != 1 || got[0] != "net/foo.c" {
			t.Errorf("patch %d parsed paths = %v, want [net/foo.c]", i, got)
		}
	}
	if s.Patches[1].DiffText == s.Patches[2].DiffText {
		t.Error("the two patches should carry different diffs")
	}
}

// TestSeriesChangelogsDoNotLeakIntoDiffs covers the mbox hazard directly: a
// changelog bullet starting with "-" sitting between two patches.
func TestSeriesChangelogsDoNotLeakIntoDiffs(t *testing.T) {
	s := loadFixture(t, "format-patch-series-3.mbox", Options{})

	if !strings.Contains(s.Patches[1].Changelog, "this bullet looks like a deletion") {
		t.Error("the bullet belongs in the changelog")
	}
	if strings.Contains(s.Patches[1].DiffText, "this bullet looks like a deletion") {
		t.Error("the bullet leaked into the diff")
	}
	if strings.Contains(s.Patches[1].DiffText, "file changed") {
		t.Error("a diffstat line leaked into the diff")
	}
}

// ---- Encodings ----

func TestQuotedPrintableAndEncodedWords(t *testing.T) {
	s := loadFixture(t, "lore-qp.mbox", Options{})
	p := s.Patches[0]

	if !strings.Contains(p.Author, "Björn Andersson") {
		t.Errorf("Author = %q, want the RFC 2047 name decoded", p.Author)
	}
	if !strings.Contains(p.Subject, "händle") || !strings.Contains(p.Subject, "naïve") {
		t.Errorf("Subject = %q, want the encoded-word subject decoded", p.Subject)
	}
	if !strings.Contains(p.Changelog, "naïve path") {
		t.Errorf("Changelog = %q, want quoted-printable decoded", p.Changelog)
	}
	if !strings.Contains(p.DiffText, "naïve") {
		t.Errorf("diff should be quoted-printable decoded:\n%s", p.DiffText)
	}
}

func TestMboxrdUnescaping(t *testing.T) {
	s := loadFixture(t, "mboxrd-escaped.mbox", Options{})

	if s.Len() != 1 {
		t.Fatalf("got %d patches, want 1 — an escaped From line is not a separator", s.Len())
	}
	if !strings.Contains(s.Patches[0].Changelog, "\nFrom here on") {
		t.Errorf("the escaped From line should be restored:\n%s", s.Patches[0].Changelog)
	}
}

func TestCRLFInput(t *testing.T) {
	s := loadFixture(t, "crlf.patch", Options{})

	if strings.Contains(s.Patches[0].DiffText, "\r") {
		t.Error("carriage returns survived into the diff")
	}
	if got := parseDiff(t, s.Patches[0]); len(got) != 1 || got[0] != "c.txt" {
		t.Errorf("parsed paths = %v, want [c.txt]", got)
	}
}

// ---- Normalisation ----

// TestQuiltPatchBecomesParseable is the gap that shut most mailing-list
// patches out: no "diff --git" header, and tab-separated timestamps.
func TestQuiltPatchBecomesParseable(t *testing.T) {
	s := loadFixture(t, "quilt-p1.diff", Options{})

	if !strings.Contains(s.Patches[0].DiffText, "diff --git a/drivers/bar.c b/drivers/bar.c") {
		t.Errorf("expected a synthesised git header:\n%s", s.Patches[0].DiffText)
	}
	if strings.Contains(s.Patches[0].DiffText, "2026-01-01") {
		t.Errorf("the timestamp should be cut from the path:\n%s", s.Patches[0].DiffText)
	}
	if got := parseDiff(t, s.Patches[0]); len(got) != 1 || got[0] != "drivers/bar.c" {
		t.Errorf("parsed paths = %v, want [drivers/bar.c]", got)
	}
}

// TestNoPrefixPatch covers `git diff --no-prefix`, where the declared paths
// carry no a//b/ and stripping one component would be wrong.
func TestNoPrefixPatch(t *testing.T) {
	s := loadFixture(t, "noprefix.patch", Options{StripLevel: -1})
	if got := parseDiff(t, s.Patches[0]); len(got) != 1 || got[0] != "thing.c" {
		t.Errorf("parsed paths = %v, want [thing.c]", got)
	}
}

// TestStripLevelShallowPathIsKept is the safety valve: a path with fewer
// components than the strip level is likelier to be -p0 than to be about the
// filesystem root, so it is kept rather than emptied.
func TestStripLevelShallowPathIsKept(t *testing.T) {
	if got := stripPath("thing.c", 1); got != "thing.c" {
		t.Errorf("stripPath(thing.c, 1) = %q, want thing.c", got)
	}
	if got := stripPath("a/thing.c", 1); got != "thing.c" {
		t.Errorf("stripPath(a/thing.c, 1) = %q, want thing.c", got)
	}
	if got := stripPath("/dev/null", 1); got != "/dev/null" {
		t.Errorf("stripPath(/dev/null, 1) = %q, want /dev/null", got)
	}
}

func TestRenameMetadataSurvives(t *testing.T) {
	s := loadFixture(t, "rename.patch", Options{})
	diff := s.Patches[0].DiffText

	for _, want := range []string{"rename from old/name.c", "rename to new/name.c", "similarity index 92%"} {
		if !strings.Contains(diff, want) {
			t.Errorf("normalisation dropped %q:\n%s", want, diff)
		}
	}
	files, err := diffparser.Parse(diff, diffparser.GitStyle, nil)
	if err != nil {
		t.Fatal(err)
	}
	if files[0].Status != model.StatusRenamed {
		t.Errorf("Status = %v, want renamed", files[0].Status)
	}
}

func TestBinaryPatchSurvives(t *testing.T) {
	s := loadFixture(t, "binary.patch", Options{})
	files, err := diffparser.Parse(s.Patches[0].DiffText, diffparser.GitStyle, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || !files[0].IsBinary {
		t.Errorf("expected one binary file, got %+v", files)
	}
}

// ---- Identity ----

// TestContentHashTracksTheBytes is the session identity contract: the same
// artifact resumes, an edited one does not.
func TestContentHashTracksTheBytes(t *testing.T) {
	a := loadFixture(t, "format-patch-single.patch", Options{})
	b := loadFixture(t, "format-patch-single.patch", Options{})
	if a.ContentHash != b.ContentHash {
		t.Error("the same file should hash the same")
	}

	edited, err := Load("From x y\nSubject: [PATCH] a\n\nbody\n---\n"+
		"diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1,1 +1,2 @@\n a\n+b\n", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if edited.ContentHash == a.ContentHash {
		t.Error("different artifacts must hash differently")
	}
}

// ---- Subject parsing ----

func TestParseSubject(t *testing.T) {
	cases := []struct {
		in                  string
		summary             string
		version, pos, total int
	}{
		{"[PATCH] net: fix foo", "net: fix foo", 1, 0, 0},
		{"[PATCH v3 2/7] net: fix foo", "net: fix foo", 3, 2, 7},
		{"[PATCH net-next v2 1/3] tidy", "tidy", 2, 1, 3},
		{"[RFC PATCH] experiment", "experiment", 1, 0, 0},
		{"[PATCH v2 0/4] cover", "cover", 2, 0, 4},
		{"Re: [PATCH] resend", "resend", 1, 0, 0},
		{"[RFC] [PATCH v5 3/3] nested brackets", "nested brackets", 5, 3, 3},
		{"no prefix at all", "no prefix at all", 1, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			summary, version, pos, total := parseSubject(tc.in)
			if summary != tc.summary || version != tc.version || pos != tc.pos || total != tc.total {
				t.Errorf("got (%q, v%d, %d/%d), want (%q, v%d, %d/%d)",
					summary, version, pos, total, tc.summary, tc.version, tc.pos, tc.total)
			}
		})
	}
}

func TestIsDiffstatLine(t *testing.T) {
	yes := []string{
		" drivers/foo.c | 3 +++",
		" net/foo.c |  12 ++++--------",
		" 1 file changed, 3 insertions(+)",
		" 3 files changed, 9 insertions(+), 2 deletions(-)",
		" logo.png | Bin 0 -> 12 bytes",
	}
	no := []string{
		"int x = a | b;",
		" context line",
		"",
		" foo | not a count",
	}
	for _, s := range yes {
		if !isDiffstatLine(s) {
			t.Errorf("isDiffstatLine(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if isDiffstatLine(s) {
			t.Errorf("isDiffstatLine(%q) = true, want false", s)
		}
	}
}

func TestUnquotePath(t *testing.T) {
	cases := map[string]string{
		`plain.c`:         `plain.c`,
		`"quoted.c"`:      `quoted.c`,
		`"with\tspace.c"`: "with\tspace.c",
		`"caf\303\251.c"`: "café.c",
		`"back\\slash.c"`: `back\slash.c`,
	}
	for in, want := range cases {
		if got := unquotePath(in); got != want {
			t.Errorf("unquotePath(%q) = %q, want %q", in, got, want)
		}
	}
}

// ---- Fixture integrity ----

// TestFixtureHunkHeadersAreConsistent guards the fixtures themselves. A
// hand-written @@ header whose counts do not match its body is not a patch any
// tool would produce, and it silently disables the parser's length budget —
// so a fixture with that mistake tests nothing while appearing to pass.
func TestFixtureHunkHeadersAreConsistent(t *testing.T) {
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			checkHunkCounts(t, normalizeNewlines(string(data)))
		})
	}
}

// checkHunkCounts walks a raw artifact and compares each @@ header's declared
// lengths against the body that follows it.
func checkHunkCounts(t *testing.T, text string) {
	t.Helper()
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "@@ ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		wantOld, okOld := rangeCount(strings.TrimPrefix(fields[1], "-"))
		wantNew, okNew := rangeCount(strings.TrimPrefix(fields[2], "+"))
		if !okOld || !okNew {
			continue // deliberately malformed header; another test covers it
		}

		gotOld, gotNew := countHunkBody(lines[i+1:])
		if gotOld != wantOld || gotNew != wantNew {
			t.Errorf("hunk at line %d declares -%d +%d but its body is -%d +%d\n  %s",
				i+1, wantOld, wantNew, gotOld, gotNew, line)
		}
	}
}

// countHunkBody tallies how many lines a hunk body actually spans on each
// side, stopping at the first line that is not diff content.
func countHunkBody(lines []string) (oldCount, newCount int) {
	for _, line := range lines {
		if line == "" || strings.HasPrefix(line, "@@") ||
			strings.HasPrefix(line, "diff ") || strings.HasPrefix(line, "From ") ||
			strings.HasPrefix(line, "-- ") {
			return oldCount, newCount
		}
		if strings.HasPrefix(line, `\`) {
			continue // "\ No newline at end of file" counts for neither side
		}
		switch line[0] {
		case '+':
			newCount++
		case '-':
			oldCount++
		case ' ':
			oldCount++
			newCount++
		default:
			return oldCount, newCount
		}
	}
	return oldCount, newCount
}

// rangeCount parses the count out of "start,count" ("start" means 1).
func rangeCount(s string) (int, bool) {
	_, count, hasComma := strings.Cut(s, ",")
	if !hasComma {
		return 1, true
	}
	n, err := strconv.Atoi(count)
	if err != nil {
		return 0, false
	}
	return n, true
}
