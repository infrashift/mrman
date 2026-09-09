package output

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs/diffparser"
)

// updateGoldens rewrites the checked-in expectations. Run with
// `go test ./internal/output/ -update` after a deliberate format change, then
// read the diff — that review is the point of golden files.
var updateGoldens = flag.Bool("update", false, "rewrite golden files")

// The reply artifact is a whole document where tab fidelity, trailing-space
// rules and blank-line placement are the entire point. A Go string literal
// full of \t and \n cannot be read for correctness, so these expectations live
// in files.

// goldenPath is where a case's expectation lives.
func goldenPath(name string) string {
	return filepath.Join("testdata", "patch", name+".golden")
}

// checkGolden compares got against the checked-in expectation.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := goldenPath(name)
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (run with -update to create it)", err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

// tabbedDiff is kernel-shaped: tab-indented C, a blank context line, and a
// hunk header carrying function context.
const tabbedDiff = "diff --git a/drivers/foo.c b/drivers/foo.c\n" +
	"index 1234567..89abcde 100644\n" +
	"--- a/drivers/foo.c\n" +
	"+++ b/drivers/foo.c\n" +
	"@@ -100,4 +100,6 @@ static int foo_probe(struct platform_device *pdev)\n" +
	" \tint ret;\n" +
	"\n" +
	"+\tif (!bar)\n" +
	"+\t\treturn -EINVAL;\n" +
	" \tret = baz(pdev);\n"

func parseFixture(t *testing.T, src string) []model.DiffFile {
	t.Helper()
	files, err := diffparser.Parse(src, diffparser.GitStyle, nil)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return files
}

// patchSession builds a session over the given paths with no comments yet.
func patchSession(t *testing.T, paths ...string) *model.ReviewSession {
	t.Helper()
	s := model.NewReviewSession("/repo", "9f3c1a2b7d4e5061", nil, model.SourcePatch)
	for _, p := range paths {
		s.AddFile(p, model.StatusModified, 0)
	}
	return s
}

// comment adds a line comment on the new side and returns it.
func comment(t *testing.T, s *model.ReviewSession, path string, line uint32, typ, body string) *model.Comment {
	t.Helper()
	side := model.LineSideNew
	c := model.NewComment(body, model.CommentTypeFromID(typ), &side)
	s.Files[path].AddLineComment(line, c)
	return c
}

func render(t *testing.T, data *PatchData) string {
	t.Helper()
	tmpl, warnings := LoadPatchReplyTemplate("")
	if len(warnings) > 0 {
		t.Fatalf("unexpected template warnings: %v", warnings)
	}
	out, err := RenderPatchReply(tmpl, data)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return out
}

func build(t *testing.T, s *model.ReviewSession, files []model.DiffFile, opts PatchOptions) *PatchData {
	t.Helper()
	// Fixture comments are all written by the default author, so unless a case
	// says otherwise the reply is exported by that author and carries no
	// badges — which is what the goldens below are pinning.
	if opts.Author == (AuthorVisibility{}) {
		opts.Author = AuthorVisibility{Username: model.DefaultAuthor}
	}
	data, err := BuildPatchData(s, files, opts)
	if err != nil {
		t.Fatalf("BuildPatchData: %v", err)
	}
	return data
}

// TestReplyPreservesTabs is the single most important test in this package.
// If it is the only one that survives a refactor, it is the right one to keep:
// a reply that rewrites kernel indentation is worse than no reply.
func TestReplyPreservesTabs(t *testing.T) {
	s := patchSession(t, "drivers/foo.c")
	comment(t, s, "drivers/foo.c", 103, "issue",
		"Wrong errno — the caller distinguishes -ENODEV here.")

	out := render(t, build(t, s, parseFixture(t, tabbedDiff), PatchOptions{}))

	if !strings.Contains(out, "> +\tif (!bar)") {
		t.Errorf("quoted line lost its tab:\n%s", out)
	}
	if !strings.Contains(out, "> +\t\treturn -EINVAL;") {
		t.Errorf("quoted line lost its double tab:\n%s", out)
	}
	if strings.Contains(out, "> +    if (!bar)") {
		t.Error("tabs were expanded to spaces in the reply")
	}
	checkGolden(t, "tabs", out)
}

// TestBlankContextLineQuotesBare covers the trailing-space rule: mailers strip
// it in transit, so emitting "> " only makes what we record disagree with what
// the recipient reads.
func TestBlankContextLineQuotesBare(t *testing.T) {
	s := patchSession(t, "drivers/foo.c")
	comment(t, s, "drivers/foo.c", 103, "note", "fine")

	out := render(t, build(t, s, parseFixture(t, tabbedDiff), PatchOptions{}))

	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, ">") && strings.TrimRight(line, " \t") != line {
			t.Errorf("quoted line has trailing whitespace: %q", line)
		}
	}
	if !strings.Contains(out, "\n>\n") {
		t.Errorf("expected a bare > for the blank context line:\n%s", out)
	}
}

// TestFullReplyShape is the whole artifact, headers and all — the thing a
// reviewer actually pastes into a mail client.
func TestFullReplyShape(t *testing.T) {
	s := patchSession(t, "drivers/foo.c")
	comment(t, s, "drivers/foo.c", 103, "issue",
		"Wrong errno — the caller distinguishes -ENODEV here.\nAlso: check bar before the lock.")
	s.ReviewComments = append(s.ReviewComments,
		model.NewComment("Looks close. Two things about the probe path.",
			model.CommentTypeFromID("none"), nil))

	data := build(t, s, parseFixture(t, tabbedDiff), PatchOptions{
		SessionSlug:     "mail@series/patch/9f3c1a2",
		DiffSourceLabel: "patch file",
		Reply: ReplyHeaders{
			Subject:     "Re: [PATCH v3 2/5] drivers/foo: guard the probe",
			InReplyTo:   "20260101120000.12345-3-dev@example.org",
			References:  []string{"20260101120000.12345-1-dev@example.org"},
			Attribution: "On Wed, 01 Jan 2026, Dev Eloper wrote:",
		},
	})
	checkGolden(t, "full_reply", render(t, data))
}

// TestNoReplyHeadersWhenNoThread covers the case that matters most for
// correctness: plain `git format-patch` writes no Message-Id, so most local
// artifacts cannot be threaded. Emitting invented headers would thread the
// reply to the wrong message, which is worse than not threading at all.
func TestNoReplyHeadersWhenNoThread(t *testing.T) {
	s := patchSession(t, "drivers/foo.c")
	comment(t, s, "drivers/foo.c", 103, "issue", "note")

	out := render(t, build(t, s, parseFixture(t, tabbedDiff), PatchOptions{}))

	for _, header := range []string{"Subject:", "In-Reply-To:", "References:"} {
		if strings.Contains(out, header) {
			t.Errorf("emitted %s with nothing to thread against:\n%s", header, out)
		}
	}
}

// TestBothSidesOfOneLine covers a deletion and an addition that share a
// display position: each comment must land on its own side.
func TestBothSidesOfOneLine(t *testing.T) {
	src := "diff --git a/x.c b/x.c\n--- a/x.c\n+++ b/x.c\n" +
		"@@ -1,2 +1,2 @@\n keep\n-old line\n+new line\n"
	s := patchSession(t, "x.c")

	oldSide := model.LineSideOld
	del := model.NewComment("why was this removed?", model.CommentTypeFromID("note"), &oldSide)
	s.Files["x.c"].AddLineComment(2, del)
	comment(t, s, "x.c", 2, "issue", "and this replacement is wrong")

	checkGolden(t, "both_sides", render(t, build(t, s, parseFixture(t, src), PatchOptions{})))
}

// TestRangeCommentLandsAfterTheRange follows the keying: a range comment is
// stored under its end line, which puts it after the whole quoted span — the
// mail idiom.
func TestRangeCommentLandsAfterTheRange(t *testing.T) {
	src := "diff --git a/x.c b/x.c\n--- a/x.c\n+++ b/x.c\n" +
		"@@ -1,1 +1,4 @@\n keep\n+one\n+two\n+three\n"
	s := patchSession(t, "x.c")

	side := model.LineSideNew
	c := model.NewComment("this whole block belongs in a helper",
		model.CommentTypeFromID("suggestion"), &side)
	rng := model.NewLineRange(2, 4)
	c.LineRange = &rng
	s.Files["x.c"].AddLineComment(4, c)

	out := render(t, build(t, s, parseFixture(t, src), PatchOptions{}))
	idxThree := strings.Index(out, "> +three")
	idxNote := strings.Index(out, "belongs in a helper")
	if idxThree < 0 || idxNote < 0 || idxNote < idxThree {
		t.Errorf("range comment should follow the whole span:\n%s", out)
	}
	checkGolden(t, "range", out)
}

// TestOutdatedAnchorIsNotInterleaved is the safety property. The comment's
// line number still resolves, so quoting it would attach the reviewer's
// criticism to code they never read.
func TestOutdatedAnchorIsNotInterleaved(t *testing.T) {
	s := patchSession(t, "drivers/foo.c")
	// A good comment alongside the stale one, so the hunk is quoted and the
	// assertion is about placement rather than about an empty document.
	comment(t, s, "drivers/foo.c", 102, "note", "this one still fits")
	stale := comment(t, s, "drivers/foo.c", 103, "issue", "this no longer applies where it says")
	stale.LineContext = &model.LineContext{Content: "\t\treturn -ENODEV;"}

	out := render(t, build(t, s, parseFixture(t, tabbedDiff), PatchOptions{
		Outdated: func(id string) bool { return id == stale.ID },
	}))

	quoted := strings.Index(out, "> +\t\treturn -EINVAL;")
	note := strings.Index(out, "no longer applies")
	if quoted < 0 {
		t.Fatalf("the diff should still be quoted:\n%s", out)
	}
	if note < quoted {
		t.Errorf("a stale comment must not be interleaved into the diff:\n%s", out)
	}
	if !strings.Contains(out, "this one still fits") {
		t.Errorf("the placeable comment should still be interleaved:\n%s", out)
	}
	if !strings.Contains(out, "could not be placed") {
		t.Errorf("expected an orphan section:\n%s", out)
	}
	// The snapshot tells the reader what the note was actually about.
	if !strings.Contains(out, "return -ENODEV;") {
		t.Errorf("expected the anchor snapshot in the orphan entry:\n%s", out)
	}
	checkGolden(t, "outdated", out)
}

// TestMovedAnchorIsBadged covers the benign sibling: a re-anchored comment is
// interleaved normally, but says it moved.
func TestMovedAnchorIsBadged(t *testing.T) {
	s := patchSession(t, "drivers/foo.c")
	moved := comment(t, s, "drivers/foo.c", 103, "issue", "still applies, just shifted")

	out := render(t, build(t, s, parseFixture(t, tabbedDiff), PatchOptions{
		AnchorLabel: func(id string) string {
			if id == moved.ID {
				return "moved"
			}
			return ""
		},
	}))
	if !strings.Contains(out, "(moved)") {
		t.Errorf("a re-anchored comment should say so:\n%s", out)
	}
	if strings.Contains(out, "could not be placed") {
		t.Error("a moved comment is placeable and must not be orphaned")
	}
}

// TestUnquotableFiles covers every kind of file that carries no diff to quote.
// Each is named rather than silently dropped, and never given invented text.
func TestUnquotableFiles(t *testing.T) {
	cases := []struct {
		name   string
		file   model.DiffFile
		reason string
	}{
		{"binary", model.DiffFile{NewPath: strPtr("logo.png"), IsBinary: true}, "binary file"},
		{"too_large", model.DiffFile{NewPath: strPtr("huge.bin"), IsTooLarge: true}, "file too large"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := tc.file.DisplayPath()
			s := patchSession(t, path)
			comment(t, s, path, 1, "note", "a note on an unquotable file")

			out := render(t, build(t, s, []model.DiffFile{tc.file}, PatchOptions{}))
			if !strings.Contains(out, tc.reason) {
				t.Errorf("expected %q in the output:\n%s", tc.reason, out)
			}
			if strings.Contains(out, "> ") {
				t.Errorf("nothing may be quoted for an unquotable file:\n%s", out)
			}
		})
	}
}

// TestCommentOnAPathNotInTheDiff covers a session carrying comments on a file
// the current diff does not include — a path filter, an ignore rule, or a
// narrower target.
func TestCommentOnAPathNotInTheDiff(t *testing.T) {
	s := patchSession(t, "drivers/foo.c", "elsewhere.c")
	comment(t, s, "drivers/foo.c", 103, "issue", "in the diff")
	comment(t, s, "elsewhere.c", 1, "note", "not in the diff")

	out := render(t, build(t, s, parseFixture(t, tabbedDiff), PatchOptions{}))
	if !strings.Contains(out, "not in the reviewed diff") {
		t.Errorf("expected the absent file to be named:\n%s", out)
	}
	if !strings.Contains(out, "> +\tif (!bar)") {
		t.Error("the file that is present should still quote")
	}
}

// TestCommentedContextOnlyQuotesCommentedHunks keeps a reply readable: the
// author already has the patch, so re-quoting untouched hunks is noise.
func TestCommentedContextOnlyQuotesCommentedHunks(t *testing.T) {
	src := "diff --git a/x.c b/x.c\n--- a/x.c\n+++ b/x.c\n" +
		"@@ -1,1 +1,2 @@\n first\n+added here\n" +
		"@@ -50,1 +51,2 @@\n second\n+added there\n"
	s := patchSession(t, "x.c")
	comment(t, s, "x.c", 52, "issue", "only this hunk was commented")

	files := parseFixture(t, src)

	commented := render(t, build(t, s, files, PatchOptions{Context: ContextCommented}))
	if strings.Contains(commented, "added here") {
		t.Errorf("uncommented hunk should be omitted:\n%s", commented)
	}
	if !strings.Contains(commented, "added there") {
		t.Errorf("commented hunk must be quoted:\n%s", commented)
	}

	all := render(t, build(t, s, files, PatchOptions{Context: ContextAll}))
	if !strings.Contains(all, "added here") || !strings.Contains(all, "added there") {
		t.Errorf("ContextAll should quote every hunk:\n%s", all)
	}
}

// TestFilesFollowPatchOrder exists because NewApp sorts DiffFiles by directory
// for the file tree. A reply quoted in that order is hard to follow against
// the mail the author sent.
func TestFilesFollowPatchOrder(t *testing.T) {
	src := "diff --git a/z/last.c b/z/last.c\n--- a/z/last.c\n+++ b/z/last.c\n" +
		"@@ -1,1 +1,2 @@\n a\n+b\n" +
		"diff --git a/a/first.c b/a/first.c\n--- a/a/first.c\n+++ b/a/first.c\n" +
		"@@ -1,1 +1,2 @@\n c\n+d\n"
	s := patchSession(t, "z/last.c", "a/first.c")
	comment(t, s, "z/last.c", 2, "note", "written first")
	comment(t, s, "a/first.c", 2, "note", "written second")

	out := render(t, build(t, s, parseFixture(t, src), PatchOptions{}))

	// Alphabetically a/first.c would come first; the patch put z/last.c first.
	if strings.Index(out, "z/last.c") > strings.Index(out, "a/first.c") {
		t.Errorf("files should follow the diff's order, not the tree's:\n%s", out)
	}
}

// TestCommentTextStartingWithQuoteStaysProse guards a real collision: a
// reviewer quoting the author's own words writes a line beginning with "> ",
// and that must not be mistaken for quoted diff.
func TestCommentTextStartingWithQuoteStaysProse(t *testing.T) {
	s := patchSession(t, "drivers/foo.c")
	comment(t, s, "drivers/foo.c", 103, "note",
		"> you wrote that bar is always set\n\nIt is not, on the error path.")

	out := render(t, build(t, s, parseFixture(t, tabbedDiff), PatchOptions{}))
	if !strings.Contains(out, "> you wrote that bar is always set") {
		t.Errorf("the reviewer's own quoting should survive verbatim:\n%s", out)
	}
	checkGolden(t, "quoted_prose", out)
}

// TestEmptySessionRefuses matches the notes export: nothing to say, nothing
// to send.
func TestEmptySessionRefuses(t *testing.T) {
	s := patchSession(t, "drivers/foo.c")
	if _, err := BuildPatchData(s, parseFixture(t, tabbedDiff), PatchOptions{}); !errors.Is(err, errs.ErrNoComments) {
		t.Errorf("err = %v, want ErrNoComments", err)
	}
}

// TestSynthesizedLinesStillQuote covers a diff whose lines carry no Raw —
// anything mrman built rather than parsed. The prefix is reconstructed from
// the origin so the quote still reads as a diff.
func TestSynthesizedLinesStillQuote(t *testing.T) {
	line := uint32(1)
	files := []model.DiffFile{{
		NewPath:     strPtr("synth.txt"),
		Status:      model.StatusAdded,
		SourceIndex: -1,
		Hunks: []model.DiffHunk{{
			Header: "@@ -0,0 +1,1 @@",
			Lines: []model.DiffLine{
				{Origin: model.OriginAddition, Content: "hello", NewLineno: &line},
			},
		}},
	}}
	s := patchSession(t, "synth.txt")
	comment(t, s, "synth.txt", 1, "note", "on a synthesized line")

	out := render(t, build(t, s, files, PatchOptions{}))
	if !strings.Contains(out, "> +hello") {
		t.Errorf("expected the origin prefix to be reconstructed:\n%s", out)
	}
}

func strPtr(s string) *string { return &s }

// TestSeriesQuotesEachPatchSeparately covers the reply half of the same
// collision. Grouping by path alone kept one entry per path, so a comment
// written on patch 2 was quoted against patch 1's diff — or orphaned.
func TestSeriesQuotesEachPatchSeparately(t *testing.T) {
	first := parseFixture(t, "diff --git a/net/foo.c b/net/foo.c\n"+
		"--- a/net/foo.c\n+++ b/net/foo.c\n@@ -1,2 +1,2 @@\n keep\n-int n;\n+int count;\n")[0]
	first.CommitID, first.SourceIndex = "patch-0001", 0
	second := parseFixture(t, "diff --git a/net/foo.c b/net/foo.c\n"+
		"--- a/net/foo.c\n+++ b/net/foo.c\n@@ -1,2 +1,3 @@\n keep\n int count;\n+count++;\n")[0]
	second.CommitID, second.SourceIndex = "patch-0002", 1

	s := patchSession(t, "net/foo.c")
	one, two := "patch-0001", "patch-0002"
	side := model.LineSideNew
	c1 := model.NewComment("naming nit on the first patch", model.CommentTypeFromID("note"), &side)
	c1.CommitID = &one
	s.Files["net/foo.c"].AddLineComment(2, c1)
	c2 := model.NewComment("off-by-one on the second", model.CommentTypeFromID("issue"), &side)
	c2.CommitID = &two
	s.Files["net/foo.c"].AddLineComment(3, c2)

	out := render(t, build(t, s, []model.DiffFile{first, second}, PatchOptions{}))

	// Both patches are quoted, in source order.
	if strings.Count(out, "diff --git a/net/foo.c") != 2 {
		t.Errorf("expected both patches quoted:\n%s", out)
	}
	if strings.Index(out, "int count;") > strings.Index(out, "count++;") {
		t.Errorf("patches should quote in source order:\n%s", out)
	}
	// Each comment appears exactly once, under its own patch.
	for _, want := range []string{"naming nit on the first patch", "off-by-one on the second"} {
		if n := strings.Count(out, want); n != 1 {
			t.Errorf("comment %q appears %d times, want 1:\n%s", want, n, out)
		}
	}
	if strings.Contains(out, "could not be placed") {
		t.Errorf("both comments are placeable:\n%s", out)
	}
	checkGolden(t, "series_two_patches", out)
}

// TestReplyBadgesTheAuthor covers the reply's half of the author badge,
// including the case the note template composes by hand: a badged comment
// that also moved, where "[TYPE @author] (moved)" has to come out in order.
func TestReplyBadgesTheAuthor(t *testing.T) {
	s := patchSession(t, "drivers/foo.c")
	mine := comment(t, s, "drivers/foo.c", 102, "note", "mine, unbadged")
	mine.Author = "ryan"
	theirs := comment(t, s, "drivers/foo.c", 103, "issue", "theirs, badged")
	theirs.Author = "claude"

	out := render(t, build(t, s, parseFixture(t, tabbedDiff), PatchOptions{
		Author: AuthorVisibility{Username: "ryan"},
		AnchorLabel: func(id string) string {
			if id == theirs.ID {
				return "moved"
			}
			return ""
		},
	}))

	if !strings.Contains(out, "[ISSUE @claude] (moved) theirs, badged") {
		t.Errorf("a badged comment that moved must render both, in order:\n%s", out)
	}
	if !strings.Contains(out, "[NOTE] mine, unbadged") {
		t.Errorf("your own comment must stay unbadged:\n%s", out)
	}
}

// TestReplyBadgesTypelessComment: with no type the badge brings its own
// bracket, so attribution does not depend on having typed the comment.
func TestReplyBadgesTypelessComment(t *testing.T) {
	s := patchSession(t, "drivers/foo.c")
	c := comment(t, s, "drivers/foo.c", 102, "", "plain remark")
	c.Author = "claude"

	out := render(t, build(t, s, parseFixture(t, tabbedDiff), PatchOptions{
		Author: AuthorVisibility{Username: "ryan"},
	}))

	if !strings.Contains(out, "[@claude] plain remark") {
		t.Errorf("typeless authored comment must carry a bare author bracket:\n%s", out)
	}
}
