package app

// reviewedmut_test.go ports the toggle-hunk-reviewed tests from tuicr's
// src/app/tests/expand_gap_tests.rs (skipped in M3) and adds behavior tests
// for staging reviewed files and editor-target queueing.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs"
)

//nolint:unparam // hunkIdx mirrors HunkReviewKey's signature; ported tests target hunk 0
func mustHunkReviewKey(t *testing.T, file *model.DiffFile, hunkIdx int) string {
	t.Helper()
	key, ok := file.HunkReviewKey(hunkIdx)
	if !ok {
		t.Fatal("missing hunk review key")
	}
	return key
}

func TestShouldToggleHunkReviewedFromHeader(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	key := mustHunkReviewKey(t, &a.DiffFiles[0], 0)

	a.DiffState.CursorLine = mustHunkHeaderLine(t, a, 0, 0)
	a.ToggleHunkReviewed()

	if !a.Session.IsHunkReviewed("test.rs", key) {
		t.Fatal("hunk must be reviewed")
	}
	assertEq(t, a.Message.Content, "Hunk marked reviewed", "message")
	ann := &a.LineAnnotations[a.DiffState.CursorLine]
	assertEq(t, ann.Kind, AnnHunkHeader, "cursor parks on header")
	assertEq(t, ann.FileIdx, 0, "header file")
	assertEq(t, ann.HunkIdx, 0, "header hunk")

	// Toggling again unreviews.
	a.ToggleHunkReviewed()
	if a.Session.IsHunkReviewed("test.rs", key) {
		t.Fatal("hunk must be unreviewed")
	}
	assertEq(t, a.Message.Content, "Hunk marked unreviewed", "unreview message")
}

func TestShouldToggleHunkReviewedFromDiffLine(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	key := mustHunkReviewKey(t, &a.DiffFiles[0], 0)

	a.DiffState.CursorLine = hunkDiffLine(t, a, 0, 0)
	a.ToggleHunkReviewed()

	if !a.Session.IsHunkReviewed("test.rs", key) {
		t.Fatal("hunk must be reviewed")
	}
}

func TestShouldWarnWhenTogglingHunkOutsideHunk(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	key := mustHunkReviewKey(t, &a.DiffFiles[0], 0)

	a.DiffState.CursorLine = 0
	a.ToggleHunkReviewed()

	if a.Session.IsHunkReviewed("test.rs", key) {
		t.Fatal("hunk must stay unreviewed")
	}
	assertEq(t, a.Message.Content, "Move cursor to a hunk to toggle reviewed", "warning")
	assertEq(t, a.Message.Type, MessageWarning, "warning type")
}

func TestToggleHunkReviewedFoldsBody(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3), makeHunk(10, 2)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	before := a.TotalLines()

	a.DiffState.CursorLine = mustHunkHeaderLine(t, a, 0, 0)
	a.ToggleHunkReviewed()

	// Reviewed hunk folds its 3 body lines plus the merged expander of the
	// gap before hunk 1 (a complete review unit).
	assertEq(t, a.TotalLines(), before-4, "folded height")
	if anyAnnotation(a, func(ann *AnnotatedLine) bool {
		return ann.Kind == AnnDiffLine && ann.FileIdx == 0 && ann.HunkIdx == 0
	}) {
		t.Fatal("reviewed hunk body must not render")
	}
	gapID := GapID{FileIdx: 0, HunkIdx: 1}
	if anyAnnotation(a, func(ann *AnnotatedLine) bool {
		return (ann.Kind == AnnExpander || ann.Kind == AnnHiddenLines) && ann.GapID == gapID
	}) {
		t.Fatal("gap controls adjoining the reviewed hunk must collapse")
	}
	if !anyAnnotation(a, func(ann *AnnotatedLine) bool {
		return ann.Kind == AnnDiffLine && ann.FileIdx == 0 && ann.HunkIdx == 1
	}) {
		t.Fatal("second hunk body must still render")
	}
	assertEq(t, a.renderedHeight(), len(a.LineAnnotations), "heights stay in lockstep")
}

func TestToggleKeepsFileAndHunkReviewedIndependent(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	key := mustHunkReviewKey(t, &a.DiffFiles[0], 0)

	a.DiffState.CursorLine = mustHunkHeaderLine(t, a, 0, 0)
	a.ToggleHunkReviewed()
	a.ToggleReviewedForFileIdx(0, false)

	if !a.Session.IsFileReviewed("test.rs") {
		t.Fatal("file must be reviewed")
	}
	if !a.Session.IsHunkReviewed("test.rs", key) {
		t.Fatal("hunk must stay reviewed")
	}
}

func TestToggleReviewedCollapsesFileAndReports(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 20)
	a.DiffState.CursorLine = hunkDiffLine(t, a, 0, 0)

	a.ToggleReviewed()

	if !a.Session.IsFileReviewed("test.rs") {
		t.Fatal("file must be reviewed")
	}
	if !a.Dirty {
		t.Fatal("toggle marks dirty")
	}
	assertEq(t, a.Message.Content, "File marked reviewed", "message")
	if anyAnnotation(a, func(ann *AnnotatedLine) bool { return ann.Kind == AnnDiffLine }) {
		t.Fatal("reviewed file must collapse")
	}
	ann := &a.LineAnnotations[a.DiffState.CursorLine]
	assertEq(t, ann.Kind, AnnFileHeader, "cursor snaps to the file header")

	a.ToggleReviewed()
	assertEq(t, a.Message.Content, "File marked unreviewed", "unreview message")
	if a.Session.IsFileReviewed("test.rs") {
		t.Fatal("file must be unreviewed")
	}
}

// stagingVcs records StageFile calls on top of the shared mock.
type stagingVcs struct {
	mockVcs
	staged []string
	err    error
}

func (s *stagingVcs) StageFile(path string) error {
	if s.err != nil {
		return s.err
	}
	s.staged = append(s.staged, path)
	return nil
}

func buildStagingApp(t *testing.T, kind DiffSourceKind) (*App, *stagingVcs) {
	t.Helper()
	info := &vcs.Info{RootPath: "/tmp", HeadCommit: "abc123", BranchName: new("main"), Type: vcs.TypeGit}
	backend := &stagingVcs{mockVcs: mockVcs{info: info, totalLines: 20}}
	session := model.NewReviewSession(info.RootPath, info.HeadCommit, info.BranchName, model.SourceUnstaged)
	files := []model.DiffFile{
		makeFileWithHunks("a.rs", []model.DiffHunk{makeHunk(1, 2)}),
		makeFileWithHunks("b.rs", []model.DiffHunk{makeHunk(1, 2)}),
	}
	return NewApp(backend, info, files, session, DiffSource{Kind: kind}), backend
}

func TestStageReviewedFilesRequiresUnstagedSource(t *testing.T) {
	a, _ := buildStagingApp(t, DiffSourceWorkingTree)
	if a.CanStage() {
		t.Fatal("working-tree source must not stage")
	}
	assertEq(t, a.StageReviewedFiles(), 0, "staged count")
	assertEq(t, a.Message.Content, "Staging only available when viewing unstaged diffs", "error")
	assertEq(t, a.Message.Type, MessageError, "error type")
}

func TestStageReviewedFilesWarnsWithNothingReviewed(t *testing.T) {
	a, _ := buildStagingApp(t, DiffSourceUnstaged)
	assertEq(t, a.StageReviewedFiles(), 0, "staged count")
	assertEq(t, a.Message.Content, "No reviewed files to stage", "warning")
}

func TestStageReviewedFilesStagesEachReviewedFile(t *testing.T) {
	a, backend := buildStagingApp(t, DiffSourceStagedAndUnstaged)
	a.Session.File("a.rs").Reviewed = true
	a.Session.File("b.rs").Reviewed = true

	assertEq(t, a.StageReviewedFiles(), 2, "staged count")
	assertEq(t, len(backend.staged), 2, "backend calls")
	assertEq(t, backend.staged[0], "a.rs", "deterministic order")
	assertEq(t, backend.staged[1], "b.rs", "deterministic order")
	assertEq(t, a.Message.Content, "Staged 2 reviewed file(s)", "message")
}

func TestStageReviewedFilesReportsBackendError(t *testing.T) {
	a, backend := buildStagingApp(t, DiffSourceUnstaged)
	backend.err = os.ErrPermission
	a.Session.File("a.rs").Reviewed = true

	assertEq(t, a.StageReviewedFiles(), 0, "staged count on error")
	assertEq(t, a.Message.Type, MessageError, "error type")
}

func buildEditorApp(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "test.rs"), []byte("fn main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	info := &vcs.Info{RootPath: root, HeadCommit: "abc123", BranchName: new("main"), Type: vcs.TypeGit}
	session := model.NewReviewSession(info.RootPath, info.HeadCommit, info.BranchName, model.SourceWorkingTree)
	files := []model.DiffFile{makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})}
	return NewApp(&mockVcs{info: info, totalLines: 20}, info, files, session, DiffSource{Kind: DiffSourceWorkingTree})
}

func TestQueueEditorFromDiffCursor(t *testing.T) {
	a := buildEditorApp(t)
	a.FocusedPanel = PanelDiff
	a.DiffState.CursorLine = hunkDiffLine(t, a, 0, 0) + 1 // line 2

	a.QueueEditorForFocusedItem()

	target, ok := a.TakePendingEditorTarget()
	if !ok {
		t.Fatal("expected a queued target")
	}
	assertEq(t, target.Path, filepath.Join(a.VcsInfo.RootPath, "test.rs"), "absolute path")
	assertLineno(t, target.Line, 2, "line")
	if _, ok := a.TakePendingEditorTarget(); ok {
		t.Fatal("take must consume the target")
	}
}

func TestQueueEditorFromFileList(t *testing.T) {
	a := buildEditorApp(t)
	a.FocusedPanel = PanelFileList
	treeIdx, ok := a.fileIdxToTreeIdx(0)
	if !ok {
		t.Fatal("file missing from tree")
	}
	a.FileListState.Select(treeIdx)

	a.QueueEditorForFocusedItem()

	target, ok := a.TakePendingEditorTarget()
	if !ok {
		t.Fatal("expected a queued target")
	}
	if target.Line != nil {
		t.Fatal("file-list launch carries no line")
	}
}

func TestQueueEditorRefusesCommitMessagePseudoFile(t *testing.T) {
	a := buildEditorApp(t)
	a.DiffFiles[0].IsCommitMessage = true
	a.FocusedPanel = PanelDiff
	a.DiffState.CursorLine = hunkDiffLine(t, a, 0, 0)

	a.QueueEditorForFocusedItem()

	if _, ok := a.TakePendingEditorTarget(); ok {
		t.Fatal("commit message must not queue")
	}
	assertEq(t, a.Message.Content, "Commit message has no local file to open", "warning")
}

func TestQueueEditorRefusesMissingFile(t *testing.T) {
	a := buildEditorApp(t)
	if err := os.Remove(filepath.Join(a.VcsInfo.RootPath, "test.rs")); err != nil {
		t.Fatal(err)
	}
	a.FocusedPanel = PanelDiff
	a.DiffState.CursorLine = hunkDiffLine(t, a, 0, 0)

	a.QueueEditorForFocusedItem()

	if _, ok := a.TakePendingEditorTarget(); ok {
		t.Fatal("missing file must not queue")
	}
	assertEq(t, a.Message.Type, MessageWarning, "warning type")
}

func TestQueueEditorRefusesNonLocalRoot(t *testing.T) {
	a := buildEditorApp(t)
	a.VcsInfo.RootPath = "forge:github.com/owner/repo"
	a.FocusedPanel = PanelDiff
	a.DiffState.CursorLine = hunkDiffLine(t, a, 0, 0)

	a.QueueEditorForFocusedItem()

	if _, ok := a.TakePendingEditorTarget(); ok {
		t.Fatal("synthetic root must not queue")
	}
	assertEq(t, a.Message.Content, "Cannot open test.rs: no local checkout", "warning")
}

func TestQueueEditorWarnsOnOtherPanels(t *testing.T) {
	a := buildEditorApp(t)
	a.FocusedPanel = PanelComments

	a.QueueEditorForFocusedItem()

	if _, ok := a.TakePendingEditorTarget(); ok {
		t.Fatal("comments panel must not queue")
	}
	assertEq(t, a.Message.Content, "Focus a file or diff line to open in editor", "warning")
}
