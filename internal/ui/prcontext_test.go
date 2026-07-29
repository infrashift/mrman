package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/model"
)

// contextForge serves whole-file context the way a forge driver does.
type contextForge struct {
	*uiFakeForge
	lineCount  int
	countErr   error
	linesErr   error
	countCalls int
	lineCalls  int
	lastReq    forge.FileLinesRequest
}

func (f *contextForge) FileLineCount(_ context.Context, req forge.FileLinesRequest) (int, error) {
	f.countCalls++
	f.lastReq = req
	if f.countErr != nil {
		return 0, f.countErr
	}
	return f.lineCount, nil
}

func (f *contextForge) FetchFileLines(_ context.Context, req forge.FileLinesRequest) ([]model.DiffLine, error) {
	f.lineCalls++
	f.lastReq = req
	if f.linesErr != nil {
		return nil, f.linesErr
	}
	lines := make([]model.DiffLine, f.lineCount)
	for i := range lines {
		n := uint32(i + 1) //nolint:gosec // test fixture line counts stay tiny
		lines[i] = model.DiffLine{
			Origin: model.OriginContext, Content: "context line", NewLineno: &n,
		}
	}
	return lines, nil
}

// contextPrModel puts the model in PR mode with a leading gap to expand.
func contextPrModel(t *testing.T, f *contextForge) *Model {
	t.Helper()
	m := testModel(t)
	m.App.Pr = &app.PrState{
		Backend: f,
		Details: &forge.PullRequestDetails{
			PullRequestSummary: forge.PullRequestSummary{Repository: testRepo(), Number: 7},
			HeadSHA:            "headsha", BaseSHA: "basesha",
		},
		Repository: ptrRepo(),
	}
	m.App.DiffSource = app.DiffSource{Kind: app.DiffSourcePullRequest}
	// Push the hunk down the file so a leading gap exists.
	m.App.DiffFiles[0].Hunks[0].OldStart = 20
	m.App.DiffFiles[0].Hunks[0].NewStart = 20
	m.App.RebuildAnnotations()
	m.syncViewport()
	return m
}

// cursorToExpander parks the cursor on the first expander row.
func cursorToExpander(t *testing.T, m *Model) {
	t.Helper()
	for i := range m.App.LineAnnotations {
		if m.App.LineAnnotations[i].Kind == app.AnnExpander {
			m.App.DiffState.CursorLine = i
			return
		}
	}
	t.Fatal("no expander row in the annotation stream")
}

func TestExpandingAGapInPrModeFetchesThenExpands(t *testing.T) {
	f := &contextForge{uiFakeForge: &uiFakeForge{}, lineCount: 60}
	m := contextPrModel(t, f)
	cursorToExpander(t, m)

	before := len(m.App.LineAnnotations)
	runCmd(t, m, m.expandGapAtCursor())

	if f.countCalls != 1 || f.lineCalls != 1 {
		t.Fatalf("one round trip expected, got count=%d lines=%d", f.countCalls, f.lineCalls)
	}
	if f.lastReq.HeadSHA != "headsha" || f.lastReq.Path != "src/x.go" {
		t.Errorf("fetch request = %+v", f.lastReq)
	}
	if len(m.App.LineAnnotations) <= before {
		t.Error("the expansion must be replayed once the snapshot lands")
	}
}

func TestSecondExpansionInTheSameFileNeedsNoFetch(t *testing.T) {
	f := &contextForge{uiFakeForge: &uiFakeForge{}, lineCount: 60}
	m := contextPrModel(t, f)
	cursorToExpander(t, m)
	runCmd(t, m, m.expandGapAtCursor())

	countBefore, linesBefore := f.countCalls, f.lineCalls
	cursorToExpander(t, m)
	if cmd := m.expandGapAtCursor(); cmd != nil {
		t.Error("a cached file must expand without another round trip")
	}
	if f.countCalls != countBefore || f.lineCalls != linesBefore {
		t.Errorf("extra forge calls: count %d→%d lines %d→%d",
			countBefore, f.countCalls, linesBefore, f.lineCalls)
	}
}

func TestContextFetchFailureWarnsAndStopsRetrying(t *testing.T) {
	f := &contextForge{uiFakeForge: &uiFakeForge{}, lineCount: 60, linesErr: errors.New("blob unavailable")}
	m := contextPrModel(t, f)
	cursorToExpander(t, m)
	runCmd(t, m, m.expandGapAtCursor())

	if m.App.Message == nil || !strings.Contains(m.App.Message.Content, "blob unavailable") {
		t.Errorf("a failed context fetch must warn, got %+v", m.App.Message)
	}
	calls := f.lineCalls
	cursorToExpander(t, m)
	runCmd(t, m, m.expandGapAtCursor())
	if f.lineCalls != calls {
		t.Error("a file the forge cannot serve must not be refetched on every press")
	}
}

func TestContextResultForASupersededPullRequestIsDropped(t *testing.T) {
	f := &contextForge{uiFakeForge: &uiFakeForge{}, lineCount: 60}
	m := contextPrModel(t, f)
	cursorToExpander(t, m)

	cmd := m.expandGapAtCursor()
	if cmd == nil {
		t.Fatal("expected a fetch command")
	}
	msg := cmd()

	// The user reloaded or switched pull requests meanwhile.
	m.App.Pr.Details.HeadSHA = "different"
	before := len(m.App.LineAnnotations)
	m.Update(msg)

	if len(m.App.LineAnnotations) != before {
		t.Error("a snapshot for a superseded pull request must be discarded")
	}
}

func TestPrModePrefetchesTheCurrentFile(t *testing.T) {
	f := &contextForge{uiFakeForge: &uiFakeForge{}, lineCount: 60}
	m := contextPrModel(t, f)

	// Just moving around arms the prefetch, so a file whose only hidden
	// context is after its last hunk still becomes expandable.
	pressPrRune(t, m, 'j')
	if f.lineCalls == 0 {
		t.Error("moving into a file must prefetch its context")
	}
	calls := f.lineCalls
	for i := 0; i < 5; i++ {
		pressPrRune(t, m, 'j')
	}
	if f.lineCalls != calls {
		t.Errorf("the prefetch must fire once per file, got %d fetches", f.lineCalls)
	}
}

func TestLocalReviewsNeverTouchTheForgeForContext(t *testing.T) {
	f := &contextForge{uiFakeForge: &uiFakeForge{}, lineCount: 60}
	m := testModel(t) // local review: no PR state
	m.App.Pr = &app.PrState{Backend: f}

	pressRune(m, 'j')
	if f.countCalls != 0 || f.lineCalls != 0 {
		t.Error("a local review must read context from the VCS, not the forge")
	}
}
