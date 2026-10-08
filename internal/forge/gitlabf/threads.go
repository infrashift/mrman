package gitlabf

import (
	"context"
	"strconv"

	gitlab "gitlab.com/gitlab-org/api/client-go"

	"github.com/infrashift/mrman/internal/forge"
)

// Pagination bounds for discussion and version listings (tuicr parity).
const (
	maxDiscussionPages = 100
	maxVersionPages    = 100
)

// ListReviewThreads fetches MR discussions and converts them to neutral
// threads: positioned (inline) discussions anchor to a file line, and
// individual notes (general MR comments) become path-less threads. System
// notes and empty bodies are dropped. GitLab exposes no outdated flag, so
// IsOutdated is always false (ThreadOutdated capability is false).
func (d *Driver) ListReviewThreads(ctx context.Context, pr *forge.PullRequestDetails) ([]forge.RemoteReviewThread, error) {
	const op = "list_review_threads"
	var threads []forge.RemoteReviewThread
	page := int64(1)
	for range maxDiscussionPages {
		rows, resp, err := d.client.Discussions.ListMergeRequestDiscussions(
			projectID(pr.Repository), int64(pr.Number), //nolint:gosec // G115: pull request numbers are small forge-assigned integers
			&gitlab.ListMergeRequestDiscussionsOptions{
				ListOptions: gitlab.ListOptions{Page: page, PerPage: 100},
			}, gitlab.WithContext(ctx))
		if err != nil {
			return nil, d.wrap(op, err)
		}
		for _, row := range rows {
			if thread, ok := convertDiscussion(row); ok {
				threads = append(threads, thread)
			}
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		page = resp.NextPage
	}
	return threads, nil
}

// convertDiscussion ports tuicr's GlabDiscussion::into_review_thread:
// ok=false drops discussions with no displayable content.
func convertDiscussion(disc *gitlab.Discussion) (forge.RemoteReviewThread, bool) {
	if disc == nil || len(disc.Notes) == 0 {
		return forge.RemoteReviewThread{}, false
	}
	root := disc.Notes[0]
	if disc.IndividualNote {
		return convertIndividualNote(disc, root)
	}
	// Only inline (positioned) discussions have a position on the root
	// note; skip non-text positions (e.g. image diffs).
	pos := root.Position
	if pos == nil || pos.PositionType != "text" {
		return forge.RemoteReviewThread{}, false
	}
	var (
		path string
		line uint32
		side forge.Side
	)
	switch {
	case pos.NewLine != 0:
		path = pos.NewPath
		line = uint32(pos.NewLine) //nolint:gosec // G115: line numbers fit uint32
		side = forge.SideNew
	case pos.OldLine != 0:
		path = pos.OldPath
		if path == "" {
			path = pos.NewPath
		}
		line = uint32(pos.OldLine) //nolint:gosec // G115: line numbers fit uint32
		side = forge.SideOld
	default:
		return forge.RemoteReviewThread{}, false
	}
	if path == "" {
		return forge.RemoteReviewThread{}, false
	}
	comments := make([]forge.RemoteReviewComment, 0, len(disc.Notes))
	for _, note := range disc.Notes {
		comments = append(comments, convertNote(note))
	}
	return forge.RemoteReviewThread{
		ID:         disc.ID,
		Path:       path,
		Line:       &line,
		Side:       side,
		StartLine:  rangeStart(pos.LineRange, side, line),
		IsResolved: root.Resolved,
		IsOutdated: false,
		Comments:   comments,
	}, true
}

// rangeStart reads a multi-line thread's first line from its line_range, on
// the thread's own side. It is nil for a single-line thread, and for a range
// whose start has no line on that side (a range starting on a deletion and
// ending on an addition), which the neutral thread cannot express.
func rangeStart(lr *gitlab.LineRange, side forge.Side, end uint32) *uint32 {
	if lr == nil || lr.StartRange == nil {
		return nil
	}
	start := lr.StartRange.NewLine
	if side == forge.SideOld {
		start = lr.StartRange.OldLine
	}
	if start <= 0 || start >= int64(end) {
		return nil
	}
	first := uint32(start) //nolint:gosec // G115: 0 < start < end, which is a uint32
	return &first
}

// convertIndividualNote maps a general MR note (no diff position) onto a
// path-less thread, skipping system events and empty bodies.
func convertIndividualNote(disc *gitlab.Discussion, root *gitlab.Note) (forge.RemoteReviewThread, bool) {
	if root.System || root.Body == "" {
		return forge.RemoteReviewThread{}, false
	}
	var comments []forge.RemoteReviewComment
	for _, note := range disc.Notes {
		if note.System || note.Body == "" {
			continue
		}
		comments = append(comments, convertNote(note))
	}
	if len(comments) == 0 {
		return forge.RemoteReviewThread{}, false
	}
	return forge.RemoteReviewThread{
		ID:       disc.ID,
		Path:     "",
		Side:     forge.SideNew,
		Comments: comments,
	}, true
}

// convertNote maps one discussion note to a neutral comment. GitLab's
// discussion payload carries no per-note permalink, so URL stays empty.
func convertNote(note *gitlab.Note) forge.RemoteReviewComment {
	return forge.RemoteReviewComment{
		ID:        strconv.FormatInt(note.ID, 10),
		Author:    note.Author.Username,
		Body:      note.Body,
		CreatedAt: note.CreatedAt,
	}
}

// ListReviewSummaries returns no summaries: GitLab has no review-level
// bodies distinct from threads (the ReviewSummaries capability is false);
// general MR notes surface through ListReviewThreads instead.
func (d *Driver) ListReviewSummaries(_ context.Context, _ *forge.PullRequestDetails) ([]forge.RemoteReviewSummary, error) {
	return nil, nil
}

// ReviewMetadata reports the viewer login plus one review record per
// approver. tuicr's full composite (approvals cross-referenced with MR
// version timestamps plus every discussion note) is deliberately
// simplified: the SDK's approval state carries no approval timestamps, so
// each approval is anchored to the latest MR version's head commit — the
// approximation GitLab itself uses when approvals reset on push. Sub-call
// failures degrade to partial metadata rather than failing the review
// (tuicr parity).
func (d *Driver) ReviewMetadata(ctx context.Context, pr *forge.PullRequestDetails) (*forge.ReviewMetadata, error) {
	const op = "review_metadata"
	metadata := &forge.ReviewMetadata{}
	if viewer, err := d.currentUsername(ctx, op); err == nil {
		metadata.ViewerLogin = viewer
	}
	latestHead := d.latestVersionHead(ctx, pr)
	approvals, _, err := d.client.MergeRequestApprovals.GetConfiguration(
		projectID(pr.Repository), int64(pr.Number), gitlab.WithContext(ctx)) //nolint:gosec // G115: pull request numbers are small forge-assigned integers
	if err != nil || approvals == nil {
		return metadata, nil //nolint:nilerr // approvals are optional metadata; the threads still render
	}
	for _, approver := range approvals.ApprovedBy {
		if approver == nil || approver.User == nil {
			continue
		}
		metadata.Reviews = append(metadata.Reviews, forge.ReviewRecord{
			Author:    approver.User.Username,
			CommitOID: latestHead,
		})
	}
	return metadata, nil
}

// latestVersionHead returns the head commit SHA of the newest MR diff
// version, "" when versions are unavailable. GitLab lists versions newest
// first.
func (d *Driver) latestVersionHead(ctx context.Context, pr *forge.PullRequestDetails) string {
	versions, _, err := d.client.MergeRequests.GetMergeRequestDiffVersions(
		projectID(pr.Repository), int64(pr.Number), //nolint:gosec // G115: pull request numbers are small forge-assigned integers
		&gitlab.GetMergeRequestDiffVersionsOptions{
			ListOptions: gitlab.ListOptions{Page: 1, PerPage: 1},
		},
		gitlab.WithContext(ctx))
	if err != nil || len(versions) == 0 {
		return ""
	}
	return versions[0].HeadCommitSHA
}
