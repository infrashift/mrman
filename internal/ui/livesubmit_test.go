package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/forge"
	_ "github.com/infrashift/mrman/internal/forge/drivers" // register drivers
	"github.com/infrashift/mrman/internal/livetest"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/theme"
)

// TestLivePullRequestSubmit posts a real review to a real pull request.
//
// This is the path fakes are least able to vouch for: a driver can build an
// inline comment that looks structurally fine and that GitHub rejects
// because the position does not resolve. Nothing but a real POST proves the
// mapping is right.
//
// It writes, so it is gated separately and needs a pull request you are
// willing to have comments posted to:
//
//	MRMAN_LIVE_PR=infrashift/scratch#1 MRMAN_LIVE_SUBMIT=1 \
//	    go test ./internal/ui/ -run TestLivePullRequestSubmit -v
func TestLivePullRequestSubmit(t *testing.T) {
	cfg := livetest.Config(t)
	repo, number := livetest.Target(t, cfg)
	livetest.RequireSubmit(t)

	backend, err := forge.ForRepository(repo, cfg)
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
	a.Username = "mrman-live-test"
	a.CommentTypePrefix = true

	m := NewModel(a, resolved)
	m.forge = staticForgeResolver(backend, repo)
	m.width, m.height = 140, 48
	m.syncViewport()

	// Comment on the first added line of each file, the way a reviewer
	// would: put the cursor on the row and type.
	anchored := 0
	for fileIdx := range a.DiffFiles {
		row, ok := firstAddedRow(a, fileIdx)
		if !ok {
			continue
		}
		// MoveCursorToAnnotation, not a bare CursorLine assignment: it is
		// what syncs CurrentFileIdx, and a comment files under that. Every
		// real path here (j/k, a click, a jump) goes through it.
		a.MoveCursorToAnnotation(row)
		a.EnterCommentMode(false)
		a.CommentBuffer = "Live submit check from mrman's integration test."
		a.CommentType = model.CommentTypeFromID("note")
		saved := a.SaveComment()
		if saved == nil {
			t.Fatalf("failed to save a line comment on %s", a.DiffFiles[fileIdx].DisplayPath())
		}
		t.Logf("anchored a comment on %s (row %d)", a.DiffFiles[fileIdx].DisplayPath(), row)
		_ = saved
		anchored++
	}
	if anchored == 0 {
		t.Fatal("no added line to anchor a comment to")
	}

	// A review-level comment becomes the review body.
	a.EnterReviewCommentMode()
	a.CommentBuffer = "Posted by mrman's live integration test; safe to dismiss."
	a.CommentType = model.CommentTypeFromID("note")
	a.SaveComment()

	// --- preflight ---
	if !a.StartSubmitWith(forge.SubmitComment, true) {
		t.Fatalf("submit preflight refused: %+v", a.Message)
	}
	t.Logf("preflight: %d inline, %d unmappable, %d review-level",
		len(a.Submit.Mappable), len(a.Submit.Unmappable), len(a.Submit.ReviewComments))
	for _, item := range a.Submit.Unmappable {
		t.Logf("  unmappable: %s — %s", item.Path, item.Reason.HumanLabel())
	}
	if len(a.Submit.Mappable) != anchored {
		t.Errorf("mapped %d of %d line comments", len(a.Submit.Mappable), anchored)
	}
	for _, c := range a.Submit.Mappable {
		t.Logf("  inline: %s:%v side=%v", c.Path, c.Line, c.Side)
	}

	// --- the actual POST ---
	runCmd(t, m, m.spawnSubmit())

	if a.Message != nil {
		t.Logf("result: %s", a.Message.Content)
	}
	if a.Message == nil || a.Message.Type == app.MessageError {
		t.Fatalf("submit failed: %+v", a.Message)
	}
	if a.Pr.Submitting {
		t.Error("a finished submit must clear the spinner")
	}

	// --- everything sent is now locked ---
	locked, total := 0, 0
	for _, c := range a.Session.ReviewComments {
		total++
		if c.IsLocked() {
			locked++
		}
	}
	for _, review := range a.Session.Files {
		for _, comments := range review.LineComments {
			for _, c := range comments {
				total++
				if c.IsLocked() {
					locked++
				}
			}
		}
	}
	if locked != total {
		t.Errorf("%d of %d submitted comments are locked; an unlocked one can be "+
			"edited locally and silently diverge from the forge", locked, total)
	}
	t.Logf("locked %d/%d comments after submit", locked, total)

	// The lock must be visible to the user, not just in the data.
	a.DiffState.CursorLine = 0
	for i := range a.LineAnnotations {
		if a.LineAnnotations[i].Kind == app.AnnLineComment {
			a.DiffState.CursorLine = i
			break
		}
	}
	if a.DeleteCommentAtCursor() {
		t.Error("a submitted comment must not be deletable")
	}
	if a.Message == nil || !strings.Contains(a.Message.Content, "read only") {
		t.Errorf("deleting a submitted comment must explain why, got %+v", a.Message)
	}
}

// firstAddedRow returns the annotation row of the file's first added line,
// which is always a valid inline-comment anchor.
func firstAddedRow(a *app.App, fileIdx int) (int, bool) {
	for i := range a.LineAnnotations {
		ann := &a.LineAnnotations[i]
		if ann.Kind != app.AnnDiffLine || ann.FileIdx != fileIdx || ann.NewLineno == nil {
			continue
		}
		line := &a.DiffFiles[fileIdx].Hunks[ann.HunkIdx].Lines[ann.LineIdx]
		if line.Origin == model.OriginAddition {
			return i, true
		}
	}
	return 0, false
}
