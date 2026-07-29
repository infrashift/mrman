// Command mrman is a terminal code-review tool: a Go reimplementation of
// tuicr with multi-forge support (GitHub, GitLab, Azure DevOps, Forgejo).
package main

import (
	"fmt"
	"os"

	"github.com/infrashift/mrman/internal/cli"
	"github.com/infrashift/mrman/internal/persistence"
	"github.com/infrashift/mrman/internal/reviewcli"
	"github.com/infrashift/mrman/internal/ui"
)

func main() {
	args, err := cli.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
	switch args.Command {
	case cli.CommandNone:
		return // help/version printed by the CLI layer
	case cli.CommandTui:
		if err := ui.Run(args.Tui); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case cli.CommandPr:
		if err := ui.RunPr(args.PrTarget, args.Tui); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case cli.CommandReviewList, cli.CommandReviewAdd, cli.CommandReviewComments:
		if err := runReview(args); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	}
}

func runReview(args *cli.Args) error {
	store, err := persistence.NewDefaultStore()
	if err != nil {
		return err
	}
	opts := reviewcli.Options{
		Repo:       args.Review.Repo,
		All:        args.Review.All,
		Session:    args.Review.Session,
		Input:      args.Review.Input,
		Type:       args.Review.Type,
		TargetFile: args.Review.TargetFile,
		Line:       args.Review.Line,
		EndLine:    args.Review.EndLine,
		Side:       args.Review.Side,
		Username:   args.Review.Username,
		Comment:    args.Review.Comment,
	}
	switch args.Command {
	case cli.CommandReviewList:
		return reviewcli.List(store, opts, os.Stdout)
	case cli.CommandReviewAdd:
		return reviewcli.Add(store, opts, os.Stdout)
	case cli.CommandReviewComments:
		return reviewcli.Comments(store, opts, os.Stdout)
	}
	return nil
}
