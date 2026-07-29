// Package agentsubmit posts a review from the command line, behind a grant
// a human must issue.
//
// It sits above both app and reviewcli: it needs the state machine to map
// comments onto the diff, and the CLI package to resolve sessions — and
// reviewcli sits below app, since it owns the comment-insertion primitive
// app builds on.
//
// This is the only CLI path that writes to a forge, and the only one gated.
//
// The grant is not a flag on this command: the agent writes this command
// line, so a flag here would authorize nothing. It is issued by a human
// running `mrman pr <target> --auto` at a terminal and held against that
// TUI's process id, so it exists only while a person has the review open and
// evaporates when they close it.
//
// This is a deliberate-action interlock, not a security boundary. An agent
// with a shell can call the forge API directly. What this prevents is an
// agent submitting a review through mrman by accident, by misreading its
// instructions, or by deciding unilaterally that it would be helpful — and
// it guarantees that when submission is possible, a human is looking at a
// screen that says so.
package agentsubmit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/infrashift/mrman/internal/app"
	"github.com/infrashift/mrman/internal/config"
	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/model"
	"github.com/infrashift/mrman/internal/persistence"
	"github.com/infrashift/mrman/internal/prload"
	"github.com/infrashift/mrman/internal/reviewcli"
	"github.com/infrashift/mrman/internal/vcs"
)

// Options are the `mrman review submit` flags.
type Options struct {
	reviewcli.Options
	// Event is comment | approve | request-changes | draft.
	Event string
}

// SubmitDenied is the JSON an agent gets when the interlock refuses.
//
// Message names the remedy, but the remedy is deliberately something only
// the human can perform: it requires relaunching mrman interactively.
// Nothing here is actionable by the agent alone, which is the point.
type SubmitDenied struct {
	Code           string   `json:"error"`
	Reason         string   `json:"reason"`
	RequestedEvent string   `json:"requested_event"`
	GrantedEvents  []string `json:"granted_events"`
	Message        string   `json:"message"`
}

// Error makes a denial usable as an error while still carrying its shape.
func (d *SubmitDenied) Error() string { return d.Message }

// submitEvents maps the CLI spelling to the forge event.
var submitEvents = map[string]forge.SubmitEvent{
	"comment":         forge.SubmitComment,
	"approve":         forge.SubmitApprove,
	"request-changes": forge.SubmitRequestChanges,
	"draft":           forge.SubmitDraft,
}

// SubmitResultOutput is what a successful submit prints.
type SubmitResultOutput struct {
	Submitted     bool   `json:"submitted"`
	Event         string `json:"event"`
	ReviewID      string `json:"review_id,omitempty"`
	URL           string `json:"url,omitempty"`
	State         string `json:"state,omitempty"`
	InlineCount   int    `json:"inline_count"`
	OmittedCount  int    `json:"omitted_count"`
	MovedToBody   int    `json:"moved_to_body"`
	LockedComment int    `json:"locked_comments"`
}

// Submit posts a review from the CLI, after checking the interlock.
//
// The grant is resolved before any session or forge work, so a refusal
// cannot leave partial state behind and cannot be mistaken for a network
// failure.
func Submit(store *persistence.Store, opts Options, out io.Writer) error {
	event, ok := submitEvents[strings.ToLower(strings.TrimSpace(opts.Event))]
	if !ok {
		return fmt.Errorf("unknown submit event %q (valid: approve, comment, draft, request-changes)", opts.Event)
	}
	path, err := reviewcli.ResolveSessionPath(store, opts.Repo, opts.Session)
	if err != nil {
		return err
	}

	// The interlock, first and unconditionally.
	granted, reason, err := store.AgentSubmitGrant(path, opts.Event)
	if err != nil {
		return err
	}
	if reason != persistence.GrantOK {
		return denial(reason, opts.Event, granted)
	}

	session, err := store.LoadSession(path)
	if err != nil {
		return err
	}
	a, err := prAppForSession(session, opts)
	if err != nil {
		return err
	}
	if !a.StartSubmitWith(event, true) {
		return submitRefusedMessage(a)
	}

	inline := len(a.Submit.Mappable)
	moved := len(a.Submit.MovedToSummary())
	omitted := len(a.Submit.Unmappable) - moved

	result, _, err := app.SubmitReview(a, "")
	if err != nil {
		return err
	}
	if _, err := store.SaveSession(a.Session); err != nil {
		return fmt.Errorf("review submitted but the session could not be saved: %w", err)
	}

	output := SubmitResultOutput{
		Submitted:     true,
		Event:         opts.Event,
		InlineCount:   inline,
		OmittedCount:  omitted,
		MovedToBody:   moved,
		LockedComment: lockedCount(a.Session),
	}
	if result != nil {
		output.ReviewID, output.URL, output.State = result.ReviewID, result.URL, result.State
	}
	return json.NewEncoder(out).Encode(output)
}

// denial builds the refusal, with a message that says what a human must do.
func denial(reason persistence.GrantReason, event string, granted []string) error {
	d := &SubmitDenied{
		Code:           "agent_submit_not_permitted",
		Reason:         string(reason),
		RequestedEvent: event,
		GrantedEvents:  granted,
	}
	if d.GrantedEvents == nil {
		d.GrantedEvents = []string{}
	}
	switch reason {
	case persistence.GrantEventNotAllowed:
		d.Message = fmt.Sprintf(
			"This session allows %s but not %q. Ask the user to reopen it with: mrman pr <target> --auto=%s",
			strings.Join(granted, ","), event, event)
	case persistence.GrantExpired:
		d.Message = "The agent-submit grant expired when its mrman session closed. " +
			"Ask the user to reopen the review with: mrman pr <target> --auto=" + event
	default:
		d.Message = "This session has no agent-submit grant. Ask the user to open the review " +
			"interactively with: mrman pr <target> --auto=" + event
	}
	return d
}

// submitRefusedMessage turns a preflight refusal into an error, using the
// status message the app already produced.
func submitRefusedMessage(a *app.App) error {
	if a.Message != nil {
		return fmt.Errorf("cannot submit: %s", a.Message.Content)
	}
	return fmt.Errorf("cannot submit: nothing to submit")
}

func lockedCount(session *model.ReviewSession) int {
	n := 0
	count := func(comments []*model.Comment) {
		for _, c := range comments {
			if c.IsLocked() {
				n++
			}
		}
	}
	count(session.ReviewComments)
	for _, review := range session.Files {
		count(review.FileComments)
		for _, comments := range review.LineComments {
			count(comments)
		}
	}
	return n
}

// prAppForSession rebuilds enough app state to run a submit against a
// persisted PR session: the forge driver, the pull request, and the diff the
// comments anchor into.
func prAppForSession(session *model.ReviewSession, opts Options) (*app.App, error) {
	if session.PrSessionKey == nil {
		return nil, fmt.Errorf("session %q is a local review, not a pull request", opts.Session)
	}
	cfg, _ := config.Load()
	key := session.PrSessionKey
	backend, err := forge.ForRepository(key.Repository, cfg.Forge)
	if err != nil {
		return nil, err
	}
	repo := key.Repository
	load, err := prload.Fetch(context.Background(), backend, &repo,
		forge.Target{Repository: &repo, Number: key.Number}, nil, "")
	if err != nil {
		return nil, err
	}
	if load.Details.HeadSHA != key.HeadSHA {
		return nil, fmt.Errorf(
			"the pull request advanced to %.7s since this session was opened (%.7s); "+
				"ask the user to reload it in mrman before submitting",
			load.Details.HeadSHA, key.HeadSHA)
	}

	a := app.NewApp(load.VCS, &submitVcsInfo, load.Files, session,
		app.DiffSource{Kind: app.DiffSourcePullRequest})
	a.Pr = &app.PrState{
		Backend: load.Backend, Repository: load.Repository,
		Details: load.Details, Commits: load.Commits,
	}
	a.CommentTypePrefix = cfg.Forge.CommentTypePrefix
	if opts.Username != "" {
		a.Username = opts.Username
	}
	return a, nil
}

// WriteDenial renders a refusal as JSON on out, so a denial is
// machine-readable on the same stream as every other result.
func WriteDenial(out io.Writer, d *SubmitDenied) error {
	return json.NewEncoder(out).Encode(d)
}

// submitVcsInfo stands in for a checkout: a PR submit reads nothing from it.
var submitVcsInfo = vcs.Info{Type: vcs.TypeGit}
