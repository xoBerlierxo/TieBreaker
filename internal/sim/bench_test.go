package sim

import (
	"context"
	"fmt"
	"runtime"
	"slices"
	"testing"
	"time"
)

// BenchmarkRun_3cars_50k is the SPEC §15 benchmark: a full run of 3 cars ×
// 50,000 futures. It reports futures/second for the README, with the worker
// count set explicitly (1, 4, and all cores) so the scaling is visible.
func BenchmarkRun_3cars_50k(b *testing.B) {
	for _, w := range []int{1, 4, runtime.NumCPU()} {
		b.Run(fmt.Sprintf("workers=%d", w), func(b *testing.B) { benchRun(b, 50_000, w) })
	}
}

// BenchmarkRun_3cars_2k is one interview quick simulation (SPEC §8.4, §16:
// p95 < 150 ms on 4 cores). Quick sims run single-threaded because the
// interview runs many of them in parallel.
func BenchmarkRun_3cars_2k(b *testing.B) {
	benchRun(b, 2_000, 1)
}

func benchRun(b *testing.B, futures, workers int) {
	in := stochasticInputs()
	o := Options{Futures: futures, ChunkSize: 1000, Workers: workers, MasterSeed: 1}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := Run(ctx, in, o); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(futures)*float64(b.N)/b.Elapsed().Seconds(), "futures/s")
}

// BenchmarkQuickSim_P95 measures the 95th-percentile latency of a quick
// simulation over many runs (reported as p95_ms).
func BenchmarkQuickSim_P95(b *testing.B) {
	in := stochasticInputs()
	ctx := context.Background()
	var durations []time.Duration
	for i := 0; b.Loop(); i++ {
		o := Options{Futures: 2_000, ChunkSize: 1000, Workers: 1, MasterSeed: uint64(i)} // #nosec G115
		start := time.Now()
		if _, err := Run(ctx, in, o); err != nil {
			b.Fatal(err)
		}
		durations = append(durations, time.Since(start))
	}
	slices.Sort(durations)
	p95 := durations[int(float64(len(durations)-1)*0.95)]
	b.ReportMetric(float64(p95.Microseconds())/1000, "p95_ms")
}
