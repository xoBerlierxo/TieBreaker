package catalog

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const realSeedDir = "../../data/seed"

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadSeedDir_RejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "x.json"), `{"variants": [{"slug": "a-b", "colour": "red"}]}`)

	_, _, err := ReadSeedDir(dir)
	if err == nil || !strings.Contains(err.Error(), "colour") {
		t.Fatalf("ReadSeedDir err = %v, want unknown-field error mentioning colour", err)
	}
}

func TestReadSeedDir_RejectsVerifiedWithoutRealSources(t *testing.T) {
	dir := t.TempDir()
	v := fictionalPetrol()
	v.Verified = true
	for _, f := range v.PresentFields() {
		v.Sources = append(v.Sources, VariantSource{FieldName: f, SourceURL: PlaceholderSourceURL, CheckedAt: NewDate(2026, 1, 1)})
	}
	b, _ := json.Marshal(ModelFile{Variants: []Variant{v}})
	writeFile(t, filepath.Join(dir, "x.json"), string(b))

	if _, _, err := ReadSeedDir(dir); err == nil {
		t.Fatal("ReadSeedDir accepted a verified variant with placeholder sources")
	}
}

func TestReadSeedDir_RejectsVerifiedAssumptionWithTODOSource(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "assumptions", "a.json"),
		`{"model_version": "sim-v1", "assumptions": [{"key": "x.y", "value": {"value": 1}, "source_url": "TODO", "checked_at": null, "verified": true}]}`)
	if _, _, err := ReadSeedDir(dir); err == nil {
		t.Fatal("verified assumption with TODO source accepted")
	}
}

// TestSeedRepo_SamplesAreFictionalAndUnverified guards SPEC §7 on the real seed
// directory: Claude Code may add at most 3 sample variants, all unverified, with
// placeholder sources. A variant marked verified must have real sources for every
// field (the developer adds those).
func TestSeedRepo_SamplesAreFictionalAndUnverified(t *testing.T) {
	variants, _, err := ReadSeedDir(realSeedDir)
	if err != nil {
		t.Fatalf("ReadSeedDir(%s): %v", realSeedDir, err)
	}
	samples := 0
	for _, v := range variants {
		placeholder := false
		for _, s := range v.Sources {
			if s.IsPlaceholder() {
				placeholder = true
			}
		}
		if placeholder {
			samples++
			if v.Verified {
				t.Errorf("%s has placeholder sources but is marked verified", v.Slug)
			}
		}
		if v.Verified {
			if err := CanVerify(v, v.Sources); err != nil {
				t.Errorf("%s is verified but: %v", v.Slug, err)
			}
		}
	}
	if samples > 3 {
		t.Errorf("found %d sample variants with placeholder sources, SPEC §7 allows at most 3", samples)
	}
}

func TestAssumptionSeed_EveryKeyPresentForEveryState(t *testing.T) {
	_, files, err := ReadSeedDir(realSeedDir)
	if err != nil {
		t.Fatal(err)
	}
	rows := seedRows(files)
	for _, state := range SupportedStates() {
		set, err := BuildAssumptionSet(state, "sim-v1", rows)
		if err != nil {
			t.Errorf("state %s: %v", state, err)
			continue
		}
		if len(set.UnverifiedKeys()) == 0 {
			t.Errorf("state %s: expected placeholder (unverified) keys in the seed data", state)
		}
	}
}

func seedRows(files []AssumptionFile) []Assumption {
	var rows []Assumption
	for _, f := range files {
		for _, a := range f.Assumptions {
			rows = append(rows, Assumption{Key: a.Key, ValueJSON: a.Value, SourceURL: a.SourceURL,
				CheckedAt: a.CheckedAt, ModelVersion: f.ModelVersion, Verified: a.Verified})
		}
	}
	return rows
}

func realSet(t *testing.T, state string) AssumptionSet {
	t.Helper()
	_, files, err := ReadSeedDir(realSeedDir)
	if err != nil {
		t.Fatal(err)
	}
	set, err := BuildAssumptionSet(state, "sim-v1", seedRows(files))
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func TestAssumptionSet_ParsesEveryKey(t *testing.T) {
	set := realSet(t, "TN")
	if set.FuelPriceINR[FuelPetrol] <= 0 || set.HomeTariffINR <= 0 || len(set.ResaleCurve[FuelEV]) == 0 {
		t.Errorf("set not fully populated: %+v", set)
	}
	if got := len(set.Provenance); got != len(RequiredAssumptionKeys("TN")) {
		t.Errorf("provenance has %d entries, want %d", got, len(RequiredAssumptionKeys("TN")))
	}
}

func TestAssumptionSet_MissingKeyIsError(t *testing.T) {
	_, files, _ := ReadSeedDir(realSeedDir)
	rows := seedRows(files)
	var trimmed []Assumption
	for _, r := range rows {
		if r.Key != "tyres.model" {
			trimmed = append(trimmed, r)
		}
	}
	_, err := BuildAssumptionSet("KA", "sim-v1", trimmed)
	if err == nil || !strings.Contains(err.Error(), "tyres.model") {
		t.Fatalf("err = %v, want missing tyres.model", err)
	}
}

func TestAssumptionSet_UnsupportedStateIsValidationError(t *testing.T) {
	if _, err := BuildAssumptionSet("XX", "sim-v1", nil); err == nil {
		t.Fatal("unsupported state accepted")
	}
}

func TestAssumptionSet_TypoInValueIsError(t *testing.T) {
	_, files, _ := ReadSeedDir(realSeedDir)
	rows := seedRows(files)
	for i := range rows {
		if rows[i].Key == "price_trend.petrol" {
			rows[i].ValueJSON = json.RawMessage(`{"mu": 0.03, "sigam": 0.05}`)
		}
	}
	if _, err := BuildAssumptionSet("MH", "sim-v1", rows); err == nil {
		t.Fatal("typo 'sigam' accepted")
	}
}

func TestRoadTax_SlabByFuelAndPrice(t *testing.T) {
	limit := int64(1_000_000)
	// Fictional slabs that exist only in this test.
	set := AssumptionSet{RoadTax: map[FuelType][]RoadTaxSlab{
		FuelPetrol: {{UpToINR: &limit, Rate: 0.10}, {UpToINR: nil, Rate: 0.15}},
		FuelEV:     {{UpToINR: nil, Rate: 0.01}},
	}}
	tests := []struct {
		fuel  FuelType
		price float64
		want  float64
	}{
		{FuelPetrol, 500_000, 50_000},
		{FuelPetrol, 1_000_000, 100_000}, // on the boundary: first slab
		{FuelPetrol, 1_200_000, 180_000},
		{FuelEV, 2_000_000, 20_000},
		{FuelDiesel, 800_000, 0}, // no slabs configured
	}
	for _, tt := range tests {
		if got := set.RoadTaxINR(tt.fuel, tt.price); math.Abs(got-tt.want) > 1e-6 {
			t.Errorf("RoadTaxINR(%s, %.0f) = %.2f, want %.2f", tt.fuel, tt.price, got, tt.want)
		}
	}
}

func TestSnapshot_RoundTripsExactly(t *testing.T) {
	set := realSet(t, "DL")
	snap, err := set.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	var back AssumptionSet
	if err := json.Unmarshal(snap, &back); err != nil {
		t.Fatal(err)
	}
	again, _ := back.Snapshot()
	if string(again) != string(snap) {
		t.Error("snapshot does not round-trip byte-for-byte")
	}
}

func TestDate_JSONRoundTrip(t *testing.T) {
	var d Date
	if err := json.Unmarshal([]byte(`"2026-11-07"`), &d); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(d)
	if string(b) != `"2026-11-07"` {
		t.Errorf("round trip = %s", b)
	}
	if err := json.Unmarshal([]byte(`"07/11/2026"`), &d); err == nil {
		t.Error("non-ISO date accepted")
	}
}
