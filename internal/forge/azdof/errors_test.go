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
