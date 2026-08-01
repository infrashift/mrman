package ui

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/config"
	"github.com/infrashift/mrman/internal/forge"
	_ "github.com/infrashift/mrman/internal/forge/drivers" // register drivers
	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/theme"
	"github.com/infrashift/mrman/internal/vcs"
)

// TestLivePullRequestReview drives the whole stack — forge driver, app state
// machine, renderer — against a real pull request, without a terminal.
//
// The unit tests all run against fakes, so they cannot catch a driver whose
// output the app misreads, or a pane that renders correctly from invented
// data and wrongly from real data. This closes that gap.
//
//	MRMAN_LIVE_PR=infrashift/scratch#1 go test ./internal/ui/ -run Live -v
func TestLivePullRequestReview(t *testing.T) {
	target := os.Getenv("MRMAN_LIVE_PR")
	if target == "" {
		t.Skip("set MRMAN_LIVE_PR=owner/repo#N to run against a real pull request")
	}
	repo, number := parseLiveTarget(t, target)

	backend, err := forge.ForRepository(repo, config.Default().Forge)
	if err != nil {
		t.Fatalf("resolve driver: %v", err)
	}
	resolved := theme.TokyoNightStorm()

	load, err := fetchPullRequest(context.Background(), backend, &repo,
		forge.Target{Repository: &repo, Number: number}, resolved.Highlighter(), "")
	if err != nil {
		t.Fatalf("fetch pull request: %v", err)
	}

	session := app.NewPrSession(load.Details)
	a := app.NewApp(load.VCS, &liveVcsInfo, load.Files, session,
		app.DiffSource{Kind: app.DiffSourcePullRequest})
	a.Pr = &app.PrState{
		Backend: load.Backend, Repository: load.Repository,
		Details: load.Details, Commits: load.Commits,
	}
	a.ShowCommitSelector = true
	a.SetupPrCommitSelector(load.Commits)

	m := NewModel(a, resolved)
	m.forge = staticForgeResolver(backend, repo)
	m.width, m.height = 140, 48
	m.mouseEnabled = true
	m.syncViewport()

	// --- the diff renders ---
	view := plainView(m)
	t.Logf("header: %s", strings.SplitN(view, "\n", 2)[0])
	for _, want := range []string{repo.Slug(), "OPEN"} {
		if !strings.Contains(view, want) {
			t.Errorf("header missing %q", want)
		}
	}
	if len(a.DiffFiles) == 0 {
		t.Fatal("no files parsed from the real diff")
	}
	t.Logf("files: %d", len(a.DiffFiles))
	for i := range a.DiffFiles {
		t.Logf("  %s (%d hunks)", a.DiffFiles[i].DisplayPath(), len(a.DiffFiles[i].Hunks))
	}

	// --- the commit strip reflects the real commits ---
	if len(load.Commits) > 1 {
		if !strings.Contains(view, "Commits ·") {
			t.Error("a multi-commit pull request must show the commit strip")
		}
		for _, c := range load.Commits {
			if !strings.Contains(view, c.Summary) {
				t.Errorf("commit %q missing from the strip", c.Summary)
			}
		}
	}

	// --- existing discussions load and render ---
	runCmd(t, m, m.loadRemoteCommentsOnOpen())
	t.Logf("threads=%d summaries=%d", len(a.Pr.Threads), len(a.Pr.Summaries))
	if len(a.Pr.Threads) == 0 {
		t.Log("note: this pull request has no review threads, so rendering is untested here")
	}
	// Assert on the document for coverage and on the frame for the top of
	// it: a pull request with many threads legitimately pushes later ones
	// below the fold, and "not on screen" is not "not rendered".
	visible := len(a.VisibleRemoteThreads())
	if rows := countRemoteThreadRows(a); visible > 0 && rows == 0 {
		t.Error("visible threads contribute no rows to the annotation stream")
	}
	// Review summaries render at review scope, above the diff, so the top
	// of the frame is where they are — and where inline threads are not,
	// once there are enough summaries to fill it.
	view = plainView(m)
	if len(a.Pr.Summaries) > 0 &&
		!strings.Contains(view, firstWords(a.Pr.Summaries[0].Body)) {
		t.Error("the first review summary must be on screen")
	}
	t.Logf("rendered: %d visible threads over %d rows, %d summaries",
		visible, countRemoteThreadRows(a), len(a.Pr.Summaries))

	// Resolved threads appear only under `:comments all`.
	var resolvedBody string
	for i := range a.Pr.Threads {
		if a.Pr.Threads[i].IsResolved {
			if root := a.Pr.Threads[i].Root(); root != nil {
				resolvedBody = firstWords(root.Body)
			}
		}
	}
	if resolvedBody != "" {
		// Assert on the document, not the viewport: a thread late in the
		// stream is legitimately below the fold at this window size.
		if got := countRemoteThreadRows(a); got == 0 {
			t.Error("the unresolved thread should already contribute rows")
		}
		hiddenRows := countRemoteThreadRows(a)

		m.setRemoteCommentsVisibility("all")
		runCmd(t, m, m.takeQueued())
		shownRows := countRemoteThreadRows(a)
		if shownRows <= hiddenRows {
			t.Errorf(":comments all must add the resolved thread: %d rows -> %d",
				hiddenRows, shownRows)
		}
		if len(a.VisibleRemoteThreads()) != len(a.Pr.Threads) {
			t.Errorf(":comments all shows %d of %d threads",
				len(a.VisibleRemoteThreads()), len(a.Pr.Threads))
		}
		t.Logf("visibility: %d thread rows unresolved-only, %d with all", hiddenRows, shownRows)

		m.setRemoteCommentsVisibility("unresolved")
		runCmd(t, m, m.takeQueued())
		if countRemoteThreadRows(a) != hiddenRows {
			t.Error("switching back must hide the resolved thread again")
		}
	}

	// --- context expansion through the forge ---
	expander := -1
	for i := range a.LineAnnotations {
		if a.LineAnnotations[i].Kind == app.AnnExpander {
			expander = i
			break
		}
	}
	if expander < 0 {
		t.Fatal("the real diff produced no expander row to test against")
	}
	a.DiffState.CursorLine = expander
	before := len(a.LineAnnotations)
	runCmd(t, m, m.expandGapAtCursor())
	if len(a.LineAnnotations) <= before {
		t.Errorf("expanding a gap fetched nothing: %d rows before and after", before)
	} else {
		t.Logf("gap expansion: %d rows -> %d", before, len(a.LineAnnotations))
	}

	// --- narrowing to one commit through GetCommitRangeDiff ---
	if backend.Capabilities().CommitRangeDiff && len(load.Commits) > 1 {
		allFiles := len(a.DiffFiles)
		a.CommitSelectionRange = &model.IndexRange{0, 0} // the newest commit only
		runCmd(t, m, m.reloadInlineSelection())
		t.Logf("narrowed to the newest commit: %d files (whole MR had %d)",
			len(a.DiffFiles), allFiles)
		if len(a.DiffFiles) == 0 {
			t.Error("narrowing to one commit produced an empty diff")
		}
	}

	// --- what a submit would post, without posting it ---
	a.CommitSelectionRange = nil
	runCmd(t, m, m.reloadInlineSelection())
	if !a.StartSubmitWith(forge.SubmitComment, false) {
		t.Log("submit preflight declined (no comments to submit) — expected on a fresh review")
	}
	a.CancelSubmit()
}

// liveVcsInfo stands in for a checkout: PR mode reads nothing from it.
var liveVcsInfo = vcs.Info{Type: vcs.TypeGit}

func parseLiveTarget(t *testing.T, target string) (forgetypes.Repository, uint64) {
	t.Helper()
	slug, numberText, ok := strings.Cut(target, "#")
	if !ok {
		t.Fatalf("MRMAN_LIVE_PR=%q, want owner/repo#N", target)
	}
	owner, name, ok := strings.Cut(slug, "/")
	if !ok {
		t.Fatalf("MRMAN_LIVE_PR=%q, want owner/repo#N", target)
	}
	number, err := strconv.ParseUint(numberText, 10, 64)
	if err != nil {
		t.Fatalf("bad pull request number in %q: %v", target, err)
	}
	return forgetypes.Repository{
		Kind: forgetypes.KindGitHub, Host: "github.com", Owner: owner, Name: name,
	}, number
}

// countRemoteThreadRows is how many rows the forge's discussions occupy in
// the annotation stream.
func countRemoteThreadRows(a *app.App) int {
	n := 0
	for i := range a.LineAnnotations {
		if a.LineAnnotations[i].Kind == app.AnnRemoteThreadLine {
			n++
		}
	}
	return n
}

// firstWords is a short, distinctive slice of a comment body to search the
// rendered frame for; a whole body would be broken by wrapping.
func firstWords(body string) string {
	fields := strings.Fields(body)
	if len(fields) > 4 {
		fields = fields[:4]
	}
	return strings.Join(fields, " ")
}
