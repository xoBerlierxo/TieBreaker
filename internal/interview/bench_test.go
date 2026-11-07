package interview

import (
	"context"
	"slices"
	"testing"
	"time"
)

// BenchmarkNextQuestion_3cars measures choosing the next question with no
// cache (the worst case): 3 cars and 9 candidate attributes, which is
// 1 + 9 × 7 = 64 quick simulations of 2,000 futures. SPEC §16: p95 < 1.5 s.
func BenchmarkNextQuestion_3cars(b *testing.B) {
	base := baseInputs(700_000, 720_000, 740_000)
	var attrs []Attribute
	for i, key := range []string{"boot", "ground_clearance", "performance", "automatic",
		"feature:esc", "feature:rear_camera", "feature:6_airbags", "feature:connected_car", "warranty"} {
		shares := []float64{0, 0.5, 1}
		if i%2 == 1 {
			shares = []float64{1, 0, 0.3}
		}
		attrs = append(attrs, manualAttr(key, shares...))
	}
	p := selectingPlanner(nil) // no cache: every call simulates everything
	names := []string{"A", "B", "C"}
	ctx := context.Background()

	var durations []time.Duration
	for i := 0; b.Loop(); i++ {
		start := time.Now()
		if _, err := p.NextTradeoff(ctx, base, attrs, names, NewTradeoffState(), uint64(i)); err != nil { // #nosec G115
			b.Fatal(err)
		}
		durations = append(durations, time.Since(start))
	}
	slices.Sort(durations)
	b.ReportMetric(float64(durations[int(float64(len(durations)-1)*0.95)].Milliseconds()), "p95_ms")
}
