package app

// tree_test.go ports tuicr's src/app/tests/tree_tests.rs in full (against
// the real App tree engine rather than a duplicated harness) plus coverage
// for the list-state and sorting helpers.

import (
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

func makeTreeFile(path string) model.DiffFile {
	return model.DiffFile{NewPath: new(path), Status: model.StatusModified}
}

func treeApp(paths ...string) *App {
	files := make([]model.DiffFile, len(paths))
	for i, p := range paths {
		files[i] = makeTreeFile(p)
	}
	return buildAppWithFiles(files, 0)
}

func visibleFileCount(a *App) int {
	n := 0
	for _, item := range a.BuildVisibleItems() {
		if !item.IsDir {
			n++
		}
	}
	return n
}

func visibleDirCount(a *App) int {
	n := 0
	for _, item := range a.BuildVisibleItems() {
		if item.IsDir {
			n++
		}
	}
	return n
}

func TestExpandAllShowsAllFiles(t *testing.T) {
	a := treeApp("src/ui/app.rs", "src/ui/help.rs", "src/main.rs")
	a.ExpandAllDirs()

	assertEq(t, visibleFileCount(a), 3, "visible files after expand all")
}

func TestCollapseAllHidesAllFiles(t *testing.T) {
	a := treeApp("src/ui/app.rs", "src/main.rs")
	a.ExpandAllDirs()
	a.CollapseAllDirs()

	assertEq(t, visibleFileCount(a), 0, "visible files after collapse all")
	assertEq(t, visibleDirCount(a), 1, "only src visible")
}

func TestCollapseParentHidesNestedDirs(t *testing.T) {
	a := treeApp("src/ui/components/button.rs")
	a.ExpandAllDirs()
	assertEq(t, visibleDirCount(a), 3, "src, src/ui, src/ui/components")

	a.ToggleDirectory("src")
	items := a.BuildVisibleItems()
	assertEq(t, len(items), 1, "only collapsed src dir")
	if !items[0].IsDir || items[0].Expanded {
		t.Errorf("expected collapsed directory row, got %+v", items[0])
	}
}

func TestRootFilesAlwaysVisible(t *testing.T) {
	a := treeApp("README.md", "Cargo.toml")
	a.CollapseAllDirs()

	assertEq(t, visibleFileCount(a), 2, "root files visible when collapsed")
}

func TestTreeDepthCorrect(t *testing.T) {
	a := treeApp("a/b/c/file.rs")
	a.ExpandAllDirs()

	items := a.BuildVisibleItems()
	if !items[0].IsDir || items[0].Depth != 0 || items[0].Path != "a" {
		t.Errorf("items[0] = %+v, want dir a at depth 0", items[0])
	}
	if !items[1].IsDir || items[1].Depth != 1 || items[1].Path != "a/b" {
		t.Errorf("items[1] = %+v, want dir a/b at depth 1", items[1])
	}
	if !items[2].IsDir || items[2].Depth != 2 || items[2].Path != "a/b/c" {
		t.Errorf("items[2] = %+v, want dir a/b/c at depth 2", items[2])
	}
	if items[3].IsDir || items[3].Depth != 3 {
		t.Errorf("items[3] = %+v, want file at depth 3", items[3])
	}
}

func TestToggleExpandsCollapsedDir(t *testing.T) {
	a := treeApp("src/main.rs")
	a.CollapseAllDirs()
	assertEq(t, visibleFileCount(a), 0, "collapsed hides file")

	a.ToggleDirectory("src")
	assertEq(t, visibleFileCount(a), 1, "toggle reveals file")
}

func TestSiblingDirsIndependent(t *testing.T) {
	a := treeApp("src/app.rs", "tests/test.rs")
	a.ExpandAllDirs()
	a.ToggleDirectory("src") // collapse src

	assertEq(t, visibleFileCount(a), 1, "only tests/test.rs visible")
}

// --- supplementary coverage for tree/list helpers ---

func TestSortFilesByDirectoryOrdersCommitMessageFirstThenDirs(t *testing.T) {
	commitMsg := makeTreeFile("COMMIT_MESSAGE")
	commitMsg.IsCommitMessage = true
	files := []model.DiffFile{
		makeTreeFile("zeta/z.rs"),
		makeTreeFile("alpha/a.rs"),
		commitMsg,
		makeTreeFile("root.rs"),
		makeTreeFile("alpha/b.rs"),
	}
	a := buildAppWithFiles(files, 0)

	var order []string
	for i := range a.DiffFiles {
		order = append(order, a.DiffFiles[i].DisplayPath())
	}
	want := []string{"COMMIT_MESSAGE", "root.rs", "alpha/a.rs", "alpha/b.rs", "zeta/z.rs"}
	for i := range want {
		assertEq(t, order[i], want[i], "sorted order")
	}
	// Cursor starts at the overview position.
	assertEq(t, a.DiffState.CursorLine, 0, "cursor at overview")
	assertEq(t, a.DiffState.CurrentFileIdx, 0, "current file reset")
}

func TestSortFilesByDirectoryPreservesCurrentFile(t *testing.T) {
	a := treeApp("b/x.rs", "a/y.rs")
	a.JumpToFile(1)
	current, _ := a.CurrentFilePath()

	a.SortFilesByDirectory(false)

	got, _ := a.CurrentFilePath()
	assertEq(t, got, current, "focused file preserved across resort")
}

func TestFileListNavigationClampsAndFollows(t *testing.T) {
	a := treeApp("a/x.rs", "a/y.rs", "b/z.rs")
	a.ExpandAllDirs()
	total := len(a.BuildVisibleItems())

	a.FileListDown(100)
	assertEq(t, a.FileListState.Selected(), total-1, "down clamps to last row")

	a.FileListUp(100)
	assertEq(t, a.FileListState.Selected(), 0, "up clamps to first row")

	item, ok := a.SelectedTreeItem()
	if !ok || !item.IsDir || item.Path != "a" {
		t.Errorf("selected tree item = %+v, want dir a", item)
	}
}

func TestFileListViewportScrolling(t *testing.T) {
	a := treeApp("a/1.rs", "a/2.rs", "a/3.rs", "b/4.rs", "b/5.rs")
	a.ExpandAllDirs()
	a.FileListState.ViewportHeight = 3

	a.FileListViewportScrollDown(2)
	assertEq(t, a.FileListState.Offset(), 2, "viewport scrolled down")
	assertEq(t, a.FileListState.Selected(), 2, "selection pulled into view")

	a.FileListViewportScrollUp(1)
	assertEq(t, a.FileListState.Offset(), 1, "viewport scrolled up")

	a.FileListState.Select(5)
	a.FileListViewportScrollUp(1)
	assertEq(t, a.FileListState.Selected(), 2, "selection pulled up to viewport bottom")
}

func TestFileListStateHorizontalScroll(t *testing.T) {
	s := FileListState{MaxContentWidth: 30, ViewportWidth: 10}
	s.ScrollRight(50)
	assertEq(t, s.ScrollX, 20, "scroll right clamps to content width")
	s.ScrollLeft(5)
	assertEq(t, s.ScrollX, 15, "scroll left")
	s.ScrollLeft(50)
	assertEq(t, s.ScrollX, 0, "scroll left clamps at zero")
}

func TestToggleFileListMovesFocusToDiff(t *testing.T) {
	a := treeApp("a.rs")
	a.FocusedPanel = PanelFileList

	a.ToggleFileList()
	assertEq(t, a.ShowFileList, false, "file list hidden")
	assertEq(t, a.FocusedPanel, PanelDiff, "focus moved to diff")
	assertEq(t, a.Message.Content, "File list: hidden", "status message")

	a.ToggleFileList()
	assertEq(t, a.ShowFileList, true, "file list shown")
	assertEq(t, a.Message.Content, "File list: visible", "status message")
}

func TestToggleDiffViewMode(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 0)

	a.ToggleDiffViewMode()
	assertEq(t, a.DiffViewMode, ViewSideBySide, "toggled to side-by-side")
	if !anyAnnotation(a, func(l *AnnotatedLine) bool { return l.Kind == AnnSideBySideLine }) {
		t.Error("side-by-side annotations should render")
	}
	assertEq(t, a.renderedHeight(), len(a.LineAnnotations), "heights match in sbs mode")

	a.ToggleDiffViewMode()
	assertEq(t, a.DiffViewMode, ViewUnified, "toggled back to unified")
}

func TestToggleDiffViewModeBlockedInPristineMode(t *testing.T) {
	a := treeApp("a.rs")
	a.IsPristineMode = true

	a.ToggleDiffViewMode()
	assertEq(t, a.DiffViewMode, ViewUnified, "mode unchanged in pristine mode")
	assertEq(t, a.Message.Content, "side-by-side not available in pristine mode", "status message")
}

func TestEnsureValidTreeSelectionFallsBackToAncestor(t *testing.T) {
	a := treeApp("src/ui/app.rs", "other.rs")
	a.ExpandAllDirs()
	a.DiffState.CurrentFileIdx = 0 // src/ui/app.rs (files sorted: other.rs first? no: "." sorts before "src")

	// Locate the actual index of src/ui/app.rs after sorting.
	for i := range a.DiffFiles {
		if a.DiffFiles[i].DisplayPath() == "src/ui/app.rs" {
			a.DiffState.CurrentFileIdx = i
		}
	}
	a.ToggleDirectory("src/ui") // hides the current file

	item, ok := a.SelectedTreeItem()
	if !ok || !item.IsDir || item.Path != "src/ui" {
		t.Errorf("selected tree item = %+v, want dir src/ui", item)
	}
}
