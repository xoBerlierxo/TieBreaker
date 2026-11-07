package decision

import (
	"context"
	"math"
	"testing"

	"github.com/xoBerlierxo/TieBreaker/internal/sim"
)

func near(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %.6f, want %.6f (±%g)", name, got, want, tol)
	}
}

func resultFrom(net ...[]float64) sim.Result {
	r := sim.Result{Futures: len(net[0])}
	for i, n := range net {
		r.Cars = append(r.Cars, sim.CarResult{ID: string(rune('A' + i)), NetCost: n, OwnershipCost: n})
	}
	return r
}

func TestPercentile_Type7KnownValues(t *testing.T) {
	s := []float64{1, 2, 3, 4, 5}
	tests := []struct{ q, want float64 }{{0, 1}, {0.5, 3}, {0.1, 1.4}, {0.9, 4.6}, {1, 5}, {0.25, 2}}
	for _, tt := range tests {
		near(t, "Percentile", Percentile(s, tt.q), tt.want, 1e-12)
	}
	near(t, "single value", Percentile([]float64{7}, 0.9), 7, 0)
	if !math.IsNaN(Percentile(nil, 0.5)) {
		t.Error("empty sample should give NaN")
	}
}

func TestPBest_TiesSplitEvenly(t *testing.T) {
	// Future 1: A wins. Future 2: A and B tie. Future 3: C wins. Future 4: all tie.
	r := resultFrom(
		[]float64{1, 5, 9, 4},
		[]float64{2, 5, 9, 4},
		[]float64{3, 6, 1, 4},
	)
	p := PBest(r)
	near(t, "A", p[0], (1+0.5+0+1.0/3)/4, 1e-12)
	near(t, "B", p[1], (0+0.5+0+1.0/3)/4, 1e-12)
	near(t, "C", p[2], (0+0+1+1.0/3)/4, 1e-12)
}

func TestPBest_SumsToOne(t *testing.T) {
	r := resultFrom([]float64{3, 1, 2, 2}, []float64{1, 1, 3, 2}, []float64{2, 4, 1, 2})
	sum := 0.0
	for _, x := range PBest(r) {
		sum += x
	}
	near(t, "sum", sum, 1, 1e-12)
}

func TestEntropy_UniformAndCertain(t *testing.T) {
	near(t, "certain", Entropy([]float64{1, 0, 0}), 0, 1e-12)
	near(t, "two equal", Entropy([]float64{0.5, 0.5}), 1, 1e-12)
	near(t, "four equal", Entropy([]float64{0.25, 0.25, 0.25, 0.25}), 2, 1e-12)
}

func TestLeaderAndGap(t *testing.T) {
	p := []float64{0.2, 0.5, 0.3}
	if Leader(p) != 1 {
		t.Errorf("Leader = %d, want 1", Leader(p))
	}
	near(t, "gap", TopTwoGap(p), 0.2, 1e-12)
	if Leader([]float64{0.4, 0.4, 0.2}) != 0 {
		t.Error("ties must go to the lowest index")
	}
}

func TestSummarize_PercentilesAndBreakdown(t *testing.T) {
	r := resultFrom([]float64{10, 20, 30, 40, 50})
	for cat := range sim.NumCategories {
		r.Cars[0].Categories[cat] = []float64{1, 2, 3, 4, 5}
	}
	s := Summarize(r)
	near(t, "p50", s.Cars[0].NetCost.P50, 30, 1e-12)
	near(t, "p10", s.Cars[0].NetCost.P10, 14, 1e-12)
	if len(s.Cars[0].Breakdown) != int(sim.NumCategories) || s.Cars[0].Breakdown[0].Category != "upfront" {
		t.Errorf("breakdown = %+v", s.Cars[0].Breakdown)
	}
	near(t, "P(best) single car", s.Cars[0].PBest, 1, 0)
}

// --- SPEC §15 deadlock sanity, using the real engine ----------------------

func sanityInputs() sim.Inputs {
	fixed := sim.Range{Lo: 0.7, Hi: 0.9}
	car := sim.Car{
		ID: "a", Fuel: sim.FuelPetrol, UpfrontINR: 700_000, ValueBasisINR: 600_000,
		ClaimedEfficiency: 18, EfficiencyFactor: sim.EfficiencyFactor{City: fixed, Highway: fixed},
		ServiceIntervalKm: 10_000, ServiceCostPerVisit: 4_000, TyreSetCostINR: 16_000, InsuranceRate: 0.03,
		RepairBaseRate: 0.3, RepairGradeMultiplier: 1, ResaleCurve: []float64{0.85, 0.75, 0.67, 0.6, 0.54},
	}
	return sim.Inputs{
		Cars:  []sim.Car{car},
		Usage: sim.Usage{KmPerMonth: sim.Range{Lo: 800, Hi: 1200}, CityShare: sim.Range{Lo: 0.4, Hi: 0.7}},
		World: sim.World{
			PetrolPriceINR: 100, DieselPriceINR: 90, CNGPriceINR: 80, HomeTariffINR: 7, PublicTariffINR: 18,
			PetrolTrend: sim.PriceTrend{Mu: 0.03, Sigma: 0.05}, KmYearSigma: 0.1, ChargingEfficiency: 0.9,
			IDVSchedule: []float64{0.95, 0.85, 0.8, 0.7, 0.6}, ServiceMinVisits: 1, GeneralInflation: 0.05,
			RepairAgeSlope: 0.15, RepairOdoSlopePer100k: 0.5, RepairUsedMultiplier: 1.2,
			RepairCostMu: math.Log(8000), RepairCostSigma: 0.8, TyreLifeKm: 40_000,
			ResaleMarketSigma: 0.08, ResaleIdioSigma: 0.1,
		},
		HorizonYears: 5, DiscountRate: 0.07,
	}
}

func TestSanity_DominantCarWinsWithPBestNear1(t *testing.T) {
	in := sanityInputs()
	better := in.Cars[0]
	worse := in.Cars[0]
	better.ID, worse.ID = "better", "worse"
	better.UpfrontINR = 600_000 // cheaper…
	in.Cars = []sim.Car{worse, better}
	// …and better on every attribute the buyer priced.
	in.Prefs = []sim.PrefAttribute{
		{Key: "safety", Shares: []float64{0, 1}, Brackets: []sim.Range{{Lo: 50_000, Hi: 100_000}}},
		{Key: "boot", Shares: []float64{0, 1}, Brackets: []sim.Range{{Lo: 25_000, Hi: 50_000}}},
	}
	res, err := sim.Run(context.Background(), in, sim.Options{Futures: 20_000, ChunkSize: 1000, MasterSeed: 1})
	if err != nil {
		t.Fatal(err)
	}
	p := PBest(res)
	if p[1] < 0.999 {
		t.Errorf("dominant car P(best) = %.4f, want ≈ 1", p[1])
	}
}

func TestSanity_IdenticalCarsSplitPBest(t *testing.T) {
	in := sanityInputs()
	a, b := in.Cars[0], in.Cars[0]
	a.ID, b.ID = "twin-a", "twin-b"
	in.Cars = []sim.Car{a, b}
	res, err := sim.Run(context.Background(), in, sim.Options{Futures: 20_000, ChunkSize: 1000, MasterSeed: 2})
	if err != nil {
		t.Fatal(err)
	}
	p := PBest(res)
	if math.Abs(p[0]-0.5) > 0.03 || math.Abs(p[1]-0.5) > 0.03 {
		t.Errorf("identical cars P(best) = %v, want ≈ [0.5 0.5]", p)
	}
}
