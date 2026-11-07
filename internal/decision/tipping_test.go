package decision

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/xoBerlierxo/TieBreaker/internal/sim"
)

func quick() QuickOptions { return QuickOptions{Futures: 1000, ChunkSize: 1000, Seed: 3} }

func TestClassify_Table(t *testing.T) {
	th := Thresholds{WinPBest: 0.55, WinGap: 0.15}
	tests := []struct {
		name   string
		p      []float64
		kind   Kind
		reason string
	}{
		{"leader above 0.55", []float64{0.6, 0.4}, KindWinner, "p_best_threshold"},
		{"exactly 0.55", []float64{0.55, 0.45}, KindWinner, "p_best_threshold"},
		{"gap rule with four cars", []float64{0.40, 0.25, 0.20, 0.15}, KindWinner, "gap_threshold"},
		{"too close", []float64{0.40, 0.35, 0.25}, KindDeadlock, "too_close"},
		{"identical twins", []float64{0.5, 0.5}, KindDeadlock, "too_close"},
		{"single car", []float64{1}, KindWinner, "only_car"},
	}
	for _, tt := range tests {
		c := Classify(tt.p, th)
		if c.Kind != tt.kind || c.Reason != tt.reason {
			t.Errorf("%s: Classify(%v) = %s/%s, want %s/%s", tt.name, tt.p, c.Kind, c.Reason, tt.kind, tt.reason)
		}
	}
}

func TestSanity_IdenticalCarsDeadlock(t *testing.T) {
	in := sanityInputs()
	a, b := in.Cars[0], in.Cars[0]
	a.ID, b.ID = "twin-a", "twin-b"
	in.Cars = []sim.Car{a, b}
	res, err := sim.Run(context.Background(), in, sim.Options{Futures: 20_000, ChunkSize: 1000, MasterSeed: 4})
	if err != nil {
		t.Fatal(err)
	}
	if c := Classify(PBest(res), Thresholds{WinPBest: 0.55, WinGap: 0.15}); c.Kind != KindDeadlock {
		t.Errorf("identical cars classified %s (%+v), want deadlock", c.Kind, c)
	}
}

func TestSanity_DominantCarIsWinnerWithPBestNear1(t *testing.T) {
	in := sanityInputs()
	worse, better := in.Cars[0], in.Cars[0]
	worse.ID, better.ID = "worse", "better"
	better.UpfrontINR = 550_000
	in.Cars = []sim.Car{worse, better}
	res, err := sim.Run(context.Background(), in, sim.Options{Futures: 20_000, ChunkSize: 1000, MasterSeed: 5})
	if err != nil {
		t.Fatal(err)
	}
	c := Classify(PBest(res), Thresholds{WinPBest: 0.55, WinGap: 0.15})
	// Cheaper on price alone (no preferences): rare extreme repair bills can
	// flip a handful of futures, so "≈ 1" here means ≥ 0.99. The stricter
	// cheaper-AND-better case is TestSanity_DominantCarWinsWithPBestNear1.
	if c.Kind != KindWinner || c.Leader != 1 || c.LeaderPBest < 0.99 {
		t.Errorf("classification = %+v, want car 1 winning with P(best) ≈ 1", c)
	}
}

// kmCrossoverInputs: a deterministic petrol car that is cheaper to buy and an
// EV that is cheaper to run. With all randomness off, the break-even distance
// can be solved by hand, so the bisection result can be checked.
func kmCrossoverInputs() sim.Inputs {
	in := sanityInputs()
	fixed := sim.Range{Lo: 0.8, Hi: 0.8}
	in.World.PetrolTrend = sim.PriceTrend{}
	in.World.KmYearSigma, in.World.ResaleMarketSigma, in.World.ResaleIdioSigma = 0, 0, 0
	in.World.ChargingEfficiency, in.World.HomeTariffINR = 1, 10
	in.World.ServiceMinVisits, in.World.GeneralInflation = 0, 0
	petrol := sim.Car{ID: "petrol", Fuel: sim.FuelPetrol, UpfrontINR: 600_000, ValueBasisINR: 600_000,
		ClaimedEfficiency: 20, EfficiencyFactor: sim.EfficiencyFactor{City: fixed, Highway: fixed},
		ServiceIntervalKm: 10_000, RepairGradeMultiplier: 1, ResaleCurve: []float64{0.5}}
	ev := petrol
	ev.ID, ev.Fuel, ev.UpfrontINR, ev.ValueBasisINR, ev.ClaimedEfficiency = "ev", sim.FuelEV, 700_000, 700_000, 8
	in.Cars = []sim.Car{petrol, ev}
	in.Usage = sim.Usage{KmPerMonth: sim.Range{Lo: 1000, Hi: 1000}, CityShare: sim.Range{Lo: 0.5, Hi: 0.5}, HomeCharging: true}
	in.HorizonYears, in.DiscountRate = 1, 0
	return in
}

func TestTipping_FindsKmCrossoverWithinTolerance(t *testing.T) {
	in := kmCrossoverInputs()
	// Per km: petrol = ₹100 / (20 × 0.8) = ₹6.25; EV = ₹10 / (8 × 0.8) = ₹1.5625.
	// Net purchase difference after 1 year: EV costs (7L − 6L) × (1 − 0.5) = ₹50,000 more.
	// Break-even: 50,000 / (6.25 − 1.5625) = 10,667 km/year = 888.9 km/month.
	sweeps := BuildSweeps(in, SweepConfig{FuelTrendLo: 0, FuelTrendHi: 0.01, PrefCap: 800_000})
	tp, err := FindTippingPoints(context.Background(), in, sweeps[:1], quick(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tp.Points) != 1 {
		t.Fatalf("points = %+v, want one km crossover", tp.Points)
	}
	p := tp.Points[0]
	if p.Input != "km_per_month" || p.Below != 0 || p.Above != 1 {
		t.Errorf("crossover = %+v, want petrol below and EV above", p)
	}
	if math.Abs(p.Crossover-888.9) > 10 {
		t.Errorf("crossover = %.1f km/month, want ≈ 888.9", p.Crossover)
	}
	if !p.InsideUserRange {
		t.Error("888.9 km/month is near the user's 1,000 km/month answer; should count as relevant")
	}
}

func TestTipping_NoLeaderChangeMeansGenuinelyClose(t *testing.T) {
	in := kmCrossoverInputs()
	in.Cars[1] = in.Cars[0]
	in.Cars[1].ID = "twin" // identical cars: every grid point is a tie, the lower index leads
	tp, err := FindTippingPoints(context.Background(), in, BuildSweeps(in, SweepConfig{FuelTrendLo: 0, FuelTrendHi: 0.1, PrefCap: 800_000}), quick(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !tp.GenuinelyClose || len(tp.Points) != 0 {
		t.Errorf("tipping = %+v, want genuinely close", tp)
	}
}

func TestTipping_HomeChargingSweptOnlyWithEV(t *testing.T) {
	in := kmCrossoverInputs()
	has := func(sw []Sweep, key string) bool {
		for _, s := range sw {
			if s.Key == key {
				return true
			}
		}
		return false
	}
	if !has(BuildSweeps(in, SweepConfig{}), "home_charging") {
		t.Error("EV present but home charging not swept")
	}
	in.Cars[1].Fuel = sim.FuelDiesel
	if has(BuildSweeps(in, SweepConfig{}), "home_charging") {
		t.Error("no EV but home charging swept")
	}
}

func TestTipping_YearsBisectionOnIntegers(t *testing.T) {
	in := kmCrossoverInputs()
	in.HorizonYears = 5
	in.Cars[0].ResaleCurve = []float64{0.8, 0.6, 0.45, 0.35, 0.3, 0.25, 0.2, 0.15, 0.1}
	in.Cars[1].ResaleCurve = in.Cars[0].ResaleCurve
	in.Usage.KmPerMonth = sim.Range{Lo: 700, Hi: 700}
	sweeps := BuildSweeps(in, SweepConfig{FuelTrendLo: 0, FuelTrendHi: 0.01})
	tp, err := FindTippingPoints(context.Background(), in, sweeps[1:2], quick(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range tp.Points {
		if p.Crossover != math.Round(p.Crossover) {
			t.Errorf("years crossover %.2f is not a whole number", p.Crossover)
		}
	}
}

func TestTipping_PrefSweepDoesNotModifyBase(t *testing.T) {
	in := kmCrossoverInputs()
	in.Prefs = []sim.PrefAttribute{{Key: "safety", Shares: []float64{0, 1}, Brackets: []sim.Range{{Lo: 50_000, Hi: 100_000}}}}
	sweeps := BuildSweeps(in, SweepConfig{PrefCap: 800_000})
	last := sweeps[len(sweeps)-1]
	_ = last.Apply(in, 400_000)
	if in.Prefs[0].Brackets[0].Lo != 50_000 {
		t.Fatal("Apply modified the base inputs' preference slice")
	}
}

func TestTipping_HonoursContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	in := kmCrossoverInputs()
	_, err := FindTippingPoints(ctx, in, BuildSweeps(in, SweepConfig{FuelTrendHi: 0.1, PrefCap: 1}), quick(), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestGridValues_IntegerDeduplicated(t *testing.T) {
	xs := gridValues(Sweep{Lo: 1, Hi: 4, Integer: true})
	for i := 1; i < len(xs); i++ {
		if xs[i] <= xs[i-1] {
			t.Fatalf("grid %v not strictly increasing", xs)
		}
	}
	if len(xs) != 4 {
		t.Errorf("grid %v, want [1 2 3 4]", xs)
	}
}
