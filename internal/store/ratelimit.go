package store

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// fixedWindowScript increments a counter and sets its expiry on first use, in
// one atomic step (a Lua script runs without interruption in Redis). Returns
// the new count and the key's remaining lifetime in milliseconds.
const fixedWindowScript = `
local n = redis.call('INCR', KEYS[1])
if n == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return {n, redis.call('PTTL', KEYS[1])}
`

// RateLimiter is a fixed-window counter in Redis (SPEC §13, §14): at most
// `limit` events per key per window. Simple and good enough to protect the
// simulation workers and the Gemini quota from abuse.
type RateLimiter struct {
	Client *redis.Client
	script *redis.Script
}

// NewRateLimiter creates a limiter.
func NewRateLimiter(client *redis.Client) *RateLimiter {
	return &RateLimiter{Client: client, script: redis.NewScript(fixedWindowScript)}
}

// Allow records one event for key and reports whether it is within the limit.
// When it isn't, retryAfter says how long until the window resets.
func (l *RateLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (allowed bool, retryAfter time.Duration, err error) {
	res, err := l.script.Run(ctx, l.Client, []string{"rl:" + key}, window.Milliseconds()).Int64Slice()
	if err != nil {
		return false, 0, fmt.Errorf("rate limiter: %w", err)
	}
	count, ttlMs := res[0], res[1]
	if count > int64(limit) {
		return false, time.Duration(max(ttlMs, 0)) * time.Millisecond, nil
	}
	return true, 0, nil
}
