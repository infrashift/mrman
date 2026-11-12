package githubf

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/shurcooL/githubv4"

	"github.com/infrashift/mrman/internal/forge"
)

var _ forge.ThreadReplier = (*Driver)(nil)

// gqlWriteTarget is what a thread write needs to know before it acts: who
// the token is, and the thread's pull request, state and first comment.
type gqlWriteTarget struct {
	Viewer struct {
		Login string
	}
	Node struct {
		Thread struct {
			ID          string
			IsResolved  bool
			PullRequest struct {
				Number     int
				Repository struct {
					Name  string
					Owner struct {
						Login string
					}
				}
			}
			Comments struct {
				Nodes []struct {
					ID     string
					Author *gqlActor
				}
			} `graphql:"comments(first: 1)"`
		} `graphql:"... on PullRequestReviewThread"`
	} `graphql:"node(id: $id)"`
}

// threadOnPR looks up review thread threadID and checks that it belongs to
// pr. Thread ids are global GraphQL node ids, so without the check a thread
// id from another pull request (or repository) would be written to, even
// though the grant or policy that allowed the write named this one.
func (d *Driver) threadOnPR(ctx context.Context, op string, pr *forge.PullRequestDetails, threadID string) (*gqlWriteTarget, error) {
	if strings.TrimSpace(threadID) == "" {
		return nil, d.err(op, forge.ErrorValidation, 0, "", errors.New("an empty thread id"))
	}
	var q gqlWriteTarget
	if err := d.gql.Query(ctx, &q, map[string]any{"id": githubv4.ID(threadID)}); err != nil {
		return nil, d.wrap(op, err)
	}
	th := &q.Node.Thread
	if th.ID == "" || th.PullRequest.Number != int(pr.Number) || //nolint:gosec // G115: pull request numbers are small forge-assigned integers
		!strings.EqualFold(th.PullRequest.Repository.Owner.Login, pr.Repository.Owner) ||
		!strings.EqualFold(th.PullRequest.Repository.Name, pr.Repository.Name) {
		return nil, d.err(op, forge.ErrorNotFound, 0, "",
			fmt.Errorf("%q is not a review thread of %s/%s#%d", threadID,
				pr.Repository.Owner, pr.Repository.Name, pr.Number))
	}
	return &q, nil
}

// ReplyToThread posts body into review thread threadID and returns the new
// comment's GraphQL node id (PRRC_…), the same id form ListReviewThreads
// reports for comments, so a later threads listing finds the reply by it.
//
// GitHub review threads are flat: a reply joins the thread, never a
// particular comment, so parentID is ignored. A reply adds to a discussion
// and changes no one's comment or status, so it is not limited to the
// token's own threads.
func (d *Driver) ReplyToThread(ctx context.Context, pr *forge.PullRequestDetails, threadID, _, body string) (string, error) {
	const op = "reply_to_thread"
	if strings.TrimSpace(body) == "" {
		return "", d.err(op, forge.ErrorValidation, 0, "", errors.New("an empty reply"))
	}
	if _, err := d.threadOnPR(ctx, op, pr, threadID); err != nil {
		return "", err
	}
	return d.addThreadReply(ctx, op, threadID, body)
}

// addThreadReply runs addPullRequestReviewThreadReply and returns the new
// comment's node id.
func (d *Driver) addThreadReply(ctx context.Context, op, threadID, body string) (string, error) {
	var m struct {
		AddPullRequestReviewThreadReply struct {
			Comment *struct {
				ID string
			}
		} `graphql:"addPullRequestReviewThreadReply(input: $input)"`
	}
	input := githubv4.AddPullRequestReviewThreadReplyInput{
		PullRequestReviewThreadID: githubv4.ID(threadID),
		Body:                      githubv4.String(body),
	}
	if err := d.gql.Mutate(ctx, &m, input, nil); err != nil {
		return "", d.wrap(op, err)
	}
	if c := m.AddPullRequestReviewThreadReply.Comment; c != nil {
		return c.ID, nil
	}
	return "", nil
}
