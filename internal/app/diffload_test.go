package app

// diffload_test.go pins the diff-load plumbing ported from tuicr's
// src/app/diff_load.rs: the synthetic staged/unstaged rows and the
// commit-message pseudo-file.

import (
	"testing"

	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs"
)

func TestStagedAndUnstagedEntriesAreSpecial(t *testing.T) {
	staged := stagedCommitEntry()
	unstaged := unstagedCommitEntry()

	assertEq(t, staged.ID, StagedSelectionID, "staged id")
	assertEq(t, staged.ShortID, "STAGED", "staged short id")
	assertEq(t, staged.Summary, "Staged changes", "staged summary")
	assertEq(t, unstaged.ID, UnstagedSelectionID, "unstaged id")
	assertEq(t, unstaged.ShortID, "UNSTAGED", "unstaged short id")
	assertEq(t, unstaged.Summary, "Unstaged changes", "unstaged summary")

	assertEq(t, isStagedCommit(&staged), true, "staged detected")
	assertEq(t, isUnstagedCommit(&unstaged), true, "unstaged detected")
	assertEq(t, isSpecialCommit(&staged), true, "staged is special")
	assertEq(t, isSpecialCommit(&unstaged), true, "unstaged is special")
	regular := commit("abc")
	assertEq(t, isSpecialCommit(&regular), false, "regular commit is not special")
}

func TestInsertCommitMessageIfSingleBuildsPseudoFile(t *testing.T) {
	body := "Longer body line 1\nline 2"
	c := commit("abc1234")
	c.Summary = "Add feature"
	c.Body = &body

	regular := makeFileWithHunks("x.go", []model.DiffHunk{makeHunk(1, 1)})
	files := InsertCommitMessageIfSingle([]model.DiffFile{regular}, []vcs.CommitInfo{c})

	assertEq(t, len(files), 2, "pseudo-file prepended")
	msg := &files[0]
	assertEq(t, msg.IsCommitMessage, true, "flagged as commit message")
	assertEq(t, msg.DisplayPath(), "Commit Message (abc1234)", "path embeds short id")
	assertEq(t, msg.Status, model.StatusAdded, "status added")
	assertEq(t, len(msg.Hunks), 1, "single hunk")

	hunk := &msg.Hunks[0]
	// Summary, blank separator, then the body lines — all context lines
	// numbered on the new side.
	want := []string{"Add feature", "", "Longer body line 1", "line 2"}
	assertEq(t, len(hunk.Lines), len(want), "line count")
	for i, content := range want {
		assertEq(t, hunk.Lines[i].Content, content, "line content")
		assertEq(t, hunk.Lines[i].Origin, model.OriginContext, "context origin")
		assertLineno(t, hunk.Lines[i].NewLineno, uint32(i)+1, "new lineno")
		if hunk.Lines[i].OldLineno != nil {
			t.Error("old lineno must be nil")
		}
	}
	assertEq(t, hunk.OldStart, uint32(0), "old start")
	assertEq(t, hunk.OldCount, uint32(0), "old count")
	assertEq(t, hunk.NewStart, uint32(1), "new start")
	assertEq(t, hunk.NewCount, uint32(4), "new count")
	assertEq(t, msg.ContentHash, model.ComputeContentHash(msg.Hunks), "content hash")

	assertEq(t, files[1].DisplayPath(), "x.go", "regular file kept after the pseudo-file")
}

func TestInsertCommitMessageIfSingleWithoutBodyUsesSummaryOnly(t *testing.T) {
	c := commit("abc")
	c.Summary = "Just a summary"
	c.Body = nil

	files := InsertCommitMessageIfSingle(nil, []vcs.CommitInfo{c})
	assertEq(t, len(files), 1, "pseudo-file created")
	assertEq(t, len(files[0].Hunks[0].Lines), 1, "summary line only")
	assertEq(t, files[0].Hunks[0].Lines[0].Content, "Just a summary", "summary content")
}

func TestInsertCommitMessageIfSingleStripsPreviousPseudoFile(t *testing.T) {
	stale := InsertCommitMessageIfSingle(nil, []vcs.CommitInfo{commit("old")})
	regular := makeFileWithHunks("x.go", []model.DiffHunk{makeHunk(1, 1)})
	files := append(append([]model.DiffFile(nil), stale...), regular)

	// No commit selected: the stale pseudo-file disappears.
	got := InsertCommitMessageIfSingle(files, nil)
	assertEq(t, len(got), 1, "stale pseudo-file stripped")
	assertEq(t, got[0].DisplayPath(), "x.go", "regular file kept")

	// A different single commit replaces it.
	got = InsertCommitMessageIfSingle(files, []vcs.CommitInfo{commit("new")})
	assertEq(t, len(got), 2, "replaced, not duplicated")
	assertEq(t, got[0].DisplayPath(), "Commit Message (new)", "new commit's message")
}

func TestInsertCommitMessageIfSingleSkipsSpecialAndMulti(t *testing.T) {
	regular := makeFileWithHunks("x.go", []model.DiffHunk{makeHunk(1, 1)})

	got := InsertCommitMessageIfSingle([]model.DiffFile{regular}, []vcs.CommitInfo{stagedCommitEntry()})
	assertEq(t, len(got), 1, "special commit gets no message file")

	got = InsertCommitMessageIfSingle([]model.DiffFile{regular}, dummyCommits("a", "b"))
	assertEq(t, len(got), 1, "multi-commit selection gets no message file")
}

func TestSingleSelectedCommitResolution(t *testing.T) {
	a := buildAppWithFiles(nil, 0)

	// No selection, single review commit -> that commit.
	a.ReviewCommits = dummyCommits("only")
	if c := a.singleSelectedCommit(); c == nil || c.ID != "only" {
		t.Fatalf("expected the only review commit, got %v", c)
	}

	// No selection, multiple review commits -> none.
	a.ReviewCommits = dummyCommits("a", "b")
	if a.singleSelectedCommit() != nil {
		t.Fatal("expected nil for multiple unselected commits")
	}

	// Single-index selection -> that commit.
	a.CommitSelectionRange = rangeOf(1, 1)
	if c := a.singleSelectedCommit(); c == nil || c.ID != "b" {
		t.Fatalf("expected commit b, got %v", c)
	}

	// Multi-index selection -> none.
	a.CommitSelectionRange = rangeOf(0, 1)
	if a.singleSelectedCommit() != nil {
		t.Fatal("expected nil for a multi-commit selection")
	}

	// Out-of-bounds selection -> none.
	a.CommitSelectionRange = rangeOf(5, 5)
	if a.singleSelectedCommit() != nil {
		t.Fatal("expected nil for an out-of-bounds selection")
	}
}
