package githubf

import (
	"context"
	"errors"
	"strings"

	"github.com/shurcooL/githubv4"

	"github.com/infrashift/mrman/internal/forge"
)

// hintNotOwnThread explains a refused resolution.
const hintNotOwnThread = "Only threads whose first comment this token's identity wrote can be " +
	"resolved from the command line; resolve other people's threads in the web UI."

var _ forge.ThreadResolver = (*Driver)(nil)

// ResolveOwnThread resolves an open review thread with resolveReviewThread,
// after posting reply into it when reply is not empty. The thread's first
// comment must be the viewer's own: its author login is compared with the
// login the token authenticates as, both read live in one query (a login
// names one account at any moment, so the comparison cannot be spoofed by a
// display name). A thread that is already resolved is left untouched.
func (d *Driver) ResolveOwnThread(ctx context.Context, pr *forge.PullRequestDetails, threadID, reply string) (forge.ResolveOutcome, error) {
	const op = "resolve_thread"
	target, err := d.threadOnPR(ctx, op, pr, threadID)
	if err != nil {
		return "", err
	}
	th := &target.Node.Thread
	if len(th.Comments.Nodes) == 0 || th.Comments.Nodes[0].Author == nil ||
		!sameLogin(th.Comments.Nodes[0].Author.Login, target.Viewer.Login) {
		return "", d.err(op, forge.ErrorForbidden, 0, hintNotOwnThread,
			errors.New("thread "+threadID+" was not started by this token's identity"))
	}
	if th.IsResolved {
		return forge.ResolveAlreadyResolved, nil
	}
	if reply != "" {
		if _, err := d.addThreadReply(ctx, op, threadID, reply); err != nil {
			return "", err
		}
	}
	var m struct {
		ResolveReviewThread struct {
			Thread *struct {
				IsResolved bool
			}
		} `graphql:"resolveReviewThread(input: $input)"`
	}
	if err := d.gql.Mutate(ctx, &m, githubv4.ResolveReviewThreadInput{ThreadID: githubv4.ID(threadID)}, nil); err != nil {
		return "", d.wrap(op, err)
	}
	return forge.ResolveResolved, nil
}

// sameLogin compares two GitHub logins. Logins are case-insensitive, and a
// GitHub App's login carries a "[bot]" suffix in some API fields and not in
// others (a Bot comment author is plain "name"), so the suffix is dropped
// from both. GitHub only lets an app share a name with the account that owns
// it, so this cannot match a stranger. An empty login matches nothing.
func sameLogin(a, b string) bool {
	a, b = strings.TrimSuffix(a, "[bot]"), strings.TrimSuffix(b, "[bot]")
	return a != "" && strings.EqualFold(a, b)
}
