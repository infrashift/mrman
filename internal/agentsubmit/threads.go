// threads.go gives a bot memory of what it already said on a pull request
// (Vizzini ADR-0016, ISSUE-024 slice 2): `review threads` lists the PR's
// existing threads, read-only, and `review resolve` closes threads the
// token's own identity started, behind the same interlock as a submit.

package agentsubmit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/infrashift/mrman/internal/config"
	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/persistence"
	"github.com/infrashift/mrman/internal/reviewcli"
	"github.com/infrashift/mrman/internal/slug"
)

// resolveEvent is the grant and policy event for `review resolve`.
const resolveEvent = "resolve"

// ErrPartialResolve reports that some threads were not resolved; the JSON
// on stdout says which and why.
var ErrPartialResolve = errors.New("some threads were not resolved")

// ThreadsOutput is what `review threads` prints.
type ThreadsOutput struct {
	PR      string                     `json:"pr"`
	HeadSHA string                     `json:"head_sha"`
	Threads []forge.RemoteReviewThread `json:"threads"`
}

// Threads prints the pull request's file-anchored review threads (resolved
// and outdated ones included) for the session's PR. It only reads.
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
	return json.NewEncoder(out).Encode(ThreadsOutput{PR: pr, HeadSHA: details.HeadSHA, Threads: threads})
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
