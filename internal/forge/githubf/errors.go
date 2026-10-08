package githubf

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

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
	if _, ok := errors.AsType[*forge.Error](err); ok {
		return err
	}
	if _, ok := errors.AsType[*github.RateLimitError](err); ok {
		return d.err(op, forge.ErrorRateLimited, http.StatusForbidden, hintRateLimited, err)
	}
	if _, ok := errors.AsType[*github.AbuseRateLimitError](err); ok {
		return d.err(op, forge.ErrorRateLimited, http.StatusForbidden, hintRateLimited, err)
	}
	if ghErr, ok := errors.AsType[*github.ErrorResponse](err); ok {
		status := 0
		if ghErr.Response != nil {
			status = ghErr.Response.StatusCode
		}
		return d.err(op, forge.FromHTTPStatus(status), status, statusHint(status), err)
	}
	// githubv4 reports a non-200 response only as a formatted string, so
	// without this a GraphQL 401 read as a network error, with no auth hint.
	if status, ok := graphqlHTTPStatus(err); ok {
		return d.err(op, forge.FromHTTPStatus(status), status, statusHint(status), err)
	}
	if strings.Contains(err.Error(), "Could not resolve to a ") {
		// GraphQL's 200 OK answer for a repository or MR the token
		// cannot see.
		return d.err(op, forge.ErrorNotFound, 0, hintNotFound, err)
	}
	// Cancellation, transport failures, and everything else untyped.
	return d.err(op, forge.Classify(err), 0, "", err)
}

// graphqlNon200 prefixes the error githubv4 returns for a non-200 response:
// "non-200 OK status code: 401 Unauthorized body: ...".
const graphqlNon200 = "non-200 OK status code: "

// graphqlHTTPStatus extracts the HTTP status from a githubv4 non-200 error.
func graphqlHTTPStatus(err error) (int, bool) {
	_, rest, found := strings.Cut(err.Error(), graphqlNon200)
	if !found || len(rest) < 3 {
		return 0, false
	}
	status, convErr := strconv.Atoi(rest[:3])
	if convErr != nil {
		return 0, false
	}
	return status, true
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
