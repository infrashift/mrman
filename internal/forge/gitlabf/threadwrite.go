package gitlabf

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	gitlab "gitlab.com/gitlab-org/api/client-go"

	"github.com/infrashift/mrman/internal/forge"
)

// hintNotOwnThread explains a refused resolution.
const hintNotOwnThread = "Only threads whose first comment this token's identity wrote can be " +
	"resolved from the command line; resolve other people's threads in the web UI."

var (
	_ forge.ThreadReplier  = (*Driver)(nil)
	_ forge.ThreadResolver = (*Driver)(nil)
)

// discussionIDPattern is the shape of a GitLab discussion id (a hex SHA on
// every instance seen), loosened only enough to stay a single URL path
// segment: the SDK formats the id into the request path unescaped.
var discussionIDPattern = regexp.MustCompile(`^[0-9A-Za-z_-]+$`)

// checkDiscussionID rejects a thread id that is not a discussion id.
func (d *Driver) checkDiscussionID(op, threadID string) error {
	if !discussionIDPattern.MatchString(threadID) {
		return d.err(op, forge.ErrorValidation, 0, "",
			fmt.Errorf("thread id %q is not a GitLab discussion id", threadID))
	}
	return nil
}

// ReplyToThread adds body as a note to discussion threadID and returns the
// new note's numeric id, the id form ListReviewThreads reports for comments.
//
// GitLab discussions are flat: a note joins the discussion, never a
// particular note, so parentID is ignored. The request is scoped to the
// merge request, so a discussion of another one answers 404. A reply adds
// to a discussion and changes no one's comment or status, so it is not
// limited to the token's own threads.
func (d *Driver) ReplyToThread(ctx context.Context, pr *forge.PullRequestDetails, threadID, _, body string) (string, error) {
	const op = "reply_to_thread"
	if err := d.checkDiscussionID(op, threadID); err != nil {
		return "", err
	}
	if strings.TrimSpace(body) == "" {
		return "", d.err(op, forge.ErrorValidation, 0, "", errors.New("an empty reply"))
	}
	return d.addDiscussionNote(ctx, op, pr, threadID, body)
}

// addDiscussionNote posts body into discussion threadID.
func (d *Driver) addDiscussionNote(ctx context.Context, op string, pr *forge.PullRequestDetails, threadID, body string) (string, error) {
	note, _, err := d.client.Discussions.AddMergeRequestDiscussionNote(
		projectID(pr.Repository), int64(pr.Number), threadID, //nolint:gosec // G115: pull request numbers are small forge-assigned integers
		&gitlab.AddMergeRequestDiscussionNoteOptions{Body: &body}, gitlab.WithContext(ctx))
	if err != nil {
		return "", d.wrap(op, err)
	}
	if note == nil {
		return "", nil
	}
	return strconv.FormatInt(note.ID, 10), nil
}

// ResolveOwnThread resolves discussion threadID, after adding reply to it
// when reply is not empty. The discussion's first note must be the
// viewer's own: its author's username is compared with the token's
// (GitLab usernames are unique, case-insensitively), never a display name.
// A discussion that is already resolved is left untouched.
func (d *Driver) ResolveOwnThread(ctx context.Context, pr *forge.PullRequestDetails, threadID, reply string) (forge.ResolveOutcome, error) {
	const op = "resolve_thread"
	if err := d.checkDiscussionID(op, threadID); err != nil {
		return "", err
	}
	viewer, err := d.currentUsername(ctx, op)
	if err != nil {
		return "", err
	}
	pid, iid := projectID(pr.Repository), int64(pr.Number) //nolint:gosec // G115: pull request numbers are small forge-assigned integers
	disc, _, err := d.client.Discussions.GetMergeRequestDiscussion(pid, iid, threadID, gitlab.WithContext(ctx))
	if err != nil {
		return "", d.wrap(op, err)
	}
	if disc == nil || len(disc.Notes) == 0 || disc.Notes[0] == nil {
		return "", d.err(op, forge.ErrorNotFound, 0, "",
			fmt.Errorf("discussion %s not found on merge request !%d", threadID, pr.Number))
	}
	root := disc.Notes[0]
	if root.Author.Username == "" || !strings.EqualFold(root.Author.Username, viewer) {
		return "", d.err(op, forge.ErrorForbidden, 0, hintNotOwnThread,
			errors.New("thread "+threadID+" was not started by this token's identity"))
	}
	if root.Resolved {
		return forge.ResolveAlreadyResolved, nil
	}
	if !root.Resolvable {
		return "", d.err(op, forge.ErrorValidation, 0, "",
			fmt.Errorf("discussion %s is a single comment, which GitLab cannot resolve", threadID))
	}
	if reply != "" {
		if _, err := d.addDiscussionNote(ctx, op, pr, threadID, reply); err != nil {
			return "", err
		}
	}
	if _, _, err := d.client.Discussions.ResolveMergeRequestDiscussion(pid, iid, threadID,
		&gitlab.ResolveMergeRequestDiscussionOptions{Resolved: new(true)}, gitlab.WithContext(ctx)); err != nil {
		return "", d.wrap(op, err)
	}
	return forge.ResolveResolved, nil
}
