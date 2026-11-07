# 02 — The Monte Carlo engine: randomness, concurrency, determinism

Milestone **M2**. This is the heart of the project: [internal/sim](../../internal/sim/) and [internal/decision](../../internal/decision/).

---

## 1. What a Monte Carlo simulation does here

We don't know next year's fuel price, how much you'll actually drive, or whether the gearbox fails in year 4. So we **simulate many possible futures** (50,000 for the final answer) and, in each one, compute every car's cost. "P(best) = 0.62" then simply means "in 62 % of the simulated futures, this car had the lowest net cost".

Two kinds of randomness exist:

- **Shared (the world):** fuel prices, km driven, the city share, how real efficiency compares with the brochure, and resale market moods. Every car sees the *same* world in a future. That's fair: if petrol gets expensive, it does so for the petrol car *and* the EV comparison at the same time.
- **Per car:** repairs and the car's own resale luck.

## 2. `math/rand/v2` and PCG streams

[internal/sim/rng.go](../../internal/sim/rng.go)

```go
pcg := rand.NewPCG(seed1, seed2)  // a small, fast, seedable generator
r   := rand.New(pcg)              // adds Float64, NormFloat64, IntN, …
pcg.Seed(a, b)                    // re-seed in place: no new allocation
```

- PCG is deterministic: the same two seeds always produce the same sequence. That is the foundation of reproducibility.
- **Never use the global `rand.Float64()`** in the engine. It is shared, randomly seeded and would make results unrepeatable. The lint rule also bans the old `math/rand`.
- **Stream layout:** one seed per future comes from the chunk generator. From it we derive separate streams for the world, for each car (keyed by the car's ID hash) and for each preference (keyed by its name). Changing one input therefore never "shifts" the random numbers of anything else. This is called **common random numbers**, and it's what makes "what if km/month were 1,100 instead of 1,000?" a fair comparison: same futures, one input changed.

## 3. Goroutines with `errgroup`

[internal/sim/run.go](../../internal/sim/run.go)

```go
g, gctx := errgroup.WithContext(ctx)
g.SetLimit(workers)                       // at most `workers` goroutines at once
for chunk := range numChunks {
    if gctx.Err() != nil { break }        // cancelled: stop launching
    g.Go(func() error { return runChunk(gctx, …, chunk, &res) })
}
err := g.Wait()                           // waits for all; returns the first error
```

- `errgroup` is a `sync.WaitGroup` plus error handling: the first goroutine to return an error cancels `gctx`, and every other chunk sees that at its next check and stops.
- `SetLimit` turns it into a **worker pool**: `g.Go` blocks while `workers` goroutines are already running.
- **Loop variables in closures:** since Go 1.22 each iteration of `for chunk := range …` gets its own `chunk`, so the closure captures the right value. Before 1.22 you had to write `chunk := chunk`.

## 4. Why the result doesn't depend on the number of workers

Two rules:

1. **Chunk i always uses `rand.NewPCG(masterSeed, i)`**, whichever goroutine runs it and whenever it runs.
2. **Future k always writes to index k** of pre-allocated slices. Different chunks write to different index ranges, so they never touch the same memory: no lock is needed, and the race detector stays quiet.

So with 1 worker or 16, the same number lands in the same place. `TestRun_IdenticalAcrossWorkerCounts_1_4_16` compares every sample **bit for bit** (`math.Float64bits`), not "approximately".

A subtle point about floating point: `a + b + c` can differ from `a + (b + c)` in the last bit. Every sum in a result path is therefore done in a fixed order (for example, the category loop), never in "whatever order goroutines finish".

## 5. Cancellation that actually stops work

```go
if (k-start)%256 == 0 {
    if err := ctx.Err(); err != nil { return err }
}
```

A CPU-bound loop never blocks on a channel, so it would never notice cancellation unless it *asks*. Checking `ctx.Err()` every 256 futures (SPEC §10.1) costs nothing measurable and makes a cancel take effect in about a millisecond.

**Goroutine leaks** happen when a goroutine waits forever on something nobody will send. `go.uber.org/goleak` in `TestMain` checks that no extra goroutines are alive when the tests finish. `errgroup.Wait()` guarantees every chunk goroutine has returned before `Run` returns.

## 6. Progress callbacks from many goroutines

Chunks finish on different goroutines in any order. The callback is wrapped in a mutex that also increments the counter, so the caller sees 1, 2, 3, … in order, never two calls at once.

## 7. Small numerical tricks worth knowing

- **Mean-one log-normal:** `exp(σZ − σ²/2)` has expected value exactly 1. A plain `exp(σZ)` has mean `exp(σ²/2)` and would silently bias costs upward.
- **Poisson by Knuth's method:** multiply uniform numbers until the product drops below `e^(−λ)`. It is exact and fast for small λ.
- **Discounting:** ₹1 paid at the end of year t is worth `1/(1+r)^t` today.

## 8. Testing a random system

- **Hand-checkable:** switch all randomness off (σ = 0, fixed ranges) and compare with arithmetic written in the test's comment (`TestOwnershipCost_HandCheckable_NewCar`).
- **Property tests:** with common random numbers, "more km never lowers energy cost" can be checked *future by future*, not just on average.
- **Golden hash:** a SHA-256 of all outputs catches any accidental model change.
- **Benchmarks:** `b.Loop()` (Go 1.24+) and `b.ReportMetric(x, "futures/s")` add custom units to the output.

## 9. pprof in one line

```bash
go test -run='^$' -bench=BenchmarkRun_3cars_2k -cpuprofile=cpu.prof ./internal/sim/
go tool pprof -top cpu.prof
```

That's how we found that about 41 % of the time is `math.Exp`: the engine is compute-bound, with no hidden overhead.

---

## Questions you should be able to answer

1. **Run the same inputs with seed 42 on 1 worker and on 16 workers: why are the results bit-identical? Name the two rules, and explain what would break if each chunk instead drew from one shared `*rand.Rand` protected by a mutex.**
   *Hint:* chunk-keyed PCG plus index-addressed writes; a shared RNG hands out numbers in scheduling order, which changes from run to run.

2. **What does `errgroup.WithContext` + `SetLimit` give you over a plain `sync.WaitGroup`? Walk through what happens when the caller cancels the context half-way through a run.**
   *Hint:* bounded parallelism and first-error cancellation; chunks see `ctx.Err()` within 256 futures; the dispatch loop stops; `Wait` returns `context.Canceled`; goleak confirms nothing leaked.

3. **Why does each car get its own random stream keyed by its ID, instead of drawing repairs from the shared world stream? Which test would fail if we changed that?**
   *Hint:* common random numbers; adding a car would shift every later draw; `TestCarStream_UnaffectedByAddingAnotherCar`.
