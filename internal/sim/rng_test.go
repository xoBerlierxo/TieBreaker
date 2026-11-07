package sim

import (
	"math"
	"math/rand/v2"
	"testing"
)

func TestDrawWorld_FixedDrawCount(t *testing.T) {
	// The world stream must always draw the same number of values, whatever
	// the inputs, so that the same seed means the same world.
	want := MaxHorizonYears*(int(numEnergies)+1) + 2 + numFuels + numFuels
	s := newStream()
	for seed := range uint64(5) {
		s.reseed(seed, worldStreamKey)
		var w worldDraws
		drawWorld(s, &w)
		if w.numDraws != want {
			t.Fatalf("drew %d values, want %d", w.numDraws, want)
		}
	}
}

func TestStream_ReseedRestartsSequence(t *testing.T) {
	s := newStream()
	s.reseed(7, 99)
	a, b := s.rng.Uint64(), s.rng.Uint64()
	s.reseed(7, 99)
	if s.rng.Uint64() != a || s.rng.Uint64() != b {
		t.Fatal("reseeding with the same values did not restart the sequence")
	}
	s.reseed(7, 100)
	if s.rng.Uint64() == a {
		t.Fatal("a different stream key produced the same first value")
	}
}

func TestCarStream_UnaffectedByAddingAnotherCar(t *testing.T) {
	two := stochasticInputs()
	two.Cars = two.Cars[:2]
	for i := range two.Prefs {
		two.Prefs[i].Shares = two.Prefs[i].Shares[:2]
	}
	three := stochasticInputs()

	a := runOrFail(t, two, opts(3000, 3, 77))
	b := runOrFail(t, three, opts(3000, 3, 77))
	for c := range 2 {
		for k := range a.Futures {
			if a.Cars[c].NetCost[k] != b.Cars[c].NetCost[k] {
				t.Fatalf("car %s future %d changed when a third car was added", a.Cars[c].ID, k)
			}
		}
	}
}

func TestPrefStream_AddingAttributeDoesNotShiftOthers(t *testing.T) {
	base := stochasticInputs()
	more := stochasticInputs()
	// An extra attribute with zero share for everyone adds nothing to any car,
	// so if streams are keyed by attribute, every result is unchanged.
	more.Prefs = append(more.Prefs, PrefAttribute{Key: "boot", Shares: []float64{0, 0, 0}, Brackets: []Range{{0, 100_000}}})

	sameBits(t, runOrFail(t, base, opts(2000, 2, 5)), runOrFail(t, more, opts(2000, 2, 5)), "extra zero-share attribute")
}

func TestPrefDist_SingleBracketWithinBounds(t *testing.T) {
	in := handInputs()
	in.Prefs = []PrefAttribute{{Key: "safety", Shares: []float64{1}, Brackets: []Range{{50_000, 100_000}}}}
	res := runOrFail(t, in, opts(5000, 2, 8))
	for _, v := range res.Cars[0].PrefValue {
		if v < 50_000 || v > 100_000 {
			t.Fatalf("preference value %v outside [50k, 100k]", v)
		}
	}
}

func TestPrefDist_MixturePicksEachBracketEqually(t *testing.T) {
	in := handInputs()
	in.Prefs = []PrefAttribute{{Key: "safety", Shares: []float64{1}, Brackets: []Range{{0, 1}, {10, 11}, {100, 101}}}}
	res := runOrFail(t, in, opts(30_000, 4, 9))
	var counts [3]int
	for _, v := range res.Cars[0].PrefValue {
		switch {
		case v <= 1:
			counts[0]++
		case v >= 10 && v <= 11:
			counts[1]++
		case v >= 100 && v <= 101:
			counts[2]++
		default:
			t.Fatalf("value %v is in no bracket", v)
		}
	}
	for i, n := range counts {
		if share := float64(n) / 30_000; math.Abs(share-1.0/3) > 0.02 {
			t.Errorf("bracket %d picked %.3f of the time, want ≈ 0.333", i, share)
		}
	}
}

func TestPoisson_MeanMatchesLambda(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for _, lambda := range []float64{0, 0.3, 2, 45} {
		sum := 0
		const n = 20_000
		for range n {
			sum += poisson(r, lambda)
		}
		if got := float64(sum) / n; math.Abs(got-lambda) > 0.05*lambda+0.02 {
			t.Errorf("poisson(%v) mean = %.3f", lambda, got)
		}
	}
}
