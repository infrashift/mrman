package forge

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

func TestFromHTTPStatusMapping(t *testing.T) {
	cases := map[int]ErrorKind{
		401: ErrorAuth,
		403: ErrorForbidden,
		404: ErrorNotFound,
		410: ErrorNotFound,
		409: ErrorConflict,
		429: ErrorRateLimited,
		400: ErrorValidation,
		422: ErrorValidation,
		418: ErrorValidation,
		500: ErrorServer,
		502: ErrorServer,
		503: ErrorServer,
		0:   ErrorNetwork,
		-1:  ErrorNetwork,
		302: ErrorServer,
	}
	for status, want := range cases {
		if got := FromHTTPStatus(status); got != want {
			t.Errorf("FromHTTPStatus(%d) = %v, want %v", status, got, want)
		}
	}
}

func TestErrorKindStrings(t *testing.T) {
	cases := map[ErrorKind]string{
		ErrorAuth:        "authentication failed",
		ErrorForbidden:   "permission denied",
		ErrorNotFound:    "not found",
		ErrorRateLimited: "rate limited",
		ErrorValidation:  "validation failed",
		ErrorConflict:    "conflict",
		ErrorNetwork:     "network error",
		ErrorServer:      "server error",
		ErrorCanceled:    "canceled",
		ErrorUnsupported: "unsupported operation",
		ErrorKind(0):     "forge error",
	}
	for kind, want := range cases {
		if got := kind.String(); got != want {
			t.Errorf("String(%d) = %q, want %q", kind, got, want)
		}
	}
}

func TestErrorMessageComposition(t *testing.T) {
	inner := errors.New("boom")
	err := StatusError(forgetypes.KindGitHub, "create_review", "github.com", 422, inner).
		WithHint("token lacks the repo scope")
	msg := err.Error()
	for _, want := range []string{
		"github:", "create_review:", "validation failed", "(HTTP 422)",
		"on github.com", "boom", "token lacks the repo scope",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q: %s", want, msg)
		}
	}
}

func TestErrorMessageMinimal(t *testing.T) {
	err := &Error{Kind: ErrorNetwork}
	if got := err.Error(); got != "network error" {
		t.Fatalf("message = %q", got)
	}
}

func TestErrorUnwrap(t *testing.T) {
	inner := errors.New("inner")
	err := NewError(forgetypes.KindGitLab, "get_diff", "gitlab.com", ErrorServer, inner)
	if !errors.Is(err, inner) {
		t.Fatal("Unwrap chain broken")
	}
	wrapped := fmt.Errorf("outer: %w", err)
	kind, ok := KindOf(wrapped)
	if !ok || kind != ErrorServer {
		t.Fatalf("KindOf = (%v, %v)", kind, ok)
	}
	if !IsKind(wrapped, ErrorServer) || IsKind(wrapped, ErrorAuth) {
		t.Fatal("IsKind mismatch")
	}
	if _, ok := KindOf(errors.New("plain")); ok {
		t.Fatal("KindOf matched a non-forge error")
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want ErrorKind
	}{
		{"context canceled", context.Canceled, ErrorCanceled},
		{"deadline exceeded", context.DeadlineExceeded, ErrorCanceled},
		{"wrapped canceled", fmt.Errorf("op: %w", context.Canceled), ErrorCanceled},
		{"unsupported sentinel", errs.ErrUnsupported, ErrorUnsupported},
		{"net error", &net.DNSError{Err: "no such host"}, ErrorNetwork},
		{"existing forge error keeps kind", NewError(forgetypes.KindGitHub, "op", "h", ErrorAuth, nil), ErrorAuth},
		{"unknown defaults to network", errors.New("mystery"), ErrorNetwork},
	}
	for _, tc := range cases {
		if got := Classify(tc.err); got != tc.want {
			t.Errorf("%s: Classify = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestWrapErrorPassesThroughExistingForgeError(t *testing.T) {
	original := StatusError(forgetypes.KindGitLab, "inner_op", "gitlab.com", 429, errors.New("slow down"))
	wrapped := WrapError(forgetypes.KindGitLab, "outer_op", "gitlab.com", original)
	if wrapped != original {
		t.Fatal("existing *Error should pass through unchanged")
	}

	plain := WrapError(forgetypes.KindGitHub, "op", "github.com", context.Canceled)
	if plain.Kind != ErrorCanceled || plain.Op != "op" || plain.ForgeID != forgetypes.KindGitHub {
		t.Fatalf("wrapped = %+v", plain)
	}
}
