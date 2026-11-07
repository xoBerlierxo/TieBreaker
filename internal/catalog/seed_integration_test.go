//go:build integration

package catalog

import (
	"context"
	"testing"
)

func TestLoadSeed_IdempotentUpsert(t *testing.T) {
	resetCatalog(t)
	ctx := context.Background()

	first, err := LoadDir(ctx, testDB, realSeedDir)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	second, err := LoadDir(ctx, testDB, realSeedDir)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if first != second {
		t.Errorf("loads differ: %+v vs %+v", first, second)
	}

	var variants, sources, assumptions int64
	testDB.Table("variants").Count(&variants)
	testDB.Table("variant_sources").Count(&sources)
	testDB.Table("assumptions").Count(&assumptions)
	if variants != int64(first.Variants) || sources != int64(first.Sources) || assumptions != int64(first.Assumptions) {
		t.Errorf("rows after two loads: variants=%d sources=%d assumptions=%d, want %+v (no duplicates)",
			variants, sources, assumptions, first)
	}
}

func TestLoadAssumptionSet_FromDatabase(t *testing.T) {
	resetCatalog(t)
	ctx := context.Background()
	if _, err := LoadDir(ctx, testDB, realSeedDir); err != nil {
		t.Fatal(err)
	}
	svc := NewService(testDB)
	for _, state := range SupportedStates() {
		set, err := svc.LoadAssumptionSet(ctx, state, "sim-v1")
		if err != nil {
			t.Errorf("%s: %v", state, err)
			continue
		}
		if set.State != state || len(set.UnverifiedKeys()) == 0 {
			t.Errorf("%s: unexpected set %+v", state, set.Provenance)
		}
	}
}
