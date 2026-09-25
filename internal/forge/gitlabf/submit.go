package gitlabf

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // GitLab's line_code format mandates SHA-1.
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	gitlab "gitlab.com/gitlab-org/api/client-go"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/submit"
)

// requestChangesMutation is the GraphQL mutation behind "request changes"
// (GitLab has no REST endpoint for it; requires GitLab >= 15.11).
const requestChangesMutation = `mutation($projectPath: ID!, $iid: String!) {` +
	` mergeRequestRequestChanges(input: { projectPath: $projectPath, iid: $iid }) { errors } }`

// CreateReview submits a review as GitLab's N+1 sequence, ported from
// tuicr's glab.rs: (1) a non-empty body posts as a general MR note (draft
// note when the event is Draft); (2) each inline comment posts as a
// positioned discussion (or draft note); (3) Approve calls the approve
// endpoint and RequestChanges runs the GraphQL mutation.
//
// Partial-failure contract: when a step fails after anything was posted,
// CreateReview returns a non-nil *forge.SubmitResult whose Partial field
// lists the comment IDs that made it (FailedAt indexes req.Comments;
// len(req.Comments) means the final approve/request-changes step) together
// with a nil error — the caller flips only the succeeded comments and
// surfaces Partial.Cause. A failure before anything was posted returns a
// plain error and a nil result.
func (d *Driver) CreateReview(ctx context.Context, pr *forge.PullRequestDetails, req forge.CreateReviewRequest) (*forge.SubmitResult, error) {
	const op = "create_review"
	if req.Event < forge.SubmitComment || req.Event > forge.SubmitDraft {
		return nil, d.err(op, forge.ErrorValidation, 0, "",
			fmt.Errorf("unknown submit event %d", req.Event))
	}
	// Draft submissions create GitLab draft notes (the pending-review
	// primitive) and stop short of publishing, mirroring GitHub's pending
	// review. The author publishes from the GitLab "Submit review" UI.
	isDraft := req.Event == forge.SubmitDraft
	pid := projectID(pr.Repository)
	iid := int64(pr.Number) //nolint:gosec // G115: pull request numbers are small forge-assigned integers

	posted := false // anything accepted by the server so far
	var succeeded []string
	firstID := ""

	partial := func(failedAt int, err error) (*forge.SubmitResult, error) {
		wrapped := d.wrapCreateReview(err)
		if !posted {
			return nil, wrapped
		}
		return &forge.SubmitResult{
			ReviewID: firstID,
			URL:      pr.URL,
			// The event step never ran (or failed), so whatever landed is
			// plain comments.
			State: "COMMENTED",
			Partial: &forge.PartialFailure{
				SucceededCommentIDs: succeeded,
				FailedAt:            failedAt,
				Cause:               wrapped,
			},
		}, nil
	}

	// (1) Post the overall review body as a general MR note (if non-empty).
	if req.Body != "" {
		var err error
		if isDraft {
			_, _, err = d.client.DraftNotes.CreateDraftNote(pid, iid,
				&gitlab.CreateDraftNoteOptions{Note: new(req.Body)},
				gitlab.WithContext(ctx))
		} else {
			_, _, err = d.client.Notes.CreateMergeRequestNote(pid, iid,
				&gitlab.CreateMergeRequestNoteOptions{Body: new(req.Body)},
				gitlab.WithContext(ctx))
		}
		if err != nil {
			return partial(0, err)
		}
		posted = true
	}

	// (2) Post each inline comment as a positioned discussion or draft note.
	for i := range req.Comments {
		comment := &req.Comments[i]
		position := buildPosition(pr, comment)
		var id string
		var err error
		if isDraft {
			var note *gitlab.DraftNote
			note, _, err = d.client.DraftNotes.CreateDraftNote(pid, iid,
				&gitlab.CreateDraftNoteOptions{
					Note:     new(comment.Body),
					Position: position,
				}, gitlab.WithContext(ctx))
			if err == nil && note != nil {
				id = strconv.FormatInt(note.ID, 10)
			}
		} else {
			var disc *gitlab.Discussion
			disc, _, err = d.client.Discussions.CreateMergeRequestDiscussion(pid, iid,
				&gitlab.CreateMergeRequestDiscussionOptions{
					Body:     new(comment.Body),
					Position: position,
				}, gitlab.WithContext(ctx))
			if err == nil && disc != nil {
				id = disc.ID
			}
		}
		if err != nil {
			return partial(i, err)
		}
		posted = true
		succeeded = append(succeeded, comment.CommentID)
		if firstID == "" && id != "" {
			firstID = id
		}
	}

	// (3) Fire the review event.
	switch req.Event {
	case forge.SubmitApprove:
		if _, _, err := d.client.MergeRequestApprovals.ApproveMergeRequest(pid, iid,
			&gitlab.ApproveMergeRequestOptions{}, gitlab.WithContext(ctx)); err != nil {
			return partial(len(req.Comments), err)
		}
	case forge.SubmitRequestChanges:
		if err := d.requestChanges(ctx, pr); err != nil {
			return partial(len(req.Comments), err)
		}
	case forge.SubmitComment, forge.SubmitDraft:
		// No event call: comments and notes are already published (or
		// pending, for drafts).
	}

	return &forge.SubmitResult{
		ReviewID: firstID,
		URL:      pr.URL,
		State:    submitState(req.Event),
	}, nil
}

// submitState synthesizes the review state GitLab never reports itself.
func submitState(event forge.SubmitEvent) string {
	switch event {
	case forge.SubmitApprove:
		return "APPROVED"
	case forge.SubmitRequestChanges:
		return "CHANGES_REQUESTED"
	case forge.SubmitDraft:
		return "PENDING"
	case forge.SubmitComment:
		return "COMMENTED"
	}
	return "COMMENTED"
}

// buildPosition encodes one inline comment as a GitLab discussion
// position. GitLab needs both old_path and new_path; for context
// (unchanged) lines it needs BOTH old_line and new_line to resolve the
// position (added lines: new_line only; deleted lines: old_line only).
// CounterpartLine carries the other side's line number for context lines.
// Multi-line comments additionally carry a line_range so GitLab anchors
// the discussion across the full selection instead of collapsing it to
// the end line.
func buildPosition(pr *forge.PullRequestDetails, comment *submit.InlineComment) *gitlab.PositionOptions {
	newPath := strings.ReplaceAll(comment.Path, `\`, "/")
	// Renamed files set OldPath to the base-side path; otherwise both
	// sides share the display path.
	oldPath := newPath
	if comment.OldPath != nil {
		oldPath = strings.ReplaceAll(*comment.OldPath, `\`, "/")
	}
	position := &gitlab.PositionOptions{
		PositionType: new("text"),
		BaseSHA:      new(pr.BaseSHA),
		StartSHA:     new(startSHA(pr)),
		HeadSHA:      new(pr.HeadSHA),
		OldPath:      new(oldPath),
		NewPath:      new(newPath),
	}
	if comment.Side == submit.SideNew {
		position.NewLine = new(int64(comment.Line))
		if comment.CounterpartLine != nil {
			position.OldLine = new(int64(*comment.CounterpartLine))
		}
	} else {
		position.OldLine = new(int64(comment.Line))
		if comment.CounterpartLine != nil {
			position.NewLine = new(int64(*comment.CounterpartLine))
		}
	}
	if comment.StartLine != nil {
		startSide := comment.Side
		if comment.StartSide != nil {
			startSide = *comment.StartSide
		}
		position.LineRange = &gitlab.LineRangeOptions{
			Start: rangeEndpoint(newPath, startSide, *comment.StartLine, comment.StartCounterpartLine),
			End:   rangeEndpoint(newPath, comment.Side, comment.Line, comment.CounterpartLine),
		}
	}
	return position
}

// rangeEndpoint builds one endpoint of a GitLab line_range entry. GitLab
// expects each endpoint to carry the type ("new"/"old"), the integer line
// number on that side, and the line_code so the server can anchor the
// range without re-walking the diff. A context (unchanged) endpoint —
// counterpart non-nil — is GitLab's untyped line: both numbers, and a
// line_code naming both.
func rangeEndpoint(newPath string, side submit.Side, line uint32, counterpart *uint32) *gitlab.LinePositionOptions {
	if counterpart != nil {
		oldLine, newLine := *counterpart, line
		if side == submit.SideOld {
			oldLine, newLine = line, *counterpart
		}
		return &gitlab.LinePositionOptions{
			OldLine:  new(int64(oldLine)),
			NewLine:  new(int64(newLine)),
			LineCode: new(lineCode(newPath, oldLine, newLine)),
		}
	}
	if side == submit.SideNew {
		return &gitlab.LinePositionOptions{
			Type:     new("new"),
			NewLine:  new(int64(line)),
			LineCode: new(lineCode(newPath, 0, line)),
		}
	}
	return &gitlab.LinePositionOptions{
		Type:     new("old"),
		OldLine:  new(int64(line)),
		LineCode: new(lineCode(newPath, line, 0)),
	}
}

// lineCode computes GitLab's diff-note line code:
// {SHA1(file_path)}_{old_line}_{new_line}. New-side comments use
// old_line=0, old-side comments new_line=0.
func lineCode(filePath string, oldLine, newLine uint32) string {
	sum := sha1.Sum([]byte(filePath)) //nolint:gosec // format mandated by GitLab
	return fmt.Sprintf("%s_%d_%d", hex.EncodeToString(sum[:]), oldLine, newLine)
}

// requestChanges runs the mergeRequestRequestChanges GraphQL mutation.
// GraphQL returns HTTP 200 even when the mutation fails, so both top-level
// errors and the mutation payload's errors array are checked.
func (d *Driver) requestChanges(ctx context.Context, pr *forge.PullRequestDetails) error {
	const op = "create_review"
	payload := map[string]any{
		"query": requestChangesMutation,
		"variables": map[string]string{
			"projectPath": projectID(pr.Repository),
			"iid":         strconv.FormatUint(pr.Number, 10),
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return d.err(op, forge.ErrorValidation, 0, "", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, d.graphqlURL,
		bytes.NewReader(body))
	if err != nil {
		return d.err(op, forge.ErrorValidation, 0, "", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if d.token != "" {
		httpReq.Header.Set("PRIVATE-TOKEN", d.token)
	}
	resp, err := d.httpClient.Do(httpReq)
	if err != nil {
		return d.err(op, forge.Classify(err), 0, "", err)
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close on a read-only body
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return d.err(op, forge.ErrorNetwork, 0, "", err)
	}
	if resp.StatusCode != http.StatusOK {
		hint := statusHint(resp.StatusCode)
		if resp.StatusCode == http.StatusForbidden {
			hint = hintReviewForbidden
		}
		return d.err(op, forge.FromHTTPStatus(resp.StatusCode), resp.StatusCode, hint,
			fmt.Errorf("graphql request failed: %s", strings.TrimSpace(string(raw))))
	}
	if messages := graphqlErrors(raw, "mergeRequestRequestChanges"); len(messages) > 0 {
		return d.err(op, forge.ErrorValidation, 0, "",
			fmt.Errorf("GitLab request-changes failed: %s", strings.Join(messages, ", ")))
	}
	return nil
}

// graphqlErrors ports tuicr's check_graphql_errors: logical failures
// surface either as top-level `errors` (objects with a message) or inside
// the mutation payload (`data.<mutation>.errors`, an array of strings). A
// non-empty array in either place is a failure; an unparsable body is
// treated as success, matching tuicr.
func graphqlErrors(raw []byte, mutation string) []string {
	var value struct {
		Errors []json.RawMessage          `json:"errors"`
		Data   map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	messages := collectGraphQLErrors(value.Errors)
	if payload, ok := value.Data[mutation]; ok {
		var inner struct {
			Errors []json.RawMessage `json:"errors"`
		}
		if err := json.Unmarshal(payload, &inner); err == nil {
			messages = append(messages, collectGraphQLErrors(inner.Errors)...)
		}
	}
	return messages
}

// collectGraphQLErrors renders each error item: plain strings pass
// through, objects prefer their `message` field, and anything else keeps
// its raw JSON so nothing is silently dropped.
func collectGraphQLErrors(items []json.RawMessage) []string {
	var out []string
	for _, item := range items {
		var s string
		if err := json.Unmarshal(item, &s); err == nil {
			out = append(out, s)
			continue
		}
		var obj struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(item, &obj); err == nil && obj.Message != "" {
			out = append(out, obj.Message)
			continue
		}
		out = append(out, string(item))
	}
	return out
}
