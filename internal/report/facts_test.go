package report

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xoBerlierxo/TieBreaker/internal/catalog"
	"github.com/xoBerlierxo/TieBreaker/internal/decision"
	"github.com/xoBerlierxo/TieBreaker/internal/interview"
	"github.com/xoBerlierxo/TieBreaker/internal/session"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/")

func ptr[T any](v T) *T { return &v }

func withName(v catalog.Variant, make, model string) catalog.Variant {
	v.Make, v.Model = make, model
	return v
}

// fixtureInput is a hand-built two-car report input (fictional cars, made-up
// numbers): car A is cheaper to own, car B is safer and automatic, and the
// buyer priced safety at ₹1L–2L and automatic at ₹25k–50k.
func fixtureInput() Input {
	rated := catalog.Variant{FuelType: catalog.FuelPetrol, Verified: false, SafetyRatingStars: ptr(3), SafetyRatingBody: ptr("Test NCAP")}
	safer := catalog.Variant{FuelType: catalog.FuelEV, Verified: false, SafetyRatingStars: ptr(5), SafetyRatingBody: ptr("Test NCAP")}
	rc := session.RunContext{
		UserState: "TN", HorizonYears: 5, PlaceholderCnt: 59,
		Cars: []session.RunCar{
			{ItemID: "item-a", Label: "Test Car A", Condition: session.ConditionNew, Variant: withName(rated, "Test", "Car A")},
			{ItemID: "item-b", Label: "Test Car B", Condition: session.ConditionNew, Variant: withName(safer, "Test", "Car B")},
		},
		Attributes: []interview.Attribute{
			{Key: "safety", Label: "safety rating", Shares: []float64{0, 1}, Best: 1, Worst: 0, ValueLabels: []string{"3★", "5★"}},
			{Key: "automatic", Label: "automatic gearbox", Shares: []float64{0, 1}, Best: 1, Worst: 0, ValueLabels: []string{"a manual gearbox", "an automatic gearbox"}},
			{Key: "boot", Label: "boot space", Shares: []float64{1, 0}, Best: 0, Worst: 1, ValueLabels: []string{"350 L of boot space", "300 L of boot space"}},
		},
		Tradeoffs: []session.TradeoffView{
			{Attribute: "safety", Steps: 2, Done: true, Bracket: interview.Bracket{Min: 100_000, Max: 200_000}},
			{Attribute: "automatic", Steps: 3, Done: true, Bracket: interview.Bracket{Min: 25_000, Max: 50_000}},
		},
		Usage:    interview.Usage{KmPerMonth: [2]float64{800, 1200}, CitySharePct: [2]float64{50, 70}, OwnershipYears: 5},
		Warnings: []session.CarWarning{{ItemID: "item-a", Code: "unverified_data", Text: "Test Car A: catalogue data is not verified"}, {Code: "placeholder_assumptions", Text: "59 model assumptions are placeholders, not sourced values"}},
	}
	breakdown := func(energy, resale float64) []decision.CategoryMedian {
		return []decision.CategoryMedian{
			{Category: "upfront", Median: 700_000}, {Category: "financing", Median: 0}, {Category: "energy", Median: energy},
			{Category: "insurance", Median: 80_000}, {Category: "service", Median: 30_000}, {Category: "repairs", Median: 10_000},
			{Category: "tyres", Median: 12_000}, {Category: "resale", Median: resale},
		}
	}
	sum := decision.Summary{Futures: 50_000, Cars: []decision.CarSummary{
		{ID: "item-a", PBest: 0.30, PCheapest: 0.85, NetCost: decision.Percentiles{P10: 600_000, P50: 650_000, P90: 720_000},
			OwnershipCost: decision.Percentiles{P10: 600_000, P50: 650_000, P90: 720_000}, Breakdown: breakdown(250_000, -400_000)},
		{ID: "item-b", PBest: 0.70, PCheapest: 0.15, NetCost: decision.Percentiles{P10: 520_000, P50: 590_000, P90: 660_000},
			OwnershipCost: decision.Percentiles{P10: 680_000, P50: 760_000, P90: 830_000}, Breakdown: breakdown(60_000, -300_000)},
	}}
	return Input{
		Context: rc, Summary: sum,
		Classification: decision.Classify([]float64{0.30, 0.70}, decision.Thresholds{WinPBest: 0.55, WinGap: 0.15}),
		Thresholds:     decision.Thresholds{WinPBest: 0.55, WinGap: 0.15},
		Tipping: decision.Tipping{Points: []decision.TippingPoint{
			{Input: "km_per_month", Crossover: 1_480, Below: 1, Above: 0, InsideUserRange: false},
		}},
		Run: RunFacts{ID: "run-fixture", ModelVersion: "sim-v1", Seed: "42", Futures: 50_000},
	}
}

func TestBuildFacts_ProsConsRankedByUserRupees(t *testing.T) {
	f := BuildFacts(fixtureInput())
	b := f.Cars[1]
	// Car B's pros: safety (midpoint ₹1.5L) beats automatic (₹37.5k); its
	// energy saving (₹1.9L) is the biggest of all.
	if len(b.Pros) != 3 || b.Pros[0].Key != "energy" || b.Pros[1].Key != "safety" || b.Pros[2].Key != "automatic" {
		t.Fatalf("car B pros = %+v, want energy, safety, automatic in that order", b.Pros)
	}
	if b.Pros[1].ValueINR != 150_000 {
		t.Errorf("safety value = %v, want 150000 (share difference 1 × bracket midpoint)", b.Pros[1].ValueINR)
	}
	if b.Cons[0].Key != "resale" {
		t.Errorf("car B's biggest con = %+v, want weaker resale", b.Cons[0])
	}
	a := f.Cars[0]
	if a.Cons[0].Key != "energy" || a.Cons[1].Key != "safety" {
		t.Errorf("car A cons = %+v", a.Cons)
	}
	// Boot differs but wasn't priced: never a pro or con, listed as not asked.
	for _, p := range append(a.Pros, a.Cons...) {
		if p.Key == "boot" {
			t.Error("unpriced 'boot' appears as a pro/con")
		}
	}
	if len(a.NotAsked) != 1 || a.NotAsked[0] != "boot space" {
		t.Errorf("not asked = %v, want [boot space]", a.NotAsked)
	}
}

func TestBuildFacts_SafetyNotRatedNeverZero(t *testing.T) {
	in := fixtureInput()
	in.Context.Cars[0].Variant.SafetyRatingStars, in.Context.Cars[0].Variant.SafetyRatingBody = nil, nil
	f := BuildFacts(in)
	if f.Cars[0].SafetyRating != "not rated" {
		t.Errorf("safety rating = %q, want \"not rated\"", f.Cars[0].SafetyRating)
	}
	if f.Cars[1].SafetyRating != "5★ (Test NCAP)" {
		t.Errorf("safety rating = %q", f.Cars[1].SafetyRating)
	}
}

func TestBuildFacts_WarningsAndWinner(t *testing.T) {
	f := BuildFacts(fixtureInput())
	if f.Kind != decision.KindWinner || f.Winner != "Test Car B" {
		t.Fatalf("kind/winner = %s/%s", f.Kind, f.Winner)
	}
	if len(f.Cars[0].Warnings) != 1 || len(f.Warnings) != 0 || f.Placeholders != 59 {
		t.Errorf("warnings: car %v, report %v, placeholders %d", f.Cars[0].Warnings, f.Warnings, f.Placeholders)
	}
	if len(f.Tipping) != 1 || !strings.Contains(f.Tipping[0].Sentence, "~1,500 km/month") {
		t.Errorf("tipping = %+v", f.Tipping)
	}
	if len(f.Cars[0].Tipping) != 1 || len(f.Cars[1].Tipping) != 1 {
		t.Error("the km tipping point involves both cars")
	}
}

func TestBuildFacts_DeadlockTypes(t *testing.T) {
	in := fixtureInput()
	in.Classification = decision.Classify([]float64{0.48, 0.52}, in.Thresholds)
	if f := BuildFacts(in); f.Kind != decision.KindDeadlock || f.DeadlockType != DeadlockTippingPoint {
		t.Errorf("with a tipping point: %s/%s", f.Kind, f.DeadlockType)
	}
	in.Tipping = decision.Tipping{GenuinelyClose: true}
	if f := BuildFacts(in); f.DeadlockType != DeadlockGenuinelyClose {
		t.Errorf("without tipping points: %s", f.DeadlockType)
	}
}

func TestBuildFacts_NoneFitShowsConstraintAndShortfall(t *testing.T) {
	cars := []session.CarView{
		{Label: "Test Car A", Item: session.Item{Eliminated: true}, Eliminations: []session.Elimination{{Message: "on-road price is ₹1,50,000 over your budget", Shortfall: 150_000}}},
		{Label: "Test Car B", Item: session.Item{Eliminated: true}, Eliminations: []session.Elimination{{Message: "has 5 seats, 2 fewer than you need"}}},
	}
	f := BuildNoneFit("TN", cars)
	if f.Kind != decision.KindNoneFit || len(f.Eliminated) != 2 || !strings.Contains(f.Eliminated[0].Reasons[0], "₹1,50,000") {
		t.Fatalf("facts = %+v", f)
	}
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden file (run with -update): %v", err)
	}
	if got != string(want) {
		t.Errorf("%s differs from %s (run with -update after checking the change)\n--- got ---\n%s", name, path, got)
	}
}

func TestRenderTemplate_GoldenFiles(t *testing.T) {
	winner := BuildFacts(fixtureInput())

	tippingIn := fixtureInput()
	tippingIn.Classification = decision.Classify([]float64{0.48, 0.52}, tippingIn.Thresholds)
	deadlockTipping := BuildFacts(tippingIn)

	closeIn := tippingIn
	closeIn.Tipping = decision.Tipping{GenuinelyClose: true}
	deadlockClose := BuildFacts(closeIn)

	noneFit := BuildNoneFit("TN", []session.CarView{
		{Label: "Test Car A", Item: session.Item{Eliminated: true}, Eliminations: []session.Elimination{{Message: "on-road price is ₹1,50,000 over your budget"}}},
	})

	for name, f := range map[string]Facts{"winner": winner, "deadlock_tipping": deadlockTipping, "deadlock_close": deadlockClose, "none_fit": noneFit} {
		t.Run(name, func(t *testing.T) {
			got, err := RenderTemplate(f)
			if err != nil {
				t.Fatal(err)
			}
			golden(t, name, got)
		})
	}
}
