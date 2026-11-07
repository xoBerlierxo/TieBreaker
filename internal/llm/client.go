// Package llm wraps the language model behind a small interface (SPEC §13), so
// the rest of the code, and every test, works without a real API key.
//
// The LLM only ever writes prose. No ranking, score or number may come from it
// (SPEC §1): its output is checked against the report facts and discarded on
// any mismatch (internal/report/validate.go).
package llm

import (
	"context"
	"errors"
)

// Client is the interface from SPEC §13.
type Client interface {
	GenerateText(ctx context.Context, req TextRequest) (string, error)
	GenerateJSON(ctx context.Context, req JSONRequest, out any) error // uses a response schema
}

// TextRequest asks for plain text.
type TextRequest struct {
	System          string // instructions
	Prompt          string // the content (never personal data, SPEC §13)
	MaxOutputTokens int32
}

// JSONRequest asks for JSON matching Schema (a JSON Schema object).
type JSONRequest struct {
	System          string
	Prompt          string
	Schema          map[string]any
	MaxOutputTokens int32
}

// Errors callers can react to (all of them mean: use the template instead).
var (
	ErrDisabled        = errors.New("llm disabled")
	ErrQuotaExceeded   = errors.New("llm daily quota exceeded")
	ErrOutputTruncated = errors.New("llm output truncated at the token limit")
	ErrEmptyOutput     = errors.New("llm returned no text")
)
