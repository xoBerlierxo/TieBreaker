//go:build integration

package llm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/xoBerlierxo/TieBreaker/internal/store"
	"github.com/xoBerlierxo/TieBreaker/internal/testutil"
)

var testRedis *redis.Client

func TestMain(m *testing.M) { os.Exit(runWithRedis(m)) }

func runWithRedis(m *testing.M) int {
	ctx := context.Background()
	url, stop, err := testutil.Redis(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer stop()
	if testRedis, err = store.OpenRedis(ctx, url); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return m.Run()
}

func TestCached_SecondIdenticalCallHitsCache(t *testing.T) {
	testRedis.FlushDB(context.Background())
	fake := &Fake{Responses: []string{"prose"}}
	c := Cached{Inner: fake, Redis: testRedis, Model: "m1", TTL: time.Hour}
	req := TextRequest{System: "s", Prompt: "same facts"}
	for range 3 {
		if out, err := c.GenerateText(context.Background(), req); err != nil || out != "prose" {
			t.Fatalf("out=%q err=%v", out, err)
		}
	}
	if fake.Calls() != 1 {
		t.Errorf("inner calls = %d, want 1 (the rest from cache)", fake.Calls())
	}
}

func TestCached_DifferentModelOrPromptMisses(t *testing.T) {
	testRedis.FlushDB(context.Background())
	fake := &Fake{Responses: []string{"prose"}}
	req := TextRequest{Prompt: "facts"}
	_, _ = Cached{Inner: fake, Redis: testRedis, Model: "m1", TTL: time.Hour}.GenerateText(context.Background(), req)
	_, _ = Cached{Inner: fake, Redis: testRedis, Model: "m2", TTL: time.Hour}.GenerateText(context.Background(), req)
	_, _ = Cached{Inner: fake, Redis: testRedis, Model: "m1", TTL: time.Hour}.GenerateText(context.Background(), TextRequest{Prompt: "other"})
	if fake.Calls() != 3 {
		t.Errorf("inner calls = %d, want 3", fake.Calls())
	}
}

func TestBudget_ExceededReturnsErrQuota(t *testing.T) {
	testRedis.FlushDB(context.Background())
	fake := &Fake{Responses: []string{"ok"}}
	day := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	b := Budget{Inner: fake, Redis: testRedis, DailyMax: 2, Now: func() time.Time { return day }}
	for i := range 2 {
		if _, err := b.GenerateText(context.Background(), TextRequest{}); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if _, err := b.GenerateText(context.Background(), TextRequest{}); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("3rd call err = %v, want ErrQuotaExceeded", err)
	}
	if fake.Calls() != 2 {
		t.Errorf("inner calls = %d, want 2", fake.Calls())
	}
	day = day.Add(24 * time.Hour) // a new day resets the budget
	if _, err := b.GenerateText(context.Background(), TextRequest{}); err != nil {
		t.Errorf("next day: %v", err)
	}
}
