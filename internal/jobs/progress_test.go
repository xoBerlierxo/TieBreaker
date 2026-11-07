package jobs

import (
	"testing"
	"time"
)

func TestProgressThrottle_EmitsOnThresholdAndAlwaysOnComplete(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	th := newThrottle(2, 250*time.Millisecond)
	th.now = func() time.Time { return now }

	steps := []struct {
		pct     int
		advance time.Duration
		want    bool
	}{
		{0, 0, true},                       // first update always goes out
		{1, 10 * time.Millisecond, false},  // +1 % soon after: suppressed
		{2, 10 * time.Millisecond, true},   // +2 %: sent
		{3, 300 * time.Millisecond, true},  // +1 % but 300 ms later: sent
		{3, 300 * time.Millisecond, false}, // no change: never sent
		{100, 0, true},                     // completion always goes out
	}
	for i, s := range steps {
		now = now.Add(s.advance)
		if got := th.allow(s.pct); got != s.want {
			t.Errorf("step %d (pct %d): allow = %v, want %v", i, s.pct, got, s.want)
		}
	}
}

func TestStatus_Terminal(t *testing.T) {
	for s, want := range map[Status]bool{StatusQueued: false, StatusRunning: false, StatusDone: true, StatusFailed: true, StatusCancelled: true} {
		if s.Terminal() != want {
			t.Errorf("%s.Terminal() = %v", s, !want)
		}
	}
}

func TestSeed_RoundTripsThroughBigint(t *testing.T) {
	for _, seed := range []uint64{0, 1, 1 << 63, ^uint64(0)} {
		if got := (Run{MasterSeed: SeedToDB(seed)}).Seed(); got != seed {
			t.Errorf("seed %d round-tripped to %d", seed, got)
		}
	}
}
