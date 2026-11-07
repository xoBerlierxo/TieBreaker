// Package apperr defines the domain errors shared by the services and the HTTP layer.
//
// Services return these errors (usually wrapped with extra context). The HTTP
// layer maps them to status codes in exactly one place (internal/http/errors.go),
// as SPEC §18 requires. This package has no dependencies, so any package can
// import it without creating an import cycle.
package apperr

import (
	"errors"
	"fmt"
)

// Sentinel errors. Check for them with errors.Is, never with ==, because
// callers usually wrap them with extra context.
var (
	ErrValidation           = errors.New("validation failed")
	ErrNotFound             = errors.New("not found")
	ErrVersionConflict      = errors.New("version conflict")
	ErrIllegalTransition    = errors.New("illegal state transition")
	ErrPreconditionRequired = errors.New("precondition required")
	ErrIdempotencyMismatch  = errors.New("idempotency key reused with a different request")
	ErrRateLimited          = errors.New("rate limited")
	ErrUnavailable          = errors.New("dependency unavailable")
	ErrPayloadTooLarge      = errors.New("payload too large")
)

// Error is a domain error with a human-readable message and optional
// structured details for the client (for example, which field was invalid).
// Kind is one of the sentinels above, so errors.Is(err, ErrNotFound) works on it.
type Error struct {
	Kind    error
	Message string
	Details map[string]any
}

func (e *Error) Error() string {
	if e.Message == "" {
		return e.Kind.Error()
	}
	return fmt.Sprintf("%s: %s", e.Kind.Error(), e.Message)
}

// Unwrap lets errors.Is and errors.As see the sentinel Kind.
func (e *Error) Unwrap() error { return e.Kind }

// New creates an *Error of the given kind.
func New(kind error, message string) *Error {
	return &Error{Kind: kind, Message: message}
}

// WithDetails returns a copy of e with the detail key set.
func (e *Error) WithDetails(key string, value any) *Error {
	details := make(map[string]any, len(e.Details)+1)
	for k, v := range e.Details {
		details[k] = v
	}
	details[key] = value
	return &Error{Kind: e.Kind, Message: e.Message, Details: details}
}

// Validation is a shortcut for the most common error: bad input on one field.
func Validation(field, message string) *Error {
	return New(ErrValidation, message).WithDetails("field", field)
}
