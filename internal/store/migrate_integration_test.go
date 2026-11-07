//go:build integration

package store

import (
	"context"
	"strings"
	"testing"
)

func TestMigrations_UpDownUpIsClean(t *testing.T) {
	ctx := context.Background()

	if err := MigrateUp(ctx, testDSN); err != nil {
		t.Fatalf("first up: %v", err)
	}
	upVersion, dirty, err := MigrationVersion(ctx, testDSN)
	if err != nil || dirty || upVersion == 0 {
		t.Fatalf("after up: version=%d dirty=%v err=%v", upVersion, dirty, err)
	}

	// Running up again is a no-op, not an error.
	if err := MigrateUp(ctx, testDSN); err != nil {
		t.Fatalf("second up should be a no-op: %v", err)
	}

	if err := MigrateDown(ctx, testDSN, 0); err != nil {
		t.Fatalf("down all: %v", err)
	}
	if v, _, err := MigrationVersion(ctx, testDSN); err != nil || v != 0 {
		t.Fatalf("after down: version=%d err=%v, want 0", v, err)
	}

	if err := MigrateUp(ctx, testDSN); err != nil {
		t.Fatalf("up after down: %v", err)
	}
	if v, _, _ := MigrationVersion(ctx, testDSN); v != upVersion {
		t.Fatalf("version after re-up = %d, want %d", v, upVersion)
	}
}

// validVariantInsert returns an INSERT for a fictional, valid variant with one
// column overridden, so each case changes exactly one thing.
func validVariantInsert(overrideCol, overrideVal string) string {
	cols := []string{"slug", "make", "model", "variant_name", "body_type", "fuel_type",
		"launch_year", "transmission", "seats", "ex_showroom_price_inr", "safety_rating_stars", "safety_rating_body",
		"discontinued_year", "battery_kwh", "claimed_efficiency", "efficiency_unit"}
	vals := map[string]string{
		"slug": "'test-car'", "make": "'Test'", "model": "'Car'", "variant_name": "'Base'",
		"body_type": "'hatchback'", "fuel_type": "'petrol'", "launch_year": "2020",
		"transmission": "'manual'", "seats": "5", "ex_showroom_price_inr": "500000",
		"safety_rating_stars": "NULL", "safety_rating_body": "NULL", "discontinued_year": "NULL",
		"battery_kwh": "NULL", "claimed_efficiency": "20", "efficiency_unit": "'km/l'",
	}
	if overrideCol != "" {
		vals[overrideCol] = overrideVal
	}
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = vals[c]
	}
	return "INSERT INTO variants (" + strings.Join(cols, ", ") + ") VALUES (" + strings.Join(parts, ", ") + ")"
}

func TestVariants_CheckConstraintsRejectInvalidRows(t *testing.T) {
	ctx := context.Background()
	if err := MigrateUp(ctx, testDSN); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db := openTestDB(t)

	// The valid baseline must be accepted, otherwise the cases below prove nothing.
	if err := db.Exec(validVariantInsert("", "")).Error; err != nil {
		t.Fatalf("valid baseline rejected: %v", err)
	}
	db.Exec("DELETE FROM variants WHERE slug = 'test-car'")

	tests := []struct {
		name, col, val string
	}{
		{"unknown fuel type", "fuel_type", "'hydrogen'"},
		{"ev without battery", "fuel_type", "'ev'"},
		{"six stars", "safety_rating_stars", "6"},
		{"stars without rating body", "safety_rating_stars", "4"},
		{"discontinued before launch", "discontinued_year", "2019"},
		{"zero seats", "seats", "0"},
		{"negative price", "ex_showroom_price_inr", "-1"},
		{"battery on a petrol car", "battery_kwh", "30"},
		{"wrong efficiency unit for petrol", "efficiency_unit", "'km/kg'"},
		{"bad slug", "slug", "'Not A Slug'"},
		{"unknown body type", "body_type", "'spaceship'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := db.Exec(validVariantInsert(tt.col, tt.val)).Error
			if err == nil {
				db.Exec("DELETE FROM variants WHERE slug = 'test-car'")
				t.Fatalf("insert with %s=%s succeeded, want a CHECK violation", tt.col, tt.val)
			}
		})
	}
}

func TestAssumptions_UniqueKeyPerModelVersion(t *testing.T) {
	ctx := context.Background()
	if err := MigrateUp(ctx, testDSN); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db := openTestDB(t)
	t.Cleanup(func() { db.Exec("DELETE FROM assumptions WHERE key = 'test.unique_key'") })

	insert := func(version string) error {
		return db.Exec(`INSERT INTO assumptions (key, value_json, source_url, model_version)
			VALUES ('test.unique_key', '{"value": 1}', 'TODO', ?)`, version).Error
	}
	if err := insert("sim-v1"); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if err := insert("sim-v1"); err == nil {
		t.Fatal("duplicate (key, model_version) accepted, want unique violation")
	}
	if err := insert("sim-v2"); err != nil {
		t.Fatalf("same key with a new model version should be allowed: %v", err)
	}
}

func TestAssumptions_VerifiedRequiresRealSource(t *testing.T) {
	ctx := context.Background()
	if err := MigrateUp(ctx, testDSN); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db := openTestDB(t)
	err := db.Exec(`INSERT INTO assumptions (key, value_json, source_url, model_version, verified)
		VALUES ('test.verified_todo', '{"value": 1}', 'TODO', 'sim-v1', true)`).Error
	if err == nil {
		db.Exec("DELETE FROM assumptions WHERE key = 'test.verified_todo'")
		t.Fatal("verified assumption with source TODO accepted, want CHECK violation")
	}
}
