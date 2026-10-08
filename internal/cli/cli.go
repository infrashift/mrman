package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/infrashift/mrman/internal/version"
)

// Parse builds the cobra command tree, parses argv (without the program
// name), and returns the resolved Args. Cobra types do not escape this
// package.
func Parse(argv []string) (*Args, error) {
	args := &Args{Command: CommandNone}

	root := newRootCmd(args)
	root.SetArgs(preprocessArgs(argv))
	if err := root.Execute(); err != nil {
		return nil, err
	}
	if err := resolveAutoGrant(root, &args.Tui); err != nil {
		return nil, err
	}
	return args, nil
}

// stdinIsTerminal is the TTY probe, a var so tests can drive both paths.
//
// This must be a real isatty, not a ModeCharDevice check: /dev/null is a
// character device, so the naive test reports "terminal" for exactly the
// redirect an agent shelling out would use.
var stdinIsTerminal = func() bool {
	return term.IsTerminal(os.Stdin.Fd())
}

// resolveAutoGrant turns the --auto flag into TuiOptions.GrantedEvents,
// refusing it when it did not come from a person at a terminal.
//
// The TTY check is the second half of the interlock: --json already cannot
// carry --auto, and this stops an agent reaching the interactive path by
// shelling out to `mrman pr 1 --auto` instead.
func resolveAutoGrant(root *cobra.Command, o *TuiOptions) error {
	flag := root.PersistentFlags().Lookup("auto")
	if flag == nil || !flag.Changed {
		return nil
	}
	if !stdinIsTerminal() {
		return errors.New(
			"--auto requires an interactive terminal: it authorizes an agent to submit " +
				"reviews, so it must be given by a person running mrman, not by a program " +
				"invoking it")
	}
	events, err := ParseAutoGrant(flag.Value.String())
	if err != nil {
		return err
	}
	o.AutoSet = true
	o.GrantedEvents = events
	return nil
}

func newRootCmd(args *Args) *cobra.Command {
	root := &cobra.Command{
		Use:           "mrman",
		Short:         "Terminal code review with vim keybindings, sessions, and forge submission",
		Long:          "mrman renders a continuous diff for review in the terminal: comment at line/range/file/review scope, track reviewed state across runs, and export or submit the review to GitHub, GitLab, Azure DevOps, or Forgejo.",
		Version:       version.String(),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			args.Command = CommandTui
			return nil
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true

	addTuiFlags(root, &args.Tui)

	root.AddCommand(
		newTuiCmd(args),
		newPrCmd(args, "pr"),
		newPrCmd(args, "mr"),
		newDiffCmd(args),
		newReviewCmd(args),
	)
	return root
}

// newDiffCmd defines `mrman diff <old> <new>`: review the difference
// between two paths, neither of which need be in any repository.
//
// It is a subcommand rather than a `--diff old new` flag because it cannot
// be one. pflag has no two-value flag, and cobra's root rejects bare
// positionals once it has subcommands, so both spellings of a flag form
// fail to parse. The <old> <new> order is diff(1)'s.
//
// It resolves to CommandTui rather than a command of its own: the result is
// an ordinary review, and routing it through ui.Run keeps one TUI entry
// point instead of a parallel one that would drift.
func newDiffCmd(args *Args) *cobra.Command {
	return &cobra.Command{
		Use:   "diff <old> <new>",
		Short: "Review the difference between two paths, with no repository",
		Long: "Review the difference between two paths, with no repository.\n\n" +
			"Both must be files, or both directories. Neither need be in version " +
			"control, and they need not be related to each other — a vendored " +
			"dependency before and after an upgrade, a generated artifact from two " +
			"runs, or the same config in two environments.\n\n" +
			"Directories are paired by the path of each file relative to its root " +
			"and walked the way --file does, honouring .gitignore.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, positional []string) error {
			if err := rejectTargetFlags(cmd); err != nil {
				return err
			}
			args.Command = CommandTui
			args.Tui.DiffOld = positional[0]
			args.Tui.DiffNew = positional[1]
			return nil
		},
	}
}

// rejectTargetFlags refuses the review-target flags that `mrman diff`
// replaces.
//
// These are persistent flags on the root command, so
// MarkFlagsMutuallyExclusive cannot express the conflict from a subcommand
// and the check has to be made here. Same shape as rejectTuiFlags: a hard
// error naming the offending flag, rather than a silent precedence rule
// nobody could predict. Presentation flags (--theme, --appearance,
// --stdout) are deliberately absent — they say how to render a review, not
// which one to open.
func rejectTargetFlags(cmd *cobra.Command) error {
	targetFlags := []string{
		"revisions", "working-tree", "file", "all-files", "patch", "patch-strip", "path",
	}
	root := cmd.Root()
	for _, name := range targetFlags {
		if f := root.PersistentFlags().Lookup(name); f != nil && f.Changed {
			return fmt.Errorf(
				"`mrman diff` is its own review target, so it cannot be combined with --%s", name)
		}
	}
	return nil
}

func addTuiFlags(cmd *cobra.Command, o *TuiOptions) {
	f := cmd.PersistentFlags()
	f.StringVarP(&o.Revisions, "revisions", "r", "", "commit range or revset to review (VCS-specific syntax)")
	f.StringVar(&o.Theme, "theme", "", "theme name (bundled, or ~/.config/mrman/themes/<name>.toml)")
	f.StringVar(&o.Appearance, "appearance", "", "light|dark|system (used when no explicit theme)")
	f.StringVarP(&o.Path, "path", "p", "", "filter the diff to a file or directory prefix")
	f.BoolVarP(&o.WorkingTree, "working-tree", "w", false, "review working tree changes, skipping the selector")
	f.StringVar(&o.File, "file", "", "review a file or directory without any VCS")
	f.BoolVarP(&o.AllFiles, "all-files", "A", false, "annotate every tracked file (pristine mode, git only)")
	f.StringVar(&o.Patch, "patch", "", "review a .patch, .diff or mbox file without any repository")
	f.IntVar(&o.PatchStrip, "patch-strip", 0, "leading path components to strip from a patch (like patch -p, default 1)")
	f.BoolVar(&o.Stdout, "stdout", false, "export review markdown to stdout instead of the clipboard")
	f.StringVar(&o.RepoURL, "repo-url", "", "override the forge repository for MR operations")
	f.StringVar(&o.Forge, "forge", "", "forge for ambiguous targets: github|gitlab|azuredevops|forgejo")
	f.BoolVar(&o.JSON, "json", false, "open the merge request headlessly and print its session as JSON")
	f.String("auto", "", "authorize agent submits for this session: comment,draft,approve,request-changes (default comment,draft)")
	f.Lookup("auto").NoOptDefVal = " " // bare --auto means "the default set"

	cmd.MarkFlagsMutuallyExclusive("file", "path")
	cmd.MarkFlagsMutuallyExclusive("file", "revisions")
	cmd.MarkFlagsMutuallyExclusive("file", "working-tree")
	cmd.MarkFlagsMutuallyExclusive("file", "all-files")
	cmd.MarkFlagsMutuallyExclusive("all-files", "path")
	cmd.MarkFlagsMutuallyExclusive("all-files", "revisions")
	cmd.MarkFlagsMutuallyExclusive("all-files", "working-tree")
	// A patch artifact is its own review target: there is no repository to
	// filter, revise or compare against.
	cmd.MarkFlagsMutuallyExclusive("patch", "file")
	cmd.MarkFlagsMutuallyExclusive("patch", "all-files")
	cmd.MarkFlagsMutuallyExclusive("patch", "path")
	cmd.MarkFlagsMutuallyExclusive("patch", "revisions")
	cmd.MarkFlagsMutuallyExclusive("patch", "working-tree")
	// Load-bearing: --json is the agent-invocable path, so it must never be
	// able to issue a grant. See internal/cli/agentgrant.go.
	cmd.MarkFlagsMutuallyExclusive("json", "auto")
}

func newTuiCmd(args *Args) *cobra.Command {
	tui := &cobra.Command{
		Use:   "tui",
		Short: "Open the review TUI (same as running mrman with no subcommand)",
		RunE: func(_ *cobra.Command, _ []string) error {
			args.Command = CommandTui
			return nil
		},
	}
	tui.AddCommand(newPrCmd(args, "pr"), newPrCmd(args, "mr"))
	return tui
}

func newPrCmd(args *Args, name string) *cobra.Command {
	short := "Review a merge request from a forge"
	if name == "mr" {
		short = "Review a merge request from a forge (alias of pr)"
	}
	return &cobra.Command{
		Use:   name + " <target>",
		Short: short,
		Long:  short + ".\n\nTarget forms: a bare number (125), owner/repo#125, or a full merge-request URL.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, positional []string) error {
			args.Command = CommandPr
			args.PrTarget = positional[0]
			return nil
		},
	}
}

func newReviewCmd(args *Args) *cobra.Command {
	review := &cobra.Command{
		Use:   "review",
		Short: "Non-interactive session operations with JSON output (agent integration surface)",
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return rejectTuiFlags(cmd)
		},
	}
	review.PersistentFlags().StringVar(&args.Review.Repo, "repo", "", "repository selector: a checkout path or a forge coordinate")

	list := &cobra.Command{
		Use:   "list",
		Short: "List review sessions as JSON",
		RunE: func(_ *cobra.Command, _ []string) error {
			args.Command = CommandReviewList
			return nil
		},
	}
	list.Flags().BoolVar(&args.Review.All, "all", false, "list every session, ignoring --repo")

	add := &cobra.Command{
		Use:   "add [comment]",
		Short: "Add a comment to a session",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, positional []string) error {
			args.Command = CommandReviewAdd
			if len(positional) == 1 {
				args.Review.Comment = positional[0]
			}
			if args.Review.Comment == "" && args.Review.Input == "" {
				return fmt.Errorf("a comment argument or --input is required")
			}
			return nil
		},
	}
	addFlags := add.Flags()
	addFlags.StringVar(&args.Review.Session, "session", "", "session slug or path (required)")
	addFlags.StringVar(&args.Review.Input, "input", "", "JSON payload, @file, or - for stdin")
	addFlags.StringVar(&args.Review.Type, "type", "none", "comment type id")
	addFlags.StringVar(&args.Review.TargetFile, "target-file", "", "file path for file/line comments")
	addFlags.Uint32Var(&args.Review.Line, "line", 0, "line number for line comments")
	addFlags.Uint32Var(&args.Review.EndLine, "end-line", 0, "end line for range comments")
	addFlags.StringVar(&args.Review.Side, "side", "new", "old|new")
	addFlags.StringVar(&args.Review.Username, "username", "", "comment author")
	_ = add.MarkFlagRequired("session")

	comments := &cobra.Command{
		Use:     "comments",
		Aliases: []string{"get"},
		Short:   "Print a session's comments as JSON",
		RunE: func(_ *cobra.Command, _ []string) error {
			args.Command = CommandReviewComments
			return nil
		},
	}
	comments.Flags().StringVar(&args.Review.Session, "session", "", "session slug or path (required)")
	_ = comments.MarkFlagRequired("session")

	watch := &cobra.Command{
		Use:   "watch",
		Short: "Stream session changes as newline-delimited JSON until it closes",
		RunE: func(_ *cobra.Command, _ []string) error {
			args.Command = CommandReviewWatch
			return nil
		},
	}
	watchFlags := watch.Flags()
	watchFlags.StringVar(&args.Review.Session, "session", "", "session slug or path (required)")
	watchFlags.IntVar(&args.Review.TimeoutSeconds, "timeout", 0, "stop after N seconds (0 waits indefinitely)")
	watchFlags.IntVar(&args.Review.IntervalMS, "interval", 0, "poll interval in milliseconds")
	watchFlags.StringVar(&args.Review.Since, "since", "", "resume after this comment id instead of sending a snapshot")

	submit := &cobra.Command{
		Use:   "submit",
		Short: "Submit a review to the forge (requires the session's agent-submit grant)",
		RunE: func(_ *cobra.Command, _ []string) error {
			args.Command = CommandReviewSubmit
			return nil
		},
	}
	submitFlags := submit.Flags()
	submitFlags.StringVar(&args.Review.Session, "session", "", "session slug or path (required)")
	submitFlags.StringVar(&args.Review.Event, "event", "comment", "comment|approve|request-changes|draft")
	submitFlags.StringVar(&args.Review.Username, "username", "", "comment author recorded on submit")

	review.AddCommand(list, add, comments, watch, submit)
	return review
}

// rejectTuiFlags reproduces tuicr's ArgumentConflict: TUI options given
// alongside review subcommands are a hard error.
//
// `mrman diff`'s two paths need no entry here: they are subcommand
// positionals, not root flags, so they cannot reach a review subcommand in
// the first place.
func rejectTuiFlags(cmd *cobra.Command) error {
	tuiFlags := []string{
		"revisions", "theme", "appearance", "path", "working-tree",
		"file", "all-files", "stdout", "repo-url", "forge",
		"patch", "patch-strip", "json", "auto",
	}
	root := cmd.Root()
	for _, name := range tuiFlags {
		if f := root.PersistentFlags().Lookup(name); f != nil && f.Changed {
			return fmt.Errorf("TUI options cannot be used with `mrman review` (--%s)", name)
		}
	}
	return nil
}
