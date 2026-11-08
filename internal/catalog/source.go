package catalog

import (
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
)

// PlaceholderSourceURL marks sample data whose real source hasn't been filled
// in yet. The ".invalid" top-level domain is reserved and can never resolve
// (RFC 2606), so it can't be mistaken for a real source.
const PlaceholderSourceURL = "https://example.invalid/placeholder"

// VariantSource records where one field's value came from (SPEC §5.1).
type VariantSource struct {
	ID        int64   `gorm:"column:id;primaryKey" json:"-"`
	VariantID string  `gorm:"column:variant_id;type:uuid" json:"-"`
	FieldName string  `gorm:"column:field_name" json:"field_name"`
	SourceURL string  `gorm:"column:source_url" json:"source_url"`
	CheckedAt Date    `gorm:"column:checked_at;type:date" json:"checked_at"`
	Note      *string `gorm:"column:note" json:"note,omitempty"`
}

// TableName tells GORM the table name explicitly.
func (VariantSource) TableName() string { return "variant_sources" }

// IsPlaceholder reports whether the source is a placeholder, not a real reference.
func (s VariantSource) IsPlaceholder() bool {
	return IsPlaceholderURL(s.SourceURL)
}

// IsPlaceholderURL reports whether a source URL is a placeholder or not a real
// http(s) link.
func IsPlaceholderURL(raw string) bool {
	if raw == "" || strings.EqualFold(raw, "TODO") {
		return true
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return true
	}
	host := strings.ToLower(u.Hostname())
	return host == "example.invalid" || strings.HasSuffix(host, ".invalid") || host == "example.com"
}

// PresentFields lists the data fields that have a value, using their column
// names. Each of these needs at least one source before the variant can be
// marked verified. It is written out field by field (no reflection) so it is
// obvious which fields count.
func (v Variant) PresentFields() []string {
	fields := []string{"make", "model", "variant_name", "body_type", "fuel_type", "launch_year", "transmission", "seats"}
	add := func(present bool, name string) {
		if present {
			fields = append(fields, name)
		}
	}
	add(v.ExShowroomPriceINR != nil, "ex_showroom_price_inr")
	add(v.DiscontinuedYear != nil, "discontinued_year")
	add(v.ClaimedEfficiency != nil, "claimed_efficiency")
	add(v.BatteryKWh != nil, "battery_kwh")
	add(v.ClaimedRangeKm != nil, "claimed_range_km")
	add(v.SafetyRatingStars != nil, "safety_rating_stars")
	add(v.Airbags != nil, "airbags")
	add(v.BootLitres != nil, "boot_litres")
	add(v.GroundClearanceMM != nil, "ground_clearance_mm")
	add(v.PowerPS != nil, "power_ps")
	add(v.KerbWeightKg != nil, "kerb_weight_kg")
	add(v.ServiceIntervalKm != nil, "service_interval_km")
	add(v.WarrantyYears != nil, "warranty_years")
	add(v.WarrantyKm != nil, "warranty_km")
	add(len(v.KeyFeatures) > 0, "key_features")
	return fields
}

// CanVerify returns nil only if every present field has at least one real
// (non-placeholder) source (SPEC §5.1: "each data field must have at least one
// source before it can be marked verified").
func CanVerify(v Variant, sources []VariantSource) error {
	sourced := make(map[string]bool, len(sources))
	for _, s := range sources {
		if !s.IsPlaceholder() {
			sourced[s.FieldName] = true
		}
	}
	var missing []string
	for _, p := range v.OnRoadPrices {
		if IsPlaceholderURL(p.SourceURL) {
			missing = append(missing, "on_road_prices."+p.State)
		}
	}
	for _, f := range v.PresentFields() {
		if !sourced[f] {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return apperr.New(apperr.ErrValidation,
			fmt.Sprintf("cannot mark verified: fields without a real source: %s", strings.Join(missing, ", "))).
			WithDetails("unsourced_fields", missing)
	}
	return nil
}
