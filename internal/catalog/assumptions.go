package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
)

// SupportedStates are the Indian states the assumptions cover at launch
// (DECISIONS.md D-P-4). It is a function so callers can't modify a shared slice.
func SupportedStates() []string { return []string{"DL", "KA", "MH", "TN"} }

// IsSupportedState reports whether code is a supported state.
func IsSupportedState(code string) bool { return slices.Contains(SupportedStates(), code) }

// Assumption is one row of the assumptions table (SPEC §5.1).
type Assumption struct {
	ID           int64           `gorm:"column:id;primaryKey" json:"-"`
	Key          string          `gorm:"column:key" json:"key"`
	ValueJSON    json.RawMessage `gorm:"column:value_json;type:jsonb" json:"value"`
	SourceURL    string          `gorm:"column:source_url" json:"source_url"`
	CheckedAt    *Date           `gorm:"column:checked_at;type:date" json:"checked_at"`
	ModelVersion string          `gorm:"column:model_version" json:"model_version"`
	Verified     bool            `gorm:"column:verified" json:"verified"`
	CreatedAt    time.Time       `gorm:"column:created_at" json:"-"`
	UpdatedAt    time.Time       `gorm:"column:updated_at" json:"-"`
}

// TableName tells GORM the table name explicitly.
func (Assumption) TableName() string { return "assumptions" }

// Range is a closed interval [Lo, Hi], written in JSON as [lo, hi].
type Range struct{ Lo, Hi float64 }

// UnmarshalJSON reads [lo, hi].
func (r *Range) UnmarshalJSON(b []byte) error {
	var pair []float64
	if err := json.Unmarshal(b, &pair); err != nil {
		return err
	}
	if len(pair) != 2 || pair[0] > pair[1] {
		return fmt.Errorf("range must be [lo, hi] with lo <= hi, got %s", b)
	}
	r.Lo, r.Hi = pair[0], pair[1]
	return nil
}

// MarshalJSON writes [lo, hi].
func (r Range) MarshalJSON() ([]byte, error) { return json.Marshal([2]float64{r.Lo, r.Hi}) }

// PriceTrend parameterises a yearly log-normal price path:
// p(t+1) = p(t) × exp(Mu + Sigma·Z), Z ~ N(0,1) (SPEC §10).
type PriceTrend struct {
	Mu    float64 `json:"mu"`
	Sigma float64 `json:"sigma"`
}

// RoadTaxSlab applies Rate to the ex-showroom price when the price is at most
// UpToINR (nil means "no upper limit"). Slabs are checked in order.
type RoadTaxSlab struct {
	UpToINR *int64  `json:"up_to_inr"`
	Rate    float64 `json:"rate"`
}

// EfficiencyFactor gives the real-world/claimed efficiency ratio ranges for
// city and highway driving (SPEC §10: real = claimed × factor).
type EfficiencyFactor struct {
	City    Range `json:"city"`
	Highway Range `json:"highway"`
}

// UsedTransfer is the cost of transferring a used car: Rate × price + FixedINR.
type UsedTransfer struct {
	Rate     float64 `json:"rate"`
	FixedINR float64 `json:"fixed_inr"`
}

// RepairModel parameterises repairs (SPEC §10): the yearly number of events is
// Poisson(λ), λ = base[fuel] × (1 + AgeSlope·age) × (1 + OdoSlopePer100k·odo/100000)
// × grade multiplier × (UsedMultiplier if bought used); each event costs
// LogNormal(CostMu, CostSigma). Inside warranty, WarrantyCoverage of the cost is covered.
type RepairModel struct {
	BaseRatePerYear  map[FuelType]float64 `json:"base_rate_per_year"`
	AgeSlope         float64              `json:"age_slope"`
	OdoSlopePer100k  float64              `json:"odo_slope_per_100k"`
	GradeMultiplier  map[string]float64   `json:"grade_multiplier"`
	UsedMultiplier   float64              `json:"used_multiplier"`
	CostMu           float64              `json:"cost_mu"`
	CostSigma        float64              `json:"cost_sigma"`
	WarrantyCoverage float64              `json:"warranty_coverage"`
}

// TyreModel: a set of tyres costs SetCostINR[segment] and lasts LifeKm.
type TyreModel struct {
	SetCostINR map[Segment]float64 `json:"set_cost_inr"`
	LifeKm     float64             `json:"life_km"`
}

// ResaleShock is the log-normal noise on resale values: one market-wide draw
// per fuel type per future (shared by cars) and one per car.
type ResaleShock struct {
	MarketSigma float64 `json:"market_sigma"`
	IdioSigma   float64 `json:"idio_sigma"`
}

// AssumptionRef records where one assumption came from, for display and for the
// "placeholder assumptions" warning.
type AssumptionRef struct {
	Key       string `json:"key"`
	SourceURL string `json:"source_url"`
	CheckedAt *Date  `json:"checked_at"`
	Verified  bool   `json:"verified"`
}

// AssumptionSet is every assumption the cost model needs for one state, typed
// and validated. It is serialised into each simulation run (Snapshot), so old
// results stay reproducible after the table changes (SPEC §5.1).
type AssumptionSet struct {
	State        string `json:"state"`
	ModelVersion string `json:"model_version"`

	// State-specific.
	FuelPriceINR        map[FuelType]float64       `json:"fuel_price_inr"` // per litre (petrol, diesel) or kg (cng)
	HomeTariffINR       float64                    `json:"home_tariff_inr_per_kwh"`
	PublicTariffINR     float64                    `json:"public_tariff_inr_per_kwh"`
	RoadTax             map[FuelType][]RoadTaxSlab `json:"road_tax"`
	RegistrationMiscINR float64                    `json:"registration_misc_inr"`
	UsedTransfer        UsedTransfer               `json:"used_transfer"`

	// Common to all states.
	PriceTrend           map[string]PriceTrend            `json:"price_trend"` // petrol, diesel, cng, electricity
	FuelTrendSweep       Range                            `json:"fuel_trend_sweep"`
	EfficiencyFactor     map[FuelType]EfficiencyFactor    `json:"efficiency_factor"`
	ChargingEfficiency   float64                          `json:"charging_efficiency"`
	KmYearSigma          float64                          `json:"km_year_sigma"`
	InsurancePremiumRate map[FuelType]float64             `json:"insurance_premium_rate"`
	IDVSchedule          []float64                        `json:"idv_schedule"`
	ServiceCostPerVisit  map[Segment]map[FuelType]float64 `json:"service_cost_per_visit"`
	ServiceMinVisits     float64                          `json:"service_min_visits_per_year"`
	GeneralInflation     float64                          `json:"general_inflation"`
	Repairs              RepairModel                      `json:"repairs"`
	Tyres                TyreModel                        `json:"tyres"`
	ResaleCurve          map[FuelType][]float64           `json:"resale_curve"` // fraction of purchase price at age 1..N
	ResaleShock          ResaleShock                      `json:"resale_shock"`
	EVBatteryWearPerYear float64                          `json:"ev_battery_wear_per_year"`

	Provenance []AssumptionRef `json:"provenance"`
}

// EnergyKeys are the price-path names used by PriceTrend.
func EnergyKeys() []string { return []string{"petrol", "diesel", "cng", "electricity"} }

// assumptionSpec describes one expected key: its name and where to decode it.
type assumptionSpec struct {
	key    string
	decode func(raw json.RawMessage, s *AssumptionSet) error
}

// specsFor lists every key an AssumptionSet for state needs, in a fixed order.
func specsFor(state string) []assumptionSpec {
	var specs []assumptionSpec
	add := func(key string, decode func(json.RawMessage, *AssumptionSet) error) {
		specs = append(specs, assumptionSpec{key, decode})
	}
	field := func(name string, dst func(*AssumptionSet) *float64) func(json.RawMessage, *AssumptionSet) error {
		return func(raw json.RawMessage, s *AssumptionSet) error {
			var m map[string]float64
			if err := json.Unmarshal(raw, &m); err != nil {
				return err
			}
			v, ok := m[name]
			if !ok {
				return fmt.Errorf("missing field %q", name)
			}
			*dst(s) = v
			return nil
		}
	}
	into := func(dst func(*AssumptionSet) any) func(json.RawMessage, *AssumptionSet) error {
		return func(raw json.RawMessage, s *AssumptionSet) error { return strictUnmarshal(raw, dst(s)) }
	}

	// State-specific keys.
	for _, f := range []struct {
		fuel FuelType
		unit string
	}{{FuelPetrol, "inr_per_litre"}, {FuelDiesel, "inr_per_litre"}, {FuelCNG, "inr_per_kg"}} {
		fuel, unit := f.fuel, f.unit
		add("fuel_price."+string(fuel)+"."+state, func(raw json.RawMessage, s *AssumptionSet) error {
			var v float64
			if err := field(unit, func(*AssumptionSet) *float64 { return &v })(raw, s); err != nil {
				return err
			}
			s.FuelPriceINR[fuel] = v
			return nil
		})
	}
	add("electricity_tariff."+state, field("inr_per_kwh", func(s *AssumptionSet) *float64 { return &s.HomeTariffINR }))
	add("public_charging_tariff."+state, field("inr_per_kwh", func(s *AssumptionSet) *float64 { return &s.PublicTariffINR }))
	add("road_tax."+state+".rule", into(func(s *AssumptionSet) any { return &s.RoadTax }))
	add("registration_misc."+state, field("inr", func(s *AssumptionSet) *float64 { return &s.RegistrationMiscINR }))
	add("used_transfer."+state+".rule", into(func(s *AssumptionSet) any { return &s.UsedTransfer }))

	// Common keys.
	for _, e := range EnergyKeys() {
		energy := e
		add("price_trend."+energy, func(raw json.RawMessage, s *AssumptionSet) error {
			var pt PriceTrend
			if err := strictUnmarshal(raw, &pt); err != nil {
				return err
			}
			s.PriceTrend[energy] = pt
			return nil
		})
	}
	add("price_trend.sweep", func(raw json.RawMessage, s *AssumptionSet) error {
		var v struct {
			MuRange Range `json:"mu_range"`
		}
		if err := strictUnmarshal(raw, &v); err != nil {
			return err
		}
		s.FuelTrendSweep = v.MuRange
		return nil
	})
	for _, f := range AllFuelTypes() {
		fuel := f
		add("real_efficiency_factor."+string(fuel), func(raw json.RawMessage, s *AssumptionSet) error {
			var ef EfficiencyFactor
			if err := strictUnmarshal(raw, &ef); err != nil {
				return err
			}
			s.EfficiencyFactor[fuel] = ef
			return nil
		})
		add("resale.curve."+string(fuel), func(raw json.RawMessage, s *AssumptionSet) error {
			var v struct {
				Fractions []float64 `json:"fractions"`
			}
			if err := strictUnmarshal(raw, &v); err != nil {
				return err
			}
			s.ResaleCurve[fuel] = v.Fractions
			return nil
		})
	}
	add("ev.charging_efficiency", field("value", func(s *AssumptionSet) *float64 { return &s.ChargingEfficiency }))
	add("ev.battery_wear", field("per_year", func(s *AssumptionSet) *float64 { return &s.EVBatteryWearPerYear }))
	add("usage.km_year_sigma", field("value", func(s *AssumptionSet) *float64 { return &s.KmYearSigma }))
	add("insurance.premium_rate", into(func(s *AssumptionSet) any { return &s.InsurancePremiumRate }))
	add("insurance.idv_schedule", func(raw json.RawMessage, s *AssumptionSet) error {
		var v struct {
			Factors []float64 `json:"factors"`
		}
		if err := strictUnmarshal(raw, &v); err != nil {
			return err
		}
		s.IDVSchedule = v.Factors
		return nil
	})
	add("service.cost_per_visit", into(func(s *AssumptionSet) any { return &s.ServiceCostPerVisit }))
	add("service.min_visits_per_year", field("value", func(s *AssumptionSet) *float64 { return &s.ServiceMinVisits }))
	add("general_inflation.rate", field("value", func(s *AssumptionSet) *float64 { return &s.GeneralInflation }))
	add("repairs.model", into(func(s *AssumptionSet) any { return &s.Repairs }))
	add("tyres.model", into(func(s *AssumptionSet) any { return &s.Tyres }))
	add("resale.shock", into(func(s *AssumptionSet) any { return &s.ResaleShock }))
	return specs
}

// RequiredAssumptionKeys lists every key needed for state, in a fixed order.
func RequiredAssumptionKeys(state string) []string {
	specs := specsFor(state)
	keys := make([]string, len(specs))
	for i, s := range specs {
		keys[i] = s.key
	}
	return keys
}

// BuildAssumptionSet decodes and validates rows into a typed set for state.
// Every required key must be present; unknown extra rows are ignored.
func BuildAssumptionSet(state, modelVersion string, rows []Assumption) (AssumptionSet, error) {
	if !IsSupportedState(state) {
		return AssumptionSet{}, apperr.Validation("user_state", fmt.Sprintf("state %q is not supported; use one of %v", state, SupportedStates()))
	}
	byKey := make(map[string]Assumption, len(rows))
	for _, r := range rows {
		byKey[r.Key] = r
	}

	s := AssumptionSet{
		State: state, ModelVersion: modelVersion,
		FuelPriceINR:     map[FuelType]float64{},
		PriceTrend:       map[string]PriceTrend{},
		EfficiencyFactor: map[FuelType]EfficiencyFactor{},
		ResaleCurve:      map[FuelType][]float64{},
	}
	var errs []error
	for _, spec := range specsFor(state) {
		row, ok := byKey[spec.key]
		if !ok {
			errs = append(errs, fmt.Errorf("missing assumption %q", spec.key))
			continue
		}
		if err := spec.decode(row.ValueJSON, &s); err != nil {
			errs = append(errs, fmt.Errorf("assumption %q: %w", spec.key, err))
			continue
		}
		s.Provenance = append(s.Provenance, AssumptionRef{
			Key: row.Key, SourceURL: row.SourceURL, CheckedAt: row.CheckedAt, Verified: row.Verified,
		})
	}
	if len(errs) == 0 {
		errs = append(errs, s.validate()...)
	}
	if err := errors.Join(errs...); err != nil {
		return AssumptionSet{}, fmt.Errorf("assumptions for %s/%s are incomplete or invalid: %w", state, modelVersion, err)
	}
	return s, nil
}

// validate checks the values make sense (positive prices, probabilities in
// range, every fuel/segment present), so the engine never sees nonsense.
func (s AssumptionSet) validate() []error {
	var errs []error
	check := func(ok bool, format string, args ...any) {
		if !ok {
			errs = append(errs, fmt.Errorf(format, args...))
		}
	}
	for _, f := range []FuelType{FuelPetrol, FuelDiesel, FuelCNG} {
		check(s.FuelPriceINR[f] > 0, "fuel price for %s must be positive", f)
	}
	check(s.HomeTariffINR > 0 && s.PublicTariffINR > 0, "electricity tariffs must be positive")
	check(s.RegistrationMiscINR >= 0, "registration_misc must not be negative")
	check(s.UsedTransfer.Rate >= 0 && s.UsedTransfer.FixedINR >= 0, "used transfer cost must not be negative")
	check(s.ChargingEfficiency > 0 && s.ChargingEfficiency <= 1, "charging efficiency must be in (0, 1]")
	check(s.KmYearSigma >= 0, "km_year_sigma must not be negative")
	check(s.ServiceMinVisits >= 0, "service min visits must not be negative")
	check(s.EVBatteryWearPerYear >= 0 && s.EVBatteryWearPerYear < 1, "battery wear must be in [0, 1)")
	check(len(s.IDVSchedule) > 0, "idv schedule must not be empty")
	check(s.Tyres.LifeKm > 0, "tyre life must be positive")
	check(s.Repairs.CostSigma >= 0 && s.Repairs.WarrantyCoverage >= 0 && s.Repairs.WarrantyCoverage <= 1, "repair model values out of range")
	check(s.ResaleShock.MarketSigma >= 0 && s.ResaleShock.IdioSigma >= 0, "resale shock sigmas must not be negative")
	check(s.FuelTrendSweep.Lo < s.FuelTrendSweep.Hi, "price_trend.sweep mu_range must have lo < hi")
	for _, e := range EnergyKeys() {
		check(s.PriceTrend[e].Sigma >= 0, "price trend sigma for %s must not be negative", e)
	}
	for _, f := range AllFuelTypes() {
		ef := s.EfficiencyFactor[f]
		check(ef.City.Lo > 0 && ef.Highway.Lo > 0, "efficiency factor for %s must be positive", f)
		check(len(s.RoadTax[f]) > 0, "road tax slabs missing for %s", f)
		check(s.InsurancePremiumRate[f] > 0, "insurance premium rate missing for %s", f)
		check(s.Repairs.BaseRatePerYear[f] >= 0, "repair base rate missing for %s", f)
		curve := s.ResaleCurve[f]
		check(len(curve) > 0, "resale curve missing for %s", f)
		for i, frac := range curve {
			check(frac >= 0 && frac <= 1, "resale curve for %s age %d must be in [0, 1]", f, i+1)
		}
		for _, seg := range []Segment{SegmentSmall, SegmentMedium, SegmentLarge} {
			check(s.ServiceCostPerVisit[seg][f] > 0, "service cost missing for %s/%s", seg, f)
		}
	}
	for _, seg := range []Segment{SegmentSmall, SegmentMedium, SegmentLarge} {
		check(s.Tyres.SetCostINR[seg] > 0, "tyre set cost missing for %s", seg)
	}
	for _, g := range []string{"good", "fair", "poor"} {
		check(s.Repairs.GradeMultiplier[g] > 0, "repair grade multiplier missing for %s", g)
	}
	return errs
}

// RoadTaxINR returns the road tax for a new car of this fuel and ex-showroom
// price, using the first slab whose upper limit covers the price.
func (s AssumptionSet) RoadTaxINR(fuel FuelType, exShowroomINR float64) float64 {
	for _, slab := range s.RoadTax[fuel] {
		if slab.UpToINR == nil || exShowroomINR <= float64(*slab.UpToINR) {
			return slab.Rate * exShowroomINR
		}
	}
	return 0
}

// UnverifiedKeys lists the keys whose values are not verified (placeholders),
// sorted, for the "placeholder assumptions" warning.
func (s AssumptionSet) UnverifiedKeys() []string {
	var keys []string
	for _, p := range s.Provenance {
		if !p.Verified {
			keys = append(keys, p.Key)
		}
	}
	slices.Sort(keys)
	return keys
}

// Snapshot serialises the set for storage in a simulation run.
func (s AssumptionSet) Snapshot() (json.RawMessage, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("snapshotting assumptions: %w", err)
	}
	return b, nil
}

// LoadAssumptionSet reads the rows for state and modelVersion from the database
// and builds the typed set.
func (s *Service) LoadAssumptionSet(ctx context.Context, state, modelVersion string) (AssumptionSet, error) {
	if !IsSupportedState(state) {
		return AssumptionSet{}, apperr.Validation("user_state", fmt.Sprintf("state %q is not supported", state))
	}
	var rows []Assumption
	if err := s.db.WithContext(ctx).
		Where("model_version = ? AND key IN ?", modelVersion, RequiredAssumptionKeys(state)).
		Find(&rows).Error; err != nil {
		return AssumptionSet{}, fmt.Errorf("loading assumptions: %w", err)
	}
	return BuildAssumptionSet(state, modelVersion, rows)
}

// ListAssumptions returns all assumption rows for a model version, for display.
func (s *Service) ListAssumptions(ctx context.Context, modelVersion string) ([]Assumption, error) {
	var rows []Assumption
	if err := s.db.WithContext(ctx).Where("model_version = ?", modelVersion).Order("key").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("listing assumptions: %w", err)
	}
	return rows, nil
}

// strictUnmarshal decodes JSON and rejects unknown fields, so a typo in a seed
// file ("sigam") is an error instead of a silent zero.
func strictUnmarshal(raw json.RawMessage, dst any) error {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}
