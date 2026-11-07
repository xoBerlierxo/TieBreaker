package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Publisher sends progress events to listeners (SSE clients).
type Publisher interface {
	Publish(ctx context.Context, ev ProgressEvent) error
}

// RedisPublisher publishes on Redis channel run:{id} (SPEC §11). Pub/sub is
// fire-and-forget: a listener that isn't connected misses the message, which
// is why progress is also stored in Postgres for catching up.
type RedisPublisher struct{ Client *redis.Client }

// Publish sends one event.
func (p RedisPublisher) Publish(ctx context.Context, ev ProgressEvent) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("encoding progress event: %w", err)
	}
	return p.Client.Publish(ctx, Channel(ev.RunID), b).Err()
}

// NopPublisher discards events (tests, or Redis not configured).
type NopPublisher struct{}

// Publish does nothing.
func (NopPublisher) Publish(context.Context, ProgressEvent) error { return nil }

// throttle decides which progress updates are worth sending: at least minStep
// percentage points or minGap time since the last one, and always 100.
// Without it, a 50-chunk run would write to Postgres and Redis 50 times a second.
type throttle struct {
	mu       sync.Mutex
	minStep  int
	minGap   time.Duration
	lastPct  int
	lastTime time.Time
	now      func() time.Time
}

func newThrottle(minStep int, minGap time.Duration) *throttle {
	return &throttle{minStep: minStep, minGap: minGap, lastPct: -1, now: time.Now}
}

func (t *throttle) allow(pct int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	if pct == 100 || t.lastPct < 0 || pct-t.lastPct >= t.minStep || (pct > t.lastPct && now.Sub(t.lastTime) >= t.minGap) {
		t.lastPct, t.lastTime = pct, now
		return true
	}
	return false
}

// Subscribe listens for a run's progress events. The subscription is
// confirmed before it returns, so a caller that subscribes *before* reading the
// run's current state from Postgres can't miss an update in between.
func Subscribe(ctx context.Context, client *redis.Client, runID string) (<-chan ProgressEvent, func(), error) {
	ps := client.Subscribe(ctx, Channel(runID))
	if _, err := ps.Receive(ctx); err != nil { // wait for the subscribe confirmation
		_ = ps.Close()
		return nil, nil, fmt.Errorf("subscribing to progress: %w", err)
	}
	out := make(chan ProgressEvent, 16)
	msgs := ps.Channel()
	done := make(chan struct{})
	go func() {
		defer close(out)
		for {
			select {
			case <-done:
				return
			case m, ok := <-msgs:
				if !ok {
					return
				}
				var ev ProgressEvent
				if json.Unmarshal([]byte(m.Payload), &ev) == nil {
					select {
					case out <- ev:
					case <-done:
						return
					}
				}
			}
		}
	}()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			close(done)
			_ = ps.Close()
		})
	}
	return out, stop, nil
}

// RedisProgress is a ProgressSource for the SSE handler.
type RedisProgress struct{ Client *redis.Client }

// Subscribe listens for one run's events (see the package-level Subscribe).
func (p RedisProgress) Subscribe(ctx context.Context, runID string) (<-chan ProgressEvent, func(), error) {
	return Subscribe(ctx, p.Client, runID)
}
