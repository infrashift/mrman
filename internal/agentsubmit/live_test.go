package agentsubmit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/forge"
	_ "github.com/infrashift/mrman/internal/forge/drivers" // register drivers
	"github.com/infrashift/mrman/internal/livetest"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/persistence"
	"github.com/infrashift/mrman/internal/prload"
	"github.com/infrashift/mrman/internal/reviewcli"
)

// TestLiveAgentSubmit exercises the interlock against a real pull request:
// refused without a grant and nothing reaches the forge, allowed with one
// and the review actually lands.
//
// The grant is pid-based, so this test process *is* the granting process —
// exactly what a TUI is from the registry's point of view. Nothing is
// simulated except the absence of a terminal.
//
//	MRMAN_LIVE_PR=owner/repo#N MRMAN_LIVE_SUBMIT=1 \
//	    go test ./internal/agentsubmit/ -run TestLiveAgentSubmit -v
func TestLiveAgentSubmit(t *testing.T) {
	cfg := livetest.Config(t)
	repo, number := livetest.Target(t, cfg)
	livetest.RequireSubmit(t)

	store := &persistence.Store{ReviewsDir: filepath.Join(t.TempDir(), "reviews")}
	backend, err := forge.ForRepository(repo, cfg)
	if err != nil {
		t.Fatalf("resolve driver: %v", err)
	}
	load, err := prload.Fetch(context.Background(), backend, &repo,
		forge.Target{Repository: &repo, Number: number}, nil, "")
	if err != nil {
		t.Fatalf("fetch pull request: %v", err)
	}

	session := app.NewPrSession(load.Details)
	app.RegisterDiffFiles(session, load.Files)
	session.ReviewComments = append(session.ReviewComments,
		model.NewComment("Posted by mrman's agent-submit interlock test.",
			model.CommentTypeFromID("note"), nil))
	path, err := store.SaveSession(session)
	if err != nil {
		t.Fatal(err)
	}

	reviewsBefore := countReviews(t, backend, load.Details)

	// --- refused with no grant, and nothing reaches the forge ---
	var out bytes.Buffer
	err = Submit(store, Options{
		Options: reviewcli.Options{Session: path}, Event: "comment",
	}, &out)
	var denied *SubmitDenied
	if !errors.As(err, &denied) {
		t.Fatalf("submit without a grant must be refused, got %v", err)
	}
	t.Logf("refused: %s", denied.Message)

	if after := countReviews(t, backend, load.Details); after != reviewsBefore {
		t.Fatalf("a refused submit reached the forge: %d reviews before, %d after",
			reviewsBefore, after)
	}
	t.Logf("forge unchanged after refusal: %d reviews", reviewsBefore)

	// --- granted: comment allowed, approve still refused ---
	if err := store.MarkSessionActiveWithGrant(session, path, []string{"comment", "draft"}); err != nil {
		t.Fatal(err)
	}

	err = Submit(store, Options{
		Options: reviewcli.Options{Session: path}, Event: "approve",
	}, &bytes.Buffer{})
	if !errors.As(err, &denied) || denied.Reason != string(persistence.GrantEventNotAllowed) {
		t.Fatalf("a comment,draft grant must not permit approve, got %v", err)
	}
	if after := countReviews(t, backend, load.Details); after != reviewsBefore {
		t.Fatalf("a refused approve reached the forge: %d -> %d", reviewsBefore, after)
	}

	out.Reset()
	if err := Submit(store, Options{
		Options: reviewcli.Options{Session: path, Username: "mrman-interlock-test"},
		Event:   "comment",
	}, &out); err != nil {
		t.Fatalf("a granted submit must succeed: %v", err)
	}

	var result SubmitResultOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("submit output must be JSON: %v (%s)", err, out.String())
	}
	t.Logf("submitted: review=%s state=%s inline=%d locked=%d",
		result.ReviewID, result.State, result.InlineCount, result.LockedComment)
	if !result.Submitted {
		t.Error("result must report submitted")
	}
	if after := countReviews(t, backend, load.Details); after != reviewsBefore+1 {
		t.Errorf("the granted submit did not land: %d reviews before, %d after",
			reviewsBefore, after)
	}
}

// countReviews asks the forge how many reviews the pull request has, which
// is the only assertion that actually proves a refusal refused. A forge
// without review summaries (GitLab) records a submitted review body as a
// general MR note instead, which surfaces as a path-less review thread.
func countReviews(t *testing.T, backend forge.Forge, details *forge.PullRequestDetails) int {
	t.Helper()
	if !backend.Capabilities().ReviewSummaries {
		threads, err := backend.ListReviewThreads(context.Background(), details)
		if err != nil {
			t.Fatalf("list review threads: %v", err)
		}
		return len(threads)
	}
	summaries, err := backend.ListReviewSummaries(context.Background(), details)
	if err != nil {
		t.Fatalf("list review summaries: %v", err)
	}
	return len(summaries)
}
