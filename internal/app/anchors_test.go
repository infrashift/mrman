package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/submit"
	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs"
)

// hunkWithContents builds a context-only hunk whose lines hold the given
// contents, numbered from newStart with old == new. Unlike makeHunk it lets a
// test repeat a line, which is the whole point of the ambiguous case.
func hunkWithContents(newStart uint32, contents ...string) model.DiffHunk {
	lines := make([]model.DiffLine, 0, len(contents))
	for i, content := range contents {
		no := newStart + uint32(i)
		lines = append(lines, model.DiffLine{
			Origin:    model.OriginContext,
			Content:   content,
			OldLineno: new(no),
			NewLineno: new(no),
		})
	}
	count := uint32(len(contents))
	return model.DiffHunk{
		Header:   fmt.Sprintf("@@ -%d,%d +%d,%d @@", newStart, count, newStart, count),
		Lines:    lines,
		OldStart: newStart, OldCount: count,
		NewStart: newStart, NewCount: count,
	}
}

// commentedApp builds an app over one file of contents whose session already
// carries a line comment keyed at line, snapshotted as having held snapshot.
// This is the shape of a reopened session: a comment written against a diff
// that may since have moved.
func commentedApp(t *testing.T, line uint32, snapshot string, contents ...string) (*App, *model.Comment) {
	t.Helper()
	return commentedAppWith(t, line, snapshot, nil, contents...)
}

// commentedAppWith is commentedApp with a hook to finish the comment before
// the app opens. The hook matters: NewApp validates during construction, so a
// comment mutated afterwards has already been judged as it was.
func commentedAppWith(t *testing.T, line uint32, snapshot string,
	prep func(*model.Comment), contents ...string) (*App, *model.Comment) {
	t.Helper()
	const path = "src/x.go"
	info := &vcs.Info{RootPath: "/tmp", HeadCommit: "abc123", BranchName: new("main"), Type: vcs.TypeGit}
	session := model.NewReviewSession(info.RootPath, info.HeadCommit, info.BranchName, model.SourceWorkingTree)
	session.Files[path] = model.NewFileReview(path, model.StatusModified, 0)

	side := model.LineSideNew
	c := model.NewComment("needs work", model.CommentTypeFromID("issue"), &side)
	c.LineContext = &model.LineContext{NewLine: new(line), OldLine: new(line), Content: snapshot}
	if prep != nil {
		prep(c)
	}
	session.Files[path].AddLineComment(line, c)

	files := []model.DiffFile{makeFileWithHunks(path, []model.DiffHunk{hunkWithContents(1, contents...)})}
	a := NewApp(&mockVcs{info: info, totalLines: 20}, info, files, session,
		DiffSource{Kind: DiffSourceWorkingTree})
	return a, c
}

// withRange returns a prep hook installing an inclusive line range.
func withRange(start, end uint32) func(*model.Comment) {
	return func(c *model.Comment) {
		rng := model.NewLineRange(start, end)
		c.LineRange = &rng
	}
}

// lineKeyOf reports which line key a comment is filed under, or 0.
//
//nolint:unparam // path is fixed today; kept so the helper reads at its call sites
func lineKeyOf(a *App, path, id string) uint32 {
	review := a.Session.File(path)
	if review == nil {
		return 0
	}
	for line, comments := range review.LineComments {
		for _, c := range comments {
			if c.ID == id {
				return line
			}
		}
	}
	return 0
}

// TestAnchorIntactWhenLineUnchanged is the common case: nothing moved, so
// validation must leave the comment exactly where it was and say nothing.
func TestAnchorIntactWhenLineUnchanged(t *testing.T) {
	a, c := commentedApp(t, 2, "b", "a", "b", "c", "d", "e")

	if got := a.AnchorVerdictFor(c.ID); got != AnchorIntact {
		t.Errorf("verdict = %v, want AnchorIntact", got)
	}
	if a.HasOutdatedAnchor(c.ID) {
		t.Error("an unchanged anchor must not read as outdated")
	}
	if got := lineKeyOf(a, "src/x.go", c.ID); got != 2 {
		t.Errorf("line key = %d, want 2", got)
	}
	if a.AnchorStats.Moved != 0 || a.AnchorStats.Outdated != 0 {
		t.Errorf("stats = %+v, want no movement", a.AnchorStats)
	}
	if a.AnchorStats.Message() != "" {
		t.Errorf("an unchanged review must report nothing, got %q", a.AnchorStats.Message())
	}
}

// TestAnchorMovedReanchorsComment is the payoff: the code the comment was
// written about is still there, three lines down, so the comment follows it
// instead of staying pinned to whatever now occupies its old line number.
func TestAnchorMovedReanchorsComment(t *testing.T) {
	a, c := commentedApp(t, 2, "d", "a", "b", "c", "d", "e")

	if got := a.AnchorVerdictFor(c.ID); got != AnchorMoved {
		t.Fatalf("verdict = %v, want AnchorMoved", got)
	}
	if a.HasOutdatedAnchor(c.ID) {
		t.Error("a re-anchored comment is not outdated — it was placed")
	}
	if got := lineKeyOf(a, "src/x.go", c.ID); got != 4 {
		t.Errorf("line key = %d, want 4 (where the content now lives)", got)
	}
	if c.LineContext == nil || c.LineContext.NewLine == nil || *c.LineContext.NewLine != 4 {
		t.Errorf("snapshot was not refreshed to the new line: %+v", c.LineContext)
	}
	if c.LineContext.Content != "d" {
		t.Errorf("snapshot content = %q, want %q", c.LineContext.Content, "d")
	}
	if a.AnchorStats.Moved != 1 {
		t.Errorf("stats = %+v, want Moved 1", a.AnchorStats)
	}
	// The move has to reach disk, or the next open redoes the search against a
	// session that still claims the old line.
	if !a.Dirty {
		t.Error("re-anchoring must mark the session dirty so the move persists")
	}
}

// TestAnchorGoneWhenContentDeleted covers the case the whole feature exists
// for: the line was deleted, the old line number still resolves to *something*,
// and posting there would attach criticism to unrelated code.
func TestAnchorGoneWhenContentDeleted(t *testing.T) {
	a, c := commentedApp(t, 2, "the line that got deleted", "a", "b", "c")

	if got := a.AnchorVerdictFor(c.ID); got != AnchorGone {
		t.Fatalf("verdict = %v, want AnchorGone", got)
	}
	if !a.HasOutdatedAnchor(c.ID) {
		t.Error("a deleted anchor must read as outdated")
	}
	// Flagged, not moved and not dropped: the comment is still the reviewer's
	// to keep, edit, or discard.
	if got := lineKeyOf(a, "src/x.go", c.ID); got != 2 {
		t.Errorf("line key = %d, want 2 — a stale comment is flagged, not relocated", got)
	}
	if a.AnchorStats.Outdated != 1 {
		t.Errorf("stats = %+v, want Outdated 1", a.AnchorStats)
	}
	if got := a.AnchorVerdictFor(c.ID).Label(); got != "outdated" {
		t.Errorf("label = %q, want %q", got, "outdated")
	}
}

// TestAnchorAmbiguousWhenContentRepeats keeps the re-anchoring honest. A line
// that reads `}` says nothing about which `}` was under discussion, so mrman
// declines to pick one rather than guessing at where criticism lands.
func TestAnchorAmbiguousWhenContentRepeats(t *testing.T) {
	a, c := commentedApp(t, 1, "}", "a", "b", "}", "d", "}")

	if got := a.AnchorVerdictFor(c.ID); got != AnchorAmbiguous {
		t.Fatalf("verdict = %v, want AnchorAmbiguous", got)
	}
	if !a.HasOutdatedAnchor(c.ID) {
		t.Error("an unresolvable anchor must read as outdated")
	}
	if got := lineKeyOf(a, "src/x.go", c.ID); got != 1 {
		t.Errorf("line key = %d, want 1 — an ambiguous match must not move the comment", got)
	}
}

// TestAnchorIntactWinsOverRepeats guards the ordering: when the keyed line
// still holds the content, a duplicate elsewhere is irrelevant.
func TestAnchorIntactWinsOverRepeats(t *testing.T) {
	a, c := commentedApp(t, 3, "}", "a", "b", "}", "d", "}")

	if got := a.AnchorVerdictFor(c.ID); got != AnchorIntact {
		t.Errorf("verdict = %v, want AnchorIntact", got)
	}
	if got := lineKeyOf(a, "src/x.go", c.ID); got != 3 {
		t.Errorf("line key = %d, want 3", got)
	}
}

// TestAnchorUnverifiedWithoutSnapshot covers comments written before
// snapshotting existed, and those an agent adds through `mrman review add`.
// No evidence is not the same as bad evidence: they must pass unflagged.
func TestAnchorUnverifiedWithoutSnapshot(t *testing.T) {
	a, c := commentedApp(t, 2, "b", "a", "b", "c")
	c.LineContext = nil
	a.ValidateCommentAnchors()

	if got := a.AnchorVerdictFor(c.ID); got != AnchorUnverified {
		t.Errorf("verdict = %v, want AnchorUnverified", got)
	}
	if a.HasOutdatedAnchor(c.ID) {
		t.Error("a comment with no snapshot must not be called outdated")
	}
	if a.AnchorStats.Checked != 0 {
		t.Errorf("stats = %+v, want nothing checked", a.AnchorStats)
	}
}

// TestAnchorUnverifiedWhenFileOutsideDiff is the path-filter guard. Opening
// with --path, an ignore rule, or a narrower target removes whole files from
// the diff; none of that says the code a comment describes has changed.
func TestAnchorUnverifiedWhenFileOutsideDiff(t *testing.T) {
	a, _ := commentedApp(t, 2, "b", "a", "b", "c")

	side := model.LineSideNew
	other := model.NewComment("elsewhere", model.CommentTypeFromID("note"), &side)
	other.LineContext = &model.LineContext{NewLine: new(uint32(9)), OldLine: new(uint32(9)), Content: "not on screen"}
	a.Session.Files["src/hidden.go"] = model.NewFileReview("src/hidden.go", model.StatusModified, 0)
	a.Session.Files["src/hidden.go"].AddLineComment(9, other)

	a.ValidateCommentAnchors()

	if got := a.AnchorVerdictFor(other.ID); got != AnchorUnverified {
		t.Errorf("verdict = %v, want AnchorUnverified for a file outside the diff", got)
	}
	if a.HasOutdatedAnchor(other.ID) {
		t.Error("a filtered-out file's comments must never read as outdated")
	}
}

// TestRangeCommentReanchorsByItsEnd covers the range case. A range comment is
// keyed by (and snapshots) its end line, so only the end is verified and the
// start follows by the same delta.
func TestRangeCommentReanchorsByItsEnd(t *testing.T) {
	a, c := commentedAppWith(t, 2, "e", withRange(1, 2), "a", "b", "c", "d", "e")

	if got := a.AnchorVerdictFor(c.ID); got != AnchorMoved {
		t.Fatalf("verdict = %v, want AnchorMoved", got)
	}
	if got := lineKeyOf(a, "src/x.go", c.ID); got != 5 {
		t.Errorf("line key = %d, want 5", got)
	}
	if c.LineRange.Start != 4 || c.LineRange.End != 5 {
		t.Errorf("range = %+v, want {4 5} — the span shifts with its end", *c.LineRange)
	}
}

// TestReanchorClampsRangeStart guards the uint32 subtraction: a range wider
// than its new end line has nowhere to put its start but line 1.
func TestReanchorClampsRangeStart(t *testing.T) {
	a, c := commentedAppWith(t, 5, "b", withRange(1, 5), "a", "b", "c", "d", "e")

	if got := lineKeyOf(a, "src/x.go", c.ID); got != 2 {
		t.Fatalf("line key = %d, want 2", got)
	}
	if c.LineRange.Start != 1 || c.LineRange.End != 2 {
		t.Errorf("range = %+v, want {1 2}", *c.LineRange)
	}
}

// TestOldSideAnchorValidatesAgainstOldLinenos covers a comment on a deleted
// line. Its key is an old-side line number, so validating it against new-side
// numbering would compare the wrong column entirely.
func TestOldSideAnchorValidatesAgainstOldLinenos(t *testing.T) {
	const path = "src/x.go"
	info := &vcs.Info{RootPath: "/tmp", HeadCommit: "abc123", BranchName: new("main"), Type: vcs.TypeGit}
	session := model.NewReviewSession(info.RootPath, info.HeadCommit, info.BranchName, model.SourceWorkingTree)
	session.Files[path] = model.NewFileReview(path, model.StatusModified, 0)

	side := model.LineSideOld
	c := model.NewComment("about the removed line", model.CommentTypeFromID("issue"), &side)
	c.LineContext = &model.LineContext{OldLine: new(uint32(2)), Content: "removed"}
	session.Files[path].AddLineComment(2, c)

	// One added line (new-side only) then the removed line at old 4: the
	// old-side numbering runs ahead of the key the comment carries.
	hunk := model.DiffHunk{
		Header: "@@ -3,2 +3,2 @@",
		Lines: []model.DiffLine{
			{Origin: model.OriginAddition, Content: "added", NewLineno: new(uint32(3))},
			{Origin: model.OriginDeletion, Content: "removed", OldLineno: new(uint32(4))},
		},
		OldStart: 3, OldCount: 2, NewStart: 3, NewCount: 2,
	}
	files := []model.DiffFile{makeFileWithHunks(path, []model.DiffHunk{hunk})}
	a := NewApp(&mockVcs{info: info, totalLines: 20}, info, files, session,
		DiffSource{Kind: DiffSourceWorkingTree})

	if got := a.AnchorVerdictFor(c.ID); got != AnchorMoved {
		t.Fatalf("verdict = %v, want AnchorMoved", got)
	}
	if got := lineKeyOf(a, path, c.ID); got != 4 {
		t.Errorf("line key = %d, want 4 — the old-side number, not the new-side one", got)
	}
	if c.LineContext.OldLine == nil || *c.LineContext.OldLine != 4 {
		t.Errorf("snapshot old line not refreshed: %+v", c.LineContext)
	}
	// The added line is new-side only and must never be a candidate for an
	// old-side anchor, whatever its content.
	if c.LineContext.Content != "removed" {
		t.Errorf("snapshot content = %q, want %q", c.LineContext.Content, "removed")
	}
}

// TestPeekPanelFlagsOutdatedAnchor covers the surface with the most to lose:
// the peek panel renders the anchored source line right beside the comment, so
// a stale anchor shows unrelated code as though it were the subject.
func TestPeekPanelFlagsOutdatedAnchor(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	side := model.LineSideNew
	c := model.NewComment("needs a guard", model.CommentTypeFromID("issue"), &side)
	c.LineContext = &model.LineContext{NewLine: new(uint32(2)), Content: "a line since deleted"}
	a.Session.File("test.rs").AddLineComment(2, c)
	a.ValidateCommentAnchors()
	if !a.HasOutdatedAnchor(c.ID) {
		t.Fatal("precondition: the anchor must be outdated")
	}

	// Fold the file so Enter peeks instead of jumping.
	a.Session.File("test.rs").Reviewed = true
	a.RebuildAnnotations()
	a.FocusPanel(PanelComments)
	a.CommentNav.Cursor = 0
	a.CommentNavSelect()
	if a.CommentPeek == nil {
		t.Fatal("a collapsed comment must open the peek panel")
	}

	var header string
	for _, line := range a.CommentPeek.Lines {
		if line.Kind == PeekCommentHeader {
			header = line.Text
			break
		}
	}
	if !strings.Contains(header, "(outdated)") {
		t.Errorf("peek header = %q, want it to flag the stale anchor", header)
	}
}

// TestAnchorStatsMessage covers the wording, including the combined case a
// single-outcome test never reaches.
func TestAnchorStatsMessage(t *testing.T) {
	cases := []struct {
		name  string
		stats AnchorStats
		want  string
	}{
		{"quiet", AnchorStats{Checked: 3}, ""},
		{"moved only", AnchorStats{Checked: 3, Moved: 2}, "2 comment(s) re-anchored"},
		{"outdated only", AnchorStats{Checked: 3, Outdated: 1},
			"1 comment(s) outdated — the code they point at changed"},
		{"both", AnchorStats{Checked: 4, Moved: 2, Outdated: 1},
			"2 comment(s) re-anchored, 1 outdated"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.stats.Message(); got != tc.want {
				t.Errorf("Message() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestAnchorVerdictLabels pins the badge text the comment box renders.
func TestAnchorVerdictLabels(t *testing.T) {
	cases := map[AnchorVerdict]string{
		AnchorUnverified: "",
		AnchorIntact:     "",
		AnchorMoved:      "moved",
		AnchorGone:       "outdated",
		AnchorAmbiguous:  "outdated",
	}
	for verdict, want := range cases {
		if got := verdict.Label(); got != want {
			t.Errorf("verdict %d: Label() = %q, want %q", verdict, got, want)
		}
	}
}

// TestOutdatedAnchorRefusedInlineAtSubmit is the submit-time interlock. The
// stale comment's line number still maps, so nothing but the anchor verdict
// stops it from posting inline against code it was not written about.
func TestOutdatedAnchorRefusedInlineAtSubmit(t *testing.T) {
	a := prTestApp(t, allCaps())
	c := addLineComment(t, a, "src/x.go", 2, "this is about the old line 2")
	c.LineContext = &model.LineContext{NewLine: new(uint32(2)), OldLine: new(uint32(2)), Content: "long gone"}
	a.ValidateCommentAnchors()
	if !a.HasOutdatedAnchor(c.ID) {
		t.Fatal("precondition: the anchor must be outdated")
	}

	if !a.StartSubmitWith(forge.SubmitComment, true) {
		t.Fatal("submit preflight failed")
	}
	if len(a.Submit.Mappable) != 0 {
		t.Errorf("a stale comment must not be posted inline, got %d inline", len(a.Submit.Mappable))
	}
	if len(a.Submit.Unmappable) != 1 {
		t.Fatalf("unmappable = %d, want 1", len(a.Submit.Unmappable))
	}
	if got := a.Submit.Unmappable[0].Reason; got != submit.StaleAnchor {
		t.Errorf("reason = %v, want submit.StaleAnchor", got)
	}
	// The resolver forces a decision per comment even on the skip-confirm
	// path: mrman does not get to guess where criticism lands.
	if a.InputMode != input.ModeSubmitResolver {
		t.Errorf("input mode = %v, want ModeSubmitResolver even with skipConfirm", a.InputMode)
	}
	if a.Submit.Unmappable[0].Action != submit.MoveToSummary {
		t.Errorf("default action = %v, want MoveToSummary", a.Submit.Unmappable[0].Action)
	}
	if !a.HasOutdatedAnchor(c.ID) {
		t.Error("the verdict must survive the submit flow")
	}
}

// TestIntactAnchorStillSubmitsInline is the other half: validation must not
// have made the ordinary case harder.
func TestIntactAnchorStillSubmitsInline(t *testing.T) {
	a := prTestApp(t, allCaps())
	c := addLineComment(t, a, "src/x.go", 2, "still true")
	c.LineContext = a.LineContextAt("src/x.go", 2, model.LineSideNew)
	a.ValidateCommentAnchors()

	if !a.StartSubmitWith(forge.SubmitComment, true) {
		t.Fatal("submit preflight failed")
	}
	if len(a.Submit.Mappable) != 1 || len(a.Submit.Unmappable) != 0 {
		t.Errorf("got %d inline / %d unmappable, want 1 / 0",
			len(a.Submit.Mappable), len(a.Submit.Unmappable))
	}
}

// TestReloadRevalidatesAnchors covers `:e` — the diff moving underneath a live
// session, which is the same hazard as reopening one.
func TestReloadRevalidatesAnchors(t *testing.T) {
	a, c := commentedApp(t, 2, "b", "a", "b", "c")

	// The rewrite pushes "b" down two lines.
	reloaded := []model.DiffFile{makeFileWithHunks("src/x.go",
		[]model.DiffHunk{hunkWithContents(1, "a", "new", "extra", "b", "c")})}
	stats := a.ApplyReloadedDiff(reloaded)

	if stats.Anchors.Moved != 1 {
		t.Errorf("anchors = %+v, want Moved 1", stats.Anchors)
	}
	if got := lineKeyOf(a, "src/x.go", c.ID); got != 4 {
		t.Errorf("line key = %d, want 4", got)
	}
	if msg := stats.Message(); !strings.Contains(msg, "re-anchored") {
		t.Errorf("reload message %q must mention the re-anchoring", msg)
	}
}

// TestApplyLoadedSelectionClearsAnchorVerdicts guards against the worst
// false positive available: narrowing to a commit subrange hides hunks, and a
// verdict computed against the full target would then read as staleness the
// reviewer caused by changing the view.
func TestApplyLoadedSelectionClearsAnchorVerdicts(t *testing.T) {
	a, c := commentedApp(t, 2, "long gone", "a", "b", "c")
	if !a.HasOutdatedAnchor(c.ID) {
		t.Fatal("precondition: the anchor must be outdated")
	}

	a.ApplyLoadedSelection(a.DiffFiles, a.Session, DiffSource{Kind: DiffSourceCommitRange})

	if got := a.AnchorVerdictFor(c.ID); got != AnchorUnverified {
		t.Errorf("verdict = %v, want AnchorUnverified after a narrowed load", got)
	}
	if a.AnchorStats != (AnchorStats{}) {
		t.Errorf("stats = %+v, want zeroed", a.AnchorStats)
	}
}
