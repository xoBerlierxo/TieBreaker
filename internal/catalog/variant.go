// Package catalog holds the car catalogue: variants, their data sources, and
// the versioned model assumptions (SPEC §4, §5.1, §7).
package catalog

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
	"github.com/xoBerlierxo/TieBreaker/internal/store"
)

// FuelType is the energy a car runs on.
type FuelType string

const (
	FuelPetrol       FuelType = "petrol"
	FuelDiesel       FuelType = "diesel"
	FuelCNG          FuelType = "cng"
	FuelEV           FuelType = "ev"
	FuelStrongHybrid FuelType = "strong_hybrid"
)

// AllFuelTypes lists every fuel type, in a fixed order. It is a function, not a
// package-level slice, so no caller can modify a shared value (SPEC §18).
func AllFuelTypes() []FuelType {
	return []FuelType{FuelPetrol, FuelDiesel, FuelCNG, FuelEV, FuelStrongHybrid}
}

// Transmission is the gearbox type.
type Transmission string

const (
	TransManual          Transmission = "manual"
	TransAMT             Transmission = "amt"
	TransCVT             Transmission = "cvt"
	TransDCT             Transmission = "dct"
	TransTorqueConverter Transmission = "torque_converter"
	TransSingleSpeed     Transmission = "single_speed"
)

// BodyType is the body style. It also decides the cost segment.
type BodyType string

const (
	BodyHatchback  BodyType = "hatchback"
	BodySedan      BodyType = "sedan"
	BodyCompactSUV BodyType = "compact_suv"
	BodySUV        BodyType = "suv"
	BodyMUV        BodyType = "muv"
)

// Efficiency units (SPEC §5.1).
const (
	UnitKmPerLitre = "km/l"
	UnitKmPerKg    = "km/kg"
	UnitKmPerKWh   = "km/kWh"
)

// Variant is one specific version of a car model, e.g. "Alpha Base".
// Pointer fields are optional: nil means "unknown / not applicable", which is
// different from zero. A nil safety rating is shown as "not rated" (SPEC §7).
//
// The same struct is the GORM model, the seed-file format and the admin API
// body, so every field has explicit column and JSON names.
type Variant struct {
	ID                 string          `gorm:"column:id;type:uuid;primaryKey;default:gen_random_uuid()" json:"id,omitempty"`
	Slug               string          `gorm:"column:slug" json:"slug"`
	Make               string          `gorm:"column:make" json:"make"`
	Model              string          `gorm:"column:model" json:"model"`
	VariantName        string          `gorm:"column:variant_name" json:"variant_name"`
	BodyType           BodyType        `gorm:"column:body_type" json:"body_type"`
	FuelType           FuelType        `gorm:"column:fuel_type" json:"fuel_type"`
	ExShowroomPriceINR *int64          `gorm:"column:ex_showroom_price_inr" json:"ex_showroom_price_inr"`
	LaunchYear         int             `gorm:"column:launch_year" json:"launch_year"`
	DiscontinuedYear   *int            `gorm:"column:discontinued_year" json:"discontinued_year"`
	ClaimedEfficiency  *float64        `gorm:"column:claimed_efficiency" json:"claimed_efficiency"`
	EfficiencyUnit     *string         `gorm:"column:efficiency_unit" json:"efficiency_unit"`
	BatteryKWh         *float64        `gorm:"column:battery_kwh" json:"battery_kwh"`
	ClaimedRangeKm     *int            `gorm:"column:claimed_range_km" json:"claimed_range_km"`
	SafetyRatingStars  *int            `gorm:"column:safety_rating_stars" json:"safety_rating_stars"`
	SafetyRatingBody   *string         `gorm:"column:safety_rating_body" json:"safety_rating_body"`
	Airbags            *int            `gorm:"column:airbags" json:"airbags"`
	BootLitres         *int            `gorm:"column:boot_litres" json:"boot_litres"`
	GroundClearanceMM  *int            `gorm:"column:ground_clearance_mm" json:"ground_clearance_mm"`
	PowerPS            *float64        `gorm:"column:power_ps" json:"power_ps"`
	KerbWeightKg       *int            `gorm:"column:kerb_weight_kg" json:"kerb_weight_kg"`
	Transmission       Transmission    `gorm:"column:transmission" json:"transmission"`
	Seats              int             `gorm:"column:seats" json:"seats"`
	ServiceIntervalKm  *int            `gorm:"column:service_interval_km" json:"service_interval_km"`
	WarrantyYears      *int            `gorm:"column:warranty_years" json:"warranty_years"`
	WarrantyKm         *int            `gorm:"column:warranty_km" json:"warranty_km"`
	KeyFeatures        store.TextArray `gorm:"column:key_features;type:text[]" json:"key_features"`
	Verified           bool            `gorm:"column:verified" json:"verified"`
	CreatedAt          time.Time       `gorm:"column:created_at" json:"created_at,omitzero"`
	UpdatedAt          time.Time       `gorm:"column:updated_at" json:"updated_at,omitzero"`

	Sources      []VariantSource `gorm:"foreignKey:VariantID" json:"sources,omitempty"`
	OnRoadPrices []OnRoadPrice   `gorm:"foreignKey:VariantID" json:"on_road_prices,omitempty"`
}

// TableName tells GORM the table name explicitly (instead of guessing).
func (Variant) TableName() string { return "variants" }

// DisplayName is the human-readable name, e.g. "Test Motors Alpha Base".
func (v Variant) DisplayName() string {
	return strings.Join([]string{v.Make, v.Model, v.VariantName}, " ")
}

var (
	slugPattern       = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	featureKeyPattern = regexp.MustCompile(`^[a-z0-9_]+$`)
)

// Validate checks every rule that the database CHECK constraints also enforce,
// so bad data is rejected with a clear message before it reaches Postgres. It
// returns the first problem found, as an apperr validation error naming the field.
func (v Variant) Validate() error {
	checks := []struct {
		ok    bool
		field string
		msg   string
	}{
		{slugPattern.MatchString(v.Slug), "slug", "must be lowercase words separated by hyphens"},
		{strings.TrimSpace(v.Make) != "", "make", "is required"},
		{strings.TrimSpace(v.Model) != "", "model", "is required"},
		{strings.TrimSpace(v.VariantName) != "", "variant_name", "is required"},
		{v.BodyType.valid(), "body_type", "is not a known body type"},
		{v.FuelType.Valid(), "fuel_type", "is not a known fuel type"},
		{v.Transmission.valid(), "transmission", "is not a known transmission"},
		{v.Seats >= 1 && v.Seats <= 9, "seats", "must be between 1 and 9"},
		{v.LaunchYear >= 1950 && v.LaunchYear <= 2100, "launch_year", "must be between 1950 and 2100"},
		{v.DiscontinuedYear == nil || *v.DiscontinuedYear >= v.LaunchYear, "discontinued_year", "must not be before launch_year"},
		{v.ExShowroomPriceINR == nil || *v.ExShowroomPriceINR > 0, "ex_showroom_price_inr", "must be positive"},
		{v.SafetyRatingStars == nil || (*v.SafetyRatingStars >= 0 && *v.SafetyRatingStars <= 5), "safety_rating_stars", "must be between 0 and 5"},
		{(v.SafetyRatingStars == nil) == (v.SafetyRatingBody == nil), "safety_rating_body", "must be given together with safety_rating_stars"},
		{(v.ClaimedEfficiency == nil) == (v.EfficiencyUnit == nil), "efficiency_unit", "must be given together with claimed_efficiency"},
		{v.EfficiencyUnit == nil || *v.EfficiencyUnit == v.FuelType.EfficiencyUnit(), "efficiency_unit", "does not match the fuel type"},
		{v.FuelType != FuelEV || v.BatteryKWh != nil, "battery_kwh", "is required for an EV"},
		{v.FuelType == FuelEV || (v.BatteryKWh == nil && v.ClaimedRangeKm == nil), "battery_kwh", "only EVs have a battery and range"},
		{positiveF(v.ClaimedEfficiency), "claimed_efficiency", "must be positive"},
		{positiveF(v.BatteryKWh), "battery_kwh", "must be positive"},
		{positiveI(v.ClaimedRangeKm), "claimed_range_km", "must be positive"},
		{nonNegativeI(v.Airbags), "airbags", "must not be negative"},
		{nonNegativeI(v.BootLitres), "boot_litres", "must not be negative"},
		{positiveI(v.GroundClearanceMM), "ground_clearance_mm", "must be positive"},
		{positiveF(v.PowerPS), "power_ps", "must be positive"},
		{positiveI(v.KerbWeightKg), "kerb_weight_kg", "must be positive"},
		{positiveI(v.ServiceIntervalKm), "service_interval_km", "must be positive"},
		{nonNegativeI(v.WarrantyYears), "warranty_years", "must not be negative"},
		{nonNegativeI(v.WarrantyKm), "warranty_km", "must not be negative"},
	}
	for _, c := range checks {
		if !c.ok {
			return apperr.Validation(c.field, c.field+" "+c.msg)
		}
	}

	if err := validateOnRoadPrices(v.OnRoadPrices); err != nil {
		return err
	}

	seen := make(map[string]bool, len(v.KeyFeatures))
	for _, f := range v.KeyFeatures {
		if !featureKeyPattern.MatchString(f) {
			return apperr.Validation("key_features", fmt.Sprintf("feature key %q must be lowercase letters, digits and underscores", f))
		}
		if seen[f] {
			return apperr.Validation("key_features", fmt.Sprintf("feature key %q is listed twice", f))
		}
		seen[f] = true
	}
	return nil
}

// Valid reports whether f is a known fuel type.
func (f FuelType) Valid() bool {
	for _, known := range AllFuelTypes() {
		if f == known {
			return true
		}
	}
	return false
}

// EfficiencyUnit is the unit claimed efficiency is expressed in for this fuel.
func (f FuelType) EfficiencyUnit() string {
	switch f {
	case FuelCNG:
		return UnitKmPerKg
	case FuelEV:
		return UnitKmPerKWh
	default:
		return UnitKmPerLitre
	}
}

func (b BodyType) valid() bool {
	switch b {
	case BodyHatchback, BodySedan, BodyCompactSUV, BodySUV, BodyMUV:
		return true
	}
	return false
}

func (t Transmission) valid() bool {
	switch t {
	case TransManual, TransAMT, TransCVT, TransDCT, TransTorqueConverter, TransSingleSpeed:
		return true
	}
	return false
}

func positiveF(p *float64) bool { return p == nil || *p > 0 }
func positiveI(p *int) bool     { return p == nil || *p > 0 }
func nonNegativeI(p *int) bool  { return p == nil || *p >= 0 }
