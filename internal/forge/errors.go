package forge

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/infrashift/mrman/internal/textsafe"

	"github.com/infrashift/mrman/internal/errs"
	"github.com/infrashift/mrman/internal/forge/forgetypes"
)

// ErrorKind classifies a forge error so the app can react uniformly across
// drivers (retry, re-auth hint, silent discard) instead of matching message
// substrings.
type ErrorKind int

// Error kinds.
const (
	// ErrorAuth is a missing or rejected credential (HTTP 401).
	ErrorAuth ErrorKind = iota + 1
	// ErrorForbidden is a valid credential lacking permission (HTTP 403).
	ErrorForbidden
	// ErrorNotFound is a missing resource (HTTP 404/410).
	ErrorNotFound
	// ErrorRateLimited is a throttled request (HTTP 429); the watcher
	// backs off.
	ErrorRateLimited
	// ErrorValidation is a rejected request payload (HTTP 400/422).
	ErrorValidation
	// ErrorConflict is a state conflict (HTTP 409).
	ErrorConflict
	// ErrorNetwork is a transport-level failure before any HTTP status.
	ErrorNetwork
	// ErrorServer is a forge-side failure (HTTP 5xx).
	ErrorServer
	// ErrorCanceled is a context cancellation; the app discards it
	// silently.
	ErrorCanceled
	// ErrorUnsupported is an operation the forge cannot perform.
	ErrorUnsupported
)

// String returns the human-readable kind label.
func (k ErrorKind) String() string {
	switch k {
	case ErrorAuth:
		return "authentication failed"
	case ErrorForbidden:
		return "permission denied"
	case ErrorNotFound:
		return "not found"
	case ErrorRateLimited:
		return "rate limited"
	case ErrorValidation:
		return "validation failed"
	case ErrorConflict:
		return "conflict"
	case ErrorNetwork:
		return "network error"
	case ErrorServer:
		return "server error"
	case ErrorCanceled:
		return "canceled"
	case ErrorUnsupported:
		return "unsupported operation"
	}
	return "forge error"
}

// FromHTTPStatus maps an HTTP status code to an ErrorKind. Zero or negative
// status (no response) maps to ErrorNetwork.
func FromHTTPStatus(status int) ErrorKind {
	switch status {
	case 401:
		return ErrorAuth
	case 403:
		return ErrorForbidden
	case 404, 410:
		return ErrorNotFound
	case 409:
		return ErrorConflict
	case 429:
		return ErrorRateLimited
	}
	switch {
	case status <= 0:
		return ErrorNetwork
	case status >= 500:
		return ErrorServer
	case status >= 400:
		return ErrorValidation
	}
	return ErrorServer
}

// Error is the uniform error every driver surfaces. The status bar renders
// Hint when present; ErrorCanceled is discarded silently.
type Error struct {
	// ForgeID names the driver that produced the error.
	ForgeID forgetypes.Kind
	// Op is the driver operation ("get_pull_request", "create_review").
	Op string
	// Host is the forge host contacted.
	Host string
	// Kind classifies the failure.
	Kind ErrorKind
	// Status is the HTTP status when applicable, 0 otherwise.
	Status int
	// Hint is an actionable per-forge suggestion, "" when none.
	Hint string
	// Err is the wrapped SDK or transport error.
	Err error
}

// Error implements the error interface. The wrapped SDK error often quotes
// the response body, which is forge-authored text, so the result is
// scrubbed before it can reach the status bar.
func (e *Error) Error() string {
	return textsafe.SanitizeLine(e.render())
}

func (e *Error) render() string {
	var b strings.Builder
	if e.ForgeID != "" {
		b.WriteString(string(e.ForgeID))
		b.WriteString(": ")
	}
	if e.Op != "" {
		b.WriteString(e.Op)
		b.WriteString(": ")
	}
	b.WriteString(e.Kind.String())
	if e.Status != 0 {
		fmt.Fprintf(&b, " (HTTP %d)", e.Status)
	}
	if e.Host != "" {
		b.WriteString(" on ")
		b.WriteString(e.Host)
	}
	if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	if e.Hint != "" {
		b.WriteString(" — ")
		b.WriteString(e.Hint)
	}
	return b.String()
}

// Unwrap exposes the wrapped error to errors.Is/As.
func (e *Error) Unwrap() error {
	return e.Err
}

// WithHint sets the actionable hint and returns the error for chaining.
func (e *Error) WithHint(hint string) *Error {
	e.Hint = hint
	return e
}

// NewError builds a forge error with an explicit kind.
func NewError(forgeID forgetypes.Kind, op, host string, kind ErrorKind, err error) *Error {
	return &Error{ForgeID: forgeID, Op: op, Host: host, Kind: kind, Err: err}
}

// StatusError builds a forge error whose kind derives from the HTTP status.
func StatusError(forgeID forgetypes.Kind, op, host string, status int, err error) *Error {
	e := NewError(forgeID, op, host, FromHTTPStatus(status), err)
	e.Status = status
	return e
}

// WrapError classifies err via Classify and wraps it. An err that already
// is a *Error passes through unchanged so kinds assigned deeper in a driver
// survive.
func WrapError(forgeID forgetypes.Kind, op, host string, err error) *Error {
	if fe, ok := errors.AsType[*Error](err); ok {
		return fe
	}
	return NewError(forgeID, op, host, Classify(err), err)
}

// Classify infers an ErrorKind from an arbitrary error: context
// cancellation maps to ErrorCanceled, errs.ErrUnsupported to
// ErrorUnsupported, net errors to ErrorNetwork, existing *Error keeps its
// kind, and anything else defaults to ErrorNetwork (the common transport
// case for non-HTTP failures).
//
// A deadline is a network error, not a cancellation: the only deadline on a
// forge request is the HTTP client's request timeout, and a request that
// timed out must be reported, where a cancellation is discarded silently.
func Classify(err error) ErrorKind {
	var fe *Error
	switch {
	case errors.As(err, &fe):
		return fe.Kind
	case errors.Is(err, context.Canceled):
		return ErrorCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return ErrorNetwork
	case errors.Is(err, errs.ErrUnsupported):
		return ErrorUnsupported
	}
	if _, ok := errors.AsType[net.Error](err); ok {
		return ErrorNetwork
	}
	return ErrorNetwork
}

// KindOf returns the ErrorKind of err when it is (or wraps) a forge
// *Error, ok=false otherwise.
func KindOf(err error) (ErrorKind, bool) {
	if fe, ok := errors.AsType[*Error](err); ok {
		return fe.Kind, true
	}
	return 0, false
}

// IsKind reports whether err is (or wraps) a forge *Error of the given
// kind.
func IsKind(err error, kind ErrorKind) bool {
	k, ok := KindOf(err)
	return ok && k == kind
}
