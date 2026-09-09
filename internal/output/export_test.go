package output

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/model"
)

// testLegend mirrors the Rust suite's comment_types() fixture.
func testLegend() []LegendEntry {
	return []LegendEntry{
		{ID: "note", Label: "note", Definition: "observations"},
		{ID: "suggestion", Label: "suggestion", Definition: "improvements"},
		{ID: "issue", Label: "issue", Definition: "problems to fix"},
		{ID: "praise", Label: "praise", Definition: "positive feedback"},
	}
}

// newSession builds an empty working-tree session on branch main.
func newSession() *model.ReviewSession {
	branch := "main"
	return model.NewReviewSession("/tmp/test-repo", "abc1234def", &branch, model.SourceWorkingTree)
}

// newTestSession mirrors the Rust suite's create_test_session(): one
// modified file with a suggestion file comment and an issue line comment.
func newTestSession() *model.ReviewSession {
	session := newSession()
	session.AddFile("src/main.rs", model.StatusModified, 0)
	review := session.File("src/main.rs")
	review.Reviewed = true
	review.AddFileComment(model.NewComment(
		"Consider adding documentation", model.CommentTypeFromID("suggestion"), nil))
	side := model.LineSideNew
	review.AddLineComment(42, model.NewComment(
		"Magic number should be a constant", model.CommentTypeFromID("issue"), &side))
	return session
}

func newSide(t *testing.T) *model.LineSide {
	t.Helper()
	side := model.LineSideNew
	return &side
}

func oldSide(t *testing.T) *model.LineSide {
	t.Helper()
	side := model.LineSideOld
	return &side
}

// renderDefault builds template data and renders it with the embedded
// default template, failing the test on any error or load warning.
func renderDefault(t *testing.T, session *model.ReviewSession, kind ScopeKind, commits []string,
	showLegend bool, types []LegendEntry, slug string) string {
	t.Helper()
	tmpl, warnings := LoadNotesTemplate("")
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings loading default template: %v", warnings)
	}
	data, err := BuildTemplateData(session, nil, kind.ScopeLine(commits), ExportOptions{
		SessionSlug:     slug,
		DiffSourceLabel: kind.Label(),
		ShowLegend:      showLegend,
		CommentTypes:    types,
		// Every fixture comment is written by the default author, so exporting
		// as that author keeps these tests about what they are about: the
		// unbadged shape mrman inherited from tuicr. Badging has its own tests.
		Author: AuthorVisibility{Username: model.DefaultAuthor},
	})
	if err != nil {
		t.Fatalf("BuildTemplateData: %v", err)
	}
	out, err := RenderNotes(tmpl, data)
	if err != nil {
		t.Fatalf("RenderNotes: %v", err)
	}
	return out
}

func TestExportGoldenBasicSession(t *testing.T) {
	got := renderDefault(t, newTestSession(), ScopeWorkingTree, nil, true, testLegend(), "")
	want := "I reviewed your code and have the following comments. Please address them.\n" +
		"\n" +
		"Comment types: SUGGESTION (improvements), ISSUE (problems to fix)\n" +
		"\n" +
		"## Local mrman Comments\n" +
		"\n" +
		"1. **[SUGGESTION]** `src/main.rs` - Consider adding documentation\n" +
		"2. **[ISSUE]** `src/main.rs:42` - Magic number should be a constant\n"
	if got != want {
		t.Errorf("golden mismatch:\ngot:\n%q\nwant:\n%q", got, want)
	}
}

func TestExportGoldenWithSlugScopeAndNotes(t *testing.T) {
	session := newTestSession()
	notes := "Overall looks good"
	session.SessionNotes = &notes
	commits := []string{"abc1234567890", "def4567890123"}
	got := renderDefault(t, session, ScopeCommitRange, commits, true, testLegend(),
		"agavra/tuicr@main/worktree")
	want := "## Session: agavra/tuicr@main/worktree\n" +
		"\n" +
		"I reviewed your code and have the following comments. Please address them.\n" +
		"\n" +
		"Reviewing commits: abc1234, def4567\n" +
		"\n" +
		"Comment types: SUGGESTION (improvements), ISSUE (problems to fix)\n" +
		"\n" +
		"Summary: Overall looks good\n" +
		"\n" +
		"## Local mrman Comments\n" +
		"\n" +
		"1. **[SUGGESTION]** `src/main.rs` - Consider adding documentation\n" +
		"2. **[ISSUE]** `src/main.rs:42` - Magic number should be a constant\n"
	if got != want {
		t.Errorf("golden mismatch:\ngot:\n%q\nwant:\n%q", got, want)
	}
}

func TestExportOmitsSlugHeaderWhenAbsent(t *testing.T) {
	got := renderDefault(t, newTestSession(), ScopeWorkingTree, nil, true, testLegend(), "")
	if strings.Contains(got, "## Session:") {
		t.Errorf("unexpected slug header in:\n%s", got)
	}
}

func TestExportKeepsCommitIdentityForCommitMessageComments(t *testing.T) {
	session := newSession()
	session.DiffSource = model.SourceCommitRange
	first := "Commit Message (ed50028)"
	second := "Commit Message (c17beb2)"
	session.AddFile(first, model.StatusAdded, 0)
	session.AddFile(second, model.StatusAdded, 0)
	session.File(first).AddLineComment(1, model.NewComment(
		"We do not need this commit", model.CommentTypeFromID("note"), newSide(t)))
	session.File(second).AddLineComment(6, model.NewComment(
		"This is wrong", model.CommentTypeFromID("note"), newSide(t)))

	got := renderDefault(t, session, ScopeCommitRange,
		[]string{"ed50028", "c17beb2"}, true, testLegend(), "")

	if !strings.Contains(got, "`Commit Message (ed50028):1`") {
		t.Errorf("first commit's message comment not attributable in:\n%s", got)
	}
	if !strings.Contains(got, "`Commit Message (c17beb2):6`") {
		t.Errorf("second commit's message comment not attributable in:\n%s", got)
	}
}

func TestExportUsesConfiguredLabelAndDefinition(t *testing.T) {
	session := newSession()
	session.AddFile("src/main.rs", model.StatusModified, 0)
	session.File("src/main.rs").AddFileComment(model.NewComment(
		"Needs clarification", model.CommentTypeFromID("note"), nil))
	custom := []LegendEntry{{ID: "note", Label: "question", Definition: "ask for clarification"}}

	got := renderDefault(t, session, ScopeWorkingTree, nil, true, custom, "")

	if !strings.Contains(got, "Comment types: QUESTION (ask for clarification)") {
		t.Errorf("missing configured legend in:\n%s", got)
	}
	if !strings.Contains(got, "**[QUESTION]**") {
		t.Errorf("missing configured type marker in:\n%s", got)
	}
}

func TestExportFallsBackToUppercasedIDForUnconfiguredType(t *testing.T) {
	session := newSession()
	session.AddFile("src/main.rs", model.StatusModified, 0)
	session.File("src/main.rs").AddFileComment(model.NewComment(
		"Odd type", model.CommentTypeFromID("nitpick"), nil))

	got := renderDefault(t, session, ScopeWorkingTree, nil, false, testLegend(), "")

	if !strings.Contains(got, "**[NITPICK]**") {
		t.Errorf("unconfigured type should fall back to uppercased id in:\n%s", got)
	}
}

func TestExportNumbersSequentially(t *testing.T) {
	got := renderDefault(t, newTestSession(), ScopeWorkingTree, nil, true, testLegend(), "")
	if !strings.Contains(got, "1. **[SUGGESTION]**") || !strings.Contains(got, "2. **[ISSUE]**") {
		t.Errorf("expected sequential numbering in:\n%s", got)
	}
}

func TestExportIndentsMultilineUnderSingleDigitMarker(t *testing.T) {
	session := newSession()
	session.AddFile("src/main.rs", model.StatusModified, 0)
	session.File("src/main.rs").AddFileComment(model.NewComment(
		"foo\nbar", model.CommentTypeFromID("suggestion"), nil))

	got := renderDefault(t, session, ScopeWorkingTree, nil, true, testLegend(), "")

	want := "1. **[SUGGESTION]** `src/main.rs` - foo\n   bar"
	if !strings.Contains(got, want) {
		t.Errorf("continuation must align under list text, got:\n%s", got)
	}
}

func TestExportIndentsMultilineUnderDoubleDigitMarker(t *testing.T) {
	session := newSession()
	session.AddFile("src/main.rs", model.StatusModified, 0)
	review := session.File("src/main.rs")
	for i := range 9 {
		review.AddFileComment(model.NewComment(
			fmt.Sprintf("comment %d", i), model.CommentTypeFromID("note"), nil))
	}
	review.AddFileComment(model.NewComment(
		"foo\nbar", model.CommentTypeFromID("suggestion"), nil))

	got := renderDefault(t, session, ScopeWorkingTree, nil, true, testLegend(), "")

	want := "10. **[SUGGESTION]** `src/main.rs` - foo\n    bar"
	if !strings.Contains(got, want) {
		t.Errorf("double-digit continuation must align under list text, got:\n%s", got)
	}
}

func TestExportTrimsCarriageReturnsInContinuations(t *testing.T) {
	session := newSession()
	session.AddFile("src/main.rs", model.StatusModified, 0)
	session.File("src/main.rs").AddFileComment(model.NewComment(
		"foo\r\nbar\r", model.CommentTypeFromID("note"), nil))

	got := renderDefault(t, session, ScopeWorkingTree, nil, false, testLegend(), "")

	if !strings.Contains(got, "1. **[NOTE]** `src/main.rs` - foo\n   bar\n") {
		t.Errorf("CR must be stripped from lines, got:\n%q", got)
	}
}

func TestExportIncludesReviewComments(t *testing.T) {
	session := newTestSession()
	session.ReviewComments = append(session.ReviewComments, model.NewComment(
		"Please split this into smaller commits", model.CommentTypeFromID("note"), nil))

	got := renderDefault(t, session, ScopeWorkingTree, nil, true, testLegend(), "")

	want := "`Review Comment (scope: working tree changes)` - Please split this into smaller commits"
	if !strings.Contains(got, want) {
		t.Errorf("missing review comment location in:\n%s", got)
	}
	if !strings.Contains(got, "1. **[NOTE]** `Review Comment") {
		t.Errorf("review comments must be numbered first in:\n%s", got)
	}
}

func TestExportIncludesCommitRangeScopeForReviewComments(t *testing.T) {
	session := newTestSession()
	session.ReviewComments = append(session.ReviewComments, model.NewComment(
		"High-level concern across commits", model.CommentTypeFromID("issue"), nil))

	got := renderDefault(t, session, ScopeCommitRange,
		[]string{"abc1234567890"}, true, testLegend(), "")

	want := "`Review Comment (scope: selected commit range)` - High-level concern across commits"
	if !strings.Contains(got, want) {
		t.Errorf("missing commit-range review scope in:\n%s", got)
	}
}

func TestExportIncludesCommitShaForScopedComments(t *testing.T) {
	session := newTestSession()
	session.File("src/main.rs").AddLineComment(10, model.NewComment(
		"Wrong variable name", model.CommentTypeFromID("issue"), newSide(t)).
		WithCommitID("abc1234567890"))

	got := renderDefault(t, session, ScopeCommitRange,
		[]string{"abc1234567890"}, true, testLegend(), "")

	if !strings.Contains(got, "`src/main.rs:10` (commit abc1234) - Wrong variable name") {
		t.Errorf("commit-scoped comment must include the short SHA, got:\n%s", got)
	}
}

func TestExportOmitsCommitSuffixForUnscopedComments(t *testing.T) {
	got := renderDefault(t, newTestSession(), ScopeWorkingTree, nil, true, testLegend(), "")
	if strings.Contains(got, "(commit ") {
		t.Errorf("unscoped comments must not have a commit suffix, got:\n%s", got)
	}
}

func TestBuildTemplateDataFailsWhenNoComments(t *testing.T) {
	_, err := BuildTemplateData(newSession(), nil, "", ExportOptions{CommentTypes: testLegend()})
	if !errors.Is(err, errs.ErrNoComments) {
		t.Fatalf("want ErrNoComments, got %v", err)
	}
}

func TestExportIncludesCommitRangeScopeLine(t *testing.T) {
	got := renderDefault(t, newTestSession(), ScopeCommitRange,
		[]string{"abc1234567890", "def4567890123"}, true, testLegend(), "")
	if !strings.Contains(got, "Reviewing commits: abc1234, def4567") {
		t.Errorf("missing commit range line in:\n%s", got)
	}
}

func TestExportIncludesSingleCommitScopeLine(t *testing.T) {
	got := renderDefault(t, newTestSession(), ScopeCommitRange,
		[]string{"abc1234567890"}, true, testLegend(), "")
	if !strings.Contains(got, "Reviewing commit: abc1234") {
		t.Errorf("missing single commit line in:\n%s", got)
	}
}

func TestExportSingleLineRangeRendersAsSingleLine(t *testing.T) {
	session := newSession()
	session.AddFile("src/main.rs", model.StatusModified, 0)
	session.File("src/main.rs").AddLineComment(42, model.NewCommentWithRange(
		"Single line comment", model.CommentTypeFromID("note"), newSide(t),
		model.SingleLineRange(42)))

	got := renderDefault(t, session, ScopeWorkingTree, nil, true, testLegend(), "")

	if !strings.Contains(got, "`src/main.rs:42`") {
		t.Errorf("missing single-line anchor in:\n%s", got)
	}
	if strings.Contains(got, "`src/main.rs:42-42`") {
		t.Errorf("single-line range must not render as a span in:\n%s", got)
	}
}

func TestExportLineRangeRendersStartAndEnd(t *testing.T) {
	session := newSession()
	session.AddFile("src/main.rs", model.StatusModified, 0)
	session.File("src/main.rs").AddLineComment(15, model.NewCommentWithRange(
		"Multi-line comment", model.CommentTypeFromID("issue"), newSide(t),
		model.NewLineRange(10, 15)))

	got := renderDefault(t, session, ScopeWorkingTree, nil, true, testLegend(), "")

	if !strings.Contains(got, "`src/main.rs:10-15`") || !strings.Contains(got, "Multi-line comment") {
		t.Errorf("missing range anchor in:\n%s", got)
	}
}

func TestExportOldSideRangeGetsTildes(t *testing.T) {
	session := newSession()
	session.AddFile("src/main.rs", model.StatusModified, 0)
	session.File("src/main.rs").AddLineComment(25, model.NewCommentWithRange(
		"Deleted lines comment", model.CommentTypeFromID("suggestion"), oldSide(t),
		model.NewLineRange(20, 25)))

	got := renderDefault(t, session, ScopeWorkingTree, nil, true, testLegend(), "")

	if !strings.Contains(got, "`src/main.rs:~20-~25`") {
		t.Errorf("old-side range must have tildes in:\n%s", got)
	}
}

func TestExportSingleOldSideLineGetsTilde(t *testing.T) {
	session := newSession()
	session.AddFile("src/main.rs", model.StatusModified, 0)
	session.File("src/main.rs").AddLineComment(30, model.NewCommentWithRange(
		"Single deleted line", model.CommentTypeFromID("note"), oldSide(t),
		model.SingleLineRange(30)))

	got := renderDefault(t, session, ScopeWorkingTree, nil, true, testLegend(), "")

	if !strings.Contains(got, "`src/main.rs:~30`") {
		t.Errorf("missing tilde anchor in:\n%s", got)
	}
	if strings.Contains(got, "`src/main.rs:~30-~30`") {
		t.Errorf("single old-side line must not render as a span in:\n%s", got)
	}
}

func TestExportUsesKeyLineWhenNoLineRange(t *testing.T) {
	session := newSession()
	session.AddFile("src/main.rs", model.StatusModified, 0)
	session.File("src/main.rs").AddLineComment(50, model.NewComment(
		"Old style comment", model.CommentTypeFromID("note"), newSide(t)))

	got := renderDefault(t, session, ScopeWorkingTree, nil, true, testLegend(), "")

	if !strings.Contains(got, "`src/main.rs:50`") {
		t.Errorf("missing key-line anchor in:\n%s", got)
	}
}

func TestExportOmitsLegendWhenShowLegendFalse(t *testing.T) {
	got := renderDefault(t, newTestSession(), ScopeWorkingTree, nil, false, testLegend(), "")
	if strings.Contains(got, "Comment types:") {
		t.Errorf("legend must be omitted, got:\n%s", got)
	}
	if !strings.Contains(got, "[SUGGESTION]") || !strings.Contains(got, "[ISSUE]") {
		t.Errorf("type markers must stay in:\n%s", got)
	}
}

func TestExportLegendListsOnlyUsedTypes(t *testing.T) {
	session := newSession()
	session.AddFile("src/main.rs", model.StatusModified, 0)
	session.File("src/main.rs").AddFileComment(model.NewComment(
		"Great work!", model.CommentTypeFromID("praise"), nil))

	got := renderDefault(t, session, ScopeWorkingTree, nil, true, testLegend(), "")

	if !strings.Contains(got, "Comment types: PRAISE (positive feedback)") {
		t.Errorf("missing used-type legend in:\n%s", got)
	}
	for _, unused := range []string{"NOTE", "SUGGESTION", "ISSUE"} {
		if strings.Contains(got, unused) {
			t.Errorf("unused type %s leaked into:\n%s", unused, got)
		}
	}
}

func TestExportLegendListsOnlyUsedCustomTypes(t *testing.T) {
	session := newSession()
	session.AddFile("src/main.rs", model.StatusModified, 0)
	session.File("src/main.rs").AddFileComment(model.NewComment(
		"Needs clarification", model.CommentTypeFromID("note"), nil))
	custom := []LegendEntry{
		{ID: "note", Label: "question", Definition: "ask for clarification"},
		{ID: "issue", Label: "issue", Definition: "problems to fix"},
	}

	got := renderDefault(t, session, ScopeWorkingTree, nil, true, custom, "")

	if !strings.Contains(got, "Comment types: QUESTION (ask for clarification)") {
		t.Errorf("missing custom legend in:\n%s", got)
	}
	if strings.Contains(got, "ISSUE") {
		t.Errorf("unused custom type leaked into:\n%s", got)
	}
}

func TestExportGoldenUntypedComment(t *testing.T) {
	session := newSession()
	session.AddFile("src/main.rs", model.StatusModified, 0)
	session.File("src/main.rs").AddLineComment(42, model.NewComment(
		"plain observation", model.CommentTypeFromID("none"), newSide(t)))
	// Mirror an unconfigured review: the resolved set is just "none".
	noneOnly := []LegendEntry{{ID: model.CommentTypeNoneID, Label: model.CommentTypeNoneID}}

	got := renderDefault(t, session, ScopeWorkingTree, nil, true, noneOnly, "")

	want := "I reviewed your code and have the following comments. Please address them.\n" +
		"\n" +
		"## Local mrman Comments\n" +
		"\n" +
		"1. `src/main.rs:42` - plain observation\n"
	if got != want {
		t.Errorf("untyped golden mismatch:\ngot:\n%q\nwant:\n%q", got, want)
	}
}

func TestScopeLinesAndLabels(t *testing.T) {
	commits := []string{"abc1234567890", "def4567890123"}
	cases := []struct {
		kind    ScopeKind
		commits []string
		line    string
		label   string
	}{
		{ScopeWorkingTree, nil, "", "working tree changes"},
		{ScopeStaged, nil, "Reviewing staged changes", "staged changes"},
		{ScopeUnstaged, nil, "Reviewing unstaged changes", "unstaged changes"},
		{ScopeStagedAndUnstaged, nil, "Reviewing staged + unstaged changes", "staged + unstaged changes"},
		{ScopeCommitRange, commits[:1], "Reviewing commit: abc1234", "selected commit range"},
		{ScopeCommitRange, commits, "Reviewing commits: abc1234, def4567", "selected commit range"},
		{ScopeStagedUnstagedAndCommits, commits,
			"Reviewing staged + unstaged + commits: abc1234, def4567",
			"selected commit range + staged/unstaged changes"},
		{ScopePullRequest, nil, "", "merge request"},
		{ScopeKind(99), nil, "", ""},
	}
	for _, tc := range cases {
		if got := tc.kind.ScopeLine(tc.commits); got != tc.line {
			t.Errorf("ScopeKind(%d).ScopeLine = %q, want %q", tc.kind, got, tc.line)
		}
		if got := tc.kind.Label(); got != tc.label {
			t.Errorf("ScopeKind(%d).Label = %q, want %q", tc.kind, got, tc.label)
		}
	}
}

func TestBuildTemplateDataFieldsAndCounts(t *testing.T) {
	session := newTestSession()
	session.AddFile("uncommented.go", model.StatusAdded, 0)
	notes := "summary text"
	session.SessionNotes = &notes
	session.ReviewComments = append(session.ReviewComments, model.NewComment(
		"overall", model.CommentTypeFromID("note"), nil))

	data, err := BuildTemplateData(session, nil, ScopeStaged.ScopeLine(nil), ExportOptions{
		SessionSlug:     "team/repo@main",
		DiffSourceLabel: ScopeStaged.Label(),
		ShowLegend:      true,
		CommentTypes:    testLegend(),
	})
	if err != nil {
		t.Fatalf("BuildTemplateData: %v", err)
	}

	if data.Slug != "team/repo@main" || data.Repo != "/tmp/test-repo" || data.Branch != "main" {
		t.Errorf("identity fields wrong: %+v", data)
	}
	if data.ScopeLine != "Reviewing staged changes" || data.DiffSourceLabel != "staged changes" {
		t.Errorf("scope fields wrong: %+v", data)
	}
	if data.SessionNotes != "summary text" {
		t.Errorf("SessionNotes = %q", data.SessionNotes)
	}
	if data.Counts.Files != 2 || data.Counts.Reviewed != 1 || data.Counts.Comments != 3 {
		t.Errorf("Counts = %+v", data.Counts)
	}
	// Only the commented file appears, and comment-less files are skipped.
	if len(data.Files) != 1 || data.Files[0].Path != "src/main.rs" || data.Files[0].Status != "modified" {
		t.Errorf("Files = %+v", data.Files)
	}
	if len(data.ReviewComments) != 1 || data.ReviewComments[0].Number != 1 {
		t.Errorf("ReviewComments = %+v", data.ReviewComments)
	}
	if got := data.Files[0].Comments; len(got) != 2 || got[0].Number != 2 || got[1].Number != 3 {
		t.Errorf("file comment numbering = %+v", got)
	}
	if data.Files[0].Comments[0].Author != model.DefaultAuthor {
		t.Errorf("Author = %q", data.Files[0].Comments[0].Author)
	}
	// No username configured here, so the default author reads as someone else.
	if !data.Files[0].Comments[0].ShowAuthor || !data.ReviewComments[0].ShowAuthor {
		t.Error("ShowAuthor must be resolved for file and review comments alike")
	}
	if len(data.CommentTypes) != 3 { // note, suggestion, issue used
		t.Errorf("CommentTypes = %+v", data.CommentTypes)
	}
}

func TestBuildTemplateDataLegendDefinitionFallsBackToID(t *testing.T) {
	session := newSession()
	session.AddFile("a.go", model.StatusModified, 0)
	session.File("a.go").AddFileComment(model.NewComment(
		"x", model.CommentTypeFromID("nit"), nil))

	data, err := BuildTemplateData(session, nil, "", ExportOptions{
		CommentTypes: []LegendEntry{{ID: "nit", Label: "nit"}},
	})
	if err != nil {
		t.Fatalf("BuildTemplateData: %v", err)
	}
	if len(data.CommentTypes) != 1 || data.CommentTypes[0].Definition != "nit" {
		t.Errorf("legend definition fallback wrong: %+v", data.CommentTypes)
	}
}

func TestExportFilesSortedByPath(t *testing.T) {
	session := newSession()
	for _, path := range []string{"zzz.go", "aaa.go", "mmm.go"} {
		session.AddFile(path, model.StatusModified, 0)
		session.File(path).AddFileComment(model.NewComment(
			"comment on "+path, model.CommentTypeFromID("note"), nil))
	}

	got := renderDefault(t, session, ScopeWorkingTree, nil, false, testLegend(), "")

	want := "1. **[NOTE]** `aaa.go` - comment on aaa.go\n" +
		"2. **[NOTE]** `mmm.go` - comment on mmm.go\n" +
		"3. **[NOTE]** `zzz.go` - comment on zzz.go\n"
	if !strings.Contains(got, want) {
		t.Errorf("files must be sorted by path, got:\n%s", got)
	}
}

func TestExportLineCommentsSortedByLineKey(t *testing.T) {
	session := newSession()
	session.AddFile("a.go", model.StatusModified, 0)
	review := session.File("a.go")
	review.AddLineComment(90, model.NewComment("late", model.CommentTypeFromID("note"), newSide(t)))
	review.AddLineComment(5, model.NewComment("early", model.CommentTypeFromID("note"), newSide(t)))
	review.AddFileComment(model.NewComment("file-wide", model.CommentTypeFromID("note"), nil))

	got := renderDefault(t, session, ScopeWorkingTree, nil, false, testLegend(), "")

	want := "1. **[NOTE]** `a.go` - file-wide\n" +
		"2. **[NOTE]** `a.go:5` - early\n" +
		"3. **[NOTE]** `a.go:90` - late\n"
	if !strings.Contains(got, want) {
		t.Errorf("file comments first, line comments by key, got:\n%s", got)
	}
}

// authoredSession builds a session whose comments were written by two
// different people: one file comment by claude, one line comment by ryan.
func authoredSession() *model.ReviewSession {
	session := newSession()
	session.AddFile("src/main.rs", model.StatusModified, 0)
	review := session.File("src/main.rs")
	review.AddFileComment(model.NewComment(
		"Consider adding documentation", model.CommentTypeFromID("suggestion"), nil).WithAuthor("claude"))
	side := model.LineSideNew
	review.AddLineComment(42, model.NewComment(
		"Magic number should be a constant", model.CommentTypeFromID("issue"), &side).WithAuthor("ryan"))
	return session
}

// renderAs exports authoredSession as the given reader.
func renderAs(t *testing.T, vis AuthorVisibility) string {
	t.Helper()
	tmpl, _ := LoadNotesTemplate("")
	data, err := BuildTemplateData(authoredSession(), nil, "", ExportOptions{
		CommentTypes: testLegend(),
		Author:       vis,
	})
	if err != nil {
		t.Fatalf("BuildTemplateData: %v", err)
	}
	out, err := RenderNotes(tmpl, data)
	if err != nil {
		t.Fatalf("RenderNotes: %v", err)
	}
	return out
}

// TestExportBadgesOtherAuthors is the point of the whole feature: an export
// handed to someone else has to say who wrote what. Before this, every
// attribution was dropped on the way out and a review by three people read as
// one anonymous list.
func TestExportBadgesOtherAuthors(t *testing.T) {
	got := renderAs(t, AuthorVisibility{Username: "ryan"})
	if !strings.Contains(got, "**[SUGGESTION @claude]**") {
		t.Errorf("someone else's comment must be badged:\n%s", got)
	}
	if strings.Contains(got, "@ryan") {
		t.Errorf("your own comment must not be badged by default:\n%s", got)
	}
}

// TestExportShowOwnAuthorBadgesEveryone covers the opt-in.
func TestExportShowOwnAuthorBadgesEveryone(t *testing.T) {
	got := renderAs(t, AuthorVisibility{Username: "ryan", ShowOwnAuthor: true})
	if !strings.Contains(got, "**[SUGGESTION @claude]**") || !strings.Contains(got, "**[ISSUE @ryan]**") {
		t.Errorf("show_own_author must attribute every comment:\n%s", got)
	}
}

// TestExportUnconfiguredUsernameBadgesEverything is the fresh-install state:
// no username set, so the default author reads as someone else. This matches
// what the TUI already draws, which is the whole reason the exports were wrong.
func TestExportUnconfiguredUsernameBadgesEverything(t *testing.T) {
	got := renderAs(t, AuthorVisibility{})
	if !strings.Contains(got, "**[SUGGESTION @claude]**") || !strings.Contains(got, "**[ISSUE @ryan]**") {
		t.Errorf("with no username configured every author is badged:\n%s", got)
	}
}

// TestExportBadgesTypelessComment: with no type there is no bracket today, so
// the badge has to bring its own.
func TestExportBadgesTypelessComment(t *testing.T) {
	session := newSession()
	session.AddFile("src/main.rs", model.StatusModified, 0)
	session.File("src/main.rs").AddFileComment(
		model.NewComment("plain remark", model.CommentTypeFromID(""), nil).WithAuthor("claude"))

	tmpl, _ := LoadNotesTemplate("")
	data, err := BuildTemplateData(session, nil, "", ExportOptions{Author: AuthorVisibility{Username: "ryan"}})
	if err != nil {
		t.Fatalf("BuildTemplateData: %v", err)
	}
	got, err := RenderNotes(tmpl, data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "**[@claude]** `src/main.rs` - plain remark") {
		t.Errorf("typeless authored comment must render a bare author bracket:\n%s", got)
	}
}

// TestExportLeavesUnauthoredCommentsAlone pins backward compatibility: a
// session written before mrman stamped authors exports exactly as it did.
func TestExportLeavesUnauthoredCommentsAlone(t *testing.T) {
	session := newSession()
	session.AddFile("src/main.rs", model.StatusModified, 0)
	session.File("src/main.rs").AddFileComment(
		model.NewComment("old note", model.CommentTypeFromID("issue"), nil).WithAuthor(""))

	tmpl, _ := LoadNotesTemplate("")
	data, err := BuildTemplateData(session, nil, "", ExportOptions{
		CommentTypes: testLegend(),
		Author:       AuthorVisibility{Username: "ryan"},
	})
	if err != nil {
		t.Fatalf("BuildTemplateData: %v", err)
	}
	got, err := RenderNotes(tmpl, data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "1. **[ISSUE]** `src/main.rs` - old note") {
		t.Errorf("an unauthored comment must render unchanged:\n%s", got)
	}
}
