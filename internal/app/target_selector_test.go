package app

// target_selector_test.go ports the local-tab parts of tuicr's
// src/app/tests/target_selector_tests.rs (the PR tab lands in M6) plus
// tests pinning EnterTargetSelector/ExitCommitSelectMode,
// ConfirmCommitSelection, and ApplyLoadedSelection.

import (
	"slices"
	"testing"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/input"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/syntax"
	"github.com/infrashift/mrman/internal/vcs"
)

// selectorVcs is the target-selector test backend: configurable recent
// commits, change status (nil means ErrUnsupported, exercising the
// verify-by-diffing fallback), and per-side diffs.
type selectorVcs struct {
	vcs.UnsupportedBase
	info    *vcs.Info
	commits []vcs.CommitInfo
	// status nil -> ChangeStatus returns ErrUnsupported.
	status *vcs.ChangeStatus
	// nil file slices -> the side diff returns ErrNoChanges.
	stagedFiles   []model.DiffFile
	unstagedFiles []model.DiffFile
	workingFiles  []model.DiffFile
	// unstagedUnsupported forces the UnstagedDiff -> WorkingTreeDiff
	// fallback.
	unstagedUnsupported bool
}

func (s *selectorVcs) Info() *vcs.Info { return s.info }

func (s *selectorVcs) RecentCommits(offset, limit int) ([]vcs.CommitInfo, error) {
	if offset >= len(s.commits) {
		return nil, nil
	}
	return slices.Clone(s.commits[offset:min(offset+limit, len(s.commits))]), nil
}

func (s *selectorVcs) ChangeStatus() (vcs.ChangeStatus, error) {
	if s.status == nil {
		return vcs.ChangeStatus{}, errs.Unsupportedf("change status")
	}
	return *s.status, nil
}

func (s *selectorVcs) StagedDiff(*syntax.Highlighter) ([]model.DiffFile, error) {
	if s.stagedFiles == nil {
		return nil, errs.ErrNoChanges
	}
	return s.stagedFiles, nil
}

func (s *selectorVcs) UnstagedDiff(*syntax.Highlighter) ([]model.DiffFile, error) {
	if s.unstagedUnsupported {
		return nil, errs.Unsupportedf("unstaged diff")
	}
	if s.unstagedFiles == nil {
		return nil, errs.ErrNoChanges
	}
	return s.unstagedFiles, nil
}

func (s *selectorVcs) WorkingTreeDiff(*syntax.Highlighter) ([]model.DiffFile, error) {
	if s.workingFiles == nil {
		return nil, errs.ErrNoChanges
	}
	return s.workingFiles, nil
}

func (s *selectorVcs) FetchContextLines(string, model.FileStatus, *string, uint32, uint32) ([]model.DiffLine, error) {
	return nil, nil
}

func (s *selectorVcs) FileLineCount(string, model.FileStatus, *string) (uint32, error) {
	return 0, nil
}

func buildTargetSelectorApp(backend *selectorVcs) *App {
	info := &vcs.Info{
		RootPath:   "/tmp",
		HeadCommit: "head",
		BranchName: strPtr("main"),
		Type:       vcs.TypeGit,
	}
	backend.info = info
	session := model.NewReviewSession(info.RootPath, info.HeadCommit, info.BranchName, model.SourceWorkingTree)
	return NewApp(backend, info, nil, session, DiffSource{Kind: DiffSourceWorkingTree})
}

func dummyCommits(ids ...string) []vcs.CommitInfo {
	commits := make([]vcs.CommitInfo, 0, len(ids))
	for _, id := range ids {
		commits = append(commits, commit(id))
	}
	return commits
}

func noChanges() *vcs.ChangeStatus { return &vcs.ChangeStatus{} }

func TestDefaultsToLocalTabAfterBuild(t *testing.T) {
	a := buildAppWithFiles(nil, 0)
	assertEq(t, a.TargetTab, TargetTabLocal, "default tab")
}

func TestCycleTargetTabTogglesBetweenLocalAndPullRequests(t *testing.T) {
	a := buildAppWithFiles(nil, 0)
	a.CycleTargetTab(true)
	assertEq(t, a.TargetTab, TargetTabPullRequests, "forward to PR tab")
	a.CycleTargetTab(false)
	assertEq(t, a.TargetTab, TargetTabLocal, "back to local tab")
}

func TestEnterTargetSelectorLoadsCommitsAndSpecialRows(t *testing.T) {
	a := buildTargetSelectorApp(&selectorVcs{
		commits: dummyCommits("c1", "c2", "c3"),
		status:  &vcs.ChangeStatus{Staged: true, Unstaged: true},
	})

	if err := a.EnterTargetSelector(TargetTabLocal); err != nil {
		t.Fatal(err)
	}

	assertEq(t, a.InputMode, input.ModeCommitSelect, "commit-select mode entered")
	assertEq(t, a.TargetTab, TargetTabLocal, "local tab active")
	assertEq(t, len(a.CommitList), 5, "3 commits + 2 special rows")
	// Staged is inserted first, then unstaged prepends before it.
	assertEq(t, a.CommitList[0].ID, UnstagedSelectionID, "unstaged row first")
	assertEq(t, a.CommitList[1].ID, StagedSelectionID, "staged row second")
	assertEq(t, a.CommitList[2].ID, "c1", "newest commit after special rows")
	assertEq(t, a.CommitListCursor, 0, "cursor reset")
	assertEq(t, a.CommitListScrollOffset, 0, "scroll reset")
	assertRange(t, a.CommitSelectionRange, nil, "no selection on open")
	assertEq(t, a.VisibleCommitCount, 5, "all rows visible")
	assertEq(t, a.HasMoreCommits, false, "short history has no more commits")
}

func TestEnterTargetSelectorFullPageArmsLoadMore(t *testing.T) {
	ids := make([]string, 15)
	for i := range ids {
		ids[i] = string(rune('a' + i))
	}
	a := buildTargetSelectorApp(&selectorVcs{commits: dummyCommits(ids...), status: noChanges()})

	if err := a.EnterTargetSelector(TargetTabLocal); err != nil {
		t.Fatal(err)
	}
	assertEq(t, len(a.CommitList), 10, "initial page of 10")
	assertEq(t, a.HasMoreCommits, true, "full page arms load-more")

	// The expand row fetches the next page; the short page exhausts it.
	if err := a.ExpandCommit(); err != nil {
		t.Fatal(err)
	}
	assertEq(t, len(a.CommitList), 15, "second page appended")
	assertEq(t, a.VisibleCommitCount, 15, "all loaded commits visible")
	assertEq(t, a.HasMoreCommits, false, "short page clears load-more")
}

func TestExpandCommitEmptyFetchClearsLoadMore(t *testing.T) {
	ids := make([]string, 10)
	for i := range ids {
		ids[i] = string(rune('a' + i))
	}
	a := buildTargetSelectorApp(&selectorVcs{commits: dummyCommits(ids...), status: noChanges()})
	if err := a.EnterTargetSelector(TargetTabLocal); err != nil {
		t.Fatal(err)
	}
	assertEq(t, a.HasMoreCommits, true, "exactly one page arms load-more")

	if err := a.ExpandCommit(); err != nil {
		t.Fatal(err)
	}
	assertEq(t, a.HasMoreCommits, false, "empty fetch clears load-more")
	assertEq(t, len(a.CommitList), 10, "list unchanged")
	if a.Message == nil || a.Message.Content != "No more commits" {
		t.Fatalf("expected 'No more commits' message, got %v", a.Message)
	}
}

func TestEnterTargetSelectorSkipsSpecialRowsOffsetWhenPaging(t *testing.T) {
	ids := make([]string, 10)
	for i := range ids {
		ids[i] = string(rune('a' + i))
	}
	a := buildTargetSelectorApp(&selectorVcs{
		commits: dummyCommits(ids...),
		status:  &vcs.ChangeStatus{Staged: true, Unstaged: true},
	})
	if err := a.EnterTargetSelector(TargetTabLocal); err != nil {
		t.Fatal(err)
	}
	assertEq(t, len(a.CommitList), 12, "10 commits + 2 special rows")
	// The next-page offset must count history commits only, not the
	// synthetic rows — otherwise page two would skip two commits.
	assertEq(t, a.loadedHistoryCommitCount(), 10, "offset excludes special rows")
}

func TestEnterTargetSelectorWithoutTargetsWarnsOnLocalTab(t *testing.T) {
	a := buildTargetSelectorApp(&selectorVcs{status: noChanges()})

	if err := a.EnterTargetSelector(TargetTabLocal); err != nil {
		t.Fatal(err)
	}
	assertEq(t, a.InputMode, input.ModeNormal, "selector does not open")
	if a.Message == nil || a.Message.Content != "No commits or staged/unstaged changes found" {
		t.Fatalf("expected no-targets message, got %v", a.Message)
	}
}

func TestEnterTargetSelectorPRTabOpensWithoutLocalTargets(t *testing.T) {
	// Opening on the Pull Requests tab is allowed even with no local
	// commits or changes — the PR tab is the user's reason for being here.
	a := buildTargetSelectorApp(&selectorVcs{status: noChanges()})

	if err := a.EnterTargetSelector(TargetTabPullRequests); err != nil {
		t.Fatal(err)
	}
	assertEq(t, a.InputMode, input.ModeCommitSelect, "selector opens")
	assertEq(t, a.TargetTab, TargetTabPullRequests, "PR tab active")
}

func TestEnterTargetSelectorSavesInlineSelection(t *testing.T) {
	a := buildTargetSelectorApp(&selectorVcs{
		commits: dummyCommits("c1", "c2"),
		status:  noChanges(),
	})
	a.ReviewCommits = dummyCommits("r1", "r2")
	a.CommitSelectionRange = rangeOf(1, 1)

	if err := a.EnterTargetSelector(TargetTabLocal); err != nil {
		t.Fatal(err)
	}
	assertRange(t, a.SavedInlineSelection, rangeOf(1, 1), "inline selection saved")
	assertRange(t, a.CommitSelectionRange, nil, "live selection cleared for the selector")
}

func TestChangeStatusFallbackVerifiesByDiffing(t *testing.T) {
	// ChangeStatus is unsupported: assume both sides, verify by diffing.
	// Staged has a file; unstaged has none -> only the STAGED row appears.
	a := buildTargetSelectorApp(&selectorVcs{
		commits:     dummyCommits("c1"),
		stagedFiles: []model.DiffFile{makeFileWithHunks("s.go", []model.DiffHunk{makeHunk(1, 1)})},
	})

	if err := a.EnterTargetSelector(TargetTabLocal); err != nil {
		t.Fatal(err)
	}
	assertEq(t, len(a.CommitList), 2, "staged row + commit")
	assertEq(t, a.CommitList[0].ID, StagedSelectionID, "staged row present")
	assertEq(t, a.CommitList[1].ID, "c1", "commit after staged row")
}

func TestChangeStatusFallbackUnstagedFallsBackToWorkingTree(t *testing.T) {
	// Backends without a separate unstaged diff (jj-style) probe the whole
	// working tree instead.
	a := buildTargetSelectorApp(&selectorVcs{
		commits:             dummyCommits("c1"),
		unstagedUnsupported: true,
		workingFiles:        []model.DiffFile{makeFileWithHunks("w.go", []model.DiffHunk{makeHunk(1, 1)})},
	})

	if err := a.EnterTargetSelector(TargetTabLocal); err != nil {
		t.Fatal(err)
	}
	assertEq(t, a.CommitList[0].ID, UnstagedSelectionID, "unstaged row from working-tree probe")
}

// --- ExitCommitSelectMode ---

func TestExitCommitSelectModeRestoresInlineSelection(t *testing.T) {
	a := buildCommitListApp(stagedCommitEntry(), commit("h1"), commit("h2"))
	a.ReviewCommits = dummyCommits("r1", "r2")
	a.SavedInlineSelection = rangeOf(1, 1)
	a.CommitListCursor = 2
	a.CommitListScrollOffset = 1
	a.HasMoreCommits = true

	action := a.ExitCommitSelectMode()

	assertEq(t, action, ExitSelectorReloadInline, "restored selection needs a reload")
	assertEq(t, a.InputMode, input.ModeNormal, "back to normal mode")
	assertEq(t, len(a.CommitList), 2, "commit list restored from review commits")
	assertEq(t, a.CommitList[0].ID, "r1", "restored newest-first list")
	assertRange(t, a.CommitSelectionRange, rangeOf(1, 1), "inline selection restored")
	assertEq(t, a.CommitListCursor, 0, "cursor reset")
	assertEq(t, a.CommitListScrollOffset, 0, "scroll reset")
	assertEq(t, a.VisibleCommitCount, 2, "visible count matches review commits")
	assertEq(t, a.HasMoreCommits, false, "inline selector never pages")
	assertRange(t, a.SavedInlineSelection, nil, "saved selection consumed")
}

func TestExitCommitSelectModeWithoutSavedSelectionNeedsNoReload(t *testing.T) {
	a := buildCommitListApp(commit("h1"))
	a.ReviewCommits = dummyCommits("r1", "r2")

	action := a.ExitCommitSelectMode()

	assertEq(t, action, ExitSelectorNone, "nothing to reload")
	assertRange(t, a.CommitSelectionRange, nil, "no selection restored")
}

func TestExitCommitSelectModeFallsBackToWorkingTreeForCommitSources(t *testing.T) {
	a := buildCommitListApp(commit("h1"))
	a.DiffSource = DiffSource{Kind: DiffSourceCommitRange, Commits: []string{"abc"}}

	action := a.ExitCommitSelectMode()

	assertEq(t, action, ExitSelectorLoadWorkingTree, "commit review falls back to working tree")
	assertEq(t, a.InputMode, input.ModeNormal, "back to normal mode")
}

func TestExitCommitSelectModeKeepsNonCommitSources(t *testing.T) {
	a := buildCommitListApp(commit("h1"))
	a.DiffSource = DiffSource{Kind: DiffSourceStaged}

	assertEq(t, a.ExitCommitSelectMode(), ExitSelectorNone, "staged review keeps its diff")
}

// --- ConfirmCommitSelection ---

func TestConfirmCommitSelectionResolvesKinds(t *testing.T) {
	cases := []struct {
		name    string
		list    []vcs.CommitInfo
		rng     *model.IndexRange
		kind    SelectionKind
		wantIDs []string
	}{
		{"staged only", []vcs.CommitInfo{stagedCommitEntry()}, nil, SelectionStaged, nil},
		{"unstaged only", []vcs.CommitInfo{unstagedCommitEntry()}, nil, SelectionUnstaged, nil},
		{
			"staged and unstaged",
			[]vcs.CommitInfo{unstagedCommitEntry(), stagedCommitEntry()},
			rangeOf(0, 1), SelectionStagedAndUnstaged, nil,
		},
		{
			"commits only",
			dummyCommits("c1", "c2"),
			rangeOf(0, 1), SelectionCommits, []string{"c1", "c2"},
		},
		{
			"specials plus commits",
			[]vcs.CommitInfo{unstagedCommitEntry(), stagedCommitEntry(), commit("c1")},
			rangeOf(0, 2), SelectionStagedUnstagedAndCommits, []string{"c1"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := buildCommitListApp(tc.list...)
			a.CommitSelectionRange = tc.rng

			sel, ok := a.ConfirmCommitSelection()
			if !ok {
				t.Fatal("expected a confirmed selection")
			}
			assertEq(t, sel.Kind, tc.kind, "selection kind")
			if !slices.Equal(sel.CommitIDs, tc.wantIDs) {
				t.Errorf("commit ids: got %v, want %v", sel.CommitIDs, tc.wantIDs)
			}
		})
	}
}

func TestConfirmCommitSelectionIDsAreNewestFirst(t *testing.T) {
	a := buildCommitListApp(commit("newest"), commit("middle"), commit("oldest"))
	a.CommitSelectionRange = rangeOf(0, 2)

	sel, ok := a.ConfirmCommitSelection()
	if !ok {
		t.Fatal("expected a confirmed selection")
	}
	if !slices.Equal(sel.CommitIDs, []string{"newest", "middle", "oldest"}) {
		t.Errorf("ids not newest-first: %v", sel.CommitIDs)
	}
}

func TestConfirmCommitSelectionFallsBackToCursorRow(t *testing.T) {
	a := buildCommitListApp(commit("c1"), commit("c2"), commit("c3"))
	a.CommitListCursor = 1

	sel, ok := a.ConfirmCommitSelection()
	if !ok {
		t.Fatal("expected a confirmed selection")
	}
	assertEq(t, sel.Kind, SelectionCommits, "cursor row confirms as commit")
	if !slices.Equal(sel.CommitIDs, []string{"c2"}) {
		t.Errorf("ids: got %v, want [c2]", sel.CommitIDs)
	}
}

func TestConfirmCommitSelectionWarnsWhenNothingSelected(t *testing.T) {
	a := buildCommitListApp() // empty list; cursor resolves to no rows
	_, ok := a.ConfirmCommitSelection()
	assertEq(t, ok, false, "nothing to confirm")
	if a.Message == nil || a.Message.Content != "Select at least one commit" {
		t.Fatalf("expected select-at-least-one message, got %v", a.Message)
	}
}

// --- ApplyLoadedSelection ---

func TestApplyLoadedSelectionSetsUpInlineSelector(t *testing.T) {
	a := buildCommitListApp(commit("newest"), commit("middle"), commit("oldest"))
	a.CommitSelectionRange = rangeOf(0, 2)
	a.CommitDiffCache[model.IndexRange{0, 0}] = nil
	a.SavedInlineSelection = rangeOf(0, 0)

	sel, ok := a.ConfirmCommitSelection()
	if !ok {
		t.Fatal("expected a confirmed selection")
	}

	files := []model.DiffFile{makeFileWithHunks("pkg/x.go", []model.DiffHunk{makeHunk(1, 2)})}
	session := model.NewReviewSession("/tmp", "newest", strPtr("main"), model.SourceCommitRange)
	// DiffSource keeps the opposite, oldest-first order.
	source := DiffSource{Kind: DiffSourceCommitRange, Commits: []string{"oldest", "middle", "newest"}}
	a.ApplyLoadedSelection(files, session, source)

	assertEq(t, a.Session == session, true, "session swapped")
	assertEq(t, a.DiffSource.Kind, DiffSourceCommitRange, "source swapped")
	assertEq(t, a.InputMode, input.ModeNormal, "back to normal mode")
	assertEq(t, len(a.ReviewCommits), 3, "review commits installed")
	assertEq(t, a.ReviewCommits[0].ID, "newest", "review commits newest-first")
	assertEq(t, a.ReviewCommits[2].ID, "oldest", "review commits end oldest")
	assertRange(t, a.CommitSelectionRange, rangeOf(0, 2), "full range selected by default")
	assertEq(t, a.CommitListCursor, 0, "cursor on range start")
	assertEq(t, a.ShowCommitSelector, true, "selector visible for multi-commit review")
	assertEq(t, a.VisibleCommitCount, 3, "all review commits visible")
	assertEq(t, a.HasMoreCommits, false, "inline selector never pages")
	assertEq(t, len(a.CommitDiffCache), 0, "diff cache reset")
	assertRange(t, a.SavedInlineSelection, nil, "saved selection cleared")
	assertEq(t, len(sel.CommitIDs), 3, "confirmed ids intact")

	// Fresh navigation state and rebuilt annotations.
	assertEq(t, a.DiffState.CursorLine, 0, "fresh cursor")
	assertEq(t, a.DiffState.ScrollOffset, 0, "fresh scroll")
	if len(a.LineAnnotations) == 0 {
		t.Fatal("annotations not rebuilt")
	}
	if a.Session.File("pkg/x.go") == nil {
		t.Fatal("diff files not registered in the new session")
	}
	// A three-commit selection gets no commit-message pseudo-file.
	for i := range a.DiffFiles {
		if a.DiffFiles[i].IsCommitMessage {
			t.Fatal("unexpected commit-message file for multi-commit selection")
		}
	}
}

func TestApplyLoadedSelectionSingleCommitInsertsCommitMessage(t *testing.T) {
	body := "Longer\ndescription"
	c := commit("abc1234")
	c.Summary = "Add feature"
	c.Body = &body

	a := buildCommitListApp(c)
	a.CommitListCursor = 0

	if _, ok := a.ConfirmCommitSelection(); !ok {
		t.Fatal("expected a confirmed selection")
	}

	files := []model.DiffFile{makeFileWithHunks("x.go", []model.DiffHunk{makeHunk(1, 1)})}
	session := model.NewReviewSession("/tmp", "abc1234", strPtr("main"), model.SourceCommitRange)
	a.ApplyLoadedSelection(files, session, DiffSource{Kind: DiffSourceCommitRange, Commits: []string{"abc1234"}})

	assertEq(t, len(a.DiffFiles), 2, "commit message + real file")
	msg := &a.DiffFiles[0]
	assertEq(t, msg.IsCommitMessage, true, "commit message file first")
	assertEq(t, msg.DisplayPath(), "Commit Message (abc1234)", "path embeds the short id")
	assertEq(t, a.ShowCommitSelector, false, "single-commit review hides the selector")
	if a.Session.File("Commit Message (abc1234)") == nil {
		t.Fatal("commit message file not registered in the session")
	}
}

func TestApplyLoadedSelectionHonorsOldestSelectionStart(t *testing.T) {
	a := buildCommitListApp(commit("newest"), commit("middle"), commit("oldest"))
	a.CommitSelectionStart = CommitSelectionOldest
	a.CommitSelectionRange = rangeOf(0, 2)

	if _, ok := a.ConfirmCommitSelection(); !ok {
		t.Fatal("expected a confirmed selection")
	}
	files := []model.DiffFile{makeFileWithHunks("x.go", []model.DiffHunk{makeHunk(1, 1)})}
	session := model.NewReviewSession("/tmp", "newest", strPtr("main"), model.SourceCommitRange)
	a.ApplyLoadedSelection(files, session, DiffSource{Kind: DiffSourceCommitRange, Commits: []string{"oldest", "middle", "newest"}})

	// initial_commit_selection = oldest opens scoped to the oldest commit;
	// the caller must then narrow the diff (strict selection).
	assertRange(t, a.CommitSelectionRange, rangeOf(2, 2), "oldest commit selected")
	assertEq(t, a.CommitListCursor, 2, "cursor on the oldest commit")
	assertEq(t, IsStrictCommitSelection(a.CommitSelectionRange, len(a.ReviewCommits)), true,
		"oldest start is a strict selection")
}

func TestApplyLoadedSelectionStagedLeavesSelectorStateAlone(t *testing.T) {
	a := buildCommitListApp(stagedCommitEntry(), commit("c1"))
	a.ReviewCommits = dummyCommits("r1", "r2")
	a.CommitListCursor = 0

	sel, ok := a.ConfirmCommitSelection()
	if !ok {
		t.Fatal("expected a confirmed selection")
	}
	assertEq(t, sel.Kind, SelectionStaged, "staged row confirms staged")

	files := []model.DiffFile{makeFileWithHunks("y.go", []model.DiffHunk{makeHunk(1, 1)})}
	session := model.NewReviewSession("/tmp", "head", strPtr("main"), model.SourceStaged)
	a.ApplyLoadedSelection(files, session, DiffSource{Kind: DiffSourceStaged})

	assertEq(t, a.DiffSource.Kind, DiffSourceStaged, "staged source installed")
	assertEq(t, a.InputMode, input.ModeNormal, "back to normal mode")
	// Mirrors tuicr's load_staged_selection: the inline selector state is
	// not reconfigured by staged/unstaged-only loads.
	assertEq(t, len(a.ReviewCommits), 2, "review commits untouched")
	if len(a.LineAnnotations) == 0 {
		t.Fatal("annotations not rebuilt")
	}
}

func TestApplyLoadedSelectionPreservesWrapSetting(t *testing.T) {
	a := buildCommitListApp(commit("c1"))
	a.DiffState.WrapLines = false
	a.DiffState.CursorLine = 7

	if _, ok := a.ConfirmCommitSelection(); !ok {
		t.Fatal("expected a confirmed selection")
	}
	files := []model.DiffFile{makeFileWithHunks("x.go", []model.DiffHunk{makeHunk(1, 1)})}
	session := model.NewReviewSession("/tmp", "c1", strPtr("main"), model.SourceCommitRange)
	a.ApplyLoadedSelection(files, session, DiffSource{Kind: DiffSourceCommitRange, Commits: []string{"c1"}})

	assertEq(t, a.DiffState.WrapLines, false, "wrap setting survives the reset")
	assertEq(t, a.DiffState.CursorLine, 0, "cursor is fresh")
}

func TestSelectedCommitSetIncludesSpecialIDs(t *testing.T) {
	a := buildAppWithFiles(nil, 0)
	a.ReviewCommits = []vcs.CommitInfo{unstagedCommitEntry(), stagedCommitEntry(), commit("aaa")}

	_, hasSet := a.SelectedCommitSet()
	assertEq(t, hasSet, false, "no selection means no set")

	a.CommitSelectionRange = rangeOf(0, 2)
	set, hasSet := a.SelectedCommitSet()
	assertEq(t, hasSet, true, "selection produces a set")
	assertEq(t, len(set), 3, "all rows in the set")
	assertEq(t, set[UnstagedSelectionID], true, "unstaged id included")
	assertEq(t, set[StagedSelectionID], true, "staged id included")
	assertEq(t, set["aaa"], true, "real sha included")
}
