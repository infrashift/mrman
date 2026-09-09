package app

import (
	"strings"
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
	info := &vcs.Info{RootPath: "/tmp", HeadCommit: "abc123", BranchName: new("main"), Type: vcs.TypeGit}
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

// TestCommitMessagePseudoFileHasNoSlash guards a collision the patch backend
// introduced. The file tree splits a display path on "/" to build its
// directories, and a patch's ShortID is its series position — so
// "Commit Message (3/3)" rendered as a directory "Commit Message (3" holding
// a file "3)". A git commit's ShortID is a SHA, so this never came up before.
func TestCommitMessagePseudoFileHasNoSlash(t *testing.T) {
	body := "Strip quotes and add merge()\n\nSigned-off-by: Dev <d@e.o>\n"
	files := InsertCommitMessageIfSingle(nil, []vcs.CommitInfo{{
		ID: "patch-0003", ShortID: "3/3", Summary: "Strip quotes", Body: &body,
	}})

	if len(files) != 1 {
		t.Fatalf("got %d files, want the commit-message pseudo-file", len(files))
	}
	path := files[0].DisplayPath()
	if strings.Contains(path, "/") {
		t.Errorf("pseudo-file path %q contains a slash and will split in the tree", path)
	}
	if !strings.Contains(path, "3-3") {
		t.Errorf("path = %q, want the series position preserved", path)
	}

	// A git-style ShortID is unaffected.
	sha := InsertCommitMessageIfSingle(nil, []vcs.CommitInfo{{
		ID: "abc1234def", ShortID: "abc1234", Summary: "x", Body: &body,
	}})
	if got := sha[0].DisplayPath(); got != "Commit Message (abc1234)" {
		t.Errorf("path = %q, want the SHA form unchanged", got)
	}
}

// seriesApp builds an app over two "patches" that both touch one path, which
// is the shape that exposed the collision: session state is keyed by display
// path, so both entries share one FileReview.
func seriesApp(t *testing.T) *App {
	t.Helper()
	info := &vcs.Info{RootPath: "/inbox", HeadCommit: "abc123", BranchName: new("series"), Type: vcs.TypePatch}
	one := makeFileWithHunks("net/foo.c", []model.DiffHunk{makeHunk(1, 3)})
	one.CommitID, one.SourceIndex = "patch-0001", 0
	two := makeFileWithHunks("net/foo.c", []model.DiffHunk{makeHunk(1, 3)})
	two.CommitID, two.SourceIndex = "patch-0002", 1

	session := model.NewReviewSession(info.RootPath, info.HeadCommit, info.BranchName, model.SourcePatch)
	a := NewApp(&mockVcs{info: info, totalLines: 20}, info, []model.DiffFile{one, two}, session,
		DiffSource{Kind: DiffSourcePatch})
	a.InstallReviewCommits([]vcs.CommitInfo{
		{ID: "patch-0002", ShortID: "2/2", Summary: "second"},
		{ID: "patch-0001", ShortID: "1/2", Summary: "first"},
	})
	return a
}

func lineCommentRows(a *App) int {
	return countAnnotations(a, func(ann *AnnotatedLine) bool { return ann.Kind == AnnLineComment })
}

// TestCommentStaysWithItsPatch is the bug this exists to fix. Two patches
// touching one file share a FileReview because session state is keyed by
// path, so a single comment rendered under both entries — and at export time
// it would be quoted against whichever diff matched first.
func TestCommentStaysWithItsPatch(t *testing.T) {
	a := seriesApp(t)

	id := "patch-0001"
	side := model.LineSideNew
	c := model.NewComment("about the first patch", model.CommentTypeFromID("issue"), &side)
	c.CommitID = &id
	a.Session.File("net/foo.c").AddLineComment(2, c)
	a.RebuildAnnotations()

	// One comment renders as one box, not two. A box is several rows, so
	// compare against an unstamped comment rendered the same way.
	stamped := lineCommentRows(a)

	a.Session.File("net/foo.c").LineComments = map[uint32][]*model.Comment{}
	loose := model.NewComment("belongs to neither", model.CommentTypeFromID("note"), &side)
	a.Session.File("net/foo.c").AddLineComment(2, loose)
	a.RebuildAnnotations()
	unstamped := lineCommentRows(a)

	if stamped == 0 {
		t.Fatal("a stamped comment must still render under its own patch")
	}
	if unstamped != stamped*2 {
		t.Fatalf("expected an unstamped comment to render under both entries "+
			"(%d rows) and a stamped one under just its own (%d)", unstamped, stamped)
	}
}

// TestUnstampedCommentsStillShowEverywhere keeps the fix from hiding work.
// A comment written before entries carried an id, or on an ordinary diff,
// has nothing to disambiguate and must not vanish.
func TestUnstampedCommentsStillShowEverywhere(t *testing.T) {
	a := seriesApp(t)
	side := model.LineSideNew
	a.Session.File("net/foo.c").AddLineComment(2,
		model.NewComment("legacy", model.CommentTypeFromID("note"), &side))
	a.RebuildAnnotations()

	if lineCommentRows(a) == 0 {
		t.Error("an unstamped comment must still render")
	}
}

// TestNewCommentTakesTheFilesPatch covers the stamping rule: the patch comes
// from the file under the cursor, not merely from the strip selection, which
// is nil for a multi-patch view.
func TestNewCommentTakesTheFilesPatch(t *testing.T) {
	a := seriesApp(t)

	a.DiffState.CurrentFileIdx = 1
	got := a.CommitIDForNewComment()
	if got == nil {
		t.Fatal("a file carrying a patch id must stamp it")
	}
	if *got != a.DiffFiles[1].CommitID {
		t.Errorf("stamped %q, want the file's own patch %q", *got, a.DiffFiles[1].CommitID)
	}

	a.DiffState.CurrentFileIdx = 0
	if got := a.CommitIDForNewComment(); got == nil || *got != a.DiffFiles[0].CommitID {
		t.Errorf("stamped %v, want %q", got, a.DiffFiles[0].CommitID)
	}
}

// TestPatchLabelOnlyWhenItDisambiguates keeps the tag from becoming noise:
// narrowed to a single patch every row would carry the same one.
func TestPatchLabelOnlyWhenItDisambiguates(t *testing.T) {
	a := seriesApp(t)
	if got := a.FilePatchLabel(0); got != "1/2" {
		t.Errorf("label = %q, want 1/2 when the diff spans two patches", got)
	}
	if !a.FilePathIsAmbiguous(0) {
		t.Error("both entries share a path, so the path is ambiguous")
	}

	// Narrowed to one patch: nothing to tell apart.
	a.DiffFiles = a.DiffFiles[:1]
	if got := a.FilePatchLabel(0); got != "" {
		t.Errorf("label = %q, want none when the diff is one patch", got)
	}
	if a.FilePathIsAmbiguous(0) {
		t.Error("a lone entry's path is not ambiguous")
	}
}

// TestOrdinaryDiffsAreUnaffected is the regression guard: a working-tree or
// squashed-range diff carries no patch id, and nothing here may change it.
func TestOrdinaryDiffsAreUnaffected(t *testing.T) {
	a, _ := gappedApp(t, DiffSourceWorkingTree)
	side := model.LineSideNew
	a.Session.File("src/x.go").AddLineComment(2,
		model.NewComment("ordinary", model.CommentTypeFromID("note"), &side))
	a.RebuildAnnotations()

	if lineCommentRows(a) == 0 {
		t.Error("a comment on an ordinary diff must render")
	}
	if got := a.FilePatchLabel(0); got != "" {
		t.Errorf("label = %q, want none outside a series", got)
	}
	if a.CommitIDForNewComment() != nil {
		t.Error("an ordinary diff stamps no patch id")
	}
}
