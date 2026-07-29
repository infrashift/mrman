package githubf

import (
	"context"
	"strings"
	"time"

	"github.com/shurcooL/githubv4"

	"github.com/infrashift/mrman/internal/forge"
)

// maxGraphQLPages bounds cursor pagination so a buggy or cyclic server
// cannot hang the client: 100 pages of 100 nodes is far beyond real PRs.
const maxGraphQLPages = 100

// gqlPageInfo is the shared relay pagination block.
type gqlPageInfo struct {
	HasNextPage bool
	EndCursor   *string
}

// gqlActor is an author reference; the login is all we consume.
type gqlActor struct {
	Login string
}

// gqlThreadComment mirrors one node of reviewThreads.comments.
type gqlThreadComment struct {
	ID        string
	Body      string
	Author    *gqlActor
	CreatedAt *time.Time
	URL       string
	ReplyTo   *struct {
		ID string
	}
}

// gqlThreadNode mirrors one reviewThreads node. Anchor fields (path, line,
// originalLine, diffSide) live on the thread, not the comments — GraphQL
// rejects the query otherwise (hard-won tuicr lesson).
type gqlThreadNode struct {
	ID           string
	IsResolved   bool
	IsOutdated   bool
	Path         string
	Line         *uint32
	OriginalLine *uint32
	StartLine    *uint32
	DiffSide     string
	Comments     struct {
		Nodes []gqlThreadComment
	} `graphql:"comments(first: 100)"`
}

// ListReviewThreads fetches all review threads with their resolved and
// outdated state via GraphQL (REST does not expose thread state).
func (d *Driver) ListReviewThreads(ctx context.Context, pr *forge.PullRequestDetails) ([]forge.RemoteReviewThread, error) {
	const op = "list_review_threads"
	var threads []forge.RemoteReviewThread
	variables := d.baseVariables(pr)
	for range maxGraphQLPages {
		var q struct {
			Repository struct {
				PullRequest struct {
					ReviewThreads struct {
						PageInfo gqlPageInfo
						Nodes    []gqlThreadNode
					} `graphql:"reviewThreads(first: 100, after: $after)"`
				} `graphql:"pullRequest(number: $number)"`
			} `graphql:"repository(owner: $owner, name: $name)"`
		}
		if err := d.gql.Query(ctx, &q, variables); err != nil {
			return nil, d.wrap(op, err)
		}
		conn := q.Repository.PullRequest.ReviewThreads
		for i := range conn.Nodes {
			threads = append(threads, convertThread(&conn.Nodes[i]))
		}
		if !nextCursor(variables, conn.PageInfo) {
			break
		}
	}
	return threads, nil
}

// convertThread maps a GraphQL thread node to the neutral type. When `line`
// is null on an outdated thread, `originalLine` still tells us roughly
// where the thread was anchored, so it serves as the fallback anchor.
func convertThread(node *gqlThreadNode) forge.RemoteReviewThread {
	side := forge.SideNew
	if node.DiffSide == "LEFT" {
		side = forge.SideOld
	}
	line := node.Line
	if line == nil {
		line = node.OriginalLine
	}
	comments := make([]forge.RemoteReviewComment, 0, len(node.Comments.Nodes))
	for i := range node.Comments.Nodes {
		raw := &node.Comments.Nodes[i]
		comment := forge.RemoteReviewComment{
			ID:        raw.ID,
			Body:      raw.Body,
			URL:       raw.URL,
			CreatedAt: raw.CreatedAt,
		}
		if raw.Author != nil {
			comment.Author = raw.Author.Login
		}
		if raw.ReplyTo != nil {
			comment.InReplyTo = raw.ReplyTo.ID
		}
		comments = append(comments, comment)
	}
	return forge.RemoteReviewThread{
		ID:         node.ID,
		Path:       node.Path,
		Line:       line,
		Side:       side,
		StartLine:  node.StartLine,
		IsResolved: node.IsResolved,
		IsOutdated: node.IsOutdated,
		Comments:   comments,
	}
}

// gqlReviewNode mirrors one reviews node for summaries.
type gqlReviewNode struct {
	ID          string
	State       string
	Body        string
	Author      *gqlActor
	SubmittedAt *time.Time
	URL         string
}

// ListReviewSummaries fetches review-level bodies. Reviews with empty
// bodies (bare approvals, integration reviews) are dropped — they have
// nothing to display. ReviewMetadata keeps them.
func (d *Driver) ListReviewSummaries(ctx context.Context, pr *forge.PullRequestDetails) ([]forge.RemoteReviewSummary, error) {
	const op = "list_review_summaries"
	var summaries []forge.RemoteReviewSummary
	variables := d.baseVariables(pr)
	for range maxGraphQLPages {
		var q struct {
			Repository struct {
				PullRequest struct {
					Reviews struct {
						PageInfo gqlPageInfo
						Nodes    []gqlReviewNode
					} `graphql:"reviews(first: 100, after: $after)"`
				} `graphql:"pullRequest(number: $number)"`
			} `graphql:"repository(owner: $owner, name: $name)"`
		}
		if err := d.gql.Query(ctx, &q, variables); err != nil {
			return nil, d.wrap(op, err)
		}
		conn := q.Repository.PullRequest.Reviews
		for i := range conn.Nodes {
			raw := &conn.Nodes[i]
			if strings.TrimSpace(raw.Body) == "" {
				continue
			}
			summary := forge.RemoteReviewSummary{
				ID:        raw.ID,
				Body:      raw.Body,
				URL:       raw.URL,
				State:     forge.ParseReviewState(raw.State),
				CreatedAt: raw.SubmittedAt,
			}
			if raw.Author != nil {
				summary.Author = raw.Author.Login
			}
			summaries = append(summaries, summary)
		}
		if !nextCursor(variables, conn.PageInfo) {
			break
		}
	}
	return summaries, nil
}

// ReviewMetadata fetches the viewer login plus every submitted review's
// author, time, and commit OID. Unlike summaries, empty-body reviews are
// kept: a bare approval still counts when deciding whether the PR gained
// commits since the viewer's last review.
func (d *Driver) ReviewMetadata(ctx context.Context, pr *forge.PullRequestDetails) (*forge.ReviewMetadata, error) {
	const op = "review_metadata"
	metadata := &forge.ReviewMetadata{}
	variables := d.baseVariables(pr)
	for range maxGraphQLPages {
		var q struct {
			Viewer struct {
				Login string
			}
			Repository struct {
				PullRequest struct {
					Reviews struct {
						PageInfo gqlPageInfo
						Nodes    []struct {
							Author      *gqlActor
							SubmittedAt *time.Time
							Commit      *struct {
								OID string
							}
						}
					} `graphql:"reviews(first: 100, after: $after)"`
				} `graphql:"pullRequest(number: $number)"`
			} `graphql:"repository(owner: $owner, name: $name)"`
		}
		if err := d.gql.Query(ctx, &q, variables); err != nil {
			return nil, d.wrap(op, err)
		}
		if metadata.ViewerLogin == "" {
			metadata.ViewerLogin = q.Viewer.Login
		}
		conn := q.Repository.PullRequest.Reviews
		for i := range conn.Nodes {
			raw := &conn.Nodes[i]
			record := forge.ReviewRecord{SubmittedAt: raw.SubmittedAt}
			if raw.Author != nil {
				record.Author = raw.Author.Login
			}
			if raw.Commit != nil {
				record.CommitOID = raw.Commit.OID
			}
			metadata.Reviews = append(metadata.Reviews, record)
		}
		if !nextCursor(variables, conn.PageInfo) {
			break
		}
	}
	return metadata, nil
}

// baseVariables builds the shared query variables; $after starts null so
// the first page needs no separate query shape.
func (d *Driver) baseVariables(pr *forge.PullRequestDetails) map[string]interface{} {
	return map[string]interface{}{
		"owner":  githubv4.String(pr.Repository.Owner),
		"name":   githubv4.String(pr.Repository.Name),
		"number": githubv4.Int(pr.Number),
		"after":  (*githubv4.String)(nil),
	}
}

// nextCursor advances the $after variable, reporting whether another page
// exists.
func nextCursor(variables map[string]interface{}, page gqlPageInfo) bool {
	if !page.HasNextPage || page.EndCursor == nil {
		return false
	}
	variables["after"] = githubv4.String(*page.EndCursor)
	return true
}
