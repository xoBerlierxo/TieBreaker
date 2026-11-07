//go:build integration

package catalog

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"

	"gorm.io/gorm"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
	"github.com/xoBerlierxo/TieBreaker/internal/store"
	"github.com/xoBerlierxo/TieBreaker/internal/testutil"
)

var testDB *gorm.DB

func TestMain(m *testing.M) { os.Exit(runWithDB(m)) }

func runWithDB(m *testing.M) int {
	ctx := context.Background()
	dsn, stop, err := testutil.Postgres(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer stop()
	if err := store.MigrateUp(ctx, dsn); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	db, err := store.OpenPostgres(ctx, dsn, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	testDB = db
	return m.Run()
}

// resetCatalog empties the catalogue tables so each test starts clean.
func resetCatalog(t *testing.T) {
	t.Helper()
	if err := testDB.Exec("TRUNCATE variants, variant_sources, assumptions RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

func mustCreate(t *testing.T, svc *Service, v Variant) Variant {
	t.Helper()
	created, err := svc.Create(context.Background(), v)
	if err != nil {
		t.Fatalf("Create(%s): %v", v.Slug, err)
	}
	return created
}

func TestSearch_MatchesMakeModelVariantCaseInsensitive(t *testing.T) {
	resetCatalog(t)
	svc := NewService(testDB)
	mustCreate(t, svc, fictionalPetrol()) // "Test Motors Alpha Base"
	mustCreate(t, svc, fictionalEV())     // "Test Motors Beta EV"

	tests := []struct {
		query string
		want  int
	}{
		{"alpha", 1},
		{"MOTORS BETA", 1},
		{"test motors", 2},
		{"", 2},
		{"nothing-like-this", 0},
		{"100%", 0}, // wildcard is escaped, so it doesn't match everything
	}
	for _, tt := range tests {
		got, err := svc.Search(context.Background(), SearchParams{Query: tt.query})
		if err != nil {
			t.Fatalf("Search(%q): %v", tt.query, err)
		}
		if len(got) != tt.want {
			t.Errorf("Search(%q) returned %d variants, want %d", tt.query, len(got), tt.want)
		}
	}
}

func TestSearch_FiltersByFuel(t *testing.T) {
	resetCatalog(t)
	svc := NewService(testDB)
	mustCreate(t, svc, fictionalPetrol())
	mustCreate(t, svc, fictionalEV())

	got, err := svc.Search(context.Background(), SearchParams{Fuel: FuelEV})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].FuelType != FuelEV {
		t.Fatalf("Search(fuel=ev) = %+v, want only the EV", got)
	}

	if _, err := svc.Search(context.Background(), SearchParams{Fuel: "steam"}); !errors.Is(err, apperr.ErrValidation) {
		t.Errorf("unknown fuel: err = %v, want validation error", err)
	}
}

func TestSearch_ValidatesAndAppliesLimit(t *testing.T) {
	resetCatalog(t)
	svc := NewService(testDB)
	mustCreate(t, svc, fictionalPetrol())
	mustCreate(t, svc, fictionalEV())

	got, err := svc.Search(context.Background(), SearchParams{Limit: 1})
	if err != nil || len(got) != 1 {
		t.Fatalf("Search(limit=1) = %d results, err %v; want 1", len(got), err)
	}
	for _, bad := range []int{-1, MaxSearchLimit + 1} {
		if _, err := svc.Search(context.Background(), SearchParams{Limit: bad}); !errors.Is(err, apperr.ErrValidation) {
			t.Errorf("limit %d: err = %v, want validation error", bad, err)
		}
	}
}

func TestGet_IncludesSourcesAndVerifiedFlag(t *testing.T) {
	resetCatalog(t)
	svc := NewService(testDB)
	v := fictionalPetrol()
	v.Sources = []VariantSource{{FieldName: "power_ps", SourceURL: PlaceholderSourceURL, CheckedAt: NewDate(2026, 1, 2)}}
	created := mustCreate(t, svc, v)

	got, err := svc.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Verified {
		t.Error("Verified = true, want false")
	}
	if len(got.Sources) != 1 || got.Sources[0].FieldName != "power_ps" {
		t.Errorf("Sources = %+v", got.Sources)
	}
	if len(got.KeyFeatures) != 1 || got.KeyFeatures[0] != "rear_camera" {
		t.Errorf("KeyFeatures round-trip = %q", got.KeyFeatures)
	}
	if got.SafetyRatingStars != nil {
		t.Error("nil safety rating came back non-nil")
	}
}

func TestGet_UnknownOrMalformedIDReturnsNotFound(t *testing.T) {
	svc := NewService(testDB)
	for _, id := range []string{"00000000-0000-4000-8000-000000000000", "not-a-uuid"} {
		if _, err := svc.Get(context.Background(), id); !errors.Is(err, apperr.ErrNotFound) {
			t.Errorf("Get(%q) err = %v, want not found", id, err)
		}
	}
}

func TestCreate_VerifiedWithoutRealSourcesRejected(t *testing.T) {
	resetCatalog(t)
	svc := NewService(testDB)
	v := fictionalPetrol()
	v.Verified = true
	if _, err := svc.Create(context.Background(), v); !errors.Is(err, apperr.ErrValidation) {
		t.Fatalf("Create(verified, no sources) err = %v, want validation error", err)
	}
}

func TestUpdateAndDelete(t *testing.T) {
	resetCatalog(t)
	svc := NewService(testDB)
	created := mustCreate(t, svc, fictionalPetrol())

	changed := fictionalPetrol()
	changed.VariantName = "Plus"
	changed.SafetyRatingStars, changed.SafetyRatingBody = ptr(4), ptr("Test NCAP")
	updated, err := svc.Update(context.Background(), created.ID, changed)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.VariantName != "Plus" || updated.SafetyRatingStars == nil || *updated.SafetyRatingStars != 4 {
		t.Errorf("Update did not apply: %+v", updated)
	}

	if err := svc.Delete(context.Background(), created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := svc.Delete(context.Background(), created.ID); !errors.Is(err, apperr.ErrNotFound) {
		t.Errorf("second Delete err = %v, want not found", err)
	}
}

func TestCreate_DuplicateSlugIsValidationError(t *testing.T) {
	resetCatalog(t)
	svc := NewService(testDB)
	mustCreate(t, svc, fictionalPetrol())
	if _, err := svc.Create(context.Background(), fictionalPetrol()); !errors.Is(err, apperr.ErrValidation) {
		t.Fatalf("duplicate slug err = %v, want validation error", err)
	}
}
