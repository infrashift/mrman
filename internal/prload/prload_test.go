package prload

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

const patch = "diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+new\n"

// stubForge answers the three calls Fetch makes; every other method is
// unimplemented on purpose, so a new call in Fetch fails loudly here.
type stubForge struct {
	forge.Forge
	details    *forge.PullRequestDetails
	detailsErr error
	diff       string
	diffErr    error
	commits    []forge.Commit
	commitsErr error
}

func (s stubForge) GetPullRequest(context.Context, forge.Target) (*forge.PullRequestDetails, error) {
	return s.details, s.detailsErr
}

func (s stubForge) GetDiff(context.Context, *forge.PullRequestDetails) (string, error) {
	return s.diff, s.diffErr
}

func (s stubForge) ListCommits(context.Context, *forge.PullRequestDetails) ([]forge.Commit, error) {
	return s.commits, s.commitsErr
}

func details() *forge.PullRequestDetails {
	return &forge.PullRequestDetails{
		PullRequestSummary: forge.PullRequestSummary{Number: 7, Title: "t"},
		HeadSHA:            "head", BaseSHA: "base",
	}
}

func TestFetchAssemblesALoad(t *testing.T) {
	repo := &forgetypes.Repository{Kind: forgetypes.KindGitHub, Host: "github.com", Owner: "o", Name: "r"}
	backend := stubForge{details: details(), diff: patch, commits: []forge.Commit{{OID: "c1"}}}
	load, err := Fetch(context.Background(), backend, repo, forge.Target{Repository: repo, Number: 7}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if load.Details.Number != 7 || len(load.Files) != 1 || len(load.Commits) != 1 || load.Repository != repo {
		t.Fatalf("load = %+v", load)
	}
	if load.VCS == nil || load.Backend == nil {
		t.Fatal("load must carry the no-op VCS and the backend")
	}
}

func TestFetchCommitsAreBestEffort(t *testing.T) {
	backend := stubForge{details: details(), diff: patch, commitsErr: errors.New("rate limited")}
	load, err := Fetch(context.Background(), backend, nil, forge.Target{}, nil, "")
	if err != nil {
		t.Fatalf("a failed commit list must not fail the open: %v", err)
	}
	if len(load.Commits) != 0 {
		t.Fatalf("commits = %+v", load.Commits)
	}
}

func TestFetchPropagatesHardFailures(t *testing.T) {
	boom := errors.New("boom")
	for name, backend := range map[string]stubForge{
		"details": {detailsErr: boom},
		"diff":    {details: details(), diffErr: boom},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Fetch(context.Background(), backend, nil, forge.Target{}, nil, ""); !errors.Is(err, boom) {
				t.Fatalf("err = %v, want the forge error", err)
			}
		})
	}
}

func TestFetchRefusesAnEmptyChangeSet(t *testing.T) {
	// An empty patch is the parser's "no changes" sentinel; a patch whose
	// only file is filtered out is Fetch's own refusal. Both must refuse.
	backend := stubForge{details: details(), diff: ""}
	_, err := Fetch(context.Background(), backend, nil, forge.Target{}, nil, "")
	if !errors.Is(err, errs.ErrNoChanges) {
		t.Fatalf("err = %v, want ErrNoChanges", err)
	}
}

func TestFetchAppliesTheCheckoutIgnoreFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir+"/.mrmanignore", "x.go\n")
	backend := stubForge{details: details(), diff: patch}
	_, err := Fetch(context.Background(), backend, nil, forge.Target{}, nil, dir)
	if err == nil || !strings.Contains(err.Error(), "no file changes") {
		t.Fatalf("an ignore rule matching every file must leave nothing to review, got %v", err)
	}
}
