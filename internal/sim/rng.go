package sim

import (
	"hash/fnv"
	"math"
	"math/rand/v2"
)

// Random-number layout (SPEC §10.1, DECISIONS.md D-M2-*):
//
//	chunk i:    rand.New(rand.NewPCG(masterSeed, uint64(i)))      ← exactly as the spec requires
//	future k:   futureSeed := chunkRNG.Uint64()                    ← one draw per future
//	world:      PCG(futureSeed, worldStreamKey)                    ← prices, usage, shared shocks
//	car c:      PCG(futureSeed, streamKey("car:"+c.ID))            ← repairs, own resale shock
//	pref a:     PCG(futureSeed, streamKey("pref:"+a.Key))          ← the ₹ value for attribute a
//
// Every stream is keyed by a stable name, so adding or removing a car or a
// preference never changes any other stream's numbers ("common random numbers").

// streamKey hashes a stable name into a PCG stream selector.
func streamKey(name string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(name)) // hash.Hash.Write never returns an error
	return h.Sum64()
}

// worldStreamKey selects the world stream within a future. It is a fixed
// constant (the 64-bit golden-ratio number), not a hash, so it can be a const.
const worldStreamKey uint64 = 0x9E3779B97F4A7C15

// stream is a reusable PCG-backed generator. Reseeding the existing PCG in
// place avoids allocating a new generator for every future and car.
type stream struct {
	pcg *rand.PCG
	rng *rand.Rand
}

func newStream() stream {
	pcg := rand.NewPCG(0, 0)
	return stream{pcg: pcg, rng: rand.New(pcg)}
}

func (s stream) reseed(seed, key uint64) { s.pcg.Seed(seed, key) }

// poisson draws a Poisson(lambda) count. Knuth's method is exact and fast for
// the small rates used here (well under 10 events a year); very large rates
// fall back to a normal approximation.
func poisson(r *rand.Rand, lambda float64) int {
	switch {
	case lambda <= 0:
		return 0
	case lambda > 30:
		n := math.Round(lambda + math.Sqrt(lambda)*r.NormFloat64())
		return int(math.Max(0, n))
	}
	limit := math.Exp(-lambda)
	k, p := 0, 1.0
	for {
		p *= r.Float64()
		if p <= limit {
			return k
		}
		k++
	}
}

// logNormalMeanOne returns exp(σZ − σ²/2), a multiplicative shock whose
// expected value is exactly 1, so it adds noise without biasing the mean.
func logNormalMeanOne(sigma, z float64) float64 {
	return math.Exp(sigma*z - sigma*sigma/2)
}
