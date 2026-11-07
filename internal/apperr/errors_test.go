package apperr

import (
	"errors"
	"fmt"
	"testing"
)

func TestError_IsMatchesSentinelThroughWrapping(t *testing.T) {
	err := fmt.Errorf("loading session: %w", New(ErrNotFound, "session abc not found"))

	if !errors.Is(err, ErrNotFound) {
		t.Fatal("errors.Is(err, ErrNotFound) = false, want true")
	}
	if errors.Is(err, ErrVersionConflict) {
		t.Fatal("errors.Is(err, ErrVersionConflict) = true, want false")
	}

	var appErr *Error
	if !errors.As(err, &appErr) {
		t.Fatal("errors.As(err, *Error) = false, want true")
	}
	if appErr.Message != "session abc not found" {
		t.Errorf("Message = %q", appErr.Message)
	}
}

func TestError_WithDetailsDoesNotMutateOriginal(t *testing.T) {
	base := New(ErrValidation, "bad input")
	withField := base.WithDetails("field", "km_per_month")

	if base.Details != nil {
		t.Errorf("original Details mutated: %v", base.Details)
	}
	if withField.Details["field"] != "km_per_month" {
		t.Errorf("Details = %v", withField.Details)
	}
}

func TestValidation_SetsFieldDetail(t *testing.T) {
	err := Validation("limit", "must be between 1 and 50")
	if !errors.Is(err, ErrValidation) || err.Details["field"] != "limit" {
		t.Errorf("Validation() = %+v", err)
	}
	if got := err.Error(); got != "validation failed: must be between 1 and 50" {
		t.Errorf("Error() = %q", got)
	}
}
