package session

import (
	"fmt"
	"time"

	"github.com/xoBerlierxo/TieBreaker/internal/catalog"
	"github.com/xoBerlierxo/TieBreaker/internal/interview"
	"github.com/xoBerlierxo/TieBreaker/internal/sim"
)

// Data warnings attached to simulation inputs and reports (SPEC §9.3).
const (
	WarnUnverifiedCar       = "unverified_data"
	WarnSafetyNotRated      = "safety_not_rated"
	WarnServiceIntervalDflt = "service_interval_assumed"
	WarnPlaceholderAssumpt  = "placeholder_assumptions"
)

// CarWarning is a warning about one car.
type CarWarning struct {
	ItemID string `json:"item_id"`
	Code   string `json:"code"`
	Text   string `json:"text"`
}

// SurvivingCar is a car that passed the constraints, with everything the
// engine needs to know about it.
type SurvivingCar struct {
	Item    Item
	Variant catalog.Variant
	Price   Price
	Label   string
}

// InputsBuild is the result of turning a session into engine inputs.
type InputsBuild struct {
	Inputs     sim.Inputs
	Cars       []SurvivingCar
	Attributes []interview.Attribute // trade-off candidates for the surviving cars
	Warnings   []CarWarning
}

// BuildInputs resolves catalogue data and assumptions into fully specified
// engine inputs (DECISIONS.md D-M2-1). The cars must be the survivors, in
// shortlist order; prefs are the answered trade-offs (may be empty).
func BuildInputs(cars []SurvivingCar, set catalog.AssumptionSet, usage interview.Usage, c Constraints,
	tradeoffs interview.TradeoffState, ladder interview.Ladder, discountRate float64, now time.Time,
) (InputsBuild, error) {
	if len(cars) == 0 {
		return InputsBuild{}, fmt.Errorf("no cars to simulate")
	}
	homeCharging := c.HomeCharging != nil && *c.HomeCharging

	var b InputsBuild
	variants := make([]catalog.Variant, len(cars))
	for i, sc := range cars {
		v := sc.Variant
		variants[i] = v
		eff, ok := v.Efficiency()
		if !ok {
			return InputsBuild{}, fmt.Errorf("%s has no efficiency data", v.DisplayName())
		}
		seg := v.Segment()
		car := sim.Car{
			ID:                  sc.Item.ID,
			Name:                sc.Label,
			Fuel:                sim.Fuel(v.FuelType),
			UpfrontINR:          sc.Price.UpfrontINR,
			ValueBasisINR:       sc.Price.ValueBasisINR,
			ClaimedEfficiency:   eff,
			EfficiencyFactor:    simFactor(set.EfficiencyFactor[v.FuelType]),
			ServiceIntervalKm:   set.ServiceDefaultKm,
			ServiceCostPerVisit: set.ServiceCostPerVisit[seg][v.FuelType],
			TyreSetCostINR:      set.Tyres.SetCostINR[seg],
			InsuranceRate:       set.InsurancePremiumRate[v.FuelType],
			RepairBaseRate:      set.Repairs.BaseRatePerYear[v.FuelType],
			ResaleCurve:         set.ResaleCurve[v.FuelType],

			FirstYearInsuranceIncluded: sc.Price.InsuranceIncluded,
		}
		if v.ServiceIntervalKm != nil {
			car.ServiceIntervalKm = float64(*v.ServiceIntervalKm)
		} else {
			b.Warnings = append(b.Warnings, CarWarning{sc.Item.ID, WarnServiceIntervalDflt,
				fmt.Sprintf("%s: service interval unknown, %.0f km assumed", sc.Label, set.ServiceDefaultKm)})
		}
		if v.WarrantyYears != nil {
			car.WarrantyYears = float64(*v.WarrantyYears)
		}
		switch {
		case v.WarrantyKm != nil:
			car.WarrantyKm = float64(*v.WarrantyKm)
		case v.WarrantyYears != nil:
			// A year limit with no km limit ("unlimited km", or km not
			// published): the warranty is limited by years only. Without this,
			// a missing km limit became 0 km and the warranty never applied.
			car.WarrantyKm = unlimitedWarrantyKm
		}
		if sc.Item.Condition == ConditionUsed {
			car.Used = true
			car.AgeYears = max(0, now.Year()-*sc.Item.Year)
			car.OdometerKm = float64(*sc.Item.OdometerKm)
			car.RepairGradeMultiplier = set.Repairs.GradeMultiplier[*sc.Item.ConditionGrade]
		} else {
			car.RepairGradeMultiplier = set.Repairs.GradeMultiplier["good"]
		}
		b.Inputs.Cars = append(b.Inputs.Cars, car)

		if !v.Verified {
			b.Warnings = append(b.Warnings, CarWarning{sc.Item.ID, WarnUnverifiedCar, sc.Label + ": catalogue data is not verified"})
		}
		if v.SafetyRatingStars == nil {
			b.Warnings = append(b.Warnings, CarWarning{sc.Item.ID, WarnSafetyNotRated, sc.Label + ": not rated for safety"})
		}
	}
	if n := len(set.UnverifiedKeys()); n > 0 {
		b.Warnings = append(b.Warnings, CarWarning{"", WarnPlaceholderAssumpt,
			fmt.Sprintf("%d model assumptions are placeholders, not sourced values", n)})
	}

	b.Inputs.Usage = sim.Usage{
		KmPerMonth:   sim.Range{Lo: usage.KmPerMonth[0], Hi: usage.KmPerMonth[1]},
		CityShare:    sim.Range{Lo: usage.CitySharePct[0] / 100, Hi: usage.CitySharePct[1] / 100},
		HomeCharging: homeCharging,
	}
	if usage.Loan != nil {
		b.Inputs.Usage.Loan = &sim.Loan{
			PrincipalINR: usage.Loan.PrincipalINR,
			AnnualRate:   usage.Loan.AnnualRatePct / 100,
			TenureMonths: usage.Loan.TenureMonths,
		}
	}
	b.Inputs.World = WorldFrom(set)
	b.Inputs.HorizonYears = usage.OwnershipYears
	b.Inputs.DiscountRate = discountRate

	b.Attributes = interview.Candidates(variants)
	b.Inputs.Prefs = interview.PrefsFor(b.Attributes, tradeoffs, ladder)
	b.Cars = cars

	if err := b.Inputs.Validate(); err != nil {
		return InputsBuild{}, fmt.Errorf("building simulation inputs: %w", err)
	}
	return b, nil
}

// unlimitedWarrantyKm stands for "no km limit". A large finite number rather
// than +Inf, because the inputs are stored as JSON, which has no infinity.
const unlimitedWarrantyKm = 1e9

// WorldFrom maps the assumption set onto the engine's shared parameters.
func WorldFrom(set catalog.AssumptionSet) sim.World {
	trend := func(k string) sim.PriceTrend {
		t := set.PriceTrend[k]
		return sim.PriceTrend{Mu: t.Mu, Sigma: t.Sigma}
	}
	return sim.World{
		PetrolPriceINR:        set.FuelPriceINR[catalog.FuelPetrol],
		DieselPriceINR:        set.FuelPriceINR[catalog.FuelDiesel],
		CNGPriceINR:           set.FuelPriceINR[catalog.FuelCNG],
		HomeTariffINR:         set.HomeTariffINR,
		PublicTariffINR:       set.PublicTariffINR,
		PetrolTrend:           trend("petrol"),
		DieselTrend:           trend("diesel"),
		CNGTrend:              trend("cng"),
		ElectricityTrend:      trend("electricity"),
		KmYearSigma:           set.KmYearSigma,
		ChargingEfficiency:    set.ChargingEfficiency,
		IDVSchedule:           set.IDVSchedule,
		ServiceMinVisits:      set.ServiceMinVisits,
		GeneralInflation:      set.GeneralInflation,
		RepairAgeSlope:        set.Repairs.AgeSlope,
		RepairOdoSlopePer100k: set.Repairs.OdoSlopePer100k,
		RepairUsedMultiplier:  set.Repairs.UsedMultiplier,
		RepairCostMu:          set.Repairs.CostMu,
		RepairCostSigma:       set.Repairs.CostSigma,
		WarrantyCoverage:      set.Repairs.WarrantyCoverage,
		TyreLifeKm:            set.Tyres.LifeKm,
		ResaleMarketSigma:     set.ResaleShock.MarketSigma,
		ResaleIdioSigma:       set.ResaleShock.IdioSigma,
		EVBatteryWearPerYear:  set.EVBatteryWearPerYear,
	}
}

func simFactor(f catalog.EfficiencyFactor) sim.EfficiencyFactor {
	return sim.EfficiencyFactor{
		City:    sim.Range{Lo: f.City.Lo, Hi: f.City.Hi},
		Highway: sim.Range{Lo: f.Highway.Lo, Hi: f.Highway.Hi},
	}
}
