package gitlabf

import (
	"context"
	"strconv"
	"strings"
	"time"

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

// ListReviewSummaries returns the MR's general discussions, the notes with
// no diff position, review bodies among them. GitLab has no review object
// (the ReviewSummaries capability is false), so these are the closest thing:
// one summary per discussion, its first note's author and body. The app
// draws no path-less thread, so without this they never appeared.
func (d *Driver) ListReviewSummaries(ctx context.Context, pr *forge.PullRequestDetails) ([]forge.RemoteReviewSummary, error) {
	threads, err := d.ListReviewThreads(ctx, pr)
	if err != nil {
		return nil, err
	}
	var summaries []forge.RemoteReviewSummary
	for _, th := range threads {
		root := th.Root()
		if th.Path != "" || root == nil || strings.TrimSpace(root.Body) == "" {
			continue
		}
		summaries = append(summaries, forge.RemoteReviewSummary{
			ID: th.ID, Author: root.Author, Body: root.Body,
			State: forge.ReviewCommented, CreatedAt: root.CreatedAt,
		})
	}
	return summaries, nil
}

// ReviewMetadata reports the viewer login plus one review record per
// approval and per comment, each stamped with the head commit of the MR
// version that was current when it was made. That is what "commits since
// your last review" needs: an approval given before later pushes must not
// count as having reviewed them, and GitLab does not reset approvals on
// push unless a project setting says so.
//
// The approvals API carries no timestamps, so approvals and requests for
// changes are read from the system notes GitLab writes for them, which can
// lag the event by a few seconds. Sub-call failures
// degrade to partial metadata rather than failing the review (tuicr
// parity); a record that cannot be placed on a version is left out, since
// a wrong commit would preselect the wrong commits.
func (d *Driver) ReviewMetadata(ctx context.Context, pr *forge.PullRequestDetails) (*forge.ReviewMetadata, error) {
	const op = "review_metadata"
	metadata := &forge.ReviewMetadata{}
	if viewer, err := d.currentUsername(ctx, op); err == nil {
		metadata.ViewerLogin = viewer
	}
	versions := d.diffVersions(ctx, pr)
	if len(versions) == 0 {
		return metadata, nil
	}
	notes, err := d.mergeRequestNotes(ctx, pr)
	if err != nil {
		return metadata, nil //nolint:nilerr // review records are optional metadata; the threads still render
	}
	for _, note := range notes {
		if note == nil || note.CreatedAt == nil || !isReviewNote(note) {
			continue
		}
		head := headAt(versions, *note.CreatedAt)
		if head == "" {
			continue
		}
		metadata.Reviews = append(metadata.Reviews, forge.ReviewRecord{
			Author:      note.Author.Username,
			SubmittedAt: note.CreatedAt,
			CommitOID:   head,
		})
	}
	return metadata, nil
}

// reviewSystemNotes are the bodies of the system notes GitLab writes when a
// reviewer approves or requests changes. GitLab writes them asynchronously:
// on gitlab.com the approval note appeared about seven seconds after the
// approval itself.
var reviewSystemNotes = []string{"approved this merge request", "requested changes"}

// isReviewNote reports whether a note is review activity: a comment, or the
// system note that records an approval or a request for changes.
func isReviewNote(note *gitlab.Note) bool {
	if note.System {
		for _, body := range reviewSystemNotes {
			if strings.HasPrefix(note.Body, body) {
				return true
			}
		}
		return false
	}
	return strings.TrimSpace(note.Body) != ""
}

// mergeRequestNotes pages through every note on the MR, system notes
// included.
func (d *Driver) mergeRequestNotes(ctx context.Context, pr *forge.PullRequestDetails) ([]*gitlab.Note, error) {
	var notes []*gitlab.Note
	page := int64(1)
	for range maxDiscussionPages {
		rows, resp, err := d.client.Notes.ListMergeRequestNotes(
			projectID(pr.Repository), int64(pr.Number), //nolint:gosec // G115: pull request numbers are small forge-assigned integers
			&gitlab.ListMergeRequestNotesOptions{ListOptions: gitlab.ListOptions{Page: page, PerPage: 100}},
			gitlab.WithContext(ctx))
		if err != nil {
			return nil, err
		}
		notes = append(notes, rows...)
		if resp == nil || resp.NextPage == 0 {
			break
		}
		page = resp.NextPage
	}
	return notes, nil
}

// diffVersions pages through the MR's diff versions, newest first as GitLab
// lists them; nil when they are unavailable.
func (d *Driver) diffVersions(ctx context.Context, pr *forge.PullRequestDetails) []*gitlab.MergeRequestDiffVersion {
	var versions []*gitlab.MergeRequestDiffVersion
	page := int64(1)
	for range maxVersionPages {
		rows, resp, err := d.client.MergeRequests.GetMergeRequestDiffVersions(
			projectID(pr.Repository), int64(pr.Number), //nolint:gosec // G115: pull request numbers are small forge-assigned integers
			&gitlab.GetMergeRequestDiffVersionsOptions{ListOptions: gitlab.ListOptions{Page: page, PerPage: 100}},
			gitlab.WithContext(ctx))
		if err != nil {
			return nil
		}
		versions = append(versions, rows...)
		if resp == nil || resp.NextPage == 0 {
			break
		}
		page = resp.NextPage
	}
	return versions
}

// headAt returns the head commit of the newest version created at or before
// t, "" when every version is newer.
func headAt(versions []*gitlab.MergeRequestDiffVersion, t time.Time) string {
	var best *gitlab.MergeRequestDiffVersion
	for _, v := range versions {
		if v == nil || v.CreatedAt == nil || v.CreatedAt.After(t) {
			continue
		}
		if best == nil || v.CreatedAt.After(*best.CreatedAt) {
			best = v
		}
	}
	if best == nil {
		return ""
	}
	return best.HeadCommitSHA
}
