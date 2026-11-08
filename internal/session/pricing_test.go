package session

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
	"github.com/xoBerlierxo/TieBreaker/internal/catalog"
	"github.com/xoBerlierxo/TieBreaker/internal/interview"
)

func ptr[T any](v T) *T { return &v }

var testNow = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

// seedSet loads the fictional fixture assumptions (testdata/seed), without a DB.
func seedSet(t *testing.T, state string) catalog.AssumptionSet {
	t.Helper()
	_, files, err := catalog.ReadSeedDir("../../testdata/seed")
	if err != nil {
		t.Fatal(err)
	}
	set, err := catalog.BuildAssumptionSet(state, "sim-v1", catalog.AssumptionRows(files))
	if err != nil {
		t.Fatal(err)
	}
	return set
}

// Fictional variants for tests (SPEC §7: no real car data).
func petrolVariant() catalog.Variant {
	return catalog.Variant{
		ID: "11111111-1111-4111-8111-111111111111", Make: "Test", Model: "Petrol", VariantName: "MT",
		BodyType: catalog.BodyHatchback, FuelType: catalog.FuelPetrol, Transmission: catalog.TransManual,
		LaunchYear: 2018, Seats: 5, ExShowroomPriceINR: ptr(int64(500_000)),
		ClaimedEfficiency: ptr(20.0), EfficiencyUnit: ptr(catalog.UnitKmPerLitre),
		SafetyRatingStars: ptr(3), SafetyRatingBody: ptr("Test NCAP"), ServiceIntervalKm: ptr(10_000),
	}
}

func evVariant() catalog.Variant {
	return catalog.Variant{
		ID: "22222222-2222-4222-8222-222222222222", Make: "Test", Model: "EV", VariantName: "40",
		BodyType: catalog.BodyCompactSUV, FuelType: catalog.FuelEV, Transmission: catalog.TransSingleSpeed,
		LaunchYear: 2022, Seats: 5, ExShowroomPriceINR: ptr(int64(1_400_000)),
		BatteryKWh: ptr(40.0), ClaimedRangeKm: ptr(400),
		SafetyRatingStars: ptr(5), SafetyRatingBody: ptr("Test NCAP"),
	}
}

func TestBuildItem_NewAndUsedRules(t *testing.T) {
	v := petrolVariant()
	tests := []struct {
		name  string
		req   AddCarRequest
		field string // "" = valid
	}{
		{"new ok", AddCarRequest{VariantID: v.ID, Condition: "new"}, ""},
		{"new with override", AddCarRequest{VariantID: v.ID, Condition: "new", OnRoadPriceOverrideINR: ptr(int64(600_000))}, ""},
		{"new with odometer", AddCarRequest{VariantID: v.ID, Condition: "new", OdometerKm: ptr(10)}, "condition"},
		{"bad condition", AddCarRequest{VariantID: v.ID, Condition: "refurbished"}, "condition"},
		{"used ok", AddCarRequest{VariantID: v.ID, Condition: "used", AskingPriceINR: ptr(int64(300_000)), Year: ptr(2020), OdometerKm: ptr(40_000)}, ""},
		{"used missing year", AddCarRequest{VariantID: v.ID, Condition: "used", AskingPriceINR: ptr(int64(300_000)), OdometerKm: ptr(40_000)}, "asking_price_inr"},
		{"used before launch", AddCarRequest{VariantID: v.ID, Condition: "used", AskingPriceINR: ptr(int64(300_000)), Year: ptr(2015), OdometerKm: ptr(4)}, "year"},
		{"used in the future", AddCarRequest{VariantID: v.ID, Condition: "used", AskingPriceINR: ptr(int64(300_000)), Year: ptr(2030), OdometerKm: ptr(4)}, "year"},
		{"used zero price", AddCarRequest{VariantID: v.ID, Condition: "used", AskingPriceINR: ptr(int64(0)), Year: ptr(2020), OdometerKm: ptr(4)}, "asking_price_inr"},
		{"used huge odometer", AddCarRequest{VariantID: v.ID, Condition: "used", AskingPriceINR: ptr(int64(1)), Year: ptr(2020), OdometerKm: ptr(2_000_000)}, "odometer_km"},
		{"used bad grade", AddCarRequest{VariantID: v.ID, Condition: "used", AskingPriceINR: ptr(int64(1)), Year: ptr(2020), OdometerKm: ptr(4), ConditionGrade: ptr("mint")}, "condition_grade"},
		{"used with override", AddCarRequest{VariantID: v.ID, Condition: "used", AskingPriceINR: ptr(int64(1)), Year: ptr(2020), OdometerKm: ptr(4), OnRoadPriceOverrideINR: ptr(int64(5))}, "on_road_price_override_inr"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildItem(tt.req, v, testNow)
			if tt.field == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var ae *apperr.Error
			if !errors.As(err, &ae) || ae.Details["field"] != tt.field {
				t.Errorf("err = %v, want validation on %s", err, tt.field)
			}
		})
	}
}

func TestBuildItem_UsedDefaultsOwnersAndGrade(t *testing.T) {
	item, err := BuildItem(AddCarRequest{Condition: "used", AskingPriceINR: ptr(int64(300_000)), Year: ptr(2020), OdometerKm: ptr(40_000)}, petrolVariant(), testNow)
	if err != nil {
		t.Fatal(err)
	}
	if *item.OwnersCount != 1 || *item.ConditionGrade != "fair" {
		t.Errorf("defaults = owners %d grade %s, want 1 and fair", *item.OwnersCount, *item.ConditionGrade)
	}
}

func TestBuildItem_NewWithoutPriceNeedsOverride(t *testing.T) {
	v := petrolVariant()
	v.ExShowroomPriceINR = nil
	if _, err := BuildItem(AddCarRequest{Condition: "new"}, v, testNow); !errors.Is(err, apperr.ErrValidation) {
		t.Fatalf("err = %v, want validation (no price)", err)
	}
}

func TestPriceOf_OnRoadAddsRoadTaxAndRegistration(t *testing.T) {
	set := seedSet(t, "TN")
	v := petrolVariant()
	p, err := PriceOf(Item{Condition: ConditionNew}, v, set)
	if err != nil {
		t.Fatal(err)
	}
	ex := 500_000.0
	want := ex + set.RoadTaxINR(catalog.FuelPetrol, ex) + set.RegistrationMiscINR
	if math.Abs(p.UpfrontINR-want) > 1e-9 || p.ValueBasisINR != ex || !p.Computed {
		t.Errorf("price = %+v, want upfront %.0f", p, want)
	}
}

func TestPriceOf_UsesOverrideWhenSet(t *testing.T) {
	p, err := PriceOf(Item{Condition: ConditionNew, OnRoadPriceOverrideINR: ptr(int64(650_000))}, petrolVariant(), seedSet(t, "TN"))
	if err != nil {
		t.Fatal(err)
	}
	if p.UpfrontINR != 650_000 || p.BudgetINR != 650_000 || p.ValueBasisINR != 500_000 || p.Computed {
		t.Errorf("price = %+v", p)
	}
}

func TestPriceOf_UsedAddsTransfer(t *testing.T) {
	set := seedSet(t, "KA")
	p, _ := PriceOf(Item{Condition: ConditionUsed, AskingPriceINR: ptr(int64(300_000))}, petrolVariant(), set)
	want := 300_000 + set.UsedTransfer.Rate*300_000 + set.UsedTransfer.FixedINR
	if p.UpfrontINR != want || p.BudgetINR != 300_000 {
		t.Errorf("price = %+v, want upfront %.0f budget 300000", p, want)
	}
}

func TestConstraints_BudgetShortfallInRupees(t *testing.T) {
	cars := []CarToCheck{
		{Item: Item{Condition: ConditionNew}, Variant: petrolVariant(), Price: Price{BudgetINR: 600_000}},
		{Item: Item{Condition: ConditionNew}, Variant: evVariant(), Price: Price{BudgetINR: 1_550_000}},
	}
	res := Constraints{MaxBudgetINR: ptr(int64(1_000_000))}.Evaluate(cars)
	if len(res[0]) != 0 {
		t.Errorf("petrol eliminated: %+v", res[0])
	}
	if len(res[1]) != 1 || res[1][0].Shortfall != 550_000 || !strings.Contains(res[1][0].Message, "₹5,50,000") {
		t.Errorf("EV elimination = %+v, want ₹5,50,000 over budget", res[1])
	}
}

func TestConstraints_SeatsAndTransmission(t *testing.T) {
	cars := []CarToCheck{{Variant: petrolVariant()}, {Variant: evVariant()}}
	res := Constraints{MinSeats: ptr(7), Transmission: ptr("automatic")}.Evaluate(cars)
	if len(res[0]) != 2 { // 5 seats and manual
		t.Errorf("petrol failures = %+v, want seats + transmission", res[0])
	}
	if len(res[1]) != 1 || res[1][0].Constraint != "min_seats" || res[1][0].Shortfall != 2 {
		t.Errorf("EV failures = %+v, want 2 seats short", res[1])
	}
}

func TestConstraints_HomeChargingDoesNotEliminate(t *testing.T) {
	res := Constraints{HomeCharging: ptr(false)}.Evaluate([]CarToCheck{{Variant: evVariant()}})
	if len(res[0]) != 0 {
		t.Errorf("EV eliminated for lack of home charging: %+v", res[0])
	}
}

func TestConstraints_ValidateRequiresHomeChargingWithEV(t *testing.T) {
	if err := (Constraints{}).Validate(true); !errors.Is(err, apperr.ErrValidation) {
		t.Errorf("missing home_charging with an EV: err = %v", err)
	}
	if err := (Constraints{}).Validate(false); err != nil {
		t.Errorf("no EV: err = %v", err)
	}
	if err := (Constraints{Transmission: ptr("cvt")}).Validate(false); err == nil {
		t.Error("transmission 'cvt' accepted; only automatic/manual are requirements")
	}
}

func TestBuildInputs_ResolvesCarsAndWarnings(t *testing.T) {
	set := seedSet(t, "MH")
	pv, ev := petrolVariant(), evVariant()
	usedYear := 2021
	cars := []SurvivingCar{
		{Item: Item{ID: "item-1", Condition: ConditionNew}, Variant: pv},
		{Item: Item{ID: "item-2", Condition: ConditionUsed, Year: &usedYear, OdometerKm: ptr(30_000), ConditionGrade: ptr("poor"), AskingPriceINR: ptr(int64(900_000))}, Variant: ev},
	}
	for i := range cars {
		p, err := PriceOf(cars[i].Item, cars[i].Variant, set)
		if err != nil {
			t.Fatal(err)
		}
		cars[i].Price, cars[i].Label = p, CarLabel(cars[i].Item, cars[i].Variant)
	}
	usage := interview.Usage{KmPerMonth: [2]float64{800, 1200}, CitySharePct: [2]float64{50, 70}, OwnershipYears: 5,
		Loan: &interview.Loan{PrincipalINR: 400_000, AnnualRatePct: 9, TenureMonths: 36}}

	b, err := BuildInputs(cars, set, usage, Constraints{HomeCharging: ptr(true)}, interview.NewTradeoffState(),
		interview.Ladder{Rungs: []int64{0, 25_000, 50_000, 100_000, 200_000, 400_000}, Cap: 800_000}, 0.07, testNow)
	if err != nil {
		t.Fatal(err)
	}
	in := b.Inputs
	if in.HorizonYears != 5 || in.Usage.CityShare.Hi != 0.7 || in.Usage.Loan.AnnualRate != 0.09 || !in.Usage.HomeCharging {
		t.Errorf("usage mapped wrongly: %+v", in.Usage)
	}
	evCar := in.Cars[1]
	if !evCar.Used || evCar.AgeYears != 5 || evCar.ClaimedEfficiency != 10 || evCar.RepairGradeMultiplier != set.Repairs.GradeMultiplier["poor"] {
		t.Errorf("used EV mapped wrongly: %+v", evCar)
	}
	if evCar.ServiceIntervalKm != set.ServiceDefaultKm {
		t.Errorf("default service interval not applied: %v", evCar.ServiceIntervalKm)
	}
	codes := map[string]int{}
	for _, w := range b.Warnings {
		codes[w.Code]++
	}
	if codes[WarnUnverifiedCar] != 2 || codes[WarnServiceIntervalDflt] != 1 || codes[WarnPlaceholderAssumpt] != 1 {
		t.Errorf("warnings = %+v", b.Warnings)
	}
	if len(b.Attributes) == 0 {
		t.Error("expected trade-off candidates between a petrol hatch and an EV")
	}
}
