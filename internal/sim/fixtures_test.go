package sim

import (
	"math"
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the package if any test leaves a goroutine running (SPEC §15).
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// All numbers below are fictional test values, not real car data (SPEC §7).

// deterministicWorld has every source of randomness switched off: σ = 0,
// fixed factors, no repairs, no resale shocks.
func deterministicWorld() World {
	return World{
		PetrolPriceINR: 100, DieselPriceINR: 90, CNGPriceINR: 80, HomeTariffINR: 8, PublicTariffINR: 20,
		PetrolTrend:        PriceTrend{Mu: math.Log(1.1), Sigma: 0}, // +10 % a year, exactly
		DieselTrend:        PriceTrend{},
		CNGTrend:           PriceTrend{},
		ElectricityTrend:   PriceTrend{},
		KmYearSigma:        0,
		ChargingEfficiency: 0.9,
		IDVSchedule:        []float64{0.95, 0.85},
		ServiceMinVisits:   0,
		GeneralInflation:   0.05,
		RepairCostMu:       math.Log(8000), RepairCostSigma: 0, RepairUsedMultiplier: 1,
		TyreLifeKm: 40_000,
	}
}

func handCar() Car {
	fixed := Range{0.75, 0.75}
	return Car{
		ID: "car-a", Name: "Test Car A", Fuel: FuelPetrol,
		UpfrontINR: 500_000, ValueBasisINR: 400_000,
		ClaimedEfficiency: 20, EfficiencyFactor: EfficiencyFactor{City: fixed, Highway: fixed},
		ServiceIntervalKm: 10_000, ServiceCostPerVisit: 4_000, TyreSetCostINR: 20_000,
		InsuranceRate: 0.03, RepairBaseRate: 0, RepairGradeMultiplier: 1,
		ResaleCurve: []float64{0.8, 0.7},
	}
}

func handInputs() Inputs {
	return Inputs{
		Cars:         []Car{handCar()},
		Usage:        Usage{KmPerMonth: Range{1000, 1000}, CityShare: Range{0.5, 0.5}},
		World:        deterministicWorld(),
		HorizonYears: 2,
		DiscountRate: 0.1,
	}
}

// stochasticInputs is a realistic three-car case with all randomness on:
// a petrol hatch, an EV and a used CNG sedan.
func stochasticInputs() Inputs {
	w := World{
		PetrolPriceINR: 100, DieselPriceINR: 90, CNGPriceINR: 80, HomeTariffINR: 7, PublicTariffINR: 18,
		PetrolTrend: PriceTrend{0.03, 0.05}, DieselTrend: PriceTrend{0.03, 0.05},
		CNGTrend: PriceTrend{0.03, 0.06}, ElectricityTrend: PriceTrend{0.02, 0.03},
		KmYearSigma: 0.1, ChargingEfficiency: 0.88,
		IDVSchedule:      []float64{0.95, 0.85, 0.8, 0.7, 0.6, 0.5},
		ServiceMinVisits: 1, GeneralInflation: 0.05,
		RepairAgeSlope: 0.15, RepairOdoSlopePer100k: 0.5, RepairUsedMultiplier: 1.2,
		RepairCostMu: math.Log(8000), RepairCostSigma: 0.8, WarrantyCoverage: 0.8,
		TyreLifeKm:        40_000,
		ResaleMarketSigma: 0.08, ResaleIdioSigma: 0.1, EVBatteryWearPerYear: 0.02,
	}
	curve := []float64{0.85, 0.75, 0.67, 0.6, 0.54, 0.49, 0.44, 0.4, 0.36, 0.32}
	petrol := Car{
		ID: "petrol", Fuel: FuelPetrol, UpfrontINR: 700_000, ValueBasisINR: 600_000,
		ClaimedEfficiency: 20, EfficiencyFactor: EfficiencyFactor{Range{0.65, 0.8}, Range{0.8, 0.95}},
		ServiceIntervalKm: 10_000, ServiceCostPerVisit: 4_000, TyreSetCostINR: 16_000, InsuranceRate: 0.03,
		RepairBaseRate: 0.3, RepairGradeMultiplier: 1, WarrantyYears: 2, WarrantyKm: 40_000, ResaleCurve: curve,
	}
	ev := Car{
		ID: "ev", Fuel: FuelEV, UpfrontINR: 1_500_000, ValueBasisINR: 1_400_000,
		ClaimedEfficiency: 10, EfficiencyFactor: EfficiencyFactor{Range{0.75, 0.9}, Range{0.65, 0.8}},
		ServiceIntervalKm: 15_000, ServiceCostPerVisit: 3_750, TyreSetCostINR: 24_000, InsuranceRate: 0.035,
		RepairBaseRate: 0.2, RepairGradeMultiplier: 1, WarrantyYears: 3, WarrantyKm: 100_000,
		ResaleCurve: []float64{0.8, 0.68, 0.58, 0.5, 0.43, 0.37, 0.32, 0.28, 0.24, 0.21},
	}
	cng := Car{
		ID: "cng", Fuel: FuelCNG, UpfrontINR: 600_000, ValueBasisINR: 580_000,
		Used: true, AgeYears: 3, OdometerKm: 45_000,
		ClaimedEfficiency: 25, EfficiencyFactor: EfficiencyFactor{Range{0.7, 0.85}, Range{0.85, 0.95}},
		ServiceIntervalKm: 10_000, ServiceCostPerVisit: 6_750, TyreSetCostINR: 24_000, InsuranceRate: 0.032,
		RepairBaseRate: 0.4, RepairGradeMultiplier: 1.3, WarrantyYears: 2, WarrantyKm: 50_000, ResaleCurve: curve,
	}
	return Inputs{
		Cars: []Car{petrol, ev, cng},
		Usage: Usage{
			KmPerMonth: Range{800, 1200}, CityShare: Range{0.5, 0.8}, HomeCharging: true,
			Loan: &Loan{PrincipalINR: 400_000, AnnualRate: 0.09, TenureMonths: 48},
		},
		World: w,
		Prefs: []PrefAttribute{
			{Key: "safety", Shares: []float64{0, 1, 0.5}, Brackets: []Range{{50_000, 100_000}}},
			{Key: "automatic", Shares: []float64{0, 1, 1}, Brackets: []Range{{25_000, 50_000}}},
		},
		HorizonYears: 5,
		DiscountRate: 0.07,
	}
}

func opts(futures, workers int, seed uint64) Options {
	return Options{Futures: futures, ChunkSize: 1000, Workers: workers, MasterSeed: seed}
}
