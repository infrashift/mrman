package ui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs"
)

// addReviewComment gives the model a comment so the navigator appears.
func addReviewComment(m *Model, body string) {
	m.App.Session.ReviewComments = append(m.App.Session.ReviewComments,
		model.NewComment(body, model.CommentTypeFromID("note"), nil))
	m.App.RebuildAnnotations()
}

// addCommits gives the model a multi-commit inline selector.
func addCommits(m *Model, n int) {
	rows := make([]vcs.CommitInfo, n)
	for i := range rows {
		id := string(rune('a' + i))
		rows[i] = vcs.CommitInfo{ID: id, ShortID: "sha" + id, Summary: "commit " + id}
	}
	m.App.ReviewCommits = rows
	m.App.CommitList = append([]vcs.CommitInfo(nil), rows...)
	m.App.VisibleCommitCount = n
	m.App.ShowCommitSelector = true
	m.App.DiffSource = app.DiffSource{Kind: app.DiffSourceCommitRange}
}

func TestCommentNavigatorRendersInTheLeftColumn(t *testing.T) {
	m := testModel(t)
	m.height = 40
	m.syncViewport()
	addReviewComment(m, "needs a test")

	out := plainView(m)
	if !strings.Contains(out, "Comments · 1") {
		t.Errorf("the navigator pane must appear once a comment exists:\n%s", out)
	}
	if !strings.Contains(out, "review") {
		t.Error("a review-scope comment must be labelled")
	}
	// The tree keeps its own pane above it.
	if !strings.Contains(out, "Files ·") {
		t.Error("the file tree must survive the split")
	}
}

func TestCommentNavigatorHiddenWithoutComments(t *testing.T) {
	m := testModel(t)
	if strings.Contains(plainView(m), "Comments ·") {
		t.Error("an empty navigator must not take screen space")
	}
}

func TestCommentNavigatorDroppedWhenTheColumnIsTooShort(t *testing.T) {
	m := testModel(t)
	addReviewComment(m, "needs a test")
	m.height = app.CommentNavMinTotalHeight + 2 // barely any room
	m.syncViewport()

	if strings.Contains(plainView(m), "Comments ·") {
		t.Error("a cramped column must keep the tree rather than split it")
	}
}

func TestCommentNavigatorLabelsLineComments(t *testing.T) {
	m := testModel(t)
	m.height = 40
	m.syncViewport()

	// Anchor a comment to src/x.go line 2.
	m.App.DiffState.CursorLine = 0
	side := model.LineSideNew
	review := m.App.Session.File("src/x.go")
	if review == nil {
		t.Fatal("expected the test file in the session")
	}
	c := model.NewComment("why 10?", model.CommentTypeFromID("note"), &side)
	review.LineComments = map[uint32][]*model.Comment{2: {c}}
	m.App.RebuildAnnotations()

	if out := plainView(m); !strings.Contains(out, "x.go:2") {
		t.Errorf("a line comment must be labelled file:line, got:\n%s", out)
	}
}

func TestCommentNavigatorFocusKeysJump(t *testing.T) {
	m := testModel(t)
	m.height = 40
	m.syncViewport()
	addReviewComment(m, "first")
	addReviewComment(m, "second")

	m.App.FocusPanel(app.PanelComments)
	pressRune(m, 'j')
	if m.App.CommentNav.Cursor != 1 {
		t.Fatalf("j must move the navigator cursor, got %d", m.App.CommentNav.Cursor)
	}
	press(m, "", tea.KeyEnter, 0)
	if m.App.FocusedPanel != app.PanelDiff {
		t.Error("Enter must jump to the comment and focus the diff")
	}
}

func TestCommitStripRendersAboveTheColumns(t *testing.T) {
	m := testModel(t)
	m.height = 40
	addCommits(m, 3)
	m.syncViewport()

	out := plainView(m)
	if !strings.Contains(out, "Commits ·") {
		t.Fatalf("a multi-commit review must show the strip:\n%s", out)
	}
	for _, want := range []string{"shaa", "commit a", "shab"} {
		if !strings.Contains(out, want) {
			t.Errorf("strip missing %q", want)
		}
	}
	// The strip sits above both columns, so it precedes the file pane.
	stripIdx := strings.Index(out, "Commits ·")
	filesIdx := strings.Index(out, "Files ·")
	if stripIdx < 0 || filesIdx < 0 || stripIdx > filesIdx {
		t.Error("the commit strip must render above the columns")
	}
}

func TestCommitStripHiddenOnSingleCommitReviews(t *testing.T) {
	m := testModel(t)
	addCommits(m, 1)
	m.syncViewport()
	if strings.Contains(plainView(m), "Commits ·") {
		t.Error("one commit is not worth a selector")
	}
}

func TestCommitStripMarksCommitsCoveredByYourLastReview(t *testing.T) {
	m := testModel(t)
	m.height = 40
	addCommits(m, 3)
	m.App.Pr = &app.PrState{
		Details:         &forge.PullRequestDetails{HeadSHA: "h", BaseSHA: "b"},
		ReviewedCommits: map[string]bool{"a": true},
	}
	m.syncViewport()

	if !strings.Contains(plainView(m), "✓") {
		t.Error("a commit already covered by your review must be marked")
	}
}

func TestLeaderChordsMoveFocusAndToggleTheStrip(t *testing.T) {
	m := testModel(t)
	m.height = 40
	addReviewComment(m, "note")
	addCommits(m, 3)
	m.syncViewport()

	// leader-j walks down the layout, leader-k back up.
	m.App.FocusPanel(app.PanelCommitSelector)
	pressRune(m, ';')
	pressRune(m, 'j')
	if m.App.FocusedPanel != app.PanelFileList {
		t.Fatalf("leader-j from the strip must reach the tree, got %v", m.App.FocusedPanel)
	}
	pressRune(m, ';')
	pressRune(m, 'k')
	if m.App.FocusedPanel != app.PanelCommitSelector {
		t.Errorf("leader-k must walk back up, got %v", m.App.FocusedPanel)
	}

	// leader-s hides the strip and moves focus off it.
	pressRune(m, ';')
	pressRune(m, 's')
	if m.App.ShowCommitSelector {
		t.Error("leader-s must toggle the strip off")
	}
	if m.App.FocusedPanel == app.PanelCommitSelector {
		t.Error("hiding a focused pane must move focus away")
	}
	if strings.Contains(plainView(m), "Commits ·") {
		t.Error("the hidden strip must not render")
	}
}

func TestTabCyclesEveryVisiblePane(t *testing.T) {
	m := testModel(t)
	m.height = 40
	addReviewComment(m, "note")
	addCommits(m, 3)
	m.syncViewport()
	m.App.FocusPanel(app.PanelDiff)

	seen := map[app.FocusedPanel]bool{}
	for i := 0; i < 4; i++ {
		press(m, "", tea.KeyTab, 0)
		seen[m.App.FocusedPanel] = true
	}
	for _, want := range []app.FocusedPanel{
		app.PanelDiff, app.PanelFileList, app.PanelComments, app.PanelCommitSelector,
	} {
		if !seen[want] {
			t.Errorf("Tab never reached %v", want)
		}
	}
}

func TestSetCommitsCommands(t *testing.T) {
	m := testModel(t)
	m.height = 40
	addCommits(m, 3)
	m.syncViewport()

	m.runCommand(input.ParseCommand("set nocommits"))
	if m.App.ShowCommitSelector {
		t.Error(":set nocommits must hide the strip")
	}
	m.runCommand(input.ParseCommand("set commits"))
	if !m.App.ShowCommitSelector {
		t.Error(":set commits must show the strip")
	}
	// Setting it to what it already is must not flip it.
	m.runCommand(input.ParseCommand("set commits"))
	if !m.App.ShowCommitSelector {
		t.Error(":set commits must be idempotent")
	}
	m.runCommand(input.ParseCommand("set commits!"))
	if m.App.ShowCommitSelector {
		t.Error(":set commits! must toggle")
	}
}

// rangeForge serves per-commit range diffs.
type rangeForge struct {
	*uiFakeForge
	patch     string
	err       error
	canRange  bool
	rangeSeen [][2]string
}

func (f *rangeForge) Capabilities() forge.Capabilities {
	caps := f.uiFakeForge.Capabilities()
	caps.CommitRangeDiff = f.canRange
	return caps
}

func (f *rangeForge) GetCommitRangeDiff(
	_ context.Context, _ *forge.PullRequestDetails, startSHA, endSHA string,
) (string, error) {
	f.rangeSeen = append(f.rangeSeen, [2]string{startSHA, endSHA})
	return f.patch, f.err
}

// prCommitModel puts the model in PR mode with a three-commit selector.
func prCommitModel(t *testing.T, f *rangeForge) *Model {
	t.Helper()
	m := testModel(t)
	m.height = 40
	m.App.Pr = &app.PrState{
		Backend: f,
		Details: &forge.PullRequestDetails{
			PullRequestSummary: testPrSummary(7, "the pr", "ana", "feat"),
			HeadSHA:            "c3", BaseSHA: "base",
		},
		Repository: ptrRepo(),
	}
	m.App.DiffSource = app.DiffSource{Kind: app.DiffSourcePullRequest}
	m.App.ShowCommitSelector = true
	m.App.SetupPrCommitSelector([]forge.Commit{
		{OID: "c1", ShortOID: "c1", Summary: "one"},
		{OID: "c2", ShortOID: "c2", Summary: "two"},
		{OID: "c3", ShortOID: "c3", Summary: "three"},
	})
	m.syncViewport()
	return m
}

func TestCycleCommitNarrowsAPullRequestToOneCommit(t *testing.T) {
	f := &rangeForge{
		uiFakeForge: &uiFakeForge{},
		canRange:    true,
		patch: "diff --git a/n.go b/n.go\n--- a/n.go\n+++ b/n.go\n" +
			"@@ -1,1 +1,2 @@\n package n\n+var Narrowed = 1\n",
	}
	m := prCommitModel(t, f)

	// ")" cycles to a single commit and reloads its range diff.
	pressPrRune(t, m, ')')

	if len(f.rangeSeen) == 0 {
		t.Fatal(") must ask the forge for the commit's range diff")
	}
	if !strings.Contains(plainView(m), "var Narrowed = 1") {
		t.Error("the narrowed diff must render")
	}
	if m.App.Pr.Reloading {
		t.Error("a finished range load must clear the spinner")
	}
}

func TestCycleCommitUsesTheParentAsTheRangeStart(t *testing.T) {
	f := &rangeForge{
		uiFakeForge: &uiFakeForge{},
		canRange:    true,
		patch:       "diff --git a/n.go b/n.go\n--- a/n.go\n+++ b/n.go\n@@ -1,1 +1,2 @@\n package n\n+x\n",
	}
	m := prCommitModel(t, f)
	// Rows newest first: [c3, c2, c1]. Select the oldest commit only.
	m.App.CommitSelectionRange = &model.IndexRange{2, 2}
	runCmd(t, m, m.reloadInlineSelection())

	if len(f.rangeSeen) != 1 {
		t.Fatalf("range calls = %d, want 1", len(f.rangeSeen))
	}
	// The oldest commit's parent is the PR base, not another commit.
	if f.rangeSeen[0] != [2]string{"base", "c1"} {
		t.Errorf("range = %v, want (base, c1)", f.rangeSeen[0])
	}
}

func TestSelectingEveryCommitSkipsTheRangeCall(t *testing.T) {
	f := &rangeForge{uiFakeForge: &uiFakeForge{}, canRange: true}
	m := prCommitModel(t, f)
	m.App.CommitSelectionRange = nil // the whole pull request

	_ = m.reloadInlineSelection()
	if len(f.rangeSeen) != 0 {
		t.Error("the cumulative diff is already loaded; asking again is a wasted round trip")
	}
}

func TestForgeWithoutRangeDiffFallsBackToTheWholePullRequest(t *testing.T) {
	f := &rangeForge{uiFakeForge: &uiFakeForge{}, canRange: false}
	m := prCommitModel(t, f)
	m.App.CommitSelectionRange = &model.IndexRange{0, 0}

	if cmd := m.reloadInlineSelection(); cmd != nil {
		t.Error("a forge without range diffs must not issue the call")
	}
	if len(f.rangeSeen) != 0 {
		t.Error("no range call should reach a forge that cannot serve it")
	}
	if m.App.CommitSelectionRange != nil {
		t.Error("the selection must widen back to the whole pull request")
	}
	if m.App.Message == nil || !strings.Contains(m.App.Message.Content, "cannot diff a commit range") {
		t.Errorf("the limitation must be explained, got %+v", m.App.Message)
	}
}

func TestStaleRangeDiffResultIsDiscarded(t *testing.T) {
	f := &rangeForge{
		uiFakeForge: &uiFakeForge{},
		canRange:    true,
		patch:       "diff --git a/n.go b/n.go\n--- a/n.go\n+++ b/n.go\n@@ -1,1 +1,2 @@\n package n\n+stale\n",
	}
	m := prCommitModel(t, f)
	m.App.CommitSelectionRange = &model.IndexRange{0, 0}

	cmd := m.reloadInlineSelection()
	msg := cmd()
	// A newer selection supersedes the one in flight.
	m.App.Pr.Gens.PrReload++
	m.Update(msg)

	if strings.Contains(plainView(m), "stale") {
		t.Error("a superseded range diff must not replace the current one")
	}
}
