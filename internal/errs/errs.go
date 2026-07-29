// Package errs defines mrman's error taxonomy, ported from tuicr's TuicrError.
//
// Several sentinels are used as control flow, not just failures: ErrNoChanges
// signals an empty diff (callers fall back or show a message), ErrUnsupported
// signals a VCS/forge capability gap (callers degrade gracefully), and
// ErrNoComments signals an empty export. Match them with errors.Is.
package errs

import (
	"errors"
	"fmt"
)

var (
	// ErrNotARepository indicates the working directory is not inside any
	// supported VCS repository.
	ErrNotARepository = errors.New("not a repository")

	// ErrNoChanges indicates a diff source produced no reviewable changes.
	ErrNoChanges = errors.New("no changes to review")

	// ErrNoComments indicates an export was requested with nothing to export.
	ErrNoComments = errors.New("no comments to export - skipping copy")

	// ErrUnsupported indicates an operation the active backend or forge does
	// not support.
	ErrUnsupported = errors.New("unsupported operation")
)

// CorruptedSession reports an unreadable or invalid persisted review session.
type CorruptedSession struct {
	Detail string
}

func (e *CorruptedSession) Error() string {
	return "review session corrupted: " + e.Detail
}

// VcsCommand reports a failed VCS subprocess invocation.
type VcsCommand struct {
	Detail string
}

func (e *VcsCommand) Error() string {
	return "VCS command failed: " + e.Detail
}

// InvalidInput reports user-supplied input that failed validation.
type InvalidInput struct {
	Detail string
}

func (e *InvalidInput) Error() string {
	return "invalid input: " + e.Detail
}

// Clipboard reports a clipboard copy failure.
type Clipboard struct {
	Detail string
}

func (e *Clipboard) Error() string {
	return "clipboard error: " + e.Detail
}

// Unsupportedf wraps ErrUnsupported with operation detail so callers can both
// errors.Is(err, ErrUnsupported) and show a specific message.
func Unsupportedf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrUnsupported, fmt.Sprintf(format, args...))
}
