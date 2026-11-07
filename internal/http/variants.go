package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/xoBerlierxo/TieBreaker/internal/catalog"
)

// CatalogService is what the catalogue handlers need. *catalog.Service
// implements it; tests use a fake.
type CatalogService interface {
	Search(ctx context.Context, p catalog.SearchParams) ([]catalog.Variant, error)
	Get(ctx context.Context, id string) (catalog.Variant, error)
	Create(ctx context.Context, v catalog.Variant) (catalog.Variant, error)
	Update(ctx context.Context, id string, v catalog.Variant) (catalog.Variant, error)
	Delete(ctx context.Context, id string) error
}

// Data warnings shown to users (SPEC §7, §9.3).
const (
	WarningUnverifiedData = "unverified_data"
	WarningSafetyNotRated = "safety_not_rated"
)

// VariantDTO is the API representation of a variant: the stored data plus a
// few derived values and data warnings, so the frontend doesn't recompute them.
type VariantDTO struct {
	catalog.Variant
	DisplayName       string   `json:"display_name"`
	IsAutomatic       bool     `json:"is_automatic"`
	EfficiencyKmUnit  *float64 `json:"efficiency_km_per_unit"`
	PowerToWeightPSPT *float64 `json:"power_to_weight_ps_per_tonne"`
	Warnings          []string `json:"warnings"`
}

func toVariantDTO(v catalog.Variant) VariantDTO {
	dto := VariantDTO{
		Variant:     v,
		DisplayName: v.DisplayName(),
		IsAutomatic: v.IsAutomatic(),
		Warnings:    VariantWarnings(v),
	}
	if eff, ok := v.Efficiency(); ok {
		dto.EfficiencyKmUnit = &eff
	}
	if ptw, ok := v.PowerToWeight(); ok {
		dto.PowerToWeightPSPT = &ptw
	}
	if dto.KeyFeatures == nil {
		dto.KeyFeatures = []string{}
	}
	return dto
}

// VariantWarnings lists the data warnings for a variant, in a fixed order.
func VariantWarnings(v catalog.Variant) []string {
	w := []string{}
	if !v.Verified {
		w = append(w, WarningUnverifiedData)
	}
	if v.SafetyRatingStars == nil {
		w = append(w, WarningSafetyNotRated)
	}
	return w
}

type variantHandler struct {
	logger  *slog.Logger
	catalog CatalogService
}

// search handles GET /v1/variants?q=&fuel=&limit=.
func (h variantHandler) search(c *gin.Context) {
	limit, err := queryInt(c, "limit", 0)
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	variants, err := h.catalog.Search(c.Request.Context(), catalog.SearchParams{
		Query: c.Query("q"),
		Fuel:  catalog.FuelType(c.Query("fuel")),
		Limit: limit,
	})
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	out := make([]VariantDTO, 0, len(variants))
	for _, v := range variants {
		v.Sources = nil // the list view stays small; sources are on the detail endpoint
		out = append(out, toVariantDTO(v))
	}
	c.JSON(http.StatusOK, gin.H{"variants": out, "count": len(out)})
}

// get handles GET /v1/variants/:id.
func (h variantHandler) get(c *gin.Context) {
	v, err := h.catalog.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, toVariantDTO(v))
}

// create handles POST /admin/v1/variants.
func (h variantHandler) create(c *gin.Context) {
	var v catalog.Variant
	if err := bindJSON(c, &v); err != nil {
		WriteError(c, h.logger, err)
		return
	}
	created, err := h.catalog.Create(c.Request.Context(), v)
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusCreated, toVariantDTO(created))
}

// update handles PUT /admin/v1/variants/:id.
func (h variantHandler) update(c *gin.Context) {
	var v catalog.Variant
	if err := bindJSON(c, &v); err != nil {
		WriteError(c, h.logger, err)
		return
	}
	updated, err := h.catalog.Update(c.Request.Context(), c.Param("id"), v)
	if err != nil {
		WriteError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, toVariantDTO(updated))
}

// remove handles DELETE /admin/v1/variants/:id.
func (h variantHandler) remove(c *gin.Context) {
	if err := h.catalog.Delete(c.Request.Context(), c.Param("id")); err != nil {
		WriteError(c, h.logger, err)
		return
	}
	c.Status(http.StatusNoContent)
}
