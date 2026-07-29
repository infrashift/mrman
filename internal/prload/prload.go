// Package prload performs the network round of opening a pull request:
// details, cumulative diff, commits, parsed into reviewable diff files.
//
// It sits below both entrypoints that need it — the TUI and the headless
// review CLI — so neither has to import the other, and so a command that
// never draws anything does not link a terminal UI framework to fetch a
// patch.
package prload

import (
	"context"
	"fmt"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/ignore"
	"github.com/infrashift/mrman/internal/syntax"
	"github.com/infrashift/mrman/internal/vcs/diffparser"
	"github.com/infrashift/mrman/internal/vcs/prnoop"
)

// Fetch performs the network round of opening a pull request — details,
// cumulative diff, commits — and parses the patch into diff files. Shared by
// every entrypoint that opens a pull request so they cannot drift into
// disagreeing about what one contains.
//
// ListCommits is best-effort: a PR is perfectly reviewable without its
// commit list, and some forges rate-limit it separately.
func Fetch(
	ctx context.Context,
	backend forge.Forge,
	repo *forgetypes.Repository,
	target forge.Target,
	highlighter *syntax.Highlighter,
	localCheckout string,
) (app.PullRequestLoad, error) {
	details, err := backend.GetPullRequest(ctx, target)
	if err != nil {
		return app.PullRequestLoad{}, err
	}
	patch, err := backend.GetDiff(ctx, details)
	if err != nil {
		return app.PullRequestLoad{}, err
	}
	commits, _ := backend.ListCommits(ctx, details)

	files, err := diffparser.Parse(patch, diffparser.GitStyle, highlighter)
	if err != nil {
		return app.PullRequestLoad{}, err
	}
	if localCheckout != "" {
		files = ignore.Load(localCheckout).FilterDiffFiles(files)
	}
	if len(files) == 0 {
		return app.PullRequestLoad{}, fmt.Errorf("pull request #%d has no file changes", details.Number)
	}

	return app.PullRequestLoad{
		Backend:    backend,
		Repository: repo,
		Details:    details,
		Commits:    commits,
		Files:      files,
		VCS:        prnoop.New(localCheckout),
	}, nil
}
