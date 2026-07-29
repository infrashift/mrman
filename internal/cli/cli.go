package cli

import (
	"fmt"

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
	return args, nil
}

func newRootCmd(args *Args) *cobra.Command {
	root := &cobra.Command{
		Use:           "mrman",
		Short:         "Terminal code review with vim keybindings, sessions, and forge submission",
		Long:          "mrman renders a continuous diff for review in the terminal: comment at line/range/file/review scope, track reviewed state across runs, and export or submit the review to GitHub, GitLab, Azure DevOps, or Forgejo.",
		Version:       version.String(),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, positional []string) error {
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
		newReviewCmd(args),
	)
	return root
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
	f.BoolVar(&o.Stdout, "stdout", false, "export review markdown to stdout instead of the clipboard")
	f.StringVar(&o.RepoURL, "repo-url", "", "override the forge repository for PR operations")
	f.StringVar(&o.Forge, "forge", "", "forge for ambiguous targets: github|gitlab|azuredevops|forgejo")

	cmd.MarkFlagsMutuallyExclusive("file", "path")
	cmd.MarkFlagsMutuallyExclusive("file", "revisions")
	cmd.MarkFlagsMutuallyExclusive("file", "working-tree")
	cmd.MarkFlagsMutuallyExclusive("file", "all-files")
	cmd.MarkFlagsMutuallyExclusive("all-files", "path")
	cmd.MarkFlagsMutuallyExclusive("all-files", "revisions")
	cmd.MarkFlagsMutuallyExclusive("all-files", "working-tree")
}

func newTuiCmd(args *Args) *cobra.Command {
	tui := &cobra.Command{
		Use:   "tui",
		Short: "Open the review TUI (same as running mrman with no subcommand)",
		RunE: func(cmd *cobra.Command, positional []string) error {
			args.Command = CommandTui
			return nil
		},
	}
	tui.AddCommand(newPrCmd(args, "pr"), newPrCmd(args, "mr"))
	return tui
}

func newPrCmd(args *Args, name string) *cobra.Command {
	short := "Review a pull request from a forge"
	if name == "mr" {
		short = "Review a merge request from a forge (alias of pr)"
	}
	return &cobra.Command{
		Use:   name + " <target>",
		Short: short,
		Long:  short + ".\n\nTarget forms: a bare number (125), owner/repo#125, or a full PR/MR URL.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, positional []string) error {
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
		RunE: func(cmd *cobra.Command, _ []string) error {
			args.Command = CommandReviewList
			return nil
		},
	}
	list.Flags().BoolVar(&args.Review.All, "all", false, "list every session, ignoring --repo")

	add := &cobra.Command{
		Use:   "add [comment]",
		Short: "Add a comment to a session",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, positional []string) error {
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
		RunE: func(cmd *cobra.Command, _ []string) error {
			args.Command = CommandReviewComments
			return nil
		},
	}
	comments.Flags().StringVar(&args.Review.Session, "session", "", "session slug or path (required)")
	_ = comments.MarkFlagRequired("session")

	review.AddCommand(list, add, comments)
	return review
}

// rejectTuiFlags reproduces tuicr's ArgumentConflict: TUI options given
// alongside review subcommands are a hard error.
func rejectTuiFlags(cmd *cobra.Command) error {
	tuiFlags := []string{
		"revisions", "theme", "appearance", "path", "working-tree",
		"file", "all-files", "stdout", "repo-url", "forge",
	}
	root := cmd.Root()
	for _, name := range tuiFlags {
		if f := root.PersistentFlags().Lookup(name); f != nil && f.Changed {
			return fmt.Errorf("TUI options cannot be used with `mrman review` (--%s)", name)
		}
	}
	return nil
}
