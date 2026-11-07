package sim

import (
	"errors"
	"fmt"
)

// MaxCars is the most cars one simulation accepts. The product allows 4 in a
// shortlist; the engine allows a little more headroom for tests.
const MaxCars = 8

// ErrInvalidInputs wraps every validation failure, so callers can tell bad
// inputs (a bug upstream) from cancellation.
var ErrInvalidInputs = errors.New("invalid simulation inputs")

// Validate checks that the inputs are complete and physically meaningful.
func (in Inputs) Validate() error {
	var errs []error
	check := func(ok bool, format string, args ...any) {
		if !ok {
			errs = append(errs, fmt.Errorf(format, args...))
		}
	}

	check(len(in.Cars) >= 1 && len(in.Cars) <= MaxCars, "need 1–%d cars, got %d", MaxCars, len(in.Cars))
	check(in.HorizonYears >= 1 && in.HorizonYears <= MaxHorizonYears, "horizon must be 1–%d years, got %d", MaxHorizonYears, in.HorizonYears)
	check(in.DiscountRate >= 0 && in.DiscountRate < 1, "discount rate must be in [0, 1), got %g", in.DiscountRate)

	ids := make(map[string]bool, len(in.Cars))
	for i, c := range in.Cars {
		check(c.ID != "", "car %d: id is required", i)
		check(!ids[c.ID], "car %d: duplicate id %q", i, c.ID)
		ids[c.ID] = true
		check(c.Fuel.index() >= 0, "car %s: unknown fuel %q", c.ID, c.Fuel)
		check(c.UpfrontINR > 0, "car %s: upfront price must be positive", c.ID)
		check(c.ValueBasisINR > 0, "car %s: value basis must be positive", c.ID)
		check(c.AgeYears >= 0 && c.OdometerKm >= 0, "car %s: age and odometer must not be negative", c.ID)
		check(c.ClaimedEfficiency > 0, "car %s: claimed efficiency must be positive", c.ID)
		check(validRange(c.EfficiencyFactor.City) && c.EfficiencyFactor.City.Lo > 0, "car %s: invalid city efficiency factor", c.ID)
		check(validRange(c.EfficiencyFactor.Highway) && c.EfficiencyFactor.Highway.Lo > 0, "car %s: invalid highway efficiency factor", c.ID)
		check(c.ServiceIntervalKm > 0, "car %s: service interval must be positive", c.ID)
		check(c.ServiceCostPerVisit >= 0 && c.TyreSetCostINR >= 0 && c.InsuranceRate >= 0, "car %s: costs must not be negative", c.ID)
		check(c.RepairBaseRate >= 0 && c.RepairGradeMultiplier >= 0, "car %s: repair rates must not be negative", c.ID)
		check(c.WarrantyYears >= 0 && c.WarrantyKm >= 0, "car %s: warranty must not be negative", c.ID)
		check(len(c.ResaleCurve) > 0, "car %s: resale curve is empty", c.ID)
		for k, f := range c.ResaleCurve {
			check(f >= 0 && f <= 1, "car %s: resale curve age %d must be in [0, 1]", c.ID, k+1)
		}
	}

	u := in.Usage
	check(validRange(u.KmPerMonth) && u.KmPerMonth.Lo >= 0, "km per month must be a valid non-negative range")
	check(validRange(u.CityShare) && u.CityShare.Lo >= 0 && u.CityShare.Hi <= 1, "city share must be a range within [0, 1]")
	if u.Loan != nil {
		check(u.Loan.PrincipalINR >= 0, "loan principal must not be negative")
		check(u.Loan.AnnualRate >= 0 && u.Loan.AnnualRate < 1, "loan rate must be in [0, 1)")
		check(u.Loan.TenureMonths >= 1 && u.Loan.TenureMonths <= 360, "loan tenure must be 1–360 months")
	}

	w := in.World
	for name, v := range map[string]float64{
		"petrol price": w.PetrolPriceINR, "diesel price": w.DieselPriceINR, "cng price": w.CNGPriceINR,
		"home tariff": w.HomeTariffINR, "public tariff": w.PublicTariffINR,
	} {
		check(v > 0, "%s must be positive", name)
	}
	for name, tr := range map[string]PriceTrend{"petrol": w.PetrolTrend, "diesel": w.DieselTrend, "cng": w.CNGTrend, "electricity": w.ElectricityTrend} {
		check(tr.Sigma >= 0, "%s trend sigma must not be negative", name)
	}
	check(w.KmYearSigma >= 0, "km year sigma must not be negative")
	check(w.ChargingEfficiency > 0 && w.ChargingEfficiency <= 1, "charging efficiency must be in (0, 1]")
	check(len(w.IDVSchedule) > 0, "IDV schedule is empty")
	for _, f := range w.IDVSchedule {
		check(f > 0 && f <= 1, "IDV schedule values must be in (0, 1]")
	}
	check(w.ServiceMinVisits >= 0 && w.GeneralInflation > -1, "invalid service/inflation values")
	check(w.RepairCostSigma >= 0 && w.RepairUsedMultiplier >= 0, "invalid repair parameters")
	check(w.WarrantyCoverage >= 0 && w.WarrantyCoverage <= 1, "warranty coverage must be in [0, 1]")
	check(w.TyreLifeKm > 0, "tyre life must be positive")
	check(w.ResaleMarketSigma >= 0 && w.ResaleIdioSigma >= 0, "resale sigmas must not be negative")
	check(w.EVBatteryWearPerYear >= 0 && w.EVBatteryWearPerYear < 1, "battery wear must be in [0, 1)")

	keys := make(map[string]bool, len(in.Prefs))
	for _, p := range in.Prefs {
		check(p.Key != "", "preference key is required")
		check(!keys[p.Key], "duplicate preference key %q", p.Key)
		keys[p.Key] = true
		check(len(p.Shares) == len(in.Cars), "preference %q: need one share per car", p.Key)
		for _, s := range p.Shares {
			check(s >= 0 && s <= 1, "preference %q: shares must be in [0, 1]", p.Key)
		}
		check(len(p.Brackets) > 0, "preference %q: at least one bracket is required", p.Key)
		for _, b := range p.Brackets {
			check(validRange(b) && b.Lo >= 0, "preference %q: brackets must be non-negative ranges", p.Key)
		}
	}

	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInputs, err)
	}
	return nil
}

func (o Options) validate() error {
	if o.Futures < 1 || o.ChunkSize < 1 || o.Workers < 0 {
		return fmt.Errorf("%w: futures and chunk size must be positive and workers non-negative", ErrInvalidInputs)
	}
	return nil
}

func validRange(r Range) bool { return r.Lo <= r.Hi }
