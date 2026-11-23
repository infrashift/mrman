// threads.go gives a bot memory of what it already said on a pull request
// (Vizzini ADR-0016, ISSUE-024 slice 2): `review threads` lists the PR's
// existing threads and pull-request-level comments, read-only; `review
// resolve` closes threads the token's own identity started, and `review
// edit` changes a pull-request-level comment it wrote, each behind the same
// interlock as a submit.

package agentsubmit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/infrashift/mrman/internal/config"
	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/persistence"
	"github.com/infrashift/mrman/internal/reviewcli"
	"github.com/infrashift/mrman/internal/slug"
)

// resolveEvent, replyEvent and editEvent are the grant and policy events
// for `review resolve`, `review reply` and `review edit`.
const (
	resolveEvent = "resolve"
	replyEvent   = "reply"
	editEvent    = "edit"
)

// ErrPartialResolve reports that some threads were not resolved; the JSON
// on stdout says which and why.
var ErrPartialResolve = errors.New("some threads were not resolved")

// ThreadsOutput is what `review threads` prints.
type ThreadsOutput struct {
	PR      string                     `json:"pr"`
	HeadSHA string                     `json:"head_sha"`
	Threads []forge.RemoteReviewThread `json:"threads"`
	// Summaries are the pull-request-level comments (review bodies on
	// GitHub, context-less threads on Azure DevOps, general notes on
	// GitLab), with the ids `review edit --summary` takes.
	Summaries []forge.RemoteReviewSummary `json:"summaries"`
}

// Threads prints the pull request's review threads (resolved and outdated
// ones included) and its pull-request-level comments for the session's PR.
// It only reads.
func Threads(store *persistence.Store, opts reviewcli.Options, out io.Writer) error {
	path, err := reviewcli.ResolveSessionPath(store, opts.Repo, opts.Session)
	if err != nil {
		return err
	}
	backend, details, pr, err := prForSession(store, path, opts.Session)
	if err != nil {
		return err
	}
	threads, err := backend.ListReviewThreads(context.Background(), details)
	if err != nil {
		return err
	}
	if threads == nil {
		threads = []forge.RemoteReviewThread{}
	}
	summaries, err := backend.ListReviewSummaries(context.Background(), details)
	if err != nil {
		return err
	}
	if summaries == nil {
		summaries = []forge.RemoteReviewSummary{}
	}
	return json.NewEncoder(out).Encode(ThreadsOutput{PR: pr, HeadSHA: details.HeadSHA, Threads: threads, Summaries: summaries})
}

// ResolveOptions configures `review resolve`.
type ResolveOptions struct {
	// Options carries the session and the grant settings (Event is ignored).
	Options
	// ThreadIDs are the forge thread ids to resolve.
	ThreadIDs []string
	// Reply, when set, is posted in each thread before it is resolved.
	Reply string
}

// ResolveFailure is one thread that was not resolved.
type ResolveFailure struct {
	Thread string `json:"thread"`
	Error  string `json:"error"`
}

// ResolveOutput is what `review resolve` prints.
type ResolveOutput struct {
	Resolved        []string             `json:"resolved"`
	AlreadyResolved []string             `json:"already_resolved"`
	Failed          []ResolveFailure     `json:"failed"`
	Authorization   *AuthorizationRecord `json:"authorization,omitempty"`
}

// Resolve resolves threads on the session's PR. It needs a human grant that
// names resolve, or (with --grant-policy) a policy that allows it; the
// driver itself refuses any thread the token's identity did not start.
func Resolve(store *persistence.Store, opts ResolveOptions, out io.Writer) error {
	if len(opts.ThreadIDs) == 0 {
		return errors.New("name at least one --thread")
	}
	path, err := reviewcli.ResolveSessionPath(store, opts.Repo, opts.Session)
	if err != nil {
		return err
	}
	if opts.GrantPolicy == "" {
		granted, reason, gerr := store.AgentSubmitGrant(path, resolveEvent)
		if gerr != nil {
			return gerr
		}
		if reason != persistence.GrantOK {
			return denial(reason, resolveEvent, granted)
		}
	}
	backend, details, pr, err := prForSession(store, path, opts.Session)
	if err != nil {
		return err
	}
	resolver, ok := forge.AsThreadResolver(backend)
	if !ok {
		return fmt.Errorf("%s: resolving threads from the command line is not supported on this forge", backend.ID())
	}
	ids := slices.Compact(slices.Sorted(slices.Values(opts.ThreadIDs)))

	output := ResolveOutput{Resolved: []string{}, AlreadyResolved: []string{}, Failed: []ResolveFailure{}}
	if opts.GrantPolicy != "" {
		if output.Authorization, err = authorizeAction(&opts.Options, resolveEvent, backend, details, pr, path, len(ids)); err != nil {
			return err
		}
	}
	for _, id := range ids {
		outcome, rerr := resolver.ResolveOwnThread(context.Background(), details, id, opts.Reply)
		switch {
		case rerr != nil:
			output.Failed = append(output.Failed, ResolveFailure{Thread: id, Error: rerr.Error()})
		case outcome == forge.ResolveAlreadyResolved:
			output.AlreadyResolved = append(output.AlreadyResolved, id)
		default:
			output.Resolved = append(output.Resolved, id)
		}
	}
	if err := json.NewEncoder(out).Encode(output); err != nil {
		return err
	}
	if len(output.Failed) > 0 {
		return ErrPartialResolve
	}
	return nil
}

// ReplyOptions configures `review reply`.
type ReplyOptions struct {
	// Options carries the session and the grant settings (Event is ignored).
	Options
	// ThreadID is the forge thread to answer in.
	ThreadID string
	// ParentID is the comment answered; empty means the thread's root.
	ParentID string
	// Body is the reply's Markdown.
	Body string
}

// ReplyOutput is what `review reply` prints.
type ReplyOutput struct {
	Thread        string               `json:"thread"`
	CommentID     string               `json:"comment_id"`
	Authorization *AuthorizationRecord `json:"authorization,omitempty"`
}

// Reply posts a reply into an existing thread of the session's PR, behind
// the same interlock as a submit (event reply).
func Reply(store *persistence.Store, opts ReplyOptions, out io.Writer) error {
	if opts.ThreadID == "" {
		return errors.New("--thread is required")
	}
	if strings.TrimSpace(opts.Body) == "" {
		return errors.New("a reply body is required")
	}
	path, err := reviewcli.ResolveSessionPath(store, opts.Repo, opts.Session)
	if err != nil {
		return err
	}
	if opts.GrantPolicy == "" {
		granted, reason, gerr := store.AgentSubmitGrant(path, replyEvent)
		if gerr != nil {
			return gerr
		}
		if reason != persistence.GrantOK {
			return denial(reason, replyEvent, granted)
		}
	}
	backend, details, pr, err := prForSession(store, path, opts.Session)
	if err != nil {
		return err
	}
	replier, ok := forge.AsThreadReplier(backend)
	if !ok {
		return fmt.Errorf("%s: replying in threads from the command line is not supported on this forge", backend.ID())
	}
	output := ReplyOutput{Thread: opts.ThreadID}
	if opts.GrantPolicy != "" {
		if output.Authorization, err = authorizeAction(&opts.Options, replyEvent, backend, details, pr, path, 1); err != nil {
			return err
		}
	}
	if output.CommentID, err = replier.ReplyToThread(context.Background(), details, opts.ThreadID, opts.ParentID, opts.Body); err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(output)
}

// EditOptions configures `review edit`.
type EditOptions struct {
	// Options carries the session and the grant settings (Event is ignored).
	Options
	// SummaryID is the pull-request-level comment to edit, as `review
	// threads` reports it under summaries.
	SummaryID string
	// Body is the comment's new Markdown.
	Body string
}

// EditOutput is what `review edit` prints.
type EditOutput struct {
	Summary       string               `json:"summary"`
	Authorization *AuthorizationRecord `json:"authorization,omitempty"`
}

// Edit replaces the body of a pull-request-level comment on the session's
// PR, behind the same interlock as a submit (event edit). The driver itself
// refuses a comment the token's identity did not write.
func Edit(store *persistence.Store, opts EditOptions, out io.Writer) error {
	if opts.SummaryID == "" {
		return errors.New("--summary is required")
	}
	if strings.TrimSpace(opts.Body) == "" {
		return errors.New("a body is required")
	}
	path, err := reviewcli.ResolveSessionPath(store, opts.Repo, opts.Session)
	if err != nil {
		return err
	}
	if opts.GrantPolicy == "" {
		granted, reason, gerr := store.AgentSubmitGrant(path, editEvent)
		if gerr != nil {
			return gerr
		}
		if reason != persistence.GrantOK {
			return denial(reason, editEvent, granted)
		}
	}
	backend, details, pr, err := prForSession(store, path, opts.Session)
	if err != nil {
		return err
	}
	editor, ok := forge.AsSummaryEditor(backend)
	if !ok {
		return fmt.Errorf("%s: editing comments from the command line is not supported on this forge", backend.ID())
	}
	output := EditOutput{Summary: opts.SummaryID}
	if opts.GrantPolicy != "" {
		if output.Authorization, err = authorizeAction(&opts.Options, editEvent, backend, details, pr, path, 1); err != nil {
			return err
		}
	}
	if err := editor.EditOwnSummary(context.Background(), details, opts.SummaryID, opts.Body); err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(output)
}

// prForSession loads the session at path and fetches its pull request: the
// forge, the PR's current details and the session slug.
func prForSession(store *persistence.Store, path, name string) (forge.Forge, *forge.PullRequestDetails, string, error) {
	session, err := store.LoadSession(path)
	if err != nil {
		return nil, nil, "", err
	}
	if session.PrSessionKey == nil {
		return nil, nil, "", fmt.Errorf("session %q is a local review, not a merge request", name)
	}
	cfg, warnings := config.Load()
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "mrman: config: %s\n", w)
	}
	key := session.PrSessionKey
	backend, err := forge.ForRepository(key.Repository, cfg.Forge)
	if err != nil {
		return nil, nil, "", err
	}
	repo := key.Repository
	details, err := backend.GetPullRequest(context.Background(), forge.Target{Repository: &repo, Number: key.Number})
	if err != nil {
		return nil, nil, "", err
	}
	pr := name
	if s, serr := slug.ForSession(session); serr == nil {
		pr = s.String()
	}
	return backend, details, pr, nil
}
