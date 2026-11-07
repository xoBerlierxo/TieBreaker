// Package httpapi is the HTTP layer: the Gin router, handlers, middleware and
// request/response DTOs (SPEC §4). The directory is internal/http, but the
// package is named httpapi so it doesn't collide with the standard library's
// net/http, which almost every file here also imports.
package httpapi

import (
	"errors"
	"log/slog"
	"maps"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
)

// ErrorBody is the single error shape used by every endpoint (SPEC §12):
//
//	{ "error": { "code": "SESSION_VERSION_CONFLICT", "message": "…", "details": {} } }
type ErrorBody struct {
	Error ErrorPayload `json:"error"`
}

// ErrorPayload is the content of ErrorBody.
type ErrorPayload struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

// statusFor is the one place where domain errors become HTTP status codes and
// error codes (SPEC §18). The order matters only for errors that wrap several
// kinds, which we don't do.
func statusFor(err error) (status int, code string) {
	var maxBytes *http.MaxBytesError
	switch {
	case errors.Is(err, apperr.ErrValidation):
		return http.StatusBadRequest, "VALIDATION_FAILED"
	case errors.Is(err, apperr.ErrNotFound):
		return http.StatusNotFound, "NOT_FOUND"
	case errors.Is(err, apperr.ErrVersionConflict):
		return http.StatusConflict, "SESSION_VERSION_CONFLICT"
	case errors.Is(err, apperr.ErrIllegalTransition):
		return http.StatusConflict, "ILLEGAL_STATE_TRANSITION"
	case errors.Is(err, apperr.ErrPreconditionRequired):
		return http.StatusPreconditionRequired, "PRECONDITION_REQUIRED"
	case errors.Is(err, apperr.ErrIdempotencyMismatch):
		return http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REUSED"
	case errors.Is(err, apperr.ErrRateLimited):
		return http.StatusTooManyRequests, "RATE_LIMITED"
	case errors.Is(err, apperr.ErrUnavailable):
		return http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE"
	case errors.Is(err, apperr.ErrPayloadTooLarge), errors.As(err, &maxBytes):
		return http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE"
	default:
		return http.StatusInternalServerError, "INTERNAL"
	}
}

// WriteError writes err in the standard error shape and aborts the request.
//
// For client errors (4xx) the message comes from the domain error, which is
// written for users. For server errors (5xx, except 503) the cause is logged
// but never sent to the client, because it may contain internal details such
// as SQL or hostnames.
func WriteError(c *gin.Context, logger *slog.Logger, err error) {
	status, code := statusFor(err)

	payload := ErrorPayload{Code: code, Message: publicMessage(err, status), Details: map[string]any{}}
	var appErr *apperr.Error
	if status != http.StatusInternalServerError && errors.As(err, &appErr) {
		maps.Copy(payload.Details, appErr.Details)
	}

	if status >= http.StatusInternalServerError {
		logger.ErrorContext(c.Request.Context(), "request failed",
			slog.String("request_id", RequestID(c)),
			slog.String("code", code),
			slog.Any("error", err),
		)
	}
	c.AbortWithStatusJSON(status, ErrorBody{Error: payload})
}

func publicMessage(err error, status int) string {
	if status == http.StatusInternalServerError {
		return "internal error"
	}
	var appErr *apperr.Error
	if errors.As(err, &appErr) {
		return appErr.Error()
	}
	var maxBytes *http.MaxBytesError
	if errors.As(err, &maxBytes) {
		return apperr.ErrPayloadTooLarge.Error()
	}
	// A bare (possibly wrapped) sentinel: use the sentinel's own text, not the
	// wrapping context, which is written for developers rather than users.
	for _, kind := range []error{
		apperr.ErrValidation, apperr.ErrNotFound, apperr.ErrVersionConflict,
		apperr.ErrIllegalTransition, apperr.ErrPreconditionRequired,
		apperr.ErrIdempotencyMismatch, apperr.ErrRateLimited,
		apperr.ErrUnavailable, apperr.ErrPayloadTooLarge,
	} {
		if errors.Is(err, kind) {
			return kind.Error()
		}
	}
	return http.StatusText(status)
}
