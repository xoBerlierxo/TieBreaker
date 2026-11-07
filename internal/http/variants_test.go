package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
	"github.com/xoBerlierxo/TieBreaker/internal/catalog"
)

// fakeCatalog is an in-memory CatalogService for handler tests.
type fakeCatalog struct {
	variants   map[string]catalog.Variant
	lastSearch catalog.SearchParams
	created    *catalog.Variant
}

func newFakeCatalog(vs ...catalog.Variant) *fakeCatalog {
	f := &fakeCatalog{variants: map[string]catalog.Variant{}}
	for _, v := range vs {
		f.variants[v.ID] = v
	}
	return f
}

func (f *fakeCatalog) Search(_ context.Context, p catalog.SearchParams) ([]catalog.Variant, error) {
	f.lastSearch = p
	if p.Limit > catalog.MaxSearchLimit || p.Limit < 0 {
		return nil, apperr.Validation("limit", "bad limit")
	}
	var out []catalog.Variant
	for _, v := range f.variants {
		out = append(out, v)
	}
	return out, nil
}

func (f *fakeCatalog) Get(_ context.Context, id string) (catalog.Variant, error) {
	v, ok := f.variants[id]
	if !ok {
		return catalog.Variant{}, apperr.New(apperr.ErrNotFound, "variant not found")
	}
	return v, nil
}

func (f *fakeCatalog) Create(_ context.Context, v catalog.Variant) (catalog.Variant, error) {
	if err := v.Validate(); err != nil {
		return catalog.Variant{}, err
	}
	v.ID = "11111111-1111-4111-8111-111111111111"
	f.created = &v
	return v, nil
}

func (f *fakeCatalog) Update(_ context.Context, id string, v catalog.Variant) (catalog.Variant, error) {
	if _, ok := f.variants[id]; !ok {
		return catalog.Variant{}, apperr.New(apperr.ErrNotFound, "variant not found")
	}
	v.ID = id
	return v, nil
}

func (f *fakeCatalog) Delete(_ context.Context, id string) error {
	if _, ok := f.variants[id]; !ok {
		return apperr.New(apperr.ErrNotFound, "variant not found")
	}
	delete(f.variants, id)
	return nil
}

func ptrTo[T any](v T) *T { return &v }

func sampleVariant() catalog.Variant {
	return catalog.Variant{
		ID: "22222222-2222-4222-8222-222222222222", Slug: "test-motors-alpha-base",
		Make: "Test Motors", Model: "Alpha", VariantName: "Base",
		BodyType: catalog.BodyHatchback, FuelType: catalog.FuelPetrol, Transmission: catalog.TransManual,
		LaunchYear: 2020, Seats: 5, ExShowroomPriceINR: ptrTo(int64(500_000)),
		ClaimedEfficiency: ptrTo(20.0), EfficiencyUnit: ptrTo(catalog.UnitKmPerLitre),
	}
}

func catalogRouter(cat CatalogService) http.Handler {
	deps := testDeps()
	deps.Catalog = cat
	deps.JWTSecret = testJWTSecret
	return NewRouter(deps)
}

func TestVariantsSearch_PassesParamsAndReturnsList(t *testing.T) {
	cat := newFakeCatalog(sampleVariant())
	rec := do(t, catalogRouter(cat), http.MethodGet, "/v1/variants?q=alpha&fuel=petrol&limit=5", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	if cat.lastSearch.Query != "alpha" || cat.lastSearch.Fuel != catalog.FuelPetrol || cat.lastSearch.Limit != 5 {
		t.Errorf("search params = %+v", cat.lastSearch)
	}
	var body struct {
		Variants []VariantDTO `json:"variants"`
		Count    int          `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Count != 1 || len(body.Variants) != 1 {
		t.Fatalf("body = %+v", body)
	}
}

func TestVariantsSearch_400OnBadLimit(t *testing.T) {
	for _, q := range []string{"limit=abc", "limit=999"} {
		rec := do(t, catalogRouter(newFakeCatalog()), http.MethodGet, "/v1/variants?"+q, "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", q, rec.Code)
		}
	}
}

func TestVariantsSearch_ReturnsUnverifiedAndNotRatedWarnings(t *testing.T) {
	rec := do(t, catalogRouter(newFakeCatalog(sampleVariant())), http.MethodGet, "/v1/variants", "")
	var body struct {
		Variants []VariantDTO `json:"variants"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body.Variants) != 1 {
		t.Fatalf("variants = %+v", body.Variants)
	}
	w := strings.Join(body.Variants[0].Warnings, ",")
	if !strings.Contains(w, WarningUnverifiedData) || !strings.Contains(w, WarningSafetyNotRated) {
		t.Errorf("warnings = %v, want unverified_data and safety_not_rated", body.Variants[0].Warnings)
	}
	if body.Variants[0].Verified {
		t.Error("verified = true")
	}
}

func TestVariantGet_404(t *testing.T) {
	rec := do(t, catalogRouter(newFakeCatalog()), http.MethodGet, "/v1/variants/33333333-3333-4333-8333-333333333333", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestVariantGet_IncludesSources(t *testing.T) {
	v := sampleVariant()
	v.Sources = []catalog.VariantSource{{FieldName: "power_ps", SourceURL: catalog.PlaceholderSourceURL, CheckedAt: catalog.NewDate(2026, 1, 1)}}
	rec := do(t, catalogRouter(newFakeCatalog(v)), http.MethodGet, "/v1/variants/"+v.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"checked_at":"2026-01-01"`) {
		t.Errorf("body missing source checked_at: %s", rec.Body.String())
	}
}

func adminRequest(t *testing.T, h http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAdminCreate_RequiresTokenAndValidatesBody(t *testing.T) {
	cat := newFakeCatalog()
	h := catalogRouter(cat)
	token, err := MintAdminToken(testJWTSecret, 5*time.Minute, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	valid, _ := json.Marshal(sampleVariant())

	if rec := adminRequest(t, h, http.MethodPost, "/admin/v1/variants", string(valid), ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", rec.Code)
	}
	if rec := adminRequest(t, h, http.MethodPost, "/admin/v1/variants", `{"slug": "x", "unknown_field": 1}`, token); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown field: status = %d, want 400", rec.Code)
	}
	if rec := adminRequest(t, h, http.MethodPost, "/admin/v1/variants", `{"slug": "Bad Slug"}`, token); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid variant: status = %d, want 400", rec.Code)
	}
	rec := adminRequest(t, h, http.MethodPost, "/admin/v1/variants", string(valid), token)
	if rec.Code != http.StatusCreated || cat.created == nil {
		t.Fatalf("valid create: status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminUpdateAndDelete(t *testing.T) {
	v := sampleVariant()
	h := catalogRouter(newFakeCatalog(v))
	token, _ := MintAdminToken(testJWTSecret, 5*time.Minute, time.Now())
	body, _ := json.Marshal(v)

	if rec := adminRequest(t, h, http.MethodPut, "/admin/v1/variants/"+v.ID, string(body), token); rec.Code != http.StatusOK {
		t.Errorf("update: status = %d", rec.Code)
	}
	if rec := adminRequest(t, h, http.MethodDelete, "/admin/v1/variants/"+v.ID, "", token); rec.Code != http.StatusNoContent {
		t.Errorf("delete: status = %d", rec.Code)
	}
	if rec := adminRequest(t, h, http.MethodDelete, "/admin/v1/variants/"+v.ID, "", token); rec.Code != http.StatusNotFound {
		t.Errorf("second delete: status = %d, want 404", rec.Code)
	}
}
