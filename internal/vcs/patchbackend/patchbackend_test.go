package patchbackend

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/patch"
	"github.com/infrashift/mrman/internal/vcs"
)

// fixture copies a checked-in artifact from the patch package into a temp dir,
// so tests exercise a real file at a real path (the backend derives identity
// from both).
func fixture(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join("..", "..", "patch", "testdata", name)
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	dst := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

func open(t *testing.T, name string) *Backend {
	t.Helper()
	b, err := New(fixture(t, name), patch.Options{})
	if err != nil {
		t.Fatalf("New(%s): %v", name, err)
	}
	return b
}

// TestSatisfiesBackend is the compile-time contract, restated as a test so a
// missing method is a test failure rather than a build error elsewhere.
func TestSatisfiesBackend(t *testing.T) {
	var _ vcs.Backend = (*Backend)(nil)
}

func TestInfoDescribesTheArtifact(t *testing.T) {
	path := fixture(t, "format-patch-single.patch")
	b, err := New(path, patch.Options{})
	if err != nil {
		t.Fatal(err)
	}
	info := b.Info()

	if info.RootPath != filepath.Dir(path) {
		t.Errorf("RootPath = %q, want the artifact's directory", info.RootPath)
	}
	if info.Type != vcs.TypePatch {
		t.Errorf("Type = %q, want %q", info.Type, vcs.TypePatch)
	}
	if info.BranchName == nil || *info.BranchName != "format-patch-single" {
		t.Errorf("BranchName = %v, want the filename stem", info.BranchName)
	}
	// Bare hex: the session anchor falls back to the first seven characters of
	// this when there is no branch, and a colon there makes the slug
	// unparseable.
	if len(info.HeadCommit) != 16 || strings.ContainsAny(info.HeadCommit, ":-") {
		t.Errorf("HeadCommit = %q, want 16 bare hex characters", info.HeadCommit)
	}
}

// TestSeriesPresentsAsCommits is the design in one assertion: each patch is a
// commit row, so every multi-commit affordance mrman already has applies.
func TestSeriesPresentsAsCommits(t *testing.T) {
	b := open(t, "format-patch-series-3.mbox")

	commits, err := b.RecentCommits(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 3 {
		t.Fatalf("got %d commits, want 3 (cover letter + 2 patches)", len(commits))
	}

	// Newest first, which is the order selectors render.
	if !strings.Contains(commits[0].Summary, "use the counter") {
		t.Errorf("commits[0].Summary = %q, want the last patch first", commits[0].Summary)
	}
	if commits[0].ShortID != "2/2" {
		t.Errorf("ShortID = %q, want the series position", commits[0].ShortID)
	}
	if !strings.Contains(commits[0].Author, "Dev Eloper") {
		t.Errorf("Author = %q", commits[0].Author)
	}
	// The changelog rides along as the commit body, which is what puts it on
	// screen as a reviewable pseudo-file.
	if commits[0].Body == nil || !strings.Contains(*commits[0].Body, "Signed-off-by") {
		t.Errorf("Body = %v, want the changelog", commits[0].Body)
	}
	if commits[0].ID == "" {
		t.Error("every patch needs a stable id to scope comments by")
	}
}

// TestCommitIDsPreferMessageID keeps the identity aligned with the wider
// world's, falling back only when the artifact carries none.
func TestCommitIDsPreferMessageID(t *testing.T) {
	withIDs := open(t, "format-patch-series-3.mbox")
	commits, err := withIDs.RecentCommits(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(commits[0].ID, "@example.org") {
		t.Errorf("ID = %q, want the Message-Id", commits[0].ID)
	}

	without := open(t, "format-patch-single.patch")
	commits, err = without.RecentCommits(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if commits[0].ID != "patch-0001" {
		t.Errorf("ID = %q, want the positional fallback", commits[0].ID)
	}
}

func TestCommitsInfoLooksUpByID(t *testing.T) {
	b := open(t, "format-patch-series-3.mbox")
	all, err := b.RecentCommits(0, 0)
	if err != nil {
		t.Fatal(err)
	}

	got, err := b.CommitsInfo([]string{all[1].ID, all[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d commits, want 2", len(got))
	}
	if _, err := b.CommitsInfo([]string{"nope"}); err != nil {
		t.Errorf("an unknown id should be skipped, not error: %v", err)
	}
}

// TestCommitRangeDiffSelectsPatches is what makes each patch its own review
// target: selecting one yields only its diff.
func TestCommitRangeDiffSelectsPatches(t *testing.T) {
	b := open(t, "format-patch-series-3.mbox")
	all, err := b.RecentCommits(0, 0)
	if err != nil {
		t.Fatal(err)
	}

	// all[0] is the newest patch ("use the counter").
	files, err := b.CommitRangeDiff(vcs.ResolvedRevisionRange{CommitIDs: []string{all[0].ID}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].DisplayPath() != "net/foo.c" {
		t.Fatalf("got %d files, want just net/foo.c", len(files))
	}
	if !hunkContains(files, "count++;") {
		t.Error("selecting the second patch should show its own change")
	}
	if hunkContains(files, "int n;") {
		t.Error("the first patch's change leaked into the second's diff")
	}

	// Both patches touch one file; selecting both yields both diffs rather
	// than colliding, because each is parsed separately.
	both, err := b.CommitRangeDiff(
		vcs.ResolvedRevisionRange{CommitIDs: []string{all[0].ID, all[1].ID}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(both) != 2 {
		t.Errorf("got %d files, want 2 — one per patch touching net/foo.c", len(both))
	}
}

func TestCommitRangeDiffWithNoSelection(t *testing.T) {
	b := open(t, "format-patch-series-3.mbox")
	if _, err := b.CommitRangeDiff(vcs.ResolvedRevisionRange{}, nil); !errors.Is(err, errs.ErrNoChanges) {
		t.Errorf("err = %v, want ErrNoChanges", err)
	}
}

// TestWorkingTreeDiffRereadsFromDisk is the :e contract. The reload path calls
// WorkingTreeDiff for any source it does not special-case, so re-reading here
// is the whole of patch reload support.
func TestWorkingTreeDiffRereadsFromDisk(t *testing.T) {
	path := fixture(t, "format-patch-single.patch")
	b, err := New(path, patch.Options{})
	if err != nil {
		t.Fatal(err)
	}
	before := b.Info().HeadCommit

	files, err := b.WorkingTreeDiff(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}

	// Rewrite the artifact, as a v2 landing in the same file would.
	replacement := strings.ReplaceAll(readFile(t, path), "return -EINVAL;", "return -ENODEV;")
	if err := os.WriteFile(path, []byte(replacement), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err = b.WorkingTreeDiff(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !hunkContains(files, "return -ENODEV;") {
		t.Error("reload did not pick up the rewritten artifact")
	}
	if b.Info().HeadCommit == before {
		t.Error("identity must move with the artifact's bytes")
	}
}

// TestChangeStatusIsQuiet covers the bogus UNSTAGED row. Without this the
// selector offers a row that fails the instant it is chosen.
func TestChangeStatusIsQuiet(t *testing.T) {
	b := open(t, "format-patch-single.patch")
	status, err := b.ChangeStatus()
	if err != nil {
		t.Fatalf("ChangeStatus: %v", err)
	}
	if status.Staged || status.Unstaged {
		t.Errorf("ChangeStatus = %+v, want neither side reporting changes", status)
	}
}

// TestContextIsUnavailable pins the honest answers for the two things a patch
// genuinely cannot provide.
func TestContextIsUnavailable(t *testing.T) {
	b := open(t, "format-patch-single.patch")

	lines, err := b.FetchContextLines("drivers/foo.c", model.StatusModified, nil, 1, 10)
	if err != nil || len(lines) != 0 {
		t.Errorf("FetchContextLines = (%d lines, %v), want (0, nil)", len(lines), err)
	}
	if _, err := b.FileLineCount("drivers/foo.c", model.StatusModified, nil); err == nil {
		t.Error("FileLineCount must not invent a length for a file that is not here")
	}
}

func TestRejectsDirectoryAndMissingFile(t *testing.T) {
	if _, err := New(t.TempDir(), patch.Options{}); err == nil {
		t.Error("a directory is not a patch artifact")
	}
	if _, err := New(filepath.Join(t.TempDir(), "nope.patch"), patch.Options{}); err == nil {
		t.Error("a missing file should error")
	}
}

func TestSanitizeStem(t *testing.T) {
	cases := map[string]string{
		"v2_net_fix":           "v2_net_fix",
		"0001-net-fix-foo":     "0001-net-fix-foo",
		"has:colon":            "has-colon", // the slug-breaking character
		"has/slash":            "has-slash", // would break segment counting
		"has@at":               "has-at",    // would break the anchor split
		"spaces and things":    "spaces-and-things",
		"--leading-trailing--": "leading-trailing",
		"":                     "patch",
		"!!!":                  "patch",
	}
	for in, want := range cases {
		if got := SanitizeStem(in); got != want {
			t.Errorf("SanitizeStem(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestBareDiffHasNoMailMetadata covers the airgapped case: a plain .diff with
// no mail around it still reviews, it just has less to say about itself.
func TestBareDiffHasNoMailMetadata(t *testing.T) {
	b := open(t, "quilt-p1.diff")
	commits, err := b.RecentCommits(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 1 {
		t.Fatalf("got %d commits, want 1", len(commits))
	}
	if commits[0].Summary != "patch 1" {
		t.Errorf("Summary = %q, want the positional fallback", commits[0].Summary)
	}

	files, err := b.WorkingTreeDiff(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].DisplayPath() != "drivers/bar.c" {
		t.Errorf("got %v, want [drivers/bar.c]", files)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func hunkContains(files []model.DiffFile, want string) bool {
	for i := range files {
		for _, h := range files[i].Hunks {
			for _, l := range h.Lines {
				if strings.Contains(l.Content, want) {
					return true
				}
			}
		}
	}
	return false
}

// TestCoverLetterSelectionSaysWhy covers a normal thing to do: the cover
// letter is a row in the commit strip like any other, and selecting it used
// to report "no changes to review" — which sends the reviewer looking for an
// empty patch rather than telling them the message is prose.
func TestCoverLetterSelectionSaysWhy(t *testing.T) {
	b := open(t, "format-patch-series-3.mbox")
	commits, err := b.RecentCommits(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Oldest row is the 0/2 cover letter.
	cover := commits[len(commits)-1]
	if cover.ShortID != "0/2" {
		t.Fatalf("expected the cover letter last, got %q", cover.ShortID)
	}

	_, err = b.CommitRangeDiff(vcs.ResolvedRevisionRange{CommitIDs: []string{cover.ID}}, nil)
	if err == nil {
		t.Fatal("selecting a cover letter should report why there is nothing to show")
	}
	if errors.Is(err, errs.ErrNoChanges) {
		t.Error("ErrNoChanges reads as an empty patch; the cover letter needs its own words")
	}
	if !strings.Contains(err.Error(), "cover letter") {
		t.Errorf("err = %q, want it to name the cover letter", err)
	}

	// A selection that includes a real patch still works.
	withReal := vcs.ResolvedRevisionRange{CommitIDs: []string{cover.ID, commits[0].ID}}
	if _, err := b.CommitRangeDiff(withReal, nil); err != nil {
		t.Errorf("a selection containing a real patch should load: %v", err)
	}
}

// TestRejectsNonRegularFiles covers `--patch <(git format-patch --stdout …)`,
// which reads like it should work and cannot: the backend re-reads the
// artifact on every reload — that is how :e works — and a pipe can only be
// read once. Without this the second read comes back empty and a perfectly
// good patch is reported as unparseable.
func TestRejectsNonRegularFiles(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe.patch")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("cannot create a fifo here: %v", err)
	}

	_, err := New(fifo, patch.Options{})
	if err == nil {
		t.Fatal("a pipe should be refused, not half-read")
	}
	if !strings.Contains(err.Error(), "regular file") {
		t.Errorf("err = %q, want it to name the problem", err)
	}
	if !strings.Contains(err.Error(), "redirect to a file") {
		t.Errorf("err = %q, want it to say what to do instead", err)
	}
}
