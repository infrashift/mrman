// Command mrman is a terminal code-review tool: a Go reimplementation of
// tuicr with multi-forge support (GitHub, GitLab, Azure DevOps, Forgejo).
package main

import (
	"bufio"
	"errors"
	"os/signal"
	"syscall"
	"time"

	"fmt"
	"os"

	"github.com/infrashift/mrman/internal/agentsubmit"

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
		if args.Tui.JSON {
			if err := ui.RunPrHeadless(args.PrTarget, args.Tui, os.Stdout); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				os.Exit(1)
			}
			return
		}
		if err := ui.RunPr(args.PrTarget, args.Tui); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case cli.CommandReviewList, cli.CommandReviewAdd, cli.CommandReviewComments,
		cli.CommandReviewWatch, cli.CommandReviewSubmit:
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
	case cli.CommandReviewWatch:
		return runWatch(store, opts, args)
	case cli.CommandReviewSubmit:
		return runSubmit(store, opts, args)
	}
	return nil
}

// runWatch streams until the session closes or the user interrupts. SIGINT
// ends the stream with a closed event rather than a broken pipe, so an agent
// sees a clean terminator either way.
func runWatch(store *persistence.Store, opts reviewcli.Options, args *cli.Args) error {
	done := make(chan struct{})
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signals
		close(done)
	}()
	defer signal.Stop(signals)

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush() //nolint:errcheck // the stream is already terminated
	return reviewcli.Watch(store, opts, reviewcli.WatchOptions{
		Interval: time.Duration(args.Review.IntervalMS) * time.Millisecond,
		Timeout:  time.Duration(args.Review.TimeoutSeconds) * time.Second,
		Since:    args.Review.Since,
		Done:     done,
	}, out)
}

// runSubmit posts a review, rendering an interlock refusal as JSON on stdout
// so an agent can parse the reason rather than scraping a message.
func runSubmit(store *persistence.Store, opts reviewcli.Options, args *cli.Args) error {
	err := agentsubmit.Submit(store, agentsubmit.Options{
		Options: opts,
		Event:   args.Review.Event,
	}, os.Stdout)
	var denied *agentsubmit.SubmitDenied
	if errors.As(err, &denied) {
		if writeErr := agentsubmit.WriteDenial(os.Stdout, denied); writeErr != nil {
			return writeErr
		}
		os.Exit(1)
	}
	if errors.Is(err, agentsubmit.ErrPartialSubmit) {
		// The JSON on stdout already says what landed and what did not.
		os.Exit(1)
	}
	return err
}
