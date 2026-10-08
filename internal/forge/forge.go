// Package forge defines mrman's forge-neutral integration layer: the Forge
// interface every driver implements, the neutral request/response types,
// the driver registry with host-to-forge resolution, remote-URL and PR
// target parsing, token resolution, and the error taxonomy.
//
// Drivers (githubf, gitlabf, azdof, forgejof) live in subpackages and
// register themselves through Register; plain-data session types live in
// the leaf package forgetypes.
package forge

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/infrashift/mrman/internal/forge/forgetypes"
	"github.com/infrashift/mrman/internal/forge/submit"
	"github.com/infrashift/mrman/internal/model"
)

// Side is the neutral diff side, re-exported from the submit package so
// thread types and drivers share one vocabulary.
type Side = submit.Side

// Neutral sides, re-exported from the submit package.
const (
	// SideOld anchors to the base side of the diff (deleted lines).
	SideOld = submit.SideOld
	// SideNew anchors to the head side of the diff (added/context lines).
	SideNew = submit.SideNew
)

// Forge is the neutral interface every forge driver implements. Every
// method takes a context and passes it to each underlying HTTP request;
// cancellation surfaces as *Error with kind ErrorCanceled.
type Forge interface {
	// ID identifies the driver ("github", "gitlab", ...).
	ID() forgetypes.Kind
	// Capabilities reports what this forge supports so the app can gate
	// commands before any network call.
	Capabilities() Capabilities

	// ListPullRequests returns one page of pull requests for q.
	ListPullRequests(ctx context.Context, q ListQuery) (*PullRequestPage, error)
	// GetPullRequest fetches full details for the targeted pull request.
	GetPullRequest(ctx context.Context, target Target) (*PullRequestDetails, error)
	// GetDiff returns the PR's cumulative unified diff.
	GetDiff(ctx context.Context, pr *PullRequestDetails) (string, error)
	// GetCommitRangeDiff returns the unified diff between two commit SHAs
	// belonging to pr. startSHA is the parent of the first commit in the
	// subrange; endSHA is the last commit.
	GetCommitRangeDiff(ctx context.Context, pr *PullRequestDetails, startSHA, endSHA string) (string, error)
	// ListCommits returns the PR's commits oldest first.
	ListCommits(ctx context.Context, pr *PullRequestDetails) ([]Commit, error)

	// FetchFileLines reads the requested file lines for context expansion.
	FetchFileLines(ctx context.Context, req FileLinesRequest) ([]model.DiffLine, error)
	// FileLineCount returns the total number of lines in the file at the
	// revision described by req; the request's line range is ignored.
	FileLineCount(ctx context.Context, req FileLinesRequest) (int, error)

	// ListReviewThreads fetches existing review discussions in posted
	// order, including resolved and outdated state.
	ListReviewThreads(ctx context.Context, pr *PullRequestDetails) ([]RemoteReviewThread, error)
	// ListReviewSummaries fetches review-level summary bodies, distinct
	// from line-anchored threads. Empty on forges without the
	// ReviewSummaries capability.
	ListReviewSummaries(ctx context.Context, pr *PullRequestDetails) ([]RemoteReviewSummary, error)
	// ReviewMetadata fetches minimal review metadata for commit-scope
	// inference ("commits since my last review").
	ReviewMetadata(ctx context.Context, pr *PullRequestDetails) (*ReviewMetadata, error)

	// CreateReview creates a review on pr. Payload building is the
	// driver's responsibility; the caller supplies neutral inputs only.
	CreateReview(ctx context.Context, pr *PullRequestDetails, req CreateReviewRequest) (*SubmitResult, error)

	// LocalCheckoutPath is an optional local checkout the driver may
	// consult as an optimization, "" when none. Never the source of
	// truth for PR contents.
	LocalCheckoutPath() string
}

// ListScope selects which pull requests to list.
type ListScope int

// List scopes.
const (
	// ScopeOpen lists all open pull requests.
	ScopeOpen ListScope = iota
	// ScopeReviewRequested lists open pull requests whose review was
	// requested from the viewer.
	ScopeReviewRequested
)

// Toggled returns the other scope.
func (s ListScope) Toggled() ListScope {
	if s == ScopeOpen {
		return ScopeReviewRequested
	}
	return ScopeOpen
}

// Label returns the short scope label shown in the selector footer.
func (s ListScope) Label() string {
	if s == ScopeReviewRequested {
		return "requested"
	}
	return "all"
}

// ListQuery describes one page of a pull-request listing.
type ListQuery struct {
	// Repository to list pull requests for.
	Repository forgetypes.Repository
	// Scope filters the listing.
	Scope ListScope
	// PageToken is the opaque forge-owned cursor; "" requests the first
	// page.
	PageToken string
	// PageSize is the requested page size.
	PageSize int
}

// PullRequestPage is one page of pull-request summaries.
type PullRequestPage struct {
	// Items holds this page's rows.
	Items []PullRequestSummary
	// NextPageToken requests the next page; "" means no more pages.
	NextPageToken string
}

// PullRequestSummary is one row in the pull-request selector.
type PullRequestSummary struct {
	Repository  forgetypes.Repository `json:"repository"`
	Number      uint64                `json:"number"`
	Title       string                `json:"title"`
	Author      string                `json:"author"`
	HeadRefName string                `json:"head_ref_name"`
	BaseRefName string                `json:"base_ref_name"`
	UpdatedAt   *time.Time            `json:"updated_at"`
	URL         string                `json:"url"`
	State       string                `json:"state"`
	IsDraft     bool                  `json:"is_draft"`
}

// PullRequestDetails is the full pull-request state a review session needs.
type PullRequestDetails struct {
	PullRequestSummary
	HeadSHA  string     `json:"head_sha"`
	BaseSHA  string     `json:"base_sha"`
	Body     string     `json:"body"`
	Closed   bool       `json:"closed"`
	MergedAt *time.Time `json:"merged_at"`
	// ForgePayload is forge-private anchoring data, opaque to the app and
	// round-tripped through sessions (GitLab start_sha, ADO iteration and
	// change-tracking ids).
	ForgePayload json.RawMessage `json:"forge_payload,omitempty"`
}

// IsReadOnly reports whether the PR no longer accepts reviews.
func (d *PullRequestDetails) IsReadOnly() bool {
	return d.Closed || d.MergedAt != nil
}

// ReadOnlyReason returns "merged" or "closed" when read-only, "" otherwise.
func (d *PullRequestDetails) ReadOnlyReason() string {
	switch {
	case d.MergedAt != nil:
		return "merged"
	case d.Closed:
		return "closed"
	}
	return ""
}

// Target identifies a pull request to open. Repository is nil for bare
// numbers until resolution against a checkout; Original preserves the user
// input for error messages.
type Target struct {
	Repository *forgetypes.Repository
	Number     uint64
	Original   string
}

// Commit is a single pull-request commit as returned by the forge.
type Commit struct {
	OID       string     `json:"oid"`
	ShortOID  string     `json:"short_oid"`
	Summary   string     `json:"summary"`
	Author    string     `json:"author"`
	Timestamp *time.Time `json:"timestamp"`
}

// FileSide says which side of a pull request diff the caller reads from.
type FileSide int

// File sides.
const (
	// FileSideBase reads from the base (old) revision.
	FileSideBase FileSide = iota
	// FileSideHead reads from the head (new) revision.
	FileSideHead
)

// FileLinesRequest asks a forge for file lines for context expansion.
type FileLinesRequest struct {
	// Repository the file lives in.
	Repository forgetypes.Repository
	// BaseSHA is the base SHA captured when the PR was opened.
	BaseSHA string
	// HeadSHA is the head SHA captured when the PR was opened.
	HeadSHA string
	// Path is the file path relative to the repository root, already
	// chosen for Side (renames use the old path on the base side).
	Path string
	// Status is the file's change status, so drivers need not recompute
	// the side mapping.
	Status model.FileStatus
	// Side to read from; the caller picks it per SideForStatus.
	Side FileSide
	// StartLine and EndLine bound the inclusive 1-based line range. A range
	// running past the end of the file stops at its last line, so
	// EndLine = math.MaxUint32 reads the whole file in one request.
	StartLine uint32
	EndLine   uint32
}

// SideForStatus resolves the diff side to read for a file status: deleted
// files read the base side, everything else the head side.
func SideForStatus(status model.FileStatus) FileSide {
	if status == model.StatusDeleted {
		return FileSideBase
	}
	return FileSideHead
}

// PathForSide picks the right path for a forge fetch given both sides'
// paths: old on the base side, new on the head side, falling back to the
// other when one is absent. ok is false when neither path exists.
func PathForSide(side FileSide, oldPath, newPath *string) (path string, ok bool) {
	p := newPath
	if side == FileSideBase {
		p = oldPath
	}
	if p == nil {
		if side == FileSideBase {
			p = newPath
		} else {
			p = oldPath
		}
	}
	if p == nil {
		return "", false
	}
	return *p, true
}

// SHA returns the SHA matching the request's side.
func (r FileLinesRequest) SHA() string {
	if r.Side == FileSideBase {
		return r.BaseSHA
	}
	return r.HeadSHA
}

// RemoteReviewComment is a single remote review comment. Anchor fields live
// on the parent RemoteReviewThread.
type RemoteReviewComment struct {
	// ID is the forge-assigned comment id (opaque string).
	ID string `json:"id"`
	// Author is the comment author's login, "" when unavailable.
	Author string `json:"author"`
	// Body is the markdown body as written on the forge.
	Body string `json:"body"`
	// CreatedAt is the creation time when the forge exposes one.
	CreatedAt *time.Time `json:"created_at"`
	// InReplyTo is the parent comment id for replies, "" for roots.
	InReplyTo string `json:"in_reply_to"`
	// URL is the permalink to the comment on the forge.
	URL string `json:"url"`
}

// RemoteReviewThread is a discussion thread on a forge: one root comment
// plus zero or more replies.
type RemoteReviewThread struct {
	// ID is the forge-assigned thread id.
	ID string `json:"id"`
	// Path is the file path the thread anchors to.
	Path string `json:"path"`
	// Line is the anchor line on Side, nil for fully-outdated threads.
	Line *uint32 `json:"line"`
	// Side is the neutral diff side of the anchor.
	Side Side `json:"side"`
	// StartLine is the multi-line range start, nil for single-line
	// threads.
	StartLine *uint32 `json:"start_line"`
	// IsResolved reports the forge's resolved state (approximated on
	// forges without the ThreadResolution capability).
	IsResolved bool `json:"is_resolved"`
	// IsOutdated reports whether the thread's anchor is stale
	// (approximated on forges without the ThreadOutdated capability).
	IsOutdated bool `json:"is_outdated"`
	// Disposition is a short lowercase label for the forge's own thread
	// state, when the forge distinguishes more than resolved/unresolved.
	// Azure DevOps does ("won't fix", "by design", "pending", ...); the
	// others do not and leave this empty, which makes the badge fall back
	// to plain "resolved". Display only: IsResolved still drives filtering,
	// so a new disposition can never accidentally hide a thread.
	Disposition string `json:"disposition,omitempty"`
	// Comments holds the root comment first, replies in posted order.
	Comments []RemoteReviewComment `json:"comments"`
}

// IsActive reports whether the thread is neither resolved nor outdated;
// the default unresolved visibility shows only active threads.
func (t *RemoteReviewThread) IsActive() bool {
	return !t.IsResolved && !t.IsOutdated
}

// Root returns the thread's root comment, nil for empty threads.
func (t *RemoteReviewThread) Root() *RemoteReviewComment {
	if len(t.Comments) == 0 {
		return nil
	}
	return &t.Comments[0]
}

// Replies returns every comment after the root.
func (t *RemoteReviewThread) Replies() []RemoteReviewComment {
	if len(t.Comments) <= 1 {
		return nil
	}
	return t.Comments[1:]
}

// ReviewState is the state of a remote review at submit time.
type ReviewState string

// Review states.
const (
	// ReviewCommented is a plain comment review.
	ReviewCommented ReviewState = "commented"
	// ReviewApproved approves the PR.
	ReviewApproved ReviewState = "approved"
	// ReviewChangesRequested requests changes.
	ReviewChangesRequested ReviewState = "changes_requested"
	// ReviewDismissed was dismissed after submission.
	ReviewDismissed ReviewState = "dismissed"
	// ReviewPending is a draft review not yet submitted.
	ReviewPending ReviewState = "pending"
)

// ParseReviewState parses a forge-reported state string; unknown values
// default to ReviewCommented, the safest display choice.
func ParseReviewState(value string) ReviewState {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "APPROVED":
		return ReviewApproved
	case "CHANGES_REQUESTED":
		return ReviewChangesRequested
	case "DISMISSED":
		return ReviewDismissed
	case "PENDING":
		return ReviewPending
	}
	return ReviewCommented
}

// BadgeLabel returns the short badge text for the state, "" for plain
// comments which carry no badge.
func (s ReviewState) BadgeLabel() string {
	switch s {
	case ReviewApproved:
		return "approved"
	case ReviewChangesRequested:
		return "changes requested"
	case ReviewDismissed:
		return "dismissed"
	case ReviewPending:
		return "pending"
	case ReviewCommented:
		return ""
	}
	return ""
}

// RemoteReviewSummary is a review-level summary comment attached directly
// to a review, distinct from line-anchored threads. Fetchers drop reviews
// with empty bodies (e.g. bare approvals).
type RemoteReviewSummary struct {
	// ID is the forge-assigned review id.
	ID string `json:"id"`
	// Author is the reviewer's login, "" when unavailable.
	Author string `json:"author"`
	// Body is the markdown body as submitted; non-empty by construction.
	Body string `json:"body"`
	// State is the review's submit state.
	State ReviewState `json:"state"`
	// CreatedAt is the submission time when available.
	CreatedAt *time.Time `json:"created_at"`
	// URL is the permalink to the review on the forge.
	URL string `json:"url"`
}

// ReviewMetadata is minimal review metadata used to infer "commits since my
// last review". Separate from displayed summaries because empty-body
// approvals still count as reviews for scoping.
type ReviewMetadata struct {
	// ViewerLogin is the authenticated user's login, "" when unknown.
	ViewerLogin string
	// Reviews lists every review record, including empty-body ones.
	Reviews []ReviewRecord
}

// ReviewRecord is one review occurrence for scope inference.
type ReviewRecord struct {
	// Author is the reviewer's login, "" when unavailable.
	Author string
	// SubmittedAt is the submission time when available.
	SubmittedAt *time.Time
	// CommitOID is the head commit the review was submitted against, ""
	// on forges without the CommitScopedReviews capability.
	CommitOID string
}

// SubmitEvent is which forge review event a submit command corresponds to.
type SubmitEvent int

// Submit events.
const (
	// SubmitComment publishes a plain comment review.
	SubmitComment SubmitEvent = iota
	// SubmitApprove publishes an approving review.
	SubmitApprove
	// SubmitRequestChanges publishes a changes-requested review.
	SubmitRequestChanges
	// SubmitDraft creates a pending (draft) review on forges with the
	// DraftReviews capability.
	SubmitDraft
)

// HumanLabel returns the short label shown in the confirmation modal.
func (e SubmitEvent) HumanLabel() string {
	switch e {
	case SubmitComment:
		return "Comment"
	case SubmitApprove:
		return "Approve"
	case SubmitRequestChanges:
		return "Request changes"
	case SubmitDraft:
		return "Draft (pending review)"
	}
	return "Comment"
}

// CreateReviewRequest asks a driver to create a review. Each driver maps
// the event and comments to its own wire format internally.
type CreateReviewRequest struct {
	// Event selects the review kind.
	Event SubmitEvent
	// CommitID is the head SHA captured at preflight so a concurrent
	// reload cannot steal the anchor.
	CommitID string
	// Body is the review body (already built via submit.BuildReviewBody).
	Body string
	// Comments holds the mapped, capability-downgraded inline comments.
	Comments []submit.InlineComment
}

// SubmitResult describes a created review.
type SubmitResult struct {
	// ReviewID is the stringified forge review id, "" when the forge has
	// none (e.g. Azure DevOps).
	ReviewID string
	// URL is where to see or finish the review, "" when not applicable.
	URL string
	// State is the review state as reported by the forge.
	State string
	// Partial is non-nil when an N+1 submit failed midway; the app flips
	// only the succeeded comments to submitted.
	Partial *PartialFailure
}

// PartialFailure reports a mid-sequence failure of a non-atomic submit.
type PartialFailure struct {
	// SucceededCommentIDs lists the local comment IDs that were posted
	// before the failure.
	SucceededCommentIDs []string
	// FailedAt is the index into CreateReviewRequest.Comments where the
	// failure occurred.
	FailedAt int
	// Cause is the underlying error.
	Cause error
}
