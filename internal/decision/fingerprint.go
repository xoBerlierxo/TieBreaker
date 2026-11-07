package decision

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"slices"

	"github.com/xoBerlierxo/TieBreaker/internal/sim"
)

// Fingerprint is a SHA-256 over every sample of a result, bit for bit. Two
// runs with the same inputs and seed must produce the same fingerprint, which
// is how a stored run is checked for exact reproduction (SPEC §6.7).
func Fingerprint(r sim.Result) string {
	h := sha256.New()
	var buf [8]byte
	for _, c := range r.Cars {
		h.Write([]byte(c.ID))
		for _, s := range [][]float64{c.NetCost, c.OwnershipCost, c.PrefValue} {
			for _, x := range s {
				binary.LittleEndian.PutUint64(buf[:], math.Float64bits(x))
				h.Write(buf[:])
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Histogram is one car's net-cost distribution over shared bin edges, so the
// results page can draw every car on the same axis.
type Histogram struct {
	Edges  []float64 `json:"edges_inr"` // len(Counts)+1, ascending
	Counts []int     `json:"counts"`
}

// Histograms bins every car's net cost into `bins` equal-width bins spanning
// the 1st to 99th percentile of all cars together (so a few extreme futures
// don't squash the chart). Samples outside go into the end bins.
func Histograms(r sim.Result, bins int) []Histogram {
	var all []float64
	for _, c := range r.Cars {
		all = append(all, c.NetCost...)
	}
	out := make([]Histogram, len(r.Cars))
	if len(all) == 0 || bins < 1 {
		return out
	}
	slices.Sort(all)
	lo, hi := Percentile(all, 0.01), Percentile(all, 0.99)
	if hi <= lo {
		hi = lo + 1
	}
	width := (hi - lo) / float64(bins)
	edges := make([]float64, bins+1)
	for i := range edges {
		edges[i] = lo + float64(i)*width
	}
	for ci, c := range r.Cars {
		counts := make([]int, bins)
		for _, x := range c.NetCost {
			b := int((x - lo) / width)
			counts[min(max(b, 0), bins-1)]++
		}
		out[ci] = Histogram{Edges: edges, Counts: counts}
	}
	return out
}
