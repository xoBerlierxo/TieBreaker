package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func corsRouter() http.Handler {
	d := testDeps()
	d.CORSAllowedOrigins = []string{"https://tiebreaker.example"}
	return NewRouter(d)
}

func TestCORS_AllowsConfiguredOriginOnly(t *testing.T) {
	for _, tt := range []struct {
		origin string
		allow  bool
	}{{"https://tiebreaker.example", true}, {"https://evil.example", false}} {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.Header.Set("Origin", tt.origin)
		rec := httptest.NewRecorder()
		corsRouter().ServeHTTP(rec, req)
		got := rec.Header().Get("Access-Control-Allow-Origin")
		if tt.allow && got != tt.origin {
			t.Errorf("%s: Allow-Origin = %q, want it echoed", tt.origin, got)
		}
		if !tt.allow && got != "" {
			t.Errorf("%s: Allow-Origin = %q, want none", tt.origin, got)
		}
		if tt.allow && !strings.Contains(rec.Header().Get("Access-Control-Expose-Headers"), "ETag") {
			t.Error("ETag must be exposed so the browser can read the session version")
		}
	}
}

func TestCORS_PreflightReturns204WithHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodOptions, "/v1/sessions/abc/answers", nil)
	req.Header.Set("Origin", "https://tiebreaker.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	corsRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", rec.Code)
	}
	allowed := rec.Header().Get("Access-Control-Allow-Headers")
	for _, h := range []string{"If-Match", "Idempotency-Key", "Content-Type"} {
		if !strings.Contains(allowed, h) {
			t.Errorf("Allow-Headers %q lacks %s", allowed, h)
		}
	}
}
