package azdof

import (
	"errors"
	"net/http"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

// Actionable hints attached to translated errors.
const (
	hintAuth = "Azure DevOps authentication failed. Set AZURE_DEVOPS_EXT_PAT " +
		"(or configure a token for this host) to a PAT with the " +
		"Code (Read & Write) scope."
	hintForbidden = "The PAT was accepted but this operation is forbidden — " +
		"make sure the PAT carries the Code (Read & Write) scope and has " +
		"access to this organization and project."
	hintNotFound = "Pull request, repository, or project not found — check " +
		"the organization URL, project, and repository names, and that the " +
		"PAT can see them."
	hintRateLimited = "Azure DevOps throttled the request. Wait for the " +
		"limit to reset and try again."
	hintNoDraft = "Azure DevOps has no draft (pending) reviews. Submit as " +
		"comment, approve, or request-changes instead."
	hintNoRangeDiff = "Azure DevOps serves no commit-range diff; commit " +
		"scoping is unavailable on this forge."
)

// err builds a forge.Error with the driver's identity filled in.
func (d *Driver) err(op string, kind forge.ErrorKind, status int, hint string, err error) error {
	fe := forge.NewError(forgetypes.KindAzureDevOps, op, d.host, kind, err)
	fe.Status = status
	return fe.WithHint(hint)
}

// wrap translates SDK errors into the forge error taxonomy. The Azure
// DevOps SDK reports HTTP failures as azuredevops.WrappedError — returned
// sometimes by value and sometimes by pointer, so both shapes are matched.
func (d *Driver) wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	var fe *forge.Error
	if errors.As(err, &fe) {
		return err
	}
	if wrapped, ok := wrappedError(err); ok {
		status := 0
		if wrapped.StatusCode != nil {
			status = *wrapped.StatusCode
		}
		return d.err(op, forge.FromHTTPStatus(status), status, statusHint(status), err)
	}
	// Cancellation, transport failures, and everything else untyped.
	return d.err(op, forge.Classify(err), 0, "", err)
}

// wrappedError extracts an azuredevops.WrappedError from err, whether it
// was returned by value or by pointer.
func wrappedError(err error) (azuredevops.WrappedError, bool) {
	var ptr *azuredevops.WrappedError
	if errors.As(err, &ptr) {
		return *ptr, true
	}
	var val azuredevops.WrappedError
	if errors.As(err, &val) {
		return val, true
	}
	return azuredevops.WrappedError{}, false
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
