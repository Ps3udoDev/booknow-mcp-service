// Package business implements the read use cases exposed by the BookNow Business MCP tools:
// business snapshot, schedule summary, appointment listing, customer search and available slots.
// Every operation is scoped to a tenant ID that comes from the authorized MCP connection.
package business

import (
	"errors"
	"fmt"
)

// Error kinds. Callers match them with errors.Is.
var (
	ErrInvalidArgument = errors.New("invalid argument")
	ErrNotFound        = errors.New("not found")
	// ErrConflict means the request is valid but the current state prevents it (slot taken, draft expired).
	ErrConflict = errors.New("conflict")
	// ErrForbidden means the actor is no longer allowed to perform the operation.
	ErrForbidden = errors.New("forbidden")
)

// NewError returns an Error of kind (one of the Err* values above) with a client-safe message.
func NewError(kind error, message string) error {
	return &Error{kind: kind, Message: message}
}

// Error carries a message that is safe to show to the MCP client (and the LLM).
type Error struct {
	kind    error
	Message string
}

func (e *Error) Error() string { return e.Message }

func (e *Error) Unwrap() error { return e.kind }

func invalidArgument(format string, args ...any) error {
	return &Error{kind: ErrInvalidArgument, Message: fmt.Sprintf(format, args...)}
}

func notFound(message string) error {
	return &Error{kind: ErrNotFound, Message: message}
}
