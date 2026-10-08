package gitlabf

import (
	"errors"
	"fmt"
	"net/http"

	gitlab "gitlab.com/gitlab-org/api/client-go"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

// Actionable hints attached to translated errors. The submit-specific one
// ports the tuicr glab.rs wording.
const (
	// hintAuthFormat takes the host, which also names the token page.
	hintAuthFormat = "GitLab authentication failed. Set GITLAB_TOKEN (or configure " +
		"a token for this host) with the `api` scope — create one at " +
		"https://%s/-/user_settings/personal_access_tokens."
	hintForbidden = "The token was accepted but this operation is forbidden — " +
		"check the token's project access and that it has the `api` scope."
	hintNotFound = "Merge request or project not found — check the target " +
		"and that the token can see this project."
	hintRateLimited = "GitLab rate limit exceeded. Wait for the limit to " +
		"reset and try again."
	hintReviewForbidden = "Cannot submit review: the GitLab token lacks merge " +
		"request write permission. Use a token with the `api` scope and at " +
		"least Reporter access to the project."
	hintApproveRefused = "GitLab refused the approval: you may already have " +
		"approved this merge request, or the project does not let you approve " +
		"it (an author approving their own merge request, for one)."
)

// err builds a forge.Error with the driver's identity filled in.
func (d *Driver) err(op string, kind forge.ErrorKind, status int, hint string, err error) error {
	fe := forge.NewError(forgetypes.KindGitLab, op, d.host, kind, err)
	fe.Status = status
	return fe.WithHint(hint)
}

// wrap translates client-go SDK errors into the forge error taxonomy.
func (d *Driver) wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*forge.Error](err); ok {
		return err
	}
	// The SDK collapses 404s into a bare sentinel instead of an
	// *ErrorResponse.
	if errors.Is(err, gitlab.ErrNotFound) {
		return d.err(op, forge.ErrorNotFound, http.StatusNotFound, hintNotFound, err)
	}
	if glErr, ok := errors.AsType[*gitlab.ErrorResponse](err); ok {
		status := 0
		if glErr.Response != nil {
			status = glErr.Response.StatusCode
		}
		return d.err(op, forge.FromHTTPStatus(status), status, d.statusHint(status), err)
	}
	// Cancellation, transport failures, and everything else untyped.
	return d.err(op, forge.Classify(err), 0, "", err)
}

// statusHint picks the default actionable hint for an HTTP status.
func (d *Driver) statusHint(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return fmt.Sprintf(hintAuthFormat, d.host)
	case http.StatusForbidden:
		return hintForbidden
	case http.StatusNotFound, http.StatusGone:
		return hintNotFound
	case http.StatusTooManyRequests:
		return hintRateLimited
	}
	return ""
}

// wrapCreateReview layers the submit-specific error mapping (ported from
// tuicr's glab.rs) over the generic translator: permission failures name
// the missing merge-request write access.
func (d *Driver) wrapCreateReview(err error) error {
	const op = "create_review"
	var glErr *gitlab.ErrorResponse
	if errors.As(err, &glErr) && glErr.Response != nil &&
		glErr.Response.StatusCode == http.StatusForbidden {
		return d.err(op, forge.ErrorForbidden, http.StatusForbidden, hintReviewForbidden, err)
	}
	return d.wrap(op, err)
}

// wrapApprove translates a failed approve call. GitLab answers 401 there
// when the approval itself is refused (already approved, or approval not
// allowed), not only for a bad token. The approve call comes after requests
// that succeeded with the same token, so a 401 here is a refusal, not an
// authentication failure.
func (d *Driver) wrapApprove(err error) error {
	const op = "create_review"
	if glErr, ok := errors.AsType[*gitlab.ErrorResponse](err); ok && glErr.Response != nil &&
		glErr.Response.StatusCode == http.StatusUnauthorized {
		return d.err(op, forge.ErrorForbidden, http.StatusUnauthorized, hintApproveRefused, err)
	}
	return d.wrapCreateReview(err)
}
