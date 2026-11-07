package sim

import (
	"testing"
)

// The three "common-sense" properties from SPEC §15. Because every run uses
// the same seed and common random numbers, the futures being compared are
// identical except for the one input that changes, so the checks hold future
// by future, not just on average.

func TestProperty_MoreKmNeverLowersEnergyCost(t *testing.T) {
	kms := []float64{300, 800, 1500, 3000}
	var prev Result
	for i, km := range kms {
		in := stochasticInputs()
		in.Usage.KmPerMonth = Range{km, km}
		res := runOrFail(t, in, opts(3000, 4, 21))
		if i > 0 {
			for c := range res.Cars {
				for k := range res.Futures {
					if res.Cars[c].Categories[CatEnergy][k] < prev.Cars[c].Categories[CatEnergy][k] {
						t.Fatalf("car %s future %d: energy fell from %.2f to %.2f when km/month rose to %.0f",
							res.Cars[c].ID, k, prev.Cars[c].Categories[CatEnergy][k], res.Cars[c].Categories[CatEnergy][k], km)
					}
				}
			}
		}
		prev = res
	}
}

func TestProperty_HigherFuelPathNeverImprovesPetrolVsEV(t *testing.T) {
	gap := func(mu float64) []float64 {
		in := stochasticInputs()
		in.Cars = in.Cars[:2] // petrol, ev
		in.Prefs = nil
		in.World.PetrolTrend.Mu = mu
		res := runOrFail(t, in, opts(3000, 4, 33))
		out := make([]float64, res.Futures)
		for k := range out {
			out[k] = res.Cars[0].NetCost[k] - res.Cars[1].NetCost[k] // petrol minus EV
		}
		return out
	}
	low, high := gap(0.0), gap(0.10)
	for k := range low {
		if high[k] < low[k] {
			t.Fatalf("future %d: a higher petrol price path improved petrol vs EV (gap %.2f → %.2f)", k, low[k], high[k])
		}
	}
}

func TestProperty_MoreYearsNeverLowersTotalEnergyCost(t *testing.T) {
	var prev Result
	for i, years := range []int{1, 3, 5, 8, 15} {
		in := stochasticInputs()
		in.HorizonYears = years
		res := runOrFail(t, in, opts(2000, 4, 44))
		if i > 0 {
			for c := range res.Cars {
				for k := range res.Futures {
					if res.Cars[c].Categories[CatEnergy][k] < prev.Cars[c].Categories[CatEnergy][k] {
						t.Fatalf("car %s future %d: energy fell when the horizon grew to %d years", res.Cars[c].ID, k, years)
					}
				}
			}
		}
		prev = res
	}
}
