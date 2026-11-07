package httpapi

import (
	"context"
	"log/slog"
	"math"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
)

// Limiter is a rate limiter (store.RateLimiter in production).
type Limiter interface {
	Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error)
}

// Limit is one rate-limit rule.
type Limit struct {
	Name   string                    // part of the Redis key, e.g. "sim-session"
	Max    int                       // events per window; 0 disables the rule
	Window time.Duration             //
	Key    func(*gin.Context) string // what is being limited (session ID, client IP)
}

// rateLimit applies the rules in order. Over the limit → 429 with a
// Retry-After header. If the limiter itself fails (Redis down), the request is
// refused with 503: these routes start expensive work, so failing closed
// protects the workers and the LLM quota.
func rateLimit(limiter Limiter, logger *slog.Logger, rules ...Limit) gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, r := range rules {
			if r.Max <= 0 {
				continue
			}
			ok, retry, err := limiter.Allow(c.Request.Context(), r.Name+":"+r.Key(c), r.Max, r.Window)
			if err != nil {
				WriteError(c, logger, apperr.New(apperr.ErrUnavailable, "rate limiter unavailable"))
				return
			}
			if !ok {
				c.Header("Retry-After", strconv.Itoa(int(math.Ceil(retry.Seconds()))))
				WriteError(c, logger, apperr.New(apperr.ErrRateLimited, "too many requests; try again later").
					WithDetails("limit", r.Name).WithDetails("retry_after_seconds", int(math.Ceil(retry.Seconds()))))
				return
			}
		}
		c.Next()
	}
}

// byClientIP limits per client IP. With no trusted proxies configured, Gin's
// ClientIP is the TCP peer address, which a client can't spoof.
func byClientIP(c *gin.Context) string { return c.ClientIP() }

// bySessionParam limits per session ID from the URL.
func bySessionParam(c *gin.Context) string { return c.Param("id") }
