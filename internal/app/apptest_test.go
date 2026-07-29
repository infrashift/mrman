package app

// apptest_test.go ports the shared App test builders from tuicr's
// src/app/tests (MockVcs, make_hunk, make_file_with_hunks,
// build_app_with_files).

import (
	"fmt"
	"testing"

	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/syntax"
	"github.com/infrashift/mrman/internal/vcs"
)

// mockVcs serves synthetic context lines "line N" for a file of totalLines
// lines, mirroring tuicr's MockVcs. totalLines == 0 mirrors DummyVcs /
// StubVcs (no context, zero-length files).
type mockVcs struct {
	vcs.UnsupportedBase
	info       *vcs.Info
	totalLines uint32
}

func (m *mockVcs) Info() *vcs.Info { return m.info }

func (m *mockVcs) WorkingTreeDiff(*syntax.Highlighter) ([]model.DiffFile, error) {
	return nil, fmt.Errorf("no changes")
}

func (m *mockVcs) FetchContextLines(_ string, _ model.FileStatus, _ *string, start, end uint32) ([]model.DiffLine, error) {
	var result []model.DiffLine
	for lineNum := start; lineNum <= min(end, m.totalLines); lineNum++ {
		n := lineNum
		oldNo, newNo := n, n
		result = append(result, model.DiffLine{
			Origin:    model.OriginContext,
			Content:   fmt.Sprintf("line %d", lineNum),
			OldLineno: &oldNo,
			NewLineno: &newNo,
		})
	}
	return result, nil
}

func (m *mockVcs) FileLineCount(string, model.FileStatus, *string) (uint32, error) {
	return m.totalLines, nil
}

func u32(v uint32) *uint32 { return &v }

func intPtr(v int) *int { return &v }

func strPtr(s string) *string { return &s }

// makeHunk builds a context-only hunk of newCount lines starting at
// newStart with old == new linenos.
func makeHunk(newStart, newCount uint32) model.DiffHunk {
	var lines []model.DiffLine
	for i := uint32(0); i < newCount; i++ {
		lines = append(lines, model.DiffLine{
			Origin:    model.OriginContext,
			Content:   fmt.Sprintf("hunk line %d", newStart+i),
			OldLineno: u32(newStart + i),
			NewLineno: u32(newStart + i),
		})
	}
	return model.DiffHunk{
		Header:   fmt.Sprintf("@@ -%d,%d +%d,%d @@", newStart, newCount, newStart, newCount),
		Lines:    lines,
		OldStart: newStart,
		OldCount: newCount,
		NewStart: newStart,
		NewCount: newCount,
	}
}

func makeFileWithHunks(path string, hunks []model.DiffHunk) model.DiffFile {
	return model.DiffFile{
		NewPath:     strPtr(path),
		Status:      model.StatusModified,
		Hunks:       hunks,
		ContentHash: model.ComputeContentHash(hunks),
	}
}

func buildAppWithFiles(files []model.DiffFile, totalLines uint32) *App {
	info := &vcs.Info{
		RootPath:   "/tmp",
		HeadCommit: "abc123",
		BranchName: strPtr("main"),
		Type:       vcs.TypeGit,
	}
	session := model.NewReviewSession(info.RootPath, info.HeadCommit, info.BranchName, model.SourceWorkingTree)
	return NewApp(
		&mockVcs{info: info, totalLines: totalLines},
		info,
		files,
		session,
		DiffSource{Kind: DiffSourceWorkingTree},
	)
}

// hunkDiffLine finds the first DiffLine annotation of the given hunk.
func hunkDiffLine(t *testing.T, a *App, fileIdx, hunkIdx int) int {
	t.Helper()
	for i := range a.LineAnnotations {
		ann := &a.LineAnnotations[i]
		if ann.Kind == AnnDiffLine && ann.FileIdx == fileIdx && ann.HunkIdx == hunkIdx {
			return i
		}
	}
	t.Fatal("missing hunk diff annotation")
	return -1
}

// countAnnotations counts annotations satisfying pred.
func countAnnotations(a *App, pred func(*AnnotatedLine) bool) int {
	n := 0
	for i := range a.LineAnnotations {
		if pred(&a.LineAnnotations[i]) {
			n++
		}
	}
	return n
}

// anyAnnotation reports whether any annotation satisfies pred.
func anyAnnotation(a *App, pred func(*AnnotatedLine) bool) bool {
	return countAnnotations(a, pred) > 0
}

// mustHunkHeaderLine is HunkHeaderLine that fails the test when missing.
func mustHunkHeaderLine(t *testing.T, a *App, fileIdx, hunkIdx int) int {
	t.Helper()
	idx, ok := a.HunkHeaderLine(fileIdx, hunkIdx)
	if !ok {
		t.Fatal("missing hunk header")
	}
	return idx
}

func cursorNewLineno(a *App) *uint32 {
	ann := &a.LineAnnotations[a.DiffState.CursorLine]
	switch ann.Kind {
	case AnnDiffLine, AnnSideBySideLine:
		return ann.NewLineno
	case AnnExpandedContext:
		if line := a.GetExpandedLine(ann.GapID, ann.LineIdx); line != nil {
			return line.NewLineno
		}
	}
	return nil
}

func cursorOldLineno(a *App) *uint32 {
	ann := &a.LineAnnotations[a.DiffState.CursorLine]
	switch ann.Kind {
	case AnnDiffLine, AnnSideBySideLine:
		return ann.OldLineno
	case AnnExpandedContext:
		if line := a.GetExpandedLine(ann.GapID, ann.LineIdx); line != nil {
			return line.OldLineno
		}
	}
	return nil
}

func assertEq[T comparable](t *testing.T, got, want T, msg string) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", msg, got, want)
	}
}

func assertLineno(t *testing.T, got *uint32, want uint32, msg string) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: got nil, want %d", msg, want)
	}
	if *got != want {
		t.Errorf("%s: got %d, want %d", msg, *got, want)
	}
}
