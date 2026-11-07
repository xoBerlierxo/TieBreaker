package httpapi

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// fakeLimiter allows the first `allow` calls, or fails with err.
type fakeLimiter struct {
	allow int
	calls int
	err   error
}

func (f *fakeLimiter) Allow(context.Context, string, int, time.Duration) (bool, time.Duration, error) {
	f.calls++
	if f.err != nil {
		return false, 0, f.err
	}
	return f.calls <= f.allow, 30 * time.Second, nil
}

func limitedRouter(l Limiter) *gin.Engine {
	r := gin.New()
	r.POST("/start", rateLimit(l, discardLogger(), Limit{Name: "x", Max: 1, Window: time.Hour, Key: byClientIP}),
		func(c *gin.Context) { c.Status(http.StatusAccepted) })
	return r
}

func TestRateLimitMiddleware_429WithRetryAfter(t *testing.T) {
	r := limitedRouter(&fakeLimiter{allow: 1})
	if rec := do(t, r, http.MethodPost, "/start", ""); rec.Code != http.StatusAccepted {
		t.Fatalf("first: %d", rec.Code)
	}
	rec := do(t, r, http.MethodPost, "/start", "")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "30" {
		t.Fatalf("second: %d Retry-After=%q, want 429 / 30", rec.Code, rec.Header().Get("Retry-After"))
	}
	if code := decodeError(t, rec).Code; code != "RATE_LIMITED" {
		t.Errorf("code = %s", code)
	}
}

func TestRateLimitMiddleware_RedisDownFailsClosed(t *testing.T) {
	rec := do(t, limitedRouter(&fakeLimiter{err: errors.New("redis down")}), http.MethodPost, "/start", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (expensive routes fail closed)", rec.Code)
	}
}

func TestRateLimitMiddleware_ZeroDisablesRule(t *testing.T) {
	l := &fakeLimiter{allow: 0}
	r := gin.New()
	r.POST("/start", rateLimit(l, discardLogger(), Limit{Name: "x", Max: 0, Window: time.Hour, Key: byClientIP}),
		func(c *gin.Context) { c.Status(http.StatusAccepted) })
	if rec := do(t, r, http.MethodPost, "/start", ""); rec.Code != http.StatusAccepted || l.calls != 0 {
		t.Fatalf("status %d, limiter calls %d; a 0 limit must disable the rule", rec.Code, l.calls)
	}
}
