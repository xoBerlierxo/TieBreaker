package interview

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
	"github.com/xoBerlierxo/TieBreaker/internal/sim"
)

// baseInputs builds fictional engine inputs for n petrol cars whose upfront
// prices are given; everything else is identical.
func baseInputs(upfronts ...float64) sim.Inputs {
	f := sim.Range{Lo: 0.7, Hi: 0.9}
	var cars []sim.Car
	for i, u := range upfronts {
		cars = append(cars, sim.Car{
			ID: string(rune('a' + i)), Fuel: sim.FuelPetrol, UpfrontINR: u, ValueBasisINR: u * 0.9,
			ClaimedEfficiency: 18, EfficiencyFactor: sim.EfficiencyFactor{City: f, Highway: f},
			ServiceIntervalKm: 10_000, ServiceCostPerVisit: 4_000, TyreSetCostINR: 16_000, InsuranceRate: 0.03,
			RepairBaseRate: 0.3, RepairGradeMultiplier: 1, ResaleCurve: []float64{0.85, 0.75, 0.67, 0.6, 0.54},
		})
	}
	return sim.Inputs{
		Cars:  cars,
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

func manualAttr(key string, shares ...float64) Attribute {
	best, worst := 0, 0
	for i, s := range shares {
		if s > shares[best] {
			best = i
		}
		if s < shares[worst] {
			worst = i
		}
	}
	return Attribute{Key: key, Label: key, Values: shares, Shares: shares, Best: best, Worst: worst}
}

func testPlanner(cache Cache) Planner {
	return Planner{
		Cfg: Config{MaxQuestions: 10, StopPBest: 0.65, MinInfoGain: 0.02, Ladder: defaultLadder()},
		Sim: QuickSimulator{Futures: 2000, ChunkSize: 1000, Cache: cache},
	}
}

// selectingPlanner never stops because of a confident leader, so tests can
// exercise question selection with clearly separated cars.
func selectingPlanner(cache Cache) Planner {
	p := testPlanner(cache)
	p.Cfg.StopPBest = 0.99
	return p
}

var names2 = []string{"Car A", "Car B"}

func TestPlanner_PicksAttributeThatCanFlipWinner(t *testing.T) {
	// Car A is ₹80k cheaper. Valuing A's advantage only reinforces A; valuing
	// B's advantage might flip the result, so that question is more informative.
	base := baseInputs(700_000, 780_000)
	attrs := []Attribute{manualAttr("reinforce", 1, 0), manualAttr("flip", 0, 1)}

	next, err := selectingPlanner(nil).NextTradeoff(context.Background(), base, attrs, names2, NewTradeoffState(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if next.Done || next.Question == nil || next.Question.Attribute != "flip" {
		t.Fatalf("next = %+v (gains %+v), want the 'flip' question", next, next.Gains)
	}
	if next.Question.ThresholdINR != 100_000 || next.Question.Step != 1 || next.Question.ID != "tradeoff:flip:1" {
		t.Errorf("question = %+v", next.Question)
	}
}

func TestInfoGain_NonNegativeUnderCRN(t *testing.T) {
	base := baseInputs(700_000, 720_000, 760_000)
	attrs := []Attribute{manualAttr("x", 0, 1, 0.5), manualAttr("y", 1, 0, 0.2), manualAttr("z", 0.3, 0, 1)}
	p := testPlanner(nil)
	gains, err := p.informationGains(context.Background(), base, attrs, 99)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range gains {
		if g.Bits < -0.01 {
			t.Errorf("gain for %s = %.4f bits, want ≥ 0 (up to Monte Carlo noise)", g.Attribute, g.Bits)
		}
	}
}

func TestStop_LeaderAboveThreshold(t *testing.T) {
	base := baseInputs(500_000, 900_000) // A is far cheaper
	next, err := testPlanner(nil).NextTradeoff(context.Background(), base, []Attribute{manualAttr("x", 0, 1)}, names2, NewTradeoffState(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !next.Done || next.Reason != ReasonLeaderSure {
		t.Fatalf("next = %+v, want done/leader_confident", next)
	}
}

func TestStop_MaxQuestionsCountsLadderSteps(t *testing.T) {
	s := NewTradeoffState()
	l := defaultLadder()
	for _, attr := range []string{"a", "b", "c", "d"} {
		st := l.Start()
		for step := 1; !l.Done(st) && s.Asked() < 10; step++ {
			thr, _ := l.NextThreshold(st)
			_ = s.Add(TradeoffStep{Attribute: attr, Step: step, ThresholdINR: thr, Yes: true})
			st = l.Apply(st, true)
		}
	}
	if s.Asked() != 10 {
		t.Fatalf("asked = %d, want 10", s.Asked())
	}
	next, _ := testPlanner(nil).NextTradeoff(context.Background(), baseInputs(700_000, 700_000), nil, names2, s, 1)
	if !next.Done || next.Reason != ReasonMaxQuestions {
		t.Fatalf("next = %+v, want max_questions", next)
	}
}

func TestStop_NoCandidates(t *testing.T) {
	next, err := testPlanner(nil).NextTradeoff(context.Background(), baseInputs(700_000, 700_000), nil, names2, NewTradeoffState(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !next.Done || next.Reason != ReasonNoCandidates {
		t.Fatalf("next = %+v, want no_candidates", next)
	}
}

func TestStop_LowInfoGain(t *testing.T) {
	// An "attribute" that gives both cars the same share can't change anything.
	next, err := testPlanner(nil).NextTradeoff(context.Background(), baseInputs(700_000, 700_000),
		[]Attribute{manualAttr("nothing", 1, 1)}, names2, NewTradeoffState(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !next.Done || next.Reason != ReasonLowInfoGain {
		t.Fatalf("next = %+v, want low_information_gain", next)
	}
}

func TestStop_SingleCar(t *testing.T) {
	next, _ := testPlanner(nil).NextTradeoff(context.Background(), baseInputs(700_000), nil, []string{"A"}, NewTradeoffState(), 1)
	if !next.Done || next.Reason != ReasonSingleCar {
		t.Fatalf("next = %+v, want single_car", next)
	}
}

func TestPlanner_ContinuesLadderInProgress(t *testing.T) {
	s := NewTradeoffState()
	_ = s.Add(TradeoffStep{Attribute: "flip", Step: 1, ThresholdINR: 100_000, Yes: false})
	attrs := []Attribute{manualAttr("reinforce", 1, 0), manualAttr("flip", 0, 1)}
	next, err := selectingPlanner(nil).NextTradeoff(context.Background(), baseInputs(700_000, 720_000), attrs, names2, s, 1)
	if err != nil {
		t.Fatal(err)
	}
	if next.Done || next.Question.ID != "tradeoff:flip:2" || next.Question.ThresholdINR != 25_000 {
		t.Fatalf("next = %+v, want step 2 of flip at ₹25,000", next.Question)
	}
}

func TestSelector_TimeoutUsesFallbackOrder(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	attrs := []Attribute{manualAttr("first", 0, 1), manualAttr("second", 1, 0)}
	next, err := testPlanner(nil).NextTradeoff(ctx, baseInputs(700_000, 710_000), attrs, names2, NewTradeoffState(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !next.FellBack || next.Question == nil || next.Question.Attribute != "first" {
		t.Fatalf("next = %+v, want fallback to the first candidate", next)
	}
}

// countingCache is an in-memory cache that counts hits and stores.
type countingCache struct {
	mu         sync.Mutex
	m          map[string][]float64
	hits, sets int
}

func (c *countingCache) Get(_ context.Context, k string) ([]float64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[k]
	if ok {
		c.hits++
	}
	return v, ok
}

func (c *countingCache) Set(_ context.Context, k string, v []float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[k] = v
	c.sets++
}

func TestSelector_CacheHitSkipsSimulation(t *testing.T) {
	cache := &countingCache{m: map[string][]float64{}}
	p := selectingPlanner(cache)
	attrs := []Attribute{manualAttr("reinforce", 1, 0), manualAttr("flip", 0, 1)}
	base := baseInputs(700_000, 760_000)

	first, err := p.NextTradeoff(context.Background(), base, attrs, names2, NewTradeoffState(), 5)
	if err != nil {
		t.Fatal(err)
	}
	setsAfterFirst := cache.sets
	second, err := p.NextTradeoff(context.Background(), base, attrs, names2, NewTradeoffState(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if cache.sets != setsAfterFirst {
		t.Errorf("second call ran %d new simulations, want 0 (all cached)", cache.sets-setsAfterFirst)
	}
	if setsAfterFirst != 1+2*7 {
		t.Errorf("first call ran %d simulations, want 15 (1 current + 2 candidates × 7)", setsAfterFirst)
	}
	if first.Question == nil || second.Question == nil || first.Question.Attribute != second.Question.Attribute {
		t.Error("cached answer differs from the computed one")
	}
}

func TestValidateTradeoffAnswer_Rules(t *testing.T) {
	l := defaultLadder()
	attrs := []Attribute{manualAttr("safety", 0, 1), manualAttr("feature:esc", 0, 1), manualAttr("feature:sunroof", 1, 0), manualAttr("feature:adas", 0, 1)}
	yes := json.RawMessage(`{"yes": true}`)

	// A fresh step 1 of a candidate is accepted, with the ₹1L threshold.
	step, err := ValidateTradeoffAnswer(attrs, NewTradeoffState(), l, 10, "tradeoff:safety:1", yes)
	if err != nil || step.ThresholdINR != 100_000 {
		t.Fatalf("step = %+v, err = %v", step, err)
	}

	s := NewTradeoffState()
	_ = s.Add(TradeoffStep{Attribute: "safety", Step: 1, ThresholdINR: 100_000, Yes: true})
	tests := []struct {
		name string
		st   TradeoffState
		max  int
		id   string
		body string
		kind error
	}{
		{"not a difference between the cars", NewTradeoffState(), 10, "tradeoff:boot:1", `{"yes": true}`, apperr.ErrValidation},
		{"switching mid-ladder", s, 10, "tradeoff:feature:esc:1", `{"yes": true}`, apperr.ErrIllegalTransition},
		{"skipping a step", s, 10, "tradeoff:safety:3", `{"yes": true}`, apperr.ErrIllegalTransition},
		{"limit reached", s, 1, "tradeoff:safety:2", `{"yes": true}`, apperr.ErrIllegalTransition},
		{"bad body", NewTradeoffState(), 10, "tradeoff:safety:1", `{"maybe": 1}`, apperr.ErrValidation},
		{"malformed id", NewTradeoffState(), 10, "tradeoff:safety", `{"yes": true}`, apperr.ErrValidation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidateTradeoffAnswer(attrs, tt.st, l, tt.max, tt.id, json.RawMessage(tt.body))
			if !errors.Is(err, tt.kind) {
				t.Errorf("err = %v, want %v", err, tt.kind)
			}
		})
	}

	// Feature cap: after two feature questions a third is refused.
	f := NewTradeoffState()
	for _, attr := range []string{"feature:esc", "feature:sunroof"} {
		st := l.Start()
		for step := 1; !l.Done(st); step++ {
			_ = f.Add(TradeoffStep{Attribute: attr, Step: step, Yes: false})
			st = l.Apply(st, false)
		}
	}
	if _, err := ValidateTradeoffAnswer(attrs, f, l, 10, "tradeoff:feature:adas:1", yes); !errors.Is(err, apperr.ErrIllegalTransition) {
		t.Errorf("third feature question: err = %v, want illegal transition", err)
	}
}

func TestParseTradeoffQuestionID(t *testing.T) {
	attr, step, ok := ParseTradeoffQuestionID("tradeoff:feature:esc:2")
	if !ok || attr != "feature:esc" || step != 2 {
		t.Fatalf("got %q %d %v", attr, step, ok)
	}
	for _, bad := range []string{"usage:km_per_month", "tradeoff:safety", "tradeoff:safety:0", "tradeoff::1"} {
		if _, _, ok := ParseTradeoffQuestionID(bad); ok {
			t.Errorf("%q parsed as valid", bad)
		}
	}
}
