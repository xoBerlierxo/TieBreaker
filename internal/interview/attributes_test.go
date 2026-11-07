package interview

import (
	"math"
	"strings"
	"testing"

	"github.com/xoBerlierxo/TieBreaker/internal/catalog"
)

func ptr[T any](v T) *T { return &v }

// testCar returns a fictional variant (made-up values for tests only).
func testCar(name string) catalog.Variant {
	return catalog.Variant{
		Make: "Test", Model: name, VariantName: "X",
		FuelType: catalog.FuelPetrol, Transmission: catalog.TransManual, Seats: 5,
		SafetyRatingStars: ptr(4), SafetyRatingBody: ptr("Test NCAP"),
		BootLitres: ptr(300), GroundClearanceMM: ptr(170),
		PowerPS: ptr(80.0), KerbWeightKg: ptr(800),
		WarrantyYears: ptr(3), WarrantyKm: ptr(100_000),
		KeyFeatures: []string{"rear_camera"},
	}
}

func keys(attrs []Attribute) []string {
	out := make([]string, len(attrs))
	for i, a := range attrs {
		out[i] = a.Key
	}
	return out
}

func TestCandidates_NeverIncludesAttributeWhereAllEqual(t *testing.T) {
	a, b := testCar("A"), testCar("B")
	if got := Candidates([]catalog.Variant{a, b}); len(got) != 0 {
		t.Fatalf("identical cars produced candidates %v", keys(got))
	}

	tests := []struct {
		name string
		mut  func(*catalog.Variant)
		key  string
	}{
		{"safety", func(v *catalog.Variant) { v.SafetyRatingStars = ptr(5) }, KeySafety},
		{"boot", func(v *catalog.Variant) { v.BootLitres = ptr(420) }, KeyBoot},
		{"ground clearance", func(v *catalog.Variant) { v.GroundClearanceMM = ptr(200) }, KeyGroundClearance},
		{"performance", func(v *catalog.Variant) { v.PowerPS = ptr(120.0) }, KeyPerformance},
		{"automatic", func(v *catalog.Variant) { v.Transmission = catalog.TransAMT }, KeyAutomatic},
		{"feature", func(v *catalog.Variant) { v.KeyFeatures = []string{"rear_camera", "esc"} }, "feature:esc"},
		{"warranty", func(v *catalog.Variant) { v.WarrantyYears = ptr(5) }, KeyWarranty},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := testCar("B")
			tt.mut(&b)
			got := keys(Candidates([]catalog.Variant{testCar("A"), b}))
			if len(got) != 1 || got[0] != tt.key {
				t.Errorf("candidates = %v, want exactly [%s]", got, tt.key)
			}
		})
	}
}

func TestCandidates_SafetySkippedWhenAnyCarUnrated(t *testing.T) {
	a, b := testCar("A"), testCar("B")
	b.SafetyRatingStars, b.SafetyRatingBody = nil, nil
	for _, k := range keys(Candidates([]catalog.Variant{a, b})) {
		if k == KeySafety {
			t.Fatal("safety asked although one car is not rated (it must never count as 0 stars)")
		}
	}
}

func TestCandidates_WarnsWhenRatingBodiesDiffer(t *testing.T) {
	a, b := testCar("A"), testCar("B")
	b.SafetyRatingStars, b.SafetyRatingBody = ptr(5), ptr("Other NCAP")
	got := Candidates([]catalog.Variant{a, b})
	if len(got) != 1 || got[0].Warning == "" {
		t.Fatalf("want a safety candidate with a warning, got %+v", got)
	}
}

func TestCandidates_OnlyKnownFeaturesAndWarrantyFallsBackToKm(t *testing.T) {
	a, b := testCar("A"), testCar("B")
	b.KeyFeatures = []string{"rear_camera", "unicorn_horn"} // not in the vocabulary
	b.WarrantyKm = ptr(150_000)                             // years equal, km differ
	got := keys(Candidates([]catalog.Variant{a, b}))
	if len(got) != 1 || got[0] != KeyWarranty {
		t.Fatalf("candidates = %v, want [warranty] only", got)
	}
}

func TestShares_BestIsOneWorstIsZeroMiddleLinear(t *testing.T) {
	a, b, c := testCar("A"), testCar("B"), testCar("C")
	a.BootLitres, b.BootLitres, c.BootLitres = ptr(300), ptr(400), ptr(350)
	got := Candidates([]catalog.Variant{a, b, c})
	if len(got) != 1 {
		t.Fatalf("got %v", keys(got))
	}
	want := []float64{0, 1, 0.5}
	for i, s := range got[0].Shares {
		if math.Abs(s-want[i]) > 1e-12 {
			t.Errorf("shares = %v, want %v", got[0].Shares, want)
		}
	}
	if got[0].Best != 1 || got[0].Worst != 0 {
		t.Errorf("best/worst = %d/%d", got[0].Best, got[0].Worst)
	}
}

func TestPerformance_UsesPowerToWeight(t *testing.T) {
	a, b := testCar("A"), testCar("B")
	// Same power, lighter car → better power-to-weight.
	b.KerbWeightKg = ptr(1000)
	got := Candidates([]catalog.Variant{a, b})
	if len(got) != 1 || got[0].Key != KeyPerformance || got[0].Best != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestQuestionText_UsesHorizonAndCarNames(t *testing.T) {
	a, b := testCar("A"), testCar("B")
	b.SafetyRatingStars = ptr(5)
	attr := Candidates([]catalog.Variant{a, b})[0]
	q := QuestionText(attr, []string{"Test A", "Test B"}, 100_000, 5)
	for _, want := range []string{"Test B", "Test A", "5★", "4★", "₹1,00,000", "5 years"} {
		if !strings.Contains(q, want) {
			t.Errorf("question %q does not mention %q", q, want)
		}
	}
}
