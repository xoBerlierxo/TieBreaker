package sim

import (
	"context"
	"math"
	"testing"
)

func runOrFail(t *testing.T, in Inputs, o Options) Result {
	t.Helper()
	res, err := Run(context.Background(), in, o)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res
}

// TestOwnershipCost_HandCheckable_NewCar is SPEC §15's hand-checkable case:
// with all randomness off, the engine must equal a value worked out by hand.
//
// Test Car A, cash purchase, 2-year horizon, 10 % discount rate, 1,000 km/month:
//
//	km/year           = 12,000; real efficiency = 20 × 0.75 = 15 km/l → 800 litres/year
//	Year 1: energy    = 800 × ₹100                    =  80,000
//	        insurance = 0.03 × (4,00,000 × 0.95)      =  11,400
//	        service   = (12,000/10,000) × 4,000 × 1   =   4,800
//	        tyres     = (12,000/40,000) × 20,000 × 1  =   6,000
//	        total                                     = 1,02,200
//	Year 2: energy    = 800 × ₹110 (price +10 %)      =  88,000
//	        insurance = 0.03 × (4,00,000 × 0.85)      =  10,200
//	        service   = 4,800 × 1.05                  =   5,040
//	        tyres     = 6,000 × 1.05                  =   6,300
//	        total                                     = 1,09,540
//	Resale at year 2  = 4,00,000 × 0.7                = 2,80,000
//
//	OwnershipCost = 5,00,000 + 1,02,200/1.1 + 1,09,540/1.21 − 2,80,000/1.21
//	              = 5,00,000 + 92,909.0909 + 90,528.9256 − 2,31,404.9587
//	              = 4,52,033.0578
func TestOwnershipCost_HandCheckable_NewCar(t *testing.T) {
	res := runOrFail(t, handInputs(), opts(10, 1, 1))
	const want = 452_033.0578512397

	for k := range res.Futures { // no randomness: every future is identical
		near(t, "ownership cost", res.Cars[0].OwnershipCost[k], want, 1e-6)
	}
	cats := res.Cars[0].Categories
	near(t, "upfront", cats[CatUpfront][0], 500_000, 1e-9)
	near(t, "energy", cats[CatEnergy][0], 80_000/1.1+88_000/1.21, 1e-6)
	near(t, "insurance", cats[CatInsurance][0], 11_400/1.1+10_200/1.21, 1e-6)
	near(t, "service", cats[CatService][0], 4_800/1.1+5_040/1.21, 1e-6)
	near(t, "tyres", cats[CatTyres][0], 6_000/1.1+6_300/1.21, 1e-6)
	near(t, "resale", cats[CatResale][0], -280_000/1.21, 1e-6)
	near(t, "repairs", cats[CatRepairs][0], 0, 0)
	near(t, "net cost without preferences", res.Cars[0].NetCost[0], want, 1e-6)
}

// TestOwnershipCost_HandCheckable_UsedCarWithLoan: a used car (age 3) bought
// with a loan, driven 0 km so only financing and resale matter.
//
//	Upfront 3,11,000 (asking 3,00,000 + transfer); loan 1,20,000 at 12 % for 12 months
//	EMI = ₹10,661.85 (see TestEMI_HandCalculated) → year 1 payments = 12 × 10,661.85 = 1,27,942.26
//	Cash at purchase = 3,11,000 − 1,20,000 = 1,91,000
//	Resale after 1 year = 3,00,000 × curve(age 4)/curve(age 3) = 3,00,000 × 0.5/0.6 = 2,50,000
//
//	OwnershipCost = 1,91,000 + 1,27,942.26/1.1 − 2,50,000/1.1
//	              = 1,91,000 + 1,16,311.14 − 2,27,272.73 = 80,038.41
func TestOwnershipCost_HandCheckable_UsedCarWithLoan(t *testing.T) {
	in := handInputs()
	c := handCar()
	c.Used, c.AgeYears, c.OdometerKm = true, 3, 30_000
	c.UpfrontINR, c.ValueBasisINR = 311_000, 300_000
	c.InsuranceRate = 0
	c.ResaleCurve = []float64{0.8, 0.7, 0.6, 0.5}
	in.Cars = []Car{c}
	in.Usage.KmPerMonth = Range{0, 0}
	in.Usage.Loan = &Loan{PrincipalINR: 120_000, AnnualRate: 0.12, TenureMonths: 12}
	in.HorizonYears = 1

	res := runOrFail(t, in, opts(5, 1, 1))
	near(t, "ownership cost", res.Cars[0].OwnershipCost[0], 80_038.41, 0.05)
	near(t, "upfront cash", res.Cars[0].Categories[CatUpfront][0], 191_000, 1e-9)
}

func TestCategories_SumToOwnershipCost(t *testing.T) {
	res := runOrFail(t, stochasticInputs(), opts(2000, 2, 7))
	for _, car := range res.Cars {
		for k := 0; k < res.Futures; k += 97 {
			sum := 0.0
			for cat := range NumCategories {
				sum += car.Categories[cat][k]
			}
			if sum != car.OwnershipCost[k] {
				t.Fatalf("car %s future %d: categories sum %v != ownership %v", car.ID, k, sum, car.OwnershipCost[k])
			}
			if math.Abs(car.NetCost[k]-(car.OwnershipCost[k]-car.PrefValue[k])) > 1e-6 {
				t.Fatalf("net cost != ownership − preference")
			}
		}
	}
}

func TestEnergy_EVUsesPublicTariffWithoutHomeCharging(t *testing.T) {
	in := handInputs()
	ev := handCar()
	ev.ID, ev.Fuel, ev.ClaimedEfficiency = "ev", FuelEV, 10
	in.Cars = []Car{ev}
	in.HorizonYears = 1

	in.Usage.HomeCharging = true
	home := runOrFail(t, in, opts(1, 1, 1)).Cars[0].Categories[CatEnergy][0]
	in.Usage.HomeCharging = false
	public := runOrFail(t, in, opts(1, 1, 1)).Cars[0].Categories[CatEnergy][0]

	// 12,000 km / (10 × 0.75 × 0.9 km per kWh drawn from the grid) = 1,777.78 kWh.
	kwh := 12_000 / (10 * 0.75 * 0.9)
	near(t, "home energy", home, kwh*8/1.1, 1e-6)
	near(t, "public energy", public, kwh*20/1.1, 1e-6)
}

func TestResale_ClampedToValueBasis(t *testing.T) {
	in := stochasticInputs()
	in.World.ResaleIdioSigma = 3 // huge shocks: some would exceed the basis without the clamp
	res := runOrFail(t, in, opts(2000, 2, 3))
	for i, car := range res.Cars {
		basis := in.Cars[i].ValueBasisINR
		for _, r := range car.Categories[CatResale] {
			if -r > basis+1e-6 || r > 0 {
				t.Fatalf("car %s: resale %v outside [0, %v]", car.ID, -r, basis)
			}
		}
	}
}

func TestRepairs_RateRisesWithAgeOdometerAndGrade(t *testing.T) {
	base := stochasticInputs()
	base.Cars = base.Cars[:1]
	base.Cars[0].WarrantyYears, base.Cars[0].WarrantyKm = 0, 0
	base.Prefs = nil
	meanRepairs := func(mut func(*Car)) float64 {
		in := base
		in.Cars = []Car{base.Cars[0]}
		mut(&in.Cars[0])
		res := runOrFail(t, in, opts(20_000, 4, 11))
		return mean(res.Cars[0].Categories[CatRepairs])
	}
	newCar := meanRepairs(func(*Car) {})
	older := meanRepairs(func(c *Car) { c.Used, c.AgeYears, c.OdometerKm = true, 6, 90_000 })
	poor := meanRepairs(func(c *Car) { c.Used, c.AgeYears, c.OdometerKm, c.RepairGradeMultiplier = true, 6, 90_000, 1.8 })
	if !(newCar < older && older < poor) {
		t.Errorf("mean repairs: new %.0f, older %.0f, poor %.0f; want strictly increasing", newCar, older, poor)
	}
}

func TestRepairs_WarrantyReducesCostInsideWarranty(t *testing.T) {
	in := stochasticInputs()
	in.Cars = in.Cars[:1]
	in.Prefs = nil
	in.Cars[0].WarrantyYears, in.Cars[0].WarrantyKm = 0, 0
	without := mean(runOrFail(t, in, opts(20_000, 4, 5)).Cars[0].Categories[CatRepairs])
	in.Cars[0].WarrantyYears, in.Cars[0].WarrantyKm = 10, 1e9
	with := mean(runOrFail(t, in, opts(20_000, 4, 5)).Cars[0].Categories[CatRepairs])
	near(t, "covered repairs ÷ uncovered", with/without, 1-in.World.WarrantyCoverage, 1e-9)
}

func mean(xs []float64) float64 {
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

// TestInsurance_FirstYearSkippedWhenIncludedInOnRoadPrice: an on-road quote
// that already contains year-1 insurance must not be charged again. With the
// hand-checkable car, insurance becomes only year 2: 0.03 × 4,00,000 × 0.85 / 1.21.
func TestInsurance_FirstYearSkippedWhenIncludedInOnRoadPrice(t *testing.T) {
	in := handInputs()
	in.Cars[0].FirstYearInsuranceIncluded = true
	res := runOrFail(t, in, opts(5, 1, 1))
	near(t, "insurance", res.Cars[0].Categories[CatInsurance][0], 10_200/1.21, 1e-6)

	// Used cars ignore the flag: their asking price never includes insurance.
	in.Cars[0].Used, in.Cars[0].AgeYears = true, 2
	used := runOrFail(t, in, opts(5, 1, 1))
	if used.Cars[0].Categories[CatInsurance][0] <= 10_200/1.21 {
		t.Error("used car skipped year-1 insurance")
	}
}
