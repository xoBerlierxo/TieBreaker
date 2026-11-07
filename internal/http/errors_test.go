package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
)

func TestMapError_DomainErrorsToStatus(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"validation", apperr.Validation("limit", "too big"), http.StatusBadRequest, "VALIDATION_FAILED"},
		{"not found", apperr.New(apperr.ErrNotFound, "no such session"), http.StatusNotFound, "NOT_FOUND"},
		{"version conflict", apperr.ErrVersionConflict, http.StatusConflict, "SESSION_VERSION_CONFLICT"},
		{"illegal transition", apperr.ErrIllegalTransition, http.StatusConflict, "ILLEGAL_STATE_TRANSITION"},
		{"missing If-Match", apperr.ErrPreconditionRequired, http.StatusPreconditionRequired, "PRECONDITION_REQUIRED"},
		{"idempotency mismatch", apperr.ErrIdempotencyMismatch, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REUSED"},
		{"rate limited", apperr.ErrRateLimited, http.StatusTooManyRequests, "RATE_LIMITED"},
		{"dependency down", apperr.ErrUnavailable, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE"},
		{"generic conflict", apperr.ErrConflict, http.StatusConflict, "CONFLICT"},
		{"unauthorized", apperr.ErrUnauthorized, http.StatusUnauthorized, "UNAUTHORIZED"},
		{"forbidden", apperr.ErrForbidden, http.StatusForbidden, "FORBIDDEN"},
		{"payload too large", apperr.ErrPayloadTooLarge, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE"},
		{"max bytes reader", &http.MaxBytesError{Limit: 10}, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE"},
		{"wrapped sentinel", fmt.Errorf("saving answer: %w", apperr.ErrVersionConflict), http.StatusConflict, "SESSION_VERSION_CONFLICT"},
		{"unknown error", errors.New("boom"), http.StatusInternalServerError, "INTERNAL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, code := statusFor(tt.err)
			if status != tt.wantStatus || code != tt.wantCode {
				t.Errorf("statusFor(%v) = (%d, %q), want (%d, %q)", tt.err, status, code, tt.wantStatus, tt.wantCode)
			}
		})
	}
}

func TestErrorResponse_HasCodeMessageDetails(t *testing.T) {
	r := gin.New()
	r.GET("/fail", func(c *gin.Context) {
		WriteError(c, discardLogger(), apperr.Validation("km_per_month", "must be between 0 and 20000"))
	})

	rec := do(t, r, http.MethodGet, "/fail", "")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	payload := decodeError(t, rec)
	if payload.Code != "VALIDATION_FAILED" {
		t.Errorf("code = %q", payload.Code)
	}
	if payload.Message != "validation failed: must be between 0 and 20000" {
		t.Errorf("message = %q", payload.Message)
	}
	if payload.Details["field"] != "km_per_month" {
		t.Errorf("details = %v", payload.Details)
	}
}

func TestErrorResponse_InternalErrorsHideTheCause(t *testing.T) {
	r := gin.New()
	r.GET("/fail", func(c *gin.Context) {
		WriteError(c, discardLogger(), errors.New("pq: password authentication failed for user tb"))
	})

	rec := do(t, r, http.MethodGet, "/fail", "")

	payload := decodeError(t, rec)
	if payload.Message != "internal error" {
		t.Errorf("message = %q, want generic message that hides the cause", payload.Message)
	}
	if payload.Details == nil {
		t.Error("details must be an empty object, not null")
	}
}

func TestErrorResponse_DetailsIsEmptyObjectNotNull(t *testing.T) {
	r := gin.New()
	r.GET("/fail", func(c *gin.Context) {
		WriteError(c, discardLogger(), apperr.ErrNotFound)
	})
	rec := do(t, r, http.MethodGet, "/fail", "")
	if got := rec.Body.String(); !strings.Contains(got, `"details":{}`) {
		t.Errorf("body = %s, want details to be {}", got)
	}
}
