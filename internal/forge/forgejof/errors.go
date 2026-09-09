package forgejof

import (
	"errors"
	"net/http"

	forgejo "codeberg.org/mvdkleijn/forgejo-sdk/forgejo/v3"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

// Actionable hints attached to translated errors.
const (
	hintAuth = "Forgejo authentication failed. Set FORGEJO_TOKEN/CODEBERG_TOKEN " +
		"(or configure a token for this host) with the read:repository scope."
	hintForbidden = "The token was accepted but this operation is forbidden — " +
		"check that it has the read:repository (and write:repository for " +
		"reviews) scope for this repository."
	hintNotFound = "Merge request or repository not found — check the target " +
		"and that the token can see this repository."
	hintRateLimited = "Forgejo rate limit exceeded. Wait for the limit to " +
		"reset and try again."
	hintReviewForbidden = "Cannot submit review: the Forgejo token lacks write " +
		"access. Grant the write:repository scope and try again."
	hintReviewRejected = "Forgejo rejected the review payload — a comment may " +
		"anchor to a line outside the MR diff, or the selected commit is not " +
		"part of this MR. Reload with :e and try again."
	hintRangeDiff = "Forgejo has no commit-range diff API; commit-range " +
		"scoping needs a local checkout that contains both commits."
)

// err builds a forge.Error with the driver's identity filled in.
func (d *Driver) err(op string, kind forge.ErrorKind, status int, hint string, err error) error {
	fe := forge.NewError(forgetypes.KindForgejo, op, d.host, kind, err)
	fe.Status = status
	return fe.WithHint(hint)
}

// wrap translates a Forgejo SDK failure into the forge error taxonomy. The
// SDK surfaces API failures as plain errors, so classification leans on the
// paired response's HTTP status; without a response the generic classifier
// separates cancellation and transport failures.
func (d *Driver) wrap(op string, resp *forgejo.Response, err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*forge.Error](err); ok {
		return err
	}
	if status := responseStatus(resp); status >= 400 {
		return d.err(op, forge.FromHTTPStatus(status), status, statusHint(status), err)
	}
	return d.err(op, forge.Classify(err), 0, "", err)
}

// responseStatus extracts the HTTP status from an SDK response, 0 when the
// request never produced one.
func responseStatus(resp *forgejo.Response) int {
	if resp == nil || resp.Response == nil {
		return 0
	}
	return resp.StatusCode
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
