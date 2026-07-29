package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/model"
)

// contextLines builds count numbered context lines, 1-indexed on the new
// side, the way a forge FetchFileLines call returns them.
func contextLines(count int) []model.DiffLine {
	lines := make([]model.DiffLine, count)
	for i := range lines {
		n := uint32(i + 1) //nolint:gosec // test fixture line counts stay tiny
		lines[i] = model.DiffLine{
			Origin:    model.OriginContext,
			Content:   "line " + string(rune('a'+i%26)),
			NewLineno: &n,
		}
	}
	return lines
}

func TestPrModeRoutesGapExpansionThroughTheForgeSnapshot(t *testing.T) {
	a := prModeApp(t)
	// The local backend is prnoop-equivalent in PR mode; the point of this
	// test is that expansion never reaches it.
	if _, ok := a.contextProvider().(*PrContextProvider); !ok {
		t.Fatalf("PR mode must use the forge snapshot provider, got %T", a.contextProvider())
	}

	local := newTestApp(t)
	if _, ok := local.contextProvider().(VcsContextProvider); !ok {
		t.Errorf("a local review must keep using the VCS provider, got %T", local.contextProvider())
	}
}

func TestExpandGapReportsNotLoadedBeforeTheSnapshotArrives(t *testing.T) {
	a := prModeApp(t)
	// Push the hunk down the file so there is a leading gap to expand.
	a.DiffFiles[0].Hunks[0].OldStart, a.DiffFiles[0].Hunks[0].NewStart = 20, 20
	a.RebuildAnnotations()

	gap := GapID{FileIdx: 0, HunkIdx: 0}
	limit := GapExpandBatch

	err := a.ExpandGap(gap, ExpandUp, &limit)
	if !errors.Is(err, ErrContextNotLoaded) {
		t.Fatalf("expansion before the fetch must report ErrContextNotLoaded, got %v", err)
	}

	req, ok := a.PrContextRequestFor(0, &PrContextReplay{Gap: gap, Direction: ExpandUp, Limit: &limit})
	if !ok {
		t.Fatal("a not-loaded file must produce a fetch request")
	}
	if req.Path() != "src/x.go" {
		t.Errorf("request path = %q", req.Path())
	}
}

func TestApplyPrContextSnapshotReplaysTheExpansion(t *testing.T) {
	a := prModeApp(t)
	gap := GapID{FileIdx: 0, HunkIdx: 0}
	limit := GapExpandBatch

	a.DiffFiles[0].Hunks[0].OldStart, a.DiffFiles[0].Hunks[0].NewStart = 20, 20
	a.RebuildAnnotations()

	req, ok := a.PrContextRequestFor(0, &PrContextReplay{Gap: gap, Direction: ExpandUp, Limit: &limit})
	if !ok {
		t.Fatal("expected a fetch request")
	}
	a.ApplyPrContextSnapshot(req, contextLines(40))

	if count, ok := a.FileLineCountCache[0]; !ok || count != 40 {
		t.Errorf("the snapshot must seed the file line count, got %d ok=%v", count, ok)
	}
	// A second expansion is served from the cache — no request this time.
	if _, again := a.PrContextRequestFor(0, nil); again {
		t.Error("a loaded file must not be refetched")
	}
}

func TestPrContextSnapshotServesRangesSynchronously(t *testing.T) {
	a := prModeApp(t)
	req, _ := a.PrContextRequestFor(0, nil)
	a.ApplyPrContextSnapshot(req, contextLines(40))

	provider := a.ensurePrContext()
	path := "src/x.go"
	lines, err := provider.FetchContextLines(nil, &path, model.StatusModified, 5, 9)
	if err != nil {
		t.Fatalf("a cached file must serve without I/O: %v", err)
	}
	if len(lines) != 5 {
		t.Fatalf("range 5..9 must yield 5 lines, got %d", len(lines))
	}
	if got := *lines[0].NewLineno; got != 5 {
		t.Errorf("first line = %d, want 5", got)
	}

	// Out-of-range requests clamp instead of panicking.
	if lines, err := provider.FetchContextLines(nil, &path, model.StatusModified, 38, 100); err != nil {
		t.Errorf("clamped range must not error: %v", err)
	} else if len(lines) != 3 {
		t.Errorf("clamped range must yield the tail, got %d lines", len(lines))
	}
	if lines, err := provider.FetchContextLines(nil, &path, model.StatusModified, 9, 5); err != nil || lines != nil {
		t.Error("an inverted range must yield nothing without erroring")
	}
}

func TestPrContextMissingFileIsNotRetried(t *testing.T) {
	a := prModeApp(t)
	req, ok := a.PrContextRequestFor(0, nil)
	if !ok {
		t.Fatal("expected a fetch request")
	}

	a.FailPrContextSnapshot(req, "404 not found")
	if a.Message == nil || !strings.Contains(a.Message.Content, "404 not found") {
		t.Errorf("a failed fetch must explain itself, got %+v", a.Message)
	}
	if _, again := a.PrContextRequestFor(0, nil); again {
		t.Error("a file the forge cannot serve must not be refetched on every keystroke")
	}

	// And expansion reports the real reason, not "not loaded yet".
	err := a.ExpandGap(GapID{FileIdx: 0, HunkIdx: 0}, ExpandUp, nil)
	if errors.Is(err, ErrContextNotLoaded) {
		t.Error("a known-missing file must not keep reporting ErrContextNotLoaded")
	}
}

func TestGoToSourceLineArmsAContextFetchInPrMode(t *testing.T) {
	a := prModeApp(t)
	// Line 400 is far past the diff, inside collapsed end-of-file context.
	a.GoToSourceLine(400, model.LineSideNew)

	// Without a snapshot there is no end-of-file gap to plan through yet,
	// so the jump simply reports it cannot reach the line. Once a snapshot
	// exists the planner takes over — that is the path below.
	req, ok := a.PrContextRequestFor(0, nil)
	if !ok {
		t.Fatal("expected the file to still be unfetched")
	}
	a.ApplyPrContextSnapshot(req, contextLines(400))

	a.GoToSourceLine(300, model.LineSideNew)
	if pending, armed := a.TakePrContextRequest(); armed {
		t.Errorf("a cached file must not arm another fetch, got %+v", pending)
	}
}

func TestPrContextRequestRejectsPathlessFiles(t *testing.T) {
	a := prModeApp(t)
	a.DiffFiles[0].OldPath = nil
	a.DiffFiles[0].NewPath = nil
	if _, ok := a.PrContextRequestFor(0, nil); ok {
		t.Error("a file with no path on either side cannot be fetched")
	}
	if _, ok := a.PrContextRequestFor(99, nil); ok {
		t.Error("an out-of-range file index must not produce a request")
	}
}

func TestDeletedFilesReadFromTheBaseSide(t *testing.T) {
	a := prModeApp(t)
	old := "src/gone.go"
	a.DiffFiles[0].Status = model.StatusDeleted
	a.DiffFiles[0].OldPath = &old
	a.DiffFiles[0].NewPath = nil

	req, ok := a.PrContextRequestFor(0, nil)
	if !ok {
		t.Fatal("expected a fetch request")
	}
	if req.Path() != old {
		t.Errorf("a deleted file must be read at its old path, got %q", req.Path())
	}
	if req.Side() != 0 { // forge.FileSideBase
		t.Errorf("a deleted file must be read from the base side, got %v", req.Side())
	}
}
