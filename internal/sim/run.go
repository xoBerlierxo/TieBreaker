package sim

import (
	"context"
	"math/rand/v2"
	"runtime"
	"sync"

	"golang.org/x/sync/errgroup"
)

// ctxCheckEvery is how often (in futures) a chunk checks for cancellation
// (SPEC §10.1: at least every 256 futures).
const ctxCheckEvery = 256

// Run simulates opt.Futures futures of owning each car and returns every
// sample (SPEC §10.1). It is a pure function of (in, opt): the same inputs and
// seed give bit-identical results for any opt.Workers.
//
// How determinism survives concurrency:
//   - Futures are split into chunks of opt.ChunkSize. Chunk i always uses
//     rand.New(rand.NewPCG(opt.MasterSeed, uint64(i))), whichever goroutine runs it.
//   - Future k always lands in sample slot k. Chunks write only to their own
//     slots, so the merged result is in chunk order however the chunks are
//     scheduled, and no locks are needed.
//
// Cancellation: each chunk checks ctx every 256 futures; the first error
// cancels the rest (errgroup) and Run returns ctx's error.
func Run(ctx context.Context, in Inputs, opt Options) (Result, error) {
	if err := in.Validate(); err != nil {
		return Result{}, err
	}
	if err := opt.validate(); err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	p := prepare(&in)
	res := newResult(&in, opt.Futures)

	numChunks := (opt.Futures + opt.ChunkSize - 1) / opt.ChunkSize
	workers := opt.Workers
	if workers == 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	workers = min(workers, numChunks)

	var (
		progressMu sync.Mutex
		doneChunks int
	)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(workers) // g.Go blocks while `workers` chunks are running

	for chunk := range numChunks {
		if gctx.Err() != nil {
			break // cancelled: don't start new chunks
		}
		g.Go(func() error {
			if err := runChunk(gctx, p, opt, chunk, &res); err != nil {
				return err
			}
			if opt.Progress != nil {
				// The mutex makes the callback see 1, 2, 3, … in order, even
				// though chunks finish on different goroutines.
				progressMu.Lock()
				doneChunks++
				opt.Progress(doneChunks, numChunks)
				progressMu.Unlock()
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return Result{}, err
	}
	// The loop can stop early on cancellation without any chunk failing.
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	return res, nil
}

// runChunk simulates futures [chunk·size, min((chunk+1)·size, futures)).
func runChunk(ctx context.Context, p *prepared, opt Options, chunk int, res *Result) error {
	start := chunk * opt.ChunkSize
	end := min(start+opt.ChunkSize, opt.Futures)

	chunkRNG := rand.New(rand.NewPCG(opt.MasterSeed, uint64(chunk))) // #nosec G115 -- chunk is a small non-negative int
	worldStream, carStream, prefStream := newStream(), newStream(), newStream()
	var (
		draws worldDraws
		paths worldPaths
	)
	prefs := make([]float64, len(p.cars))

	for k := start; k < end; k++ {
		if (k-start)%ctxCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}

		futureSeed := chunkRNG.Uint64()
		worldStream.reseed(futureSeed, worldStreamKey)
		drawWorld(worldStream, &draws)
		buildPaths(p, &draws, &paths)
		prefValues(p, futureSeed, prefStream, prefs)

		for c := range p.cars {
			carStream.reseed(futureSeed, p.cars[c].streamKey)
			cats := evaluateCar(p, &p.cars[c], &draws, &paths, carStream)

			own := 0.0
			for cat := range NumCategories { // fixed order: the sum is reproducible
				own += cats[cat]
			}
			cr := &res.Cars[c]
			for cat := range NumCategories {
				cr.Categories[cat][k] = cats[cat]
			}
			cr.OwnershipCost[k] = own
			cr.PrefValue[k] = prefs[c]
			cr.NetCost[k] = own - prefs[c]
		}
	}
	return nil
}

func newResult(in *Inputs, futures int) Result {
	res := Result{Futures: futures, Cars: make([]CarResult, len(in.Cars))}
	for i, c := range in.Cars {
		cr := CarResult{
			ID:            c.ID,
			NetCost:       make([]float64, futures),
			OwnershipCost: make([]float64, futures),
			PrefValue:     make([]float64, futures),
		}
		for cat := range NumCategories {
			cr.Categories[cat] = make([]float64, futures)
		}
		res.Cars[i] = cr
	}
	return res
}
