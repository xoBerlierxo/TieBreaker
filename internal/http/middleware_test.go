package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// echoBodyRouter builds a router with the standard middleware and one route
// that reads the whole request body.
func echoBodyRouter(maxBytes int64) *gin.Engine {
	deps := testDeps()
	deps.MaxBodyBytes = maxBytes
	r := NewRouter(deps)
	r.POST("/echo", func(c *gin.Context) {
		b, err := io.ReadAll(c.Request.Body)
		if err != nil {
			WriteError(c, deps.Logger, err)
			return
		}
		c.String(http.StatusOK, string(b))
	})
	return r
}

func TestBodyLimit_Returns413WhenContentLengthTooLarge(t *testing.T) {
	r := echoBodyRouter(10)

	rec := do(t, r, http.MethodPost, "/echo", strings.Repeat("x", 11))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if code := decodeError(t, rec).Code; code != "PAYLOAD_TOO_LARGE" {
		t.Errorf("code = %q", code)
	}
}

func TestBodyLimit_Returns413WhenChunkedBodyTooLarge(t *testing.T) {
	r := echoBodyRouter(10)

	// No Content-Length: the limit must still apply while the body is read.
	req := httptest.NewRequest(http.MethodPost, "/echo", io.NopCloser(strings.NewReader(strings.Repeat("x", 50))))
	req.ContentLength = -1
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestBodyLimit_AllowsBodyWithinLimit(t *testing.T) {
	rec := do(t, echoBodyRouter(10), http.MethodPost, "/echo", "hello")
	if rec.Code != http.StatusOK || rec.Body.String() != "hello" {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}
}

func TestRequestID_GeneratedWhenMissing(t *testing.T) {
	rec := do(t, NewRouter(testDeps()), http.MethodGet, "/healthz", "")

	id := rec.Header().Get("X-Request-ID")
	if len(id) != 32 {
		t.Errorf("X-Request-ID = %q, want 32 hex chars", id)
	}
}

func TestRequestID_KeepsValidIncomingAndReplacesInvalid(t *testing.T) {
	tests := []struct {
		name     string
		incoming string
		wantKeep bool
	}{
		{"valid id kept", "abc-123_DEF", true},
		{"too long replaced", strings.Repeat("a", 129), false},
		{"bad characters replaced", "abc\ninjected", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			req.Header.Set("X-Request-ID", tt.incoming)
			rec := httptest.NewRecorder()
			NewRouter(testDeps()).ServeHTTP(rec, req)

			got := rec.Header().Get("X-Request-ID")
			if (got == tt.incoming) != tt.wantKeep {
				t.Errorf("X-Request-ID = %q, incoming %q, wantKeep=%v", got, tt.incoming, tt.wantKeep)
			}
		})
	}
}

func TestRecovery_PanicReturns500ErrorShape(t *testing.T) {
	r := NewRouter(testDeps())
	r.GET("/panic", func(*gin.Context) { panic("handler bug") })

	rec := do(t, r, http.MethodGet, "/panic", "")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if code := decodeError(t, rec).Code; code != "INTERNAL" {
		t.Errorf("code = %q, want INTERNAL", code)
	}
}

func TestUnknownRoute_Returns404ErrorShape(t *testing.T) {
	rec := do(t, NewRouter(testDeps()), http.MethodGet, "/nope", "")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if code := decodeError(t, rec).Code; code != "NOT_FOUND" {
		t.Errorf("code = %q, want NOT_FOUND", code)
	}
}
