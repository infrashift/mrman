package app

// navigation_extra_test.go supplements the ported suites with coverage for
// the remaining read-only surface: layout constants, page scrolls, hunk and
// file navigation in multi-file view, gap cursor hits, source-line jump
// messages, diff stats, and the reviewed-file collapse.

import (
	"testing"

	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs"
)

func TestLinenoWidthAndGutters(t *testing.T) {
	assertEq(t, LinenoWidth(0), 4, "zero uses minimum")
	assertEq(t, LinenoWidth(9999), 4, "four digits")
	assertEq(t, LinenoWidth(10000), 5, "five digits")
	assertEq(t, LinenoWidth(123456), 6, "six digits")

	// These counted the cursor indicator as one column when it renders two
	// ("▶ " / "  "), which made every side-by-side row a column wider than
	// the pane it was sized for. Asserting the constants against the same
	// wrong arithmetic is what let that survive; the authority now is
	// ui.TestDiffRowsFitTheirPane, which measures rendered rows.
	assertEq(t, UnifiedGutter(4), 9, "unified gutter")
	assertEq(t, SbsLeftGutter(4), 8, "sbs left gutter")
	assertEq(t, SbsOverhead(4), 17, "sbs overhead")
}

func TestAppLinenoWidthUsesHunkAndCacheMax(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 5)})
	a := buildAppWithFiles([]model.DiffFile{file}, 123456)
	assertEq(t, a.LinenoWidth(), 6, "cache max dominates hunk max")
}

func TestDiffSourceIncludesWorktreeChanges(t *testing.T) {
	yes := []DiffSourceKind{
		DiffSourceWorkingTree, DiffSourceUnstaged,
		DiffSourceStagedAndUnstaged, DiffSourceStagedUnstagedAndCommits,
	}
	no := []DiffSourceKind{DiffSourceStaged, DiffSourceCommitRange, DiffSourcePullRequest}
	for _, k := range yes {
		assertEq(t, DiffSource{Kind: k}.IncludesWorktreeChanges(), true, "worktree source")
	}
	for _, k := range no {
		assertEq(t, DiffSource{Kind: k}.IncludesWorktreeChanges(), false, "non-worktree source")
	}
}

func TestStagedSourceDisablesEofGap(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 5)})
	info := &vcs.Info{RootPath: "/tmp", HeadCommit: "abc", Type: vcs.TypeGit}
	session := model.NewReviewSession(info.RootPath, info.HeadCommit, nil, model.SourceStaged)
	a := NewApp(&mockVcs{info: info, totalLines: 100}, info, []model.DiffFile{file}, session,
		DiffSource{Kind: DiffSourceStaged})

	assertEq(t, len(a.FileLineCountCache), 0, "no line-count cache for staged diffs")
	if anyAnnotation(a, func(l *AnnotatedLine) bool { return l.Kind == AnnExpander }) {
		t.Error("no EOF expander for staged diffs")
	}
}

func TestRefCommitForCommitRange(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 5)})
	info := &vcs.Info{RootPath: "/tmp", HeadCommit: "abc", Type: vcs.TypeGit}
	session := model.NewReviewSession(info.RootPath, info.HeadCommit, nil, model.SourceCommitRange)
	a := NewApp(&mockVcs{info: info, totalLines: 100}, info, []model.DiffFile{file}, session,
		DiffSource{Kind: DiffSourceCommitRange, Commits: []string{"c1", "c2"}})

	ref := a.refCommit()
	if ref == nil || *ref != "c2" {
		t.Errorf("refCommit = %v, want c2", ref)
	}
}

func TestScrollDownAndUpMoveCursorAndScroll(t *testing.T) {
	a := buildScrollApp(40, 20, 5)
	a.DiffState.CursorLine = 3
	a.DiffState.ScrollOffset = 0

	a.ScrollDown(10)
	assertEq(t, a.DiffState.CursorLine, 13, "cursor after half page down")
	// ensureCursorVisible pulls the scroll back to keep the top margin:
	// cursor(13) - margin(5) = 8.
	assertEq(t, a.DiffState.ScrollOffset, 8, "scroll after half page down")

	a.ScrollUp(10)
	assertEq(t, a.DiffState.CursorLine, 3, "cursor after half page up")
	assertEq(t, a.DiffState.ScrollOffset, 0, "scroll after half page up")

	// ScrollUp to a decoration index skips backward off decorations:
	// index 1 is the file header, so cursor lands on 0 (review header is
	// not decoration).
	a.DiffState.CursorLine = 11
	a.ScrollUp(10)
	assertEq(t, a.DiffState.CursorLine, 0, "cursor skipped decoration backward")
}

func TestScrollViewKeepsCursorInsideMargins(t *testing.T) {
	a := buildScrollApp(40, 20, 5)
	a.DiffState.CursorLine = 2
	a.DiffState.ScrollOffset = 0

	a.ScrollViewDown(10)
	assertEq(t, a.DiffState.ScrollOffset, 10, "view scrolled down")
	assertEq(t, a.DiffState.CursorLine, 15, "cursor pulled to top margin")

	a.DiffState.CursorLine = 40
	a.ScrollViewUp(15)
	assertEq(t, a.DiffState.ScrollOffset, 0, "view scrolled up but cursor was below bottom")
	assertEq(t, a.DiffState.CursorLine, 19, "cursor pulled to viewport bottom")
}

func TestHorizontalScrollRespectsWrap(t *testing.T) {
	a := buildScrollApp(10, 20, 0)
	a.DiffState.MaxContentWidth = 100
	a.DiffState.ViewportWidth = 40

	// Wrapping on (default): horizontal scroll is a no-op.
	a.ScrollRight(10)
	assertEq(t, a.DiffState.ScrollX, 0, "no horizontal scroll while wrapping")

	a.ToggleDiffWrap()
	assertEq(t, a.DiffState.WrapLines, false, "wrap off")
	assertEq(t, a.Message.Content, "Diff wrapping: off", "wrap message")

	a.ScrollRight(1000)
	assertEq(t, a.DiffState.ScrollX, 60, "scroll right clamps to content width")
	a.ScrollLeft(10)
	assertEq(t, a.DiffState.ScrollX, 50, "scroll left")

	a.ToggleDiffWrap()
	assertEq(t, a.DiffState.WrapLines, true, "wrap back on")
	assertEq(t, a.DiffState.ScrollX, 0, "scroll x reset when wrapping")
}

func TestCursorToTopAndBottom(t *testing.T) {
	a := buildScrollApp(40, 20, 5)
	a.DiffState.CursorLine = 30

	a.CursorToTop()
	assertEq(t, a.DiffState.ScrollOffset, 25, "zt: cursor minus margin")

	a.CursorToBottom()
	// zb: cursor - (visible - 1 - margin) = 30 - (20 - 1 - 5) = 16
	assertEq(t, a.DiffState.ScrollOffset, 16, "zb: cursor at bottom margin")
}

func TestJumpToBottomAndIsCursorVisible(t *testing.T) {
	a := buildScrollApp(40, 20, 5)

	a.JumpToBottom()
	assertEq(t, a.DiffState.CursorLine, a.MaxCursorLine(), "cursor at max")
	assertEq(t, a.DiffState.ScrollOffset, a.MaxCursorLine()+1-20, "last line at viewport bottom")
	assertEq(t, a.IsCursorVisible(), true, "cursor visible after G")

	a.DiffState.ScrollOffset = 0
	a.DiffState.CursorLine = 30
	assertEq(t, a.IsCursorVisible(), false, "cursor below viewport is invisible")
}

// TestHunkPositionsCountExpanderRows is a regression test: HunkPositions used
// to re-derive its indices from render heights and forgot the expander rows a
// gap in the context inserts, so ] and [ landed short of the hunk header on
// any diff with hidden context. Every earlier test happened to use a fixture
// with no gaps — hunks from line 1, or a VCS serving no context at all — so
// nothing caught it. This one has a gap above each hunk.
func TestHunkPositionsCountExpanderRows(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(10, 3), makeHunk(50, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 200)

	positions := a.HunkPositions()
	if len(positions) != 2 {
		t.Fatalf("expected 2 hunk positions, got %v", positions)
	}
	for hunkIdx, pos := range positions {
		want, ok := a.HunkHeaderLine(0, hunkIdx)
		if !ok {
			t.Fatalf("no header annotation for hunk %d", hunkIdx)
		}
		assertEq(t, pos, want, "hunk position must be the header's annotation index")
		assertEq(t, a.LineAnnotations[pos].Kind, AnnHunkHeader, "position must point at a header")
	}

	// And the navigation built on it lands on a header, not on a diff line or
	// an expander partway there.
	a.DiffState.CursorLine = positions[0]
	a.NextHunk()
	assertEq(t, a.DiffState.CursorLine, positions[1], "] lands on the next hunk header")
	a.PrevHunk()
	assertEq(t, a.DiffState.CursorLine, positions[0], "[ lands back on the first header")
}

func TestNextPrevFileAndHunkInMultiFileView(t *testing.T) {
	files := []model.DiffFile{
		makeFileWithHunks("a.rs", []model.DiffHunk{makeHunk(1, 3), makeHunk(10, 2)}),
		makeFileWithHunks("b.rs", []model.DiffHunk{makeHunk(1, 3)}),
	}
	a := buildAppWithFiles(files, 0)
	a.DiffState.ViewportHeight = 30
	a.DiffState.VisibleLineCount = 30

	a.NextFile()
	assertEq(t, a.DiffState.CurrentFileIdx, 1, "next file")
	a.NextFile()
	assertEq(t, a.DiffState.CurrentFileIdx, 1, "next file at end is a no-op")
	a.PrevFile()
	assertEq(t, a.DiffState.CurrentFileIdx, 0, "prev file")
	a.PrevFile()
	assertEq(t, a.DiffState.CurrentFileIdx, 0, "prev file at start is a no-op")

	a.DiffState.CursorLine = 0
	a.NextHunk()
	positions := a.HunkPositions()
	assertEq(t, a.DiffState.CursorLine, positions[0], "first hunk")
	a.NextHunk()
	assertEq(t, a.DiffState.CursorLine, positions[1], "second hunk")
	a.PrevHunk()
	assertEq(t, a.DiffState.CursorLine, positions[0], "back to first hunk")
	a.PrevHunk()
	assertEq(t, a.DiffState.CursorLine, 0, "prev hunk before first goes to top")
}

func TestGapAtCursorHits(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 2), makeHunk(40, 2)})
	a := buildAppWithFiles([]model.DiffFile{file}, 41) // EOF exactly covered: no EOF gap
	gapID := GapID{FileIdx: 0, HunkIdx: 1}
	if err := a.ExpandGap(gapID, ExpandDown, new(5)); err != nil {
		t.Fatal(err)
	}

	kinds := map[GapHitKind]bool{}
	for i := range a.LineAnnotations {
		a.DiffState.CursorLine = i
		if hit, ok := a.GapAtCursor(); ok {
			kinds[hit.Kind] = true
			assertEq(t, hit.GapID, gapID, "hit gap id")
		}
	}
	if !kinds[GapHitExpander] || !kinds[GapHitHiddenLines] || !kinds[GapHitExpandedContent] {
		t.Errorf("expected all three hit kinds, got %v", kinds)
	}

	a.DiffState.CursorLine = 0
	if _, ok := a.GapAtCursor(); ok {
		t.Error("review header is not a gap hit")
	}
}

func TestCollapseGapAndClearExpandedGaps(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(30, 5)})
	a := buildAppWithFiles([]model.DiffFile{file}, 100)
	gapID := GapID{FileIdx: 0, HunkIdx: 0}
	if err := a.ExpandGap(gapID, ExpandUp, new(10)); err != nil {
		t.Fatal(err)
	}

	a.CollapseGap(gapID)
	if _, ok := a.ExpandedBottom[gapID]; ok {
		t.Error("collapse should drop expansion state")
	}
	if !anyAnnotation(a, func(l *AnnotatedLine) bool {
		return l.Kind == AnnExpander && l.GapID == gapID
	}) {
		t.Error("collapsed gap should render an expander again")
	}

	if err := a.ExpandGap(gapID, ExpandDown, new(3)); err != nil {
		t.Fatal(err)
	}
	a.ClearExpandedGaps()
	assertEq(t, len(a.ExpandedTop), 0, "expanded top cleared")
	assertEq(t, len(a.ExpandedBottom), 0, "expanded bottom cleared")
	assertEq(t, len(a.FileLineCountCache), 0, "line count cache cleared")
}

func TestGoToSourceLineMessages(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 5)})
	a := buildAppWithFiles([]model.DiffFile{file}, 0) // zero-length file: no EOF gap

	// Nearest: line 9 is not in the diff (hunk covers 1-5, no gap data).
	a.GoToSourceLine(9, model.LineSideNew)
	assertEq(t, a.Message.Content, "Line 9 not in diff, jumped to nearest", "nearest message")
	assertLineno(t, cursorNewLineno(a), 5, "cursor on nearest line")

	// NotFound: old side of a file with no old-side-only rows still has old
	// linenos here (context lines), so use a fresh file with additions only.
	addHunk := model.DiffHunk{
		Header:   "@@ -0,0 +1,2 @@",
		OldStart: 0, OldCount: 0, NewStart: 1, NewCount: 2,
		Lines: []model.DiffLine{
			{Origin: model.OriginAddition, Content: "a", NewLineno: new(uint32(1))},
			{Origin: model.OriginAddition, Content: "b", NewLineno: new(uint32(2))},
		},
	}
	added := model.DiffFile{NewPath: new("new.rs"), Status: model.StatusAdded, Hunks: []model.DiffHunk{addHunk}}
	b := buildAppWithFiles([]model.DiffFile{added}, 0)
	// Drop the (zero) line-count cache entry so no phantom EOF gap is
	// probed for the old side of a pure-addition file.
	b.FileLineCountCache = map[int]uint32{}
	b.GoToSourceLine(1, model.LineSideOld)
	assertEq(t, b.Message.Content, "Line 1 (old) not found in current file", "not-found warning")
	assertEq(t, b.Message.Type, MessageWarning, "warning type")
}

func TestDiffStatCountsAdditionsAndDeletions(t *testing.T) {
	hunk := model.DiffHunk{
		Header:   "@@ -1,3 +1,3 @@",
		OldStart: 1, OldCount: 3, NewStart: 1, NewCount: 3,
		Lines: []model.DiffLine{
			{Origin: model.OriginContext, Content: "ctx", OldLineno: new(uint32(1)), NewLineno: new(uint32(1))},
			{Origin: model.OriginDeletion, Content: "old", OldLineno: new(uint32(2))},
			{Origin: model.OriginAddition, Content: "new", NewLineno: new(uint32(2))},
			{Origin: model.OriginAddition, Content: "new2", NewLineno: new(uint32(3))},
		},
	}
	file := model.DiffFile{NewPath: new("test.rs"), Status: model.StatusModified, Hunks: []model.DiffHunk{hunk}}
	a := buildAppWithFiles([]model.DiffFile{file}, 0)

	files, adds, dels := a.DiffStat()
	assertEq(t, files, 1, "file count")
	assertEq(t, adds, 2, "additions")
	assertEq(t, dels, 1, "deletions")
	assertEq(t, a.FileCount(), 1, "FileCount")
	assertEq(t, a.ReviewedCount(), 0, "ReviewedCount")
}

func TestReviewedFileCollapsesInMultiFileView(t *testing.T) {
	files := []model.DiffFile{
		makeFileWithHunks("a.rs", []model.DiffHunk{makeHunk(1, 3)}),
		makeFileWithHunks("b.rs", []model.DiffHunk{makeHunk(1, 3)}),
	}
	a := buildAppWithFiles(files, 0)
	before := a.TotalLines()

	a.Session.File("a.rs").Reviewed = true
	a.RebuildAnnotations()

	if a.TotalLines() >= before {
		t.Error("reviewed file should collapse to its header")
	}
	assertEq(t, a.TotalLines(), len(a.LineAnnotations), "heights stay in lockstep")
	if anyAnnotation(a, func(l *AnnotatedLine) bool {
		return l.Kind == AnnDiffLine && l.FileIdx == 0
	}) {
		t.Error("reviewed file's body should not render")
	}

	// Single-file view ignores the reviewed collapse and adds a banner.
	a.IsSingleFileView = true
	a.DiffState.CurrentFileIdx = 0
	a.RebuildAnnotations()
	if !anyAnnotation(a, func(l *AnnotatedLine) bool {
		return l.Kind == AnnDiffLine && l.FileIdx == 0
	}) {
		t.Error("single-file view renders the reviewed body")
	}
	assertEq(t, a.EffectiveFileHeight(0, &a.DiffFiles[0]),
		1+a.fileRenderBodyHeight(0, &a.DiffFiles[0]), "banner + body in single-file view")
}

func TestIsCursorInOverviewAndCurrentFileHelpers(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 0)

	a.DiffState.CursorLine = 0
	assertEq(t, a.IsCursorInOverview(), true, "cursor on review header is overview")
	a.DiffState.CursorLine = 2
	assertEq(t, a.IsCursorInOverview(), false, "cursor in file body is not overview")

	path, ok := a.CurrentFilePath()
	assertEq(t, ok, true, "current file exists")
	assertEq(t, path, "test.rs", "current file path")

	empty := buildAppWithFiles(nil, 0)
	if f := empty.CurrentFile(); f != nil {
		t.Error("no current file in an empty diff")
	}
}

func TestHunkAtCursorAndLineAtCursor(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 0)

	a.DiffState.CursorLine = mustHunkHeaderLine(t, a, 0, 0)
	fileIdx, hunkIdx, ok := a.HunkAtCursor()
	assertEq(t, ok, true, "hunk at header")
	assertEq(t, fileIdx, 0, "file idx")
	assertEq(t, hunkIdx, 0, "hunk idx")

	a.DiffState.CursorLine = hunkDiffLine(t, a, 0, 0)
	_, _, ok = a.HunkAtCursor()
	assertEq(t, ok, true, "hunk at diff line")

	lineno, side, ok := a.LineAtCursor()
	assertEq(t, ok, true, "line at cursor")
	assertEq(t, lineno, uint32(1), "line number")
	assertEq(t, side, model.LineSideNew, "prefers new side")

	a.DiffState.CursorLine = 0
	if _, _, ok := a.HunkAtCursor(); ok {
		t.Error("review header is not a hunk")
	}
	if _, _, ok := a.LineAtCursor(); ok {
		t.Error("review header is not a diff line")
	}
}

func TestMoveCursorToAnnotationSyncsFileAndScroll(t *testing.T) {
	files := []model.DiffFile{
		makeFileWithHunks("a.rs", []model.DiffHunk{makeHunk(1, 3)}),
		makeFileWithHunks("b.rs", []model.DiffHunk{makeHunk(1, 3)}),
	}
	a := buildAppWithFiles(files, 0)
	a.DiffState.ViewportHeight = 5

	target := hunkDiffLine(t, a, 1, 0)
	a.MoveCursorToAnnotation(target)
	assertEq(t, a.DiffState.CursorLine, target, "cursor moved")
	assertEq(t, a.DiffState.CurrentFileIdx, 1, "current file synced")
	assertEq(t, a.DiffState.ScrollOffset, target+1-5, "scrolled to reveal")

	a.MoveCursorToAnnotation(0)
	assertEq(t, a.DiffState.ScrollOffset, 0, "scrolled back up")
}

func TestSyncViewportWidthRebuildsOnChange(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 3)})
	a := buildAppWithFiles([]model.DiffFile{file}, 0)

	a.SyncViewportWidth(80)
	assertEq(t, a.DiffState.ViewportWidth, 80, "viewport width set")
	assertEq(t, a.TotalLines(), len(a.LineAnnotations), "annotations rebuilt")
	a.SyncViewportWidth(80) // no-op path
}

func TestJumpToFileExpandsAncestorsAndSelectsTreeRow(t *testing.T) {
	files := []model.DiffFile{
		makeFileWithHunks("src/deep/x.rs", []model.DiffHunk{makeHunk(1, 3)}),
		makeFileWithHunks("top.rs", []model.DiffHunk{makeHunk(1, 3)}),
	}
	a := buildAppWithFiles(files, 0)
	a.CollapseAllDirs()

	// After sorting, src/deep/x.rs sits after top.rs (root dir "." first).
	var deepIdx int
	for i := range a.DiffFiles {
		if a.DiffFiles[i].DisplayPath() == "src/deep/x.rs" {
			deepIdx = i
		}
	}
	a.JumpToFile(deepIdx)

	if !a.ExpandedDirs["src"] || !a.ExpandedDirs["src/deep"] {
		t.Error("ancestors should be expanded")
	}
	item, ok := a.SelectedTreeItem()
	if !ok || item.IsDir || item.FileIdx != deepIdx {
		t.Errorf("selected tree item = %+v, want file %d", item, deepIdx)
	}
	// Cursor skipped the file-header decoration to the hunk header.
	assertEq(t, a.LineAnnotations[a.DiffState.CursorLine].Kind, AnnHunkHeader,
		"cursor lands on content")
}

func TestUpdateCurrentFileFromCursorTracksFiles(t *testing.T) {
	files := []model.DiffFile{
		makeFileWithHunks("a.rs", []model.DiffHunk{makeHunk(1, 3)}),
		makeFileWithHunks("b.rs", []model.DiffHunk{makeHunk(1, 3)}),
	}
	a := buildAppWithFiles(files, 0)
	a.DiffState.ViewportHeight = 30
	a.DiffState.VisibleLineCount = 30

	a.DiffState.CursorLine = hunkDiffLine(t, a, 1, 0)
	a.CursorDown(0)
	// CursorDown(0) does not move, so force the update via CursorDown(1).
	a.CursorDown(1)
	assertEq(t, a.DiffState.CurrentFileIdx, 1, "current file follows cursor")

	// Cursor above all files resolves to file 0.
	a.DiffState.CursorLine = 1
	a.CursorUp(1)
	assertEq(t, a.DiffState.CurrentFileIdx, 0, "overview resolves to file 0")
}

func TestSingleFileViewFileListFollow(t *testing.T) {
	files := []model.DiffFile{
		makeFileWithHunks("a.rs", []model.DiffHunk{makeHunk(1, 3)}),
		makeFileWithHunks("b.rs", []model.DiffHunk{makeHunk(1, 3)}),
	}
	a := buildAppWithFiles(files, 0)
	a.IsSingleFileView = true
	a.DiffState.CurrentFileIdx = 0
	a.RebuildAnnotations()

	a.FileListDown(1)
	assertEq(t, a.DiffState.CurrentFileIdx, 1, "file list j reveals the file immediately")
	a.FileListUp(1)
	assertEq(t, a.DiffState.CurrentFileIdx, 0, "file list k reveals the file immediately")
}

func TestExpandGapErrorForInvalidGap(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(1, 5)})
	a := buildAppWithFiles([]model.DiffFile{file}, 0)

	// Gap before hunk 0 has zero width (hunk starts at line 1).
	err := a.ExpandGap(GapID{FileIdx: 0, HunkIdx: 0}, ExpandBoth, nil)
	if err == nil {
		t.Error("expanding an empty gap should error")
	}
}

func TestGapSizeVariants(t *testing.T) {
	file := makeFileWithHunks("test.rs", []model.DiffHunk{makeHunk(10, 5), makeHunk(30, 5)})
	a := buildAppWithFiles([]model.DiffFile{file}, 50)

	size, ok := a.GapSize(GapID{FileIdx: 0, HunkIdx: 0})
	assertEq(t, ok, true, "top gap exists")
	assertEq(t, size, uint32(9), "top gap size")

	size, ok = a.GapSize(GapID{FileIdx: 0, HunkIdx: 1})
	assertEq(t, ok, true, "between gap exists")
	assertEq(t, size, uint32(15), "between gap size")

	size, ok = a.GapSize(GapID{FileIdx: 0, HunkIdx: 2})
	assertEq(t, ok, true, "eof gap exists")
	assertEq(t, size, uint32(16), "eof gap size")

	if _, ok := a.GapSize(GapID{FileIdx: 9, HunkIdx: 0}); ok {
		t.Error("out-of-range file has no gap")
	}
}

// TestFileRenderBodyHeightMatchesAnnotations pins the property the shared
// walk exists for: the height arithmetic and the annotation list agree to
// the row, in both view modes, with file and line comments present.
func TestFileRenderBodyHeightMatchesAnnotations(t *testing.T) {
	for _, mode := range []DiffViewMode{ViewUnified, ViewSideBySide} {
		a := newTestApp(t)
		a.DiffViewMode = mode
		a.DiffState.ViewportWidth = 80
		file := &a.DiffFiles[0]
		path := file.DisplayPath()
		review := a.Session.File(path)
		if review == nil {
			t.Fatalf("file %s missing from session", path)
		}
		review.AddFileComment(model.NewComment("file level", model.CommentTypeFromID("note"), nil))
		side := model.LineSideNew
		review.AddLineComment(3, model.NewComment("on line three\nsecond line", model.CommentTypeFromID("issue"), &side))
		a.RebuildAnnotations()

		// Body rows are everything after this file's header.
		body := 0
		for i, ann := range a.LineAnnotations {
			if ann.Kind == AnnFileHeader && ann.FileIdx == 0 {
				body = len(a.LineAnnotations) - i - 1
				break
			}
		}
		if got := a.fileRenderBodyHeight(0, file); got != body {
			t.Errorf("mode %v: fileRenderBodyHeight = %d, annotations after the header = %d", mode, got, body)
		}
	}
}
