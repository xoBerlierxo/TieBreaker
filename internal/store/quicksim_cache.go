package store

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// QuickSimCache stores interview quick-simulation results (P(best) per car) in
// Redis, keyed by the inputs hash (SPEC §8.4). It implements interview.Cache.
// Redis problems are logged and treated as cache misses: the cache only makes
// things faster, so it must never make a request fail.
type QuickSimCache struct {
	Client *redis.Client
	TTL    time.Duration
	Logger *slog.Logger
}

// Get returns the cached P(best) vector, if present.
func (c QuickSimCache) Get(ctx context.Context, key string) ([]float64, bool) {
	raw, err := c.Client.Get(ctx, key).Bytes()
	if err != nil {
		if !errors.Is(err, redis.Nil) && c.Logger != nil {
			c.Logger.WarnContext(ctx, "quick-sim cache read failed", slog.Any("error", err))
		}
		return nil, false
	}
	var p []float64
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, false
	}
	return p, true
}

// Set stores a P(best) vector with the configured TTL.
func (c QuickSimCache) Set(ctx context.Context, key string, pBest []float64) {
	raw, err := json.Marshal(pBest)
	if err != nil {
		return
	}
	if err := c.Client.Set(ctx, key, raw, c.TTL).Err(); err != nil && c.Logger != nil {
		c.Logger.WarnContext(ctx, "quick-sim cache write failed", slog.Any("error", err))
	}
}
