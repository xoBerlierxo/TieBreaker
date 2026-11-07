package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
)

const (
	requestIDHeader = "X-Request-ID"
	requestIDKey    = "request_id" // key in the Gin context
)

// validRequestID limits incoming request IDs to safe characters, so a client
// can't inject newlines or huge values into our logs.
var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// RequestID returns the request ID set by the requestID middleware.
func RequestID(c *gin.Context) string {
	return c.GetString(requestIDKey)
}

// requestID keeps a valid incoming X-Request-ID (useful for tracing through a
// proxy) or generates a new random one, and echoes it in the response.
func requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(requestIDHeader)
		if !validRequestID.MatchString(id) {
			id = newRequestID()
		}
		c.Set(requestIDKey, id)
		c.Header(requestIDHeader, id)
		c.Next()
	}
}

func newRequestID() string {
	var b [16]byte
	// crypto/rand.Read never returns an error on supported platforms (Go 1.24+).
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// accessLog writes one structured log line per request. It logs the route
// template (e.g. /v1/sessions/:id), not the raw path, so session IDs don't end
// up in the logs.
func accessLog(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		status := c.Writer.Status()
		level := slog.LevelInfo
		if status >= http.StatusInternalServerError {
			level = slog.LevelError
		}
		logger.LogAttrs(c.Request.Context(), level, "http request",
			slog.String("request_id", RequestID(c)),
			slog.String("method", c.Request.Method),
			slog.String("route", route),
			slog.Int("status", status),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			slog.Int("bytes", c.Writer.Size()),
		)
	}
}

// recovery turns a panic inside a handler into a logged 500 error response,
// so one buggy request can't crash the server.
func recovery(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.ErrorContext(c.Request.Context(), "panic in handler",
					slog.String("request_id", RequestID(c)),
					slog.Any("panic", recovered),
					slog.String("stack", string(debug.Stack())),
				)
				WriteError(c, logger, errors.New("panic recovered"))
			}
		}()
		c.Next()
	}
}

// bodyLimit rejects request bodies larger than maxBytes (SPEC §14).
//
// If the client declares a too-large Content-Length, the request is rejected
// immediately. Otherwise the body is wrapped in http.MaxBytesReader, so reading
// past the limit fails with *http.MaxBytesError, which WriteError maps to 413.
func bodyLimit(logger *slog.Logger, maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > maxBytes {
			WriteError(c, logger, apperr.New(apperr.ErrPayloadTooLarge, "request body too large"))
			return
		}
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}
