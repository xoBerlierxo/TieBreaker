package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cached wraps a Client with a Redis cache keyed by a hash of the model and
// the whole request (SPEC §13: results cached by input hash). Identical facts
// never cost a second API call. Redis errors are logged and ignored.
type Cached struct {
	Inner  Client
	Redis  *redis.Client
	Model  string
	TTL    time.Duration
	Logger *slog.Logger
}

func (c Cached) key(kind string, req any) string {
	b, _ := json.Marshal(req)
	sum := sha256.Sum256(append([]byte(kind+"|"+c.Model+"|"), b...))
	return "llm:" + hex.EncodeToString(sum[:])
}

// GenerateText implements Client.
func (c Cached) GenerateText(ctx context.Context, req TextRequest) (string, error) {
	k := c.key("text", req)
	if v, err := c.Redis.Get(ctx, k).Result(); err == nil {
		return v, nil
	}
	out, err := c.Inner.GenerateText(ctx, req)
	if err != nil {
		return "", err
	}
	if err := c.Redis.Set(ctx, k, out, c.TTL).Err(); err != nil && c.Logger != nil {
		c.Logger.WarnContext(ctx, "llm cache write failed", slog.Any("error", err))
	}
	return out, nil
}

// GenerateJSON implements Client.
func (c Cached) GenerateJSON(ctx context.Context, req JSONRequest, out any) error {
	k := c.key("json", req)
	if v, err := c.Redis.Get(ctx, k).Bytes(); err == nil {
		return json.Unmarshal(v, out)
	}
	if err := c.Inner.GenerateJSON(ctx, req, out); err != nil {
		return err
	}
	if b, err := json.Marshal(out); err == nil {
		if err := c.Redis.Set(ctx, k, b, c.TTL).Err(); err != nil && c.Logger != nil {
			c.Logger.WarnContext(ctx, "llm cache write failed", slog.Any("error", err))
		}
	}
	return nil
}

// Budget wraps a Client with a daily call cap in Redis, protecting the API
// key's quota (SPEC §13). Over the cap → ErrQuotaExceeded (callers fall back
// to the template). If Redis is down, calls are refused (fail closed).
type Budget struct {
	Inner    Client
	Redis    *redis.Client
	DailyMax int
	Now      func() time.Time
}

func (b Budget) take(ctx context.Context) error {
	now := time.Now
	if b.Now != nil {
		now = b.Now
	}
	key := "llm:budget:" + now().UTC().Format("2006-01-02")
	n, err := b.Redis.Incr(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("llm budget: %w", err)
	}
	if n == 1 {
		b.Redis.Expire(ctx, key, 48*time.Hour)
	}
	if n > int64(b.DailyMax) {
		return ErrQuotaExceeded
	}
	return nil
}

// GenerateText implements Client.
func (b Budget) GenerateText(ctx context.Context, req TextRequest) (string, error) {
	if err := b.take(ctx); err != nil {
		return "", err
	}
	return b.Inner.GenerateText(ctx, req)
}

// GenerateJSON implements Client.
func (b Budget) GenerateJSON(ctx context.Context, req JSONRequest, out any) error {
	if err := b.take(ctx); err != nil {
		return err
	}
	return b.Inner.GenerateJSON(ctx, req, out)
}

// IsFallback reports whether err just means "use the template".
func IsFallback(err error) bool {
	return errors.Is(err, ErrQuotaExceeded) || errors.Is(err, ErrOutputTruncated) ||
		errors.Is(err, ErrEmptyOutput) || errors.Is(err, ErrDisabled) || errors.Is(err, context.DeadlineExceeded)
}
