package interview

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"

	"github.com/xoBerlierxo/TieBreaker/internal/decision"
	"github.com/xoBerlierxo/TieBreaker/internal/sim"
)

// Cache stores quick-simulation results (P(best) per car) by inputs hash
// (SPEC §8.4: "cache quick-sim results by inputs hash"). Implementations must
// be safe for concurrent use. A failing cache must behave like a miss.
type Cache interface {
	Get(ctx context.Context, key string) ([]float64, bool)
	Set(ctx context.Context, key string, pBest []float64)
}

// NoCache is a Cache that never stores anything.
type NoCache struct{}

func (NoCache) Get(context.Context, string) ([]float64, bool) { return nil, false }
func (NoCache) Set(context.Context, string, []float64)        {}

// QuickSimulator runs the small simulations the interview uses to compare
// questions. Each one runs single-threaded: the interview runs many of them in
// parallel instead, which keeps every core busy without oversubscribing.
type QuickSimulator struct {
	Futures   int
	ChunkSize int
	Cache     Cache
}

// PBest returns P(best) per car for the inputs, from the cache when possible.
func (q QuickSimulator) PBest(ctx context.Context, in sim.Inputs, seed uint64) ([]float64, error) {
	key, err := CacheKey(in, seed, q.Futures)
	if err != nil {
		return nil, err
	}
	cache := q.Cache
	if cache == nil {
		cache = NoCache{}
	}
	if p, ok := cache.Get(ctx, key); ok && len(p) == len(in.Cars) {
		return p, nil
	}
	res, err := sim.Run(ctx, in, sim.Options{Futures: q.Futures, ChunkSize: q.ChunkSize, Workers: 1, MasterSeed: seed})
	if err != nil {
		return nil, err
	}
	p := decision.PBest(res)
	cache.Set(ctx, key, p)
	return p, nil
}

// CacheKey hashes everything that determines a quick-sim result: the inputs
// (canonical JSON: struct fields always serialise in the same order), the
// seed, the number of futures and the model version.
func CacheKey(in sim.Inputs, seed uint64, futures int) (string, error) {
	b, err := json.Marshal(in)
	if err != nil {
		return "", fmt.Errorf("hashing quick-sim inputs: %w", err)
	}
	h := sha256.New()
	h.Write(b)
	var buf [16]byte
	binary.LittleEndian.PutUint64(buf[:8], seed)
	binary.LittleEndian.PutUint64(buf[8:], uint64(futures)) // #nosec G115 -- futures is positive
	h.Write(buf[:])
	h.Write([]byte(sim.ModelVersion))
	return "quicksim:" + hex.EncodeToString(h.Sum(nil)), nil
}

// SeedFor derives the interview's fixed seed from the session ID. Every quick
// simulation in one interview uses it, so every comparison is made on the same
// futures (common random numbers, DECISIONS.md D-P-14).
func SeedFor(sessionID string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("interview:" + sessionID))
	return h.Sum64()
}
