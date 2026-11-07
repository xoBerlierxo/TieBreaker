package store

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// OpenRedis parses a redis:// URL, applies timeouts and checks that Redis
// answers. Every Redis call then has a bounded duration (SPEC §14: timeouts on
// every outgoing call).
func OpenRedis(ctx context.Context, rawURL string) (*redis.Client, error) {
	opts, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parsing REDIS_URL: %w", err)
	}
	opts.DialTimeout = 5 * time.Second
	opts.ReadTimeout = 2 * time.Second
	opts.WriteTimeout = 2 * time.Second
	// Respect context deadlines on every command.
	opts.ContextTimeoutEnabled = true

	client := redis.NewClient(opts)

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("pinging redis: %w", err)
	}
	return client, nil
}

// RedisPinger adapts a *redis.Client to the httpapi.Pinger interface.
type RedisPinger struct{ Client *redis.Client }

// Ping checks that Redis answers within the context's deadline.
func (p RedisPinger) Ping(ctx context.Context) error {
	return p.Client.Ping(ctx).Err()
}
