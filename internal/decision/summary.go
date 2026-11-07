// Package decision turns simulation samples into decisions (SPEC §4, §9):
// P(best), P(cheapest), percentiles, winner/deadlock classification and
// tipping points. Like internal/sim it is pure: no I/O, no globals.
package decision

import (
	"math"
	"slices"

	"github.com/xoBerlierxo/TieBreaker/internal/sim"
)

// Percentiles are the 10th, 50th and 90th percentiles of a sample.
type Percentiles struct {
	P10 float64 `json:"p10"`
	P50 float64 `json:"p50"`
	P90 float64 `json:"p90"`
}

// CategoryMedian is the median of one cost category.
type CategoryMedian struct {
	Category string  `json:"category"`
	Median   float64 `json:"median_inr"`
}

// CarSummary is everything the report needs about one car's distribution.
type CarSummary struct {
	ID            string           `json:"id"`
	PBest         float64          `json:"p_best"`     // share of futures where this car has the lowest NetCost
	PCheapest     float64          `json:"p_cheapest"` // share of futures where it has the lowest OwnershipCost
	NetCost       Percentiles      `json:"net_cost"`
	OwnershipCost Percentiles      `json:"ownership_cost"`
	MeanNetCost   float64          `json:"mean_net_cost"`
	Breakdown     []CategoryMedian `json:"breakdown"` // fixed category order
}

// Summary describes a whole run.
type Summary struct {
	Futures int          `json:"futures"`
	Cars    []CarSummary `json:"cars"` // same order as the inputs
	Leader  int          `json:"leader"`
	Entropy float64      `json:"entropy_bits"`
}

// PBest returns, for each car, the share of futures in which it has the lowest
// net cost (SPEC §1). If several cars tie exactly in a future, the win is split
// equally between them, so two identical cars get ½ each instead of the first
// one taking every win.
func PBest(r sim.Result) []float64 {
	return winShares(r, func(c sim.CarResult) []float64 { return c.NetCost })
}

// PCheapest is like PBest, but on ownership cost alone (preferences ignored).
func PCheapest(r sim.Result) []float64 {
	return winShares(r, func(c sim.CarResult) []float64 { return c.OwnershipCost })
}

func winShares(r sim.Result, series func(sim.CarResult) []float64) []float64 {
	n := len(r.Cars)
	wins := make([]float64, n)
	if r.Futures == 0 || n == 0 {
		return wins
	}
	cols := make([][]float64, n)
	for c := range r.Cars {
		cols[c] = series(r.Cars[c])
	}
	for k := range r.Futures {
		best := math.Inf(1)
		ties := 0
		for c := range n {
			switch v := cols[c][k]; {
			case v < best:
				best, ties = v, 1
			case v == best:
				ties++
			}
		}
		share := 1 / float64(ties)
		for c := range n {
			if cols[c][k] == best {
				wins[c] += share
			}
		}
	}
	for c := range wins {
		wins[c] /= float64(r.Futures)
	}
	return wins
}

// Summarize computes the full per-car summary of a run.
func Summarize(r sim.Result) Summary {
	pBest, pCheap := PBest(r), PCheapest(r)
	s := Summary{Futures: r.Futures, Cars: make([]CarSummary, len(r.Cars))}
	for c, car := range r.Cars {
		cs := CarSummary{
			ID:            car.ID,
			PBest:         pBest[c],
			PCheapest:     pCheap[c],
			NetCost:       percentiles(car.NetCost),
			OwnershipCost: percentiles(car.OwnershipCost),
			MeanNetCost:   mean(car.NetCost),
			Breakdown:     make([]CategoryMedian, 0, sim.NumCategories),
		}
		for cat := range sim.NumCategories {
			cs.Breakdown = append(cs.Breakdown, CategoryMedian{Category: cat.String(), Median: Percentile(sortedCopy(car.Categories[cat]), 0.5)})
		}
		s.Cars[c] = cs
	}
	s.Leader = Leader(pBest)
	s.Entropy = Entropy(pBest)
	return s
}

// Leader returns the index of the largest P(best); ties go to the lowest index,
// so the answer is deterministic.
func Leader(p []float64) int {
	best := 0
	for i := 1; i < len(p); i++ {
		if p[i] > p[best] {
			best = i
		}
	}
	return best
}

// TopTwoGap returns P(best) of the leader minus that of the runner-up.
func TopTwoGap(p []float64) float64 {
	if len(p) < 2 {
		return 1
	}
	sorted := slices.Clone(p)
	slices.Sort(sorted)
	return sorted[len(sorted)-1] - sorted[len(sorted)-2]
}

// Entropy is the Shannon entropy of a winner distribution, in bits:
// H = −Σ p·log₂(p). 0 means one car always wins; log₂(n) means every car wins
// equally often. The interview's information-gain threshold (0.02) is in bits.
func Entropy(p []float64) float64 {
	h := 0.0
	for _, x := range p {
		if x > 0 {
			h -= x * math.Log2(x)
		}
	}
	return h
}

// Percentile returns the q-quantile (0 ≤ q ≤ 1) of an ascending-sorted sample,
// with linear interpolation between order statistics (the common "type 7"
// definition, as in NumPy's default and Excel's PERCENTILE.INC).
func Percentile(sorted []float64, q float64) float64 {
	n := len(sorted)
	switch n {
	case 0:
		return math.NaN()
	case 1:
		return sorted[0]
	}
	h := q * float64(n-1)
	lo := int(math.Floor(h))
	if lo >= n-1 {
		return sorted[n-1]
	}
	return sorted[lo] + (h-float64(lo))*(sorted[lo+1]-sorted[lo])
}

func percentiles(xs []float64) Percentiles {
	s := sortedCopy(xs)
	return Percentiles{P10: Percentile(s, 0.1), P50: Percentile(s, 0.5), P90: Percentile(s, 0.9)}
}

func sortedCopy(xs []float64) []float64 {
	s := slices.Clone(xs)
	slices.Sort(s)
	return s
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}
