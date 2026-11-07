package report

import (
	"strings"
	"testing"

	"github.com/xoBerlierxo/TieBreaker/internal/decision"
)

var catalogNames = []string{"Test", "Car A", "Car B", "Zeta Turbo", "Zeta", "Sample Motors Alpha 1.2 MT"}

// minimalFacts has one money value of ₹1,02,340 and P(best) 62.34 %, the
// SPEC §9.4 example.
func minimalFacts() Facts {
	return Facts{
		Kind: decision.KindWinner, Winner: "Test Car A", HorizonYears: 5,
		Decision: &decision.Classification{Kind: decision.KindWinner, LeaderPBest: 0.6234, Gap: 0.2468},
		Cars: []CarFacts{
			{Label: "Test Car A", Make: "Test", Model: "Car A", PBest: 0.6234, NetCost: decision.Percentiles{P10: 90_000, P50: 102_340, P90: 120_000}},
			{Label: "Test Car B", Make: "Test", Model: "Car B", PBest: 0.3766, NetCost: decision.Percentiles{P10: 200_000, P50: 210_000, P90: 230_000}},
		},
		Thresholds: decision.Thresholds{WinPBest: 0.55, WinGap: 0.15},
	}
}

func TestValidate_Table(t *testing.T) {
	tests := []struct {
		name  string
		prose string
		ok    bool
	}{
		{"exact values", "Test Car A won in 62% of futures, with a median net cost of ₹1,02,340.", true},
		{"about one lakh for 1,02,340", "Test Car A costs about ₹1 lakh over five years.", true},
		{"1.02 lakh", "Test Car A has a median net cost of ₹1.02 lakh.", true},
		{"rounded Indian grouping", "Test Car A comes in at roughly ₹1,00,000.", true},
		{"two lakh is wrong", "Test Car A costs about ₹1.5 lakh.", false},
		{"Rs prefix", "Test Car A costs about Rs 1 lakh.", true},
		{"rounded percent", "Test Car A won about 62 percent of the time.", true},
		{"wrong percent", "Test Car A won 75% of the time.", false},
		{"number not in facts", "Test Car A saves ₹37,000 a year.", false},
		{"crore magnitude error", "Test Car A costs ₹1.02 crore.", false},
		{"car count as a word", "Of the two cars, Test Car A wins.", true},
		{"wrong count word", "Of the seven cars, Test Car A wins.", false},
		{"horizon years", "Over 5 years, Test Car A is cheaper.", true},
		{"percentile names", "Test Car A's 10th to 90th percentile range is narrow.", true},
		{"unknown car", "Test Car A beats the Zeta Turbo easily.", false},
		{"winner not named", "The first car is cheaper.", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := ValidateProse(tt.prose, minimalFacts(), catalogNames)
			if tt.ok && len(v) > 0 {
				t.Errorf("rejected valid prose %q: %+v", tt.prose, v)
			}
			if !tt.ok && len(v) == 0 {
				t.Errorf("accepted invalid prose %q", tt.prose)
			}
		})
	}
}

func TestValidate_AcceptsFactsFromTheFullFixture(t *testing.T) {
	f := BuildFacts(fixtureInput())
	prose := "Test Car B is the best fit: it had the lowest net cost in 70% of the simulated futures. " +
		"Its median net cost is about ₹5.9 lakh against about ₹6.5 lakh for Test Car A. " +
		"You value its safety rating (5★ vs 3★) at about ₹1.5 lakh, and it saves about ₹1.9 lakh in fuel/energy cost. " +
		"Above ~1,500 km/month, Test Car A would win instead."
	if v := ValidateProse(prose, f, catalogNames); len(v) > 0 {
		t.Fatalf("faithful prose rejected: %+v", v)
	}
}

func TestValidate_RejectsWinnerClaimInDeadlock(t *testing.T) {
	f := minimalFacts()
	f.Kind, f.Winner = decision.KindDeadlock, ""
	v := ValidateProse("Test Car A is the clear winner.", f, catalogNames)
	if len(v) == 0 || !strings.Contains(v[0].Reason, "deadlock") {
		t.Fatalf("violations = %+v, want a deadlock-claim violation", v)
	}
}

func TestValidate_NumbersInsideCarNamesAreIgnored(t *testing.T) {
	f := minimalFacts()
	f.Cars[0].Label, f.Winner = "Sample Motors Alpha 1.2 MT", "Sample Motors Alpha 1.2 MT"
	f.Cars[0].Make, f.Cars[0].Model = "Sample Motors", "Alpha"
	if v := ValidateProse("Sample Motors Alpha 1.2 MT won in 62% of futures.", f, catalogNames); len(v) > 0 {
		t.Fatalf("the 1.2 in a car name was treated as a fact: %+v", v)
	}
}

func TestDigitPrecision(t *testing.T) {
	// A trailing zero counts as rounding: "102340" may be rounded to tens.
	tests := map[string]float64{"1.02": 0.01, "102341": 1, "102340": 10, "100000": 100000, "1500": 100, "0": 1, "62": 1}
	for in, want := range tests {
		if got := digitPrecision(in); got != want {
			t.Errorf("digitPrecision(%q) = %v, want %v", in, got, want)
		}
	}
}
