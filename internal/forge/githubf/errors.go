package githubf

import (
	"errors"
	"net/http"

	"github.com/google/go-github/v76/github"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

// Actionable hints attached to translated errors. The submit-specific ones
// port the tuicr gh.rs wording.
const (
	hintAuth = "GitHub authentication failed. Set GITHUB_TOKEN/GH_TOKEN " +
		"(or configure a token for this host) with the `repo` scope."
	hintForbidden = "The token was accepted but this operation is forbidden — " +
		"check the token's repository access and scopes."
	hintNotFound = "Merge request or repository not found — check the target " +
		"and that the token can see this repository."
	hintRateLimited = "GitHub rate limit exceeded. Wait for the limit to reset " +
		"and try again."
	hintReviewForbidden = "Cannot submit review: the GitHub token lacks merge " +
		"request write permission. Grant the `repo` scope (fine-grained: " +
		"Pull requests: Read & write) and try again."
	hintPendingReview = "You already have a pending review on this MR. " +
		"Finish or discard it on GitHub, then try again."
	hintUnknownCommit = "GitHub rejected the review: the selected commit is " +
		"not part of this MR (it may have been removed by a force-push). " +
		"Reload with :e and try again."
)

// err builds a forge.Error with the driver's identity filled in.
func (d *Driver) err(op string, kind forge.ErrorKind, status int, hint string, err error) error {
	fe := forge.NewError(forgetypes.KindGitHub, op, d.host, kind, err)
	fe.Status = status
	return fe.WithHint(hint)
}

// wrap translates go-github SDK errors into the forge error taxonomy.
func (d *Driver) wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	var fe *forge.Error
	if errors.As(err, &fe) {
		return err
	}
	var rateErr *github.RateLimitError
	if errors.As(err, &rateErr) {
		return d.err(op, forge.ErrorRateLimited, http.StatusForbidden, hintRateLimited, err)
	}
	var abuseErr *github.AbuseRateLimitError
	if errors.As(err, &abuseErr) {
		return d.err(op, forge.ErrorRateLimited, http.StatusForbidden, hintRateLimited, err)
	}
	var ghErr *github.ErrorResponse
	if errors.As(err, &ghErr) {
		status := 0
		if ghErr.Response != nil {
			status = ghErr.Response.StatusCode
		}
		return d.err(op, forge.FromHTTPStatus(status), status, statusHint(status), err)
	}
	// Cancellation, transport failures, and everything else untyped.
	return d.err(op, forge.Classify(err), 0, "", err)
}

// statusHint picks the default actionable hint for an HTTP status.
func statusHint(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return hintAuth
	case http.StatusForbidden:
		return hintForbidden
	case http.StatusNotFound, http.StatusGone:
		return hintNotFound
	case http.StatusTooManyRequests:
		return hintRateLimited
	}
	return ""
}
