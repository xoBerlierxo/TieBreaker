package catalog

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
)

func ptr[T any](v T) *T { return &v }

// fictionalPetrol returns a valid, fictional petrol variant. Values are made
// up for tests only and describe no real car (SPEC §7).
func fictionalPetrol() Variant {
	return Variant{
		Slug: "test-motors-alpha-base", Make: "Test Motors", Model: "Alpha", VariantName: "Base",
		BodyType: BodyHatchback, FuelType: FuelPetrol, Transmission: TransManual,
		LaunchYear: 2020, Seats: 5,
		ExShowroomPriceINR: ptr(int64(500_000)),
		ClaimedEfficiency:  ptr(20.0), EfficiencyUnit: ptr(UnitKmPerLitre),
		PowerPS: ptr(80.0), KerbWeightKg: ptr(800),
		KeyFeatures: []string{"rear_camera"},
	}
}

func fictionalEV() Variant {
	return Variant{
		Slug: "test-motors-beta-ev", Make: "Test Motors", Model: "Beta", VariantName: "EV",
		BodyType: BodyCompactSUV, FuelType: FuelEV, Transmission: TransSingleSpeed,
		LaunchYear: 2022, Seats: 5,
		ExShowroomPriceINR: ptr(int64(1_000_000)),
		BatteryKWh:         ptr(40.0), ClaimedRangeKm: ptr(400),
	}
}

func TestVariantValidate_Table(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*Variant)
		wantField string // "" means valid
	}{
		{"valid petrol", func(*Variant) {}, ""},
		{"missing make", func(v *Variant) { v.Make = " " }, "make"},
		{"bad slug", func(v *Variant) { v.Slug = "Has Spaces" }, "slug"},
		{"unknown fuel", func(v *Variant) { v.FuelType = "hydrogen" }, "fuel_type"},
		{"unknown body", func(v *Variant) { v.BodyType = "spaceship" }, "body_type"},
		{"unknown transmission", func(v *Variant) { v.Transmission = "magic" }, "transmission"},
		{"zero seats", func(v *Variant) { v.Seats = 0 }, "seats"},
		{"ten seats", func(v *Variant) { v.Seats = 10 }, "seats"},
		{"discontinued before launch", func(v *Variant) { v.DiscontinuedYear = ptr(2019) }, "discontinued_year"},
		{"launch year too old", func(v *Variant) { v.LaunchYear = 1900 }, "launch_year"},
		{"zero price", func(v *Variant) { v.ExShowroomPriceINR = ptr(int64(0)) }, "ex_showroom_price_inr"},
		{"six stars", func(v *Variant) { v.SafetyRatingStars = ptr(6); v.SafetyRatingBody = ptr("Test NCAP") }, "safety_rating_stars"},
		{"stars without body", func(v *Variant) { v.SafetyRatingStars = ptr(4) }, "safety_rating_body"},
		{"efficiency without unit", func(v *Variant) { v.EfficiencyUnit = nil }, "efficiency_unit"},
		{"wrong unit for petrol", func(v *Variant) { v.EfficiencyUnit = ptr(UnitKmPerKg) }, "efficiency_unit"},
		{"battery on petrol", func(v *Variant) { v.BatteryKWh = ptr(30.0) }, "battery_kwh"},
		{"negative boot", func(v *Variant) { v.BootLitres = ptr(-1) }, "boot_litres"},
		{"empty feature key", func(v *Variant) { v.KeyFeatures = []string{""} }, "key_features"},
		{"duplicate feature key", func(v *Variant) { v.KeyFeatures = []string{"esc", "esc"} }, "key_features"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := fictionalPetrol()
			tt.mutate(&v)
			err := v.Validate()
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			var appErr *apperr.Error
			if !errors.As(err, &appErr) || !errors.Is(err, apperr.ErrValidation) {
				t.Fatalf("Validate() = %v, want an apperr validation error", err)
			}
			if appErr.Details["field"] != tt.wantField {
				t.Errorf("field = %v, want %s (err: %v)", appErr.Details["field"], tt.wantField, err)
			}
		})
	}
}

func TestVariantValidate_EVRules(t *testing.T) {
	ev := fictionalEV()
	if err := ev.Validate(); err != nil {
		t.Fatalf("valid EV rejected: %v", err)
	}
	ev.BatteryKWh = nil
	if err := ev.Validate(); err == nil {
		t.Fatal("EV without battery accepted")
	}
}

func TestCanVerify_FailsWhenAnyNonNullFieldUnsourced(t *testing.T) {
	v := fictionalPetrol()
	var sources []VariantSource
	for _, f := range v.PresentFields() {
		if f == "power_ps" {
			continue // leave one field unsourced
		}
		sources = append(sources, VariantSource{FieldName: f, SourceURL: "https://brochure.test/alpha.pdf"})
	}

	err := CanVerify(v, sources)
	if err == nil || !strings.Contains(err.Error(), "power_ps") {
		t.Fatalf("CanVerify = %v, want error naming power_ps", err)
	}

	sources = append(sources, VariantSource{FieldName: "power_ps", SourceURL: "https://brochure.test/alpha.pdf"})
	if err := CanVerify(v, sources); err != nil {
		t.Fatalf("CanVerify with every field sourced = %v, want nil", err)
	}
}

func TestCanVerify_IgnoresNullOptionalFields(t *testing.T) {
	v := fictionalPetrol()
	for _, f := range v.PresentFields() {
		if f == "safety_rating_stars" || f == "boot_litres" {
			t.Fatalf("PresentFields includes null field %s", f)
		}
	}
}

func TestCanVerify_RejectsPlaceholderSources(t *testing.T) {
	v := fictionalPetrol()
	var sources []VariantSource
	for _, f := range v.PresentFields() {
		sources = append(sources, VariantSource{FieldName: f, SourceURL: PlaceholderSourceURL})
	}
	if err := CanVerify(v, sources); err == nil {
		t.Fatal("CanVerify accepted placeholder sources")
	}
}

func TestPowerToWeight_PSPerTonne(t *testing.T) {
	v := fictionalPetrol() // 80 PS, 800 kg → 100 PS/tonne
	got, ok := v.PowerToWeight()
	if !ok || math.Abs(got-100) > 1e-9 {
		t.Fatalf("PowerToWeight() = %v, %v; want 100, true", got, ok)
	}
	v.KerbWeightKg = nil
	if _, ok := v.PowerToWeight(); ok {
		t.Error("PowerToWeight() ok with missing weight")
	}
}

func TestEVEfficiency_DerivedFromRangeAndBattery(t *testing.T) {
	ev := fictionalEV() // 400 km / 40 kWh = 10 km/kWh
	got, ok := ev.Efficiency()
	if !ok || math.Abs(got-10) > 1e-9 {
		t.Fatalf("Efficiency() = %v, %v; want 10, true", got, ok)
	}
	ev.ClaimedEfficiency, ev.EfficiencyUnit = ptr(7.0), ptr(UnitKmPerKWh)
	if got, _ := ev.Efficiency(); got != 7 {
		t.Errorf("explicit efficiency should win: got %v", got)
	}
}

func TestIsAutomatic_EverythingExceptManual(t *testing.T) {
	for _, tr := range []Transmission{TransAMT, TransCVT, TransDCT, TransTorqueConverter, TransSingleSpeed} {
		v := Variant{Transmission: tr}
		if !v.IsAutomatic() {
			t.Errorf("%s: IsAutomatic() = false, want true", tr)
		}
	}
	if (Variant{Transmission: TransManual}).IsAutomatic() {
		t.Error("manual: IsAutomatic() = true")
	}
}

func TestSegment_FromBodyType(t *testing.T) {
	tests := map[BodyType]Segment{
		BodyHatchback: SegmentSmall, BodySedan: SegmentMedium, BodyCompactSUV: SegmentMedium,
		BodySUV: SegmentLarge, BodyMUV: SegmentLarge,
	}
	for body, want := range tests {
		if got := (Variant{BodyType: body}).Segment(); got != want {
			t.Errorf("%s: Segment() = %s, want %s", body, got, want)
		}
	}
}

func TestVariantValidate_OnRoadPrices(t *testing.T) {
	ok := OnRoadPrice{State: "TN", OnRoadPriceINR: 900_000, SourceURL: "https://example.test/p", CheckedAt: NewDate(2026, 11, 8)}
	tests := []struct {
		name  string
		price []OnRoadPrice
		valid bool
	}{
		{"valid", []OnRoadPrice{ok}, true},
		{"unknown state", []OnRoadPrice{{State: "XX", OnRoadPriceINR: 1, SourceURL: "https://x.test", CheckedAt: NewDate(2026, 1, 1)}}, false},
		{"duplicate state", []OnRoadPrice{ok, ok}, false},
		{"zero price", []OnRoadPrice{{State: "TN", SourceURL: "https://x.test", CheckedAt: NewDate(2026, 1, 1)}}, false},
		{"no source", []OnRoadPrice{{State: "TN", OnRoadPriceINR: 1, CheckedAt: NewDate(2026, 1, 1)}}, false},
	}
	for _, tt := range tests {
		v := fictionalPetrol()
		v.OnRoadPrices = tt.price
		if err := v.Validate(); (err == nil) != tt.valid {
			t.Errorf("%s: Validate() = %v", tt.name, err)
		}
	}
}

func TestCanVerify_RejectsPlaceholderOnRoadPriceSource(t *testing.T) {
	v := fictionalPetrol()
	var sources []VariantSource
	for _, f := range v.PresentFields() {
		sources = append(sources, VariantSource{FieldName: f, SourceURL: "https://brochure.test/a.pdf"})
	}
	v.OnRoadPrices = []OnRoadPrice{{State: "TN", OnRoadPriceINR: 1, SourceURL: "TODO", CheckedAt: NewDate(2026, 1, 1)}}
	if err := CanVerify(v, sources); err == nil {
		t.Fatal("verified with a TODO on-road price source")
	}
}
