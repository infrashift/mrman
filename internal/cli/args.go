// Package cli parses the mrman command line into a plain Args value.
//
// The command tree and flag semantics are ported from tuicr's clap definition
// (.reference/tuicr/src/cli.rs): the same TUI options are accepted at the root
// and on the tui/pr subcommands, review subcommands reject TUI options, and
// hyphen-leading values are tolerated for --revisions and the review-add
// comment positional.
package cli

// Command identifies which top-level operation was requested.
type Command int

const (
	// CommandNone means no command ran (help or version was printed).
	CommandNone Command = iota
	// CommandTui opens the interactive review TUI (the default).
	CommandTui
	// CommandPr opens the TUI on a forge pull/merge request target.
	CommandPr
	// CommandReviewList lists persisted review sessions as JSON.
	CommandReviewList
	// CommandReviewAdd adds a comment to a session non-interactively.
	CommandReviewAdd
	// CommandReviewComments prints a session's comments as JSON.
	CommandReviewComments
	// CommandReviewWatch streams session changes as newline-delimited JSON.
	CommandReviewWatch
	// CommandReviewSubmit posts a review, behind the agent-submit grant.
	CommandReviewSubmit
	// CommandVersion prints the version line.
	CommandVersion
)

// TuiOptions carries every flag accepted by the TUI entrypoints. The zero
// value means "not set"; bools merge with OR and strings with later-wins, as
// in tuicr's TuiOptions::merge.
type TuiOptions struct {
	Revisions   string
	Theme       string
	Appearance  string
	Path        string
	WorkingTree bool
	File        string
	AllFiles    bool
	// Patch is a .patch, .diff or mbox artifact to review without a
	// repository; PatchStrip is how many leading path components to drop
	// from the paths it declares (0 means the -p1 convention).
	Patch      string
	PatchStrip int
	Stdout     bool
	RepoURL    string
	Forge      string
	// JSON opens a pull-request session headlessly and prints it, instead
	// of launching the TUI.
	JSON bool
	// AutoSet records that --auto was given; GrantedEvents is what it
	// authorizes. Kept separate so "flag absent" and "flag present but
	// granting nothing" cannot be confused.
	AutoSet       bool
	GrantedEvents []string
}

// ReviewOptions carries the flags of the review subcommands.
type ReviewOptions struct {
	Repo       string
	All        bool
	Session    string
	Input      string
	Type       string
	TargetFile string
	Line       uint32
	EndLine    uint32
	Side       string
	Username   string
	Comment    string
	// Event is the submit event for `review submit`.
	Event string
	// TimeoutSeconds and IntervalMS bound `review watch`; Since resumes it.
	TimeoutSeconds int
	IntervalMS     int
	Since          string
}

// Args is the fully parsed command line handed to main for dispatch.
type Args struct {
	Command  Command
	Tui      TuiOptions
	PrTarget string
	Review   ReviewOptions
}
