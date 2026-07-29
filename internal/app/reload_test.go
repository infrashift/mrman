package app

import (
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/model"
)

// diffFile builds a one-hunk file with the given content lines.
func diffFile(path string, contents ...string) model.DiffFile {
	p := path
	lines := make([]model.DiffLine, len(contents))
	for i, c := range contents {
		n := uint32(i + 1) //nolint:gosec // test fixture line counts stay tiny
		lines[i] = model.DiffLine{Origin: model.OriginAddition, Content: c, NewLineno: &n}
	}
	hunks := []model.DiffHunk{{
		Lines: lines, OldStart: 1, OldCount: 0,
		NewStart: 1, NewCount: uint32(len(lines)), //nolint:gosec // ditto
	}}
	return model.DiffFile{
		NewPath: &p, Status: model.StatusModified, Hunks: hunks,
		ContentHash: model.ComputeContentHash(hunks),
	}
}

func TestReloadKeepsCommentsButInvalidatesStaleReviews(t *testing.T) {
	a := newTestApp(t)
	path := a.DiffFiles[0].DisplayPath()
	a.ToggleReviewedForFileIdx(0, false)
	a.Session.ReviewComments = append(a.Session.ReviewComments,
		model.NewComment("looks off", model.CommentTypeFromID("note"), nil))

	// Same path, different content.
	files := []model.DiffFile{diffFile(path, "var A = 11", "var C = 3")}
	stats := a.ApplyReloadedDiff(files)

	if len(a.Session.ReviewComments) != 1 {
		t.Error("a reload must never drop comments")
	}
	// A review of the old content says nothing about the new content.
	if a.Session.IsFileReviewed(path) {
		t.Error("a file whose content moved must lose its reviewed mark")
	}
	if stats.Files != 1 || stats.Changed != 1 || stats.Unreviewed != 1 {
		t.Errorf("stats = %+v, want 1 file / 1 changed / 1 unreviewed", stats)
	}
	if !strings.Contains(stats.Message(), "changed since last review") {
		t.Errorf("message = %q", stats.Message())
	}
}

func TestReloadKeepsReviewedMarksOnUnchangedFiles(t *testing.T) {
	a := newTestApp(t)
	path := a.DiffFiles[0].DisplayPath()
	a.ToggleReviewedForFileIdx(0, false)

	// Byte-identical reload: nothing to re-review.
	same := []model.DiffFile{a.DiffFiles[0]}
	stats := a.ApplyReloadedDiff(same)

	if !a.Session.IsFileReviewed(path) {
		t.Error("an unchanged file must keep its reviewed mark")
	}
	if stats.Changed != 0 || stats.Unreviewed != 0 {
		t.Errorf("stats = %+v, want nothing changed", stats)
	}
}

func TestReloadStatsCountAddedAndRemovedFiles(t *testing.T) {
	before := []model.DiffFile{diffFile("a.go", "x"), diffFile("gone.go", "y")}
	after := []model.DiffFile{diffFile("a.go", "x"), diffFile("new.go", "z")}

	stats := reloadStats(before, after)
	if stats.Files != 2 || stats.Added != 1 || stats.Removed != 1 || stats.Changed != 0 {
		t.Errorf("stats = %+v", stats)
	}
	msg := stats.Message()
	for _, want := range []string{"2 file(s)", "1 new", "1 gone"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q missing %q", msg, want)
		}
	}
	// An unchanged reload says so without noise.
	if got := reloadStats(before, before).Message(); got != "Reloaded 2 file(s)" {
		t.Errorf("unchanged reload message = %q", got)
	}
}

func TestReloadDropsExpandedGapsAndPrContext(t *testing.T) {
	a := prModeApp(t)
	req, _ := a.PrContextRequestFor(0, nil)
	a.ApplyPrContextSnapshot(req, contextLines(40))
	a.ExpandedTop[GapID{FileIdx: 0}] = []model.DiffLine{{Content: "stale"}}
	a.DiffState.CursorLine = 5

	a.ApplyReloadedDiff([]model.DiffFile{diffFile("src/x.go", "fresh")})

	if len(a.ExpandedTop) != 0 {
		t.Error("expanded gaps were computed against the old content and must be dropped")
	}
	if _, needsFetch := a.PrContextRequestFor(0, nil); !needsFetch {
		t.Error("the PR file snapshot cache must be dropped so it cannot serve stale lines")
	}
	if a.DiffState.CursorLine != 0 {
		t.Error("the cursor must reset — the line it was on may no longer exist")
	}
}

func TestStartPrReloadGuardsStaleResults(t *testing.T) {
	a := prModeApp(t)
	req, ok := a.StartPrReload()
	if !ok {
		t.Fatal("expected a reload request in PR mode")
	}
	if !a.Pr.Reloading {
		t.Error("a reload must raise the spinner flag")
	}
	if a.PrReloadIsStale(req) {
		t.Error("the request that was just issued is not stale")
	}

	// A second reload supersedes the first.
	fresh, _ := a.StartPrReload()
	if !a.PrReloadIsStale(req) {
		t.Error("a superseded reload result must be discarded")
	}
	if a.PrReloadIsStale(fresh) {
		t.Error("the current reload must still apply")
	}

	// A result for a different pull request is always stale.
	other := fresh
	other.Key.Number = 999
	if !a.PrReloadIsStale(other) {
		t.Error("a result for a different pull request must be discarded")
	}
}

func TestPrReloadIsStaleOutsidePrMode(t *testing.T) {
	a := newTestApp(t)
	if _, ok := a.StartPrReload(); ok {
		t.Error("a local review has no pull request to reload")
	}
	if !a.PrReloadIsStale(PrReloadRequest{}) {
		t.Error("outside PR mode every reload result is stale")
	}
}

func TestPrHeadMovedDetectsANewHead(t *testing.T) {
	a := prModeApp(t)
	same := &PullRequestLoad{Details: &forge.PullRequestDetails{HeadSHA: "head"}}
	moved := &PullRequestLoad{Details: &forge.PullRequestDetails{HeadSHA: "head2"}}

	if a.PrHeadMoved(same) {
		t.Error("an unchanged head is not a move")
	}
	if !a.PrHeadMoved(moved) {
		t.Error("a new head commit must be detected — the session is keyed by it")
	}
}
