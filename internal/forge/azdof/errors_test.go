package azdof

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7"

	"github.com/infrashift/mrman/internal/forge"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

// newBareDriver builds a driver without a server for pure translation tests.
func newBareDriver(t *testing.T) *Driver {
	t.Helper()
	d, err := New(Options{OrgURL: "https://dev.azure.com/org"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return d
}

// wrappedStatus builds the SDK's value-typed error for a status code.
func wrappedStatus(status int) error {
	message := fmt.Sprintf("HTTP %d", status)
	return azuredevops.WrappedError{Message: &message, StatusCode: &status}
}

func TestWrapTranslatesStatuses(t *testing.T) {
	d := newBareDriver(t)
	tests := []struct {
		status   int
		wantKind forge.ErrorKind
		wantHint string
	}{
		{http.StatusUnauthorized, forge.ErrorAuth, "Code (Read & Write)"},
		{http.StatusForbidden, forge.ErrorForbidden, "Code (Read & Write)"},
		{http.StatusNotFound, forge.ErrorNotFound, "organization URL"},
		{http.StatusConflict, forge.ErrorConflict, ""},
		{http.StatusTooManyRequests, forge.ErrorRateLimited, "throttled"},
		{http.StatusUnprocessableEntity, forge.ErrorValidation, ""},
		{http.StatusInternalServerError, forge.ErrorServer, ""},
	}
	for _, tt := range tests {
		err := d.wrap("op", wrappedStatus(tt.status))
		fe := mustForgeErr(t, err)
		if fe.Kind != tt.wantKind {
			t.Errorf("status %d: kind = %v, want %v", tt.status, fe.Kind, tt.wantKind)
		}
		if fe.Status != tt.status {
			t.Errorf("status %d: recorded status = %d", tt.status, fe.Status)
		}
		if tt.wantHint != "" && !containsFold(fe.Hint, tt.wantHint) {
			t.Errorf("status %d: hint %q missing %q", tt.status, fe.Hint, tt.wantHint)
		}
	}
}

func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

func TestWrapPointerWrappedError(t *testing.T) {
	d := newBareDriver(t)
	status := http.StatusNotFound
	err := d.wrap("op", &azuredevops.WrappedError{StatusCode: &status})
	wantForgeErr(t, err, forge.ErrorNotFound)
}

func TestWrapPassesThroughForgeErrors(t *testing.T) {
	d := newBareDriver(t)
	original := forge.NewError(forgetypes.KindAzureDevOps, "inner", "host", forge.ErrorRateLimited, errors.New("x"))
	got := d.wrap("outer", original)
	var fe *forge.Error
	if !errors.As(got, &fe) || fe != original {
		t.Fatalf("forge errors must pass through unchanged, got %v", got)
	}
}

func TestWrapClassifiesCancellation(t *testing.T) {
	d := newBareDriver(t)
	err := d.wrap("op", fmt.Errorf("request: %w", context.Canceled))
	wantForgeErr(t, err, forge.ErrorCanceled)
}

func TestWrapClassifiesUnknownAsNetwork(t *testing.T) {
	d := newBareDriver(t)
	err := d.wrap("op", errors.New("connection reset"))
	wantForgeErr(t, err, forge.ErrorNetwork)
}

func TestWrapNil(t *testing.T) {
	d := newBareDriver(t)
	if err := d.wrap("op", nil); err != nil {
		t.Fatalf("wrap(nil) = %v", err)
	}
}

// A TF401027 refusal names the missing Git permission: the hint says what to
// grant, to whom, instead of blaming the PAT's scope.
func TestWrapForbiddenNamesTheMissingPermission(t *testing.T) {
	d := newBareDriver(t)
	forbidden := func(message string) *forge.Error {
		status := http.StatusForbidden
		return mustForgeErr(t, d.wrap("create_review", azuredevops.WrappedError{Message: &message, StatusCode: &status}))
	}
	build := forbidden("TF401027: You need the Git 'PullRequestContribute' permission to perform this action. " +
		"Details: identity 'Build\\cff044ed-6ca0-4495-a0bd-521cc145ceeb', scope 'repository'.")
	for _, want := range []string{"build identity", `"Contribute to pull requests"`, "Build Service (<org>)", "Security"} {
		if !strings.Contains(build.Hint, want) {
			t.Errorf("pipeline hint %q lacks %q", build.Hint, want)
		}
	}
	if strings.Contains(build.Hint, "PAT carries") || build.Kind != forge.ErrorForbidden {
		t.Errorf("pipeline hint still blames the PAT, or kind %v: %q", build.Kind, build.Hint)
	}

	user := forbidden("TF401027: You need the Git 'GenericContribute' permission to perform this action. " +
		"Details: identity 'Vera Viewer', scope 'repository'.")
	if !strings.Contains(user.Hint, `"Contribute"`) || !strings.Contains(user.Hint, "project administrator") ||
		strings.Contains(user.Hint, "Build Service") {
		t.Errorf("user hint %q", user.Hint)
	}
	// A permission without a known label is named as Azure DevOps names it.
	if odd := forbidden("TF401027: You need the Git 'EditPolicies' permission to perform this action. " +
		"Details: identity 'Build\\x', scope 'repository'."); !strings.Contains(odd.Hint, `"EditPolicies"`) {
		t.Errorf("unlabelled permission hint %q", odd.Hint)
	}
	// Any other 403 keeps the token hint.
	if other := forbidden("VS403403: access denied"); !strings.Contains(other.Hint, "Code (Read & Write)") {
		t.Errorf("generic 403 hint %q", other.Hint)
	}
}
