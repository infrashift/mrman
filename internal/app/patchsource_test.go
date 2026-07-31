package app

import (
	"testing"

	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/vcs"
)

// countingVcs records whether the app ever asked it for file lengths, which
// is the eager call NewApp makes for every file at startup.
type countingVcs struct {
	mockVcs
	lineCountCalls int
}

func (c *countingVcs) FileLineCount(path string, s model.FileStatus, ref *string) (uint32, error) {
	c.lineCountCalls++
	return c.mockVcs.FileLineCount(path, s, ref)
}

// gappedApp builds an app over one file whose two hunks are far enough apart
// to leave hidden lines between them.
func gappedApp(t *testing.T, kind DiffSourceKind) (*App, *countingVcs) {
	t.Helper()
	info := &vcs.Info{RootPath: "/tmp", HeadCommit: "abc123", BranchName: strPtr("main"), Type: vcs.TypeGit}
	backend := &countingVcs{mockVcs: mockVcs{info: info, totalLines: 100}}
	session := model.NewReviewSession(info.RootPath, info.HeadCommit, info.BranchName, model.SourceWorkingTree)
	files := []model.DiffFile{
		makeFileWithHunks("src/x.go", []model.DiffHunk{makeHunk(1, 3), makeHunk(40, 3)}),
	}
	return NewApp(backend, info, files, session, DiffSource{Kind: kind}), backend
}

func countGapAnnotations(a *App) int {
	return countAnnotations(a, func(ann *AnnotatedLine) bool {
		return ann.Kind == AnnExpander || ann.Kind == AnnHiddenLines
	})
}

// TestPatchSourceDrawsNoGaps is the difference between an honest review and a
// dead keypress. A patch carries only the context inside its own hunks, so an
// expander between them would invite the reviewer to press a key that can
// never do anything — with no message explaining why.
func TestPatchSourceDrawsNoGaps(t *testing.T) {
	normal, _ := gappedApp(t, DiffSourceWorkingTree)
	if countGapAnnotations(normal) == 0 {
		t.Fatal("precondition: this fixture should produce gap rows for a normal review")
	}

	patched, _ := gappedApp(t, DiffSourcePatch)
	if got := countGapAnnotations(patched); got != 0 {
		t.Errorf("patch review rendered %d gap rows, want 0", got)
	}
}

// TestPatchSourceNeverAsksForFileLength covers the eager call NewApp makes for
// every file. The patch backend cannot answer it — the file it applies to is
// not here — and an error per file at startup is noise, so the source is
// gated before the question is asked.
func TestPatchSourceNeverAsksForFileLength(t *testing.T) {
	_, normalBackend := gappedApp(t, DiffSourceWorkingTree)
	if normalBackend.lineCountCalls == 0 {
		t.Fatal("precondition: a normal review measures its files at startup")
	}

	patched, patchBackend := gappedApp(t, DiffSourcePatch)
	if patchBackend.lineCountCalls != 0 {
		t.Errorf("patch review called FileLineCount %d times, want 0", patchBackend.lineCountCalls)
	}
	if len(patched.FileLineCountCache) != 0 {
		t.Errorf("line-count cache = %v, want empty", patched.FileLineCountCache)
	}
}

// TestPatchSourceUsesTheNoContextProvider pins the seam. Making expansion a
// capability of the provider rather than another switch on the diff source
// means a future patch review that *can* resolve context (against a local
// object store, say) turns it on in one place.
func TestPatchSourceUsesTheNoContextProvider(t *testing.T) {
	patched, _ := gappedApp(t, DiffSourcePatch)
	if _, ok := patched.contextProvider().(noContextProvider); !ok {
		t.Errorf("provider = %T, want noContextProvider", patched.contextProvider())
	}
	if patched.contextGapsEnabled() {
		t.Error("contextGapsEnabled must be false for a patch review")
	}

	normal, _ := gappedApp(t, DiffSourceWorkingTree)
	if !normal.contextGapsEnabled() {
		t.Error("a checkout has the rest of the file on disk")
	}
}

// TestNoContextProviderAnswersEmptily keeps a stray call harmless: nothing to
// expand is not an error the reviewer can act on.
func TestNoContextProviderAnswersEmptily(t *testing.T) {
	var p noContextProvider
	path := "src/x.go"

	lines, err := p.FetchContextLines(nil, &path, model.StatusModified, 1, 10)
	if err != nil || len(lines) != 0 {
		t.Errorf("FetchContextLines = (%d, %v), want (0, nil)", len(lines), err)
	}
	count, err := p.FileLineCount(nil, &path, model.StatusModified)
	if err != nil || count != 0 {
		t.Errorf("FileLineCount = (%d, %v), want (0, nil)", count, err)
	}
}

// TestPatchSourceIsNotAWorktree keeps the post-editor reload away from a patch
// review: there is no worktree behind it for an editor to have changed.
func TestPatchSourceIsNotAWorktree(t *testing.T) {
	if (DiffSource{Kind: DiffSourcePatch}).IncludesWorktreeChanges() {
		t.Error("a patch artifact is not a worktree")
	}
}
