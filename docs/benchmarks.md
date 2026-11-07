# Benchmarks

Measured on 2026-11-07. Re-run with:

```bash
go test -run='^$' -bench=Benchmark -benchmem ./internal/sim/
```

**Machine:** AMD Ryzen 5 5600H (6 cores / 12 threads), Windows 11, Go 1.26.1, amd64.
**Workload:** `stochasticInputs()` in `internal/sim/fixtures_test.go`. It has 3 cars (a new petrol, an EV, a used CNG), a 5-year horizon, a loan, 2 priced preferences and every source of randomness switched on.

## Simulation engine (`internal/sim`)

| Benchmark | Workers | Time per run | Futures / second | Allocated per run |
|---|---|---|---|---|
| `BenchmarkRun_3cars_50k` (full run) | 1 | 117 ms | ~429,000 | 13.3 MB, 446 allocs |
| `BenchmarkRun_3cars_50k` (full run) | 4 | 33 ms | ~1,494,000 | 13.3 MB, 444 allocs |
| `BenchmarkRun_3cars_50k` (full run) | 12 | 18 ms | ~2,728,000 | 13.3 MB, 447 allocs |
| `BenchmarkRun_3cars_2k` (quick sim) | 1 | 4.8 ms | ~416,000 | 0.55 MB, 155 allocs |
| `BenchmarkQuickSim_P95` | 1 | p95 = **7.4 ms** | — | — |

### Against the SPEC §16 targets

| Target | Measured | Status |
|---|---|---|
| Quick simulation, 3 cars × 2,000 futures: p95 < 150 ms | p95 7.4 ms (1 worker) | ✅ about 20× under |
| Full simulation, 3 cars × 50,000 futures: < 5 s on 4 cores | 33 ms (4 workers) | ✅ about 150× under |
| `next-question` p95 < 1.5 s | measured in M3 (`BenchmarkNextQuestion_3cars`) | — |

### Notes

- **Where the time goes:** about 41 % in `math.Exp` (price paths, log-normal shocks, repair costs) and about 26 % in `NormFloat64`; the profile was taken with `-cpuprofile`. There is no measurable fixed overhead per run.
- **Allocations:** the allocations are the result arrays themselves: 3 cars × 11 series × 50,000 futures × 8 bytes ≈ 13 MB. The per-future loop allocates nothing, because the PCG generators are re-seeded in place.
- **Scaling:** going from 1 to 4 workers gives 3.5×; going to 12 threads gives 6.4× on 6 physical cores.
