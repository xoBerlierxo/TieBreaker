package sim

import "math"

// prepared holds everything that is the same in every future, computed once
// per run: discount factors, price-path growth parameters, and each car's
// deterministic costs (financing, which doesn't depend on the future).
type prepared struct {
	in      *Inputs
	horizon int

	discount   [MaxHorizonYears + 1]float64 // discount[t] = 1/(1+r)^t
	inflation  [MaxHorizonYears + 1]float64 // inflation[t] = (1+i)^(t-1), cost escalation in year t
	trend      [numEnergies]PriceTrend
	startPrice [numEnergies]float64 // today's price per unit (electricity: home tariff)

	cars     []preparedCar
	prefKeys []uint64
}

type preparedCar struct {
	*Car
	streamKey  uint64
	energy     Energy
	fuelIdx    int
	unitPrice  float64 // today's price per litre / kg / kWh this car actually pays
	cashUp     float64 // upfront paid in cash at t=0 (upfront − loan principal)
	financePV  float64 // discounted EMIs + discounted payoff of any balance left at the horizon
	idvNorm    float64 // divides the IDV schedule: 1 for new cars, schedule(age at purchase) for used
	resaleBase float64 // resale curve at purchase age (1 for new cars)
}

func prepare(in *Inputs) *prepared {
	p := &prepared{in: in, horizon: in.HorizonYears}

	for t := 0; t <= MaxHorizonYears; t++ {
		p.discount[t] = discountFactor(in.DiscountRate, t)
		p.inflation[t] = math.Pow(1+in.World.GeneralInflation, float64(t-1))
	}

	w := in.World
	p.trend = [numEnergies]PriceTrend{w.PetrolTrend, w.DieselTrend, w.CNGTrend, w.ElectricityTrend}
	p.startPrice = [numEnergies]float64{w.PetrolPriceINR, w.DieselPriceINR, w.CNGPriceINR, w.HomeTariffINR}

	p.cars = make([]preparedCar, len(in.Cars))
	for i := range in.Cars {
		c := &in.Cars[i]
		pc := preparedCar{
			Car:       c,
			streamKey: streamKey("car:" + c.ID),
			energy:    c.Fuel.energy(),
			fuelIdx:   c.Fuel.index(),
		}

		pc.unitPrice = p.startPrice[pc.energy]
		if c.Fuel == FuelEV && !in.Usage.HomeCharging {
			pc.unitPrice = w.PublicTariffINR // SPEC §10: no home charging → public tariff
		}

		// Financing is deterministic: the same in every future.
		pc.cashUp = c.UpfrontINR
		if loan := in.Usage.Loan; loan != nil && loan.PrincipalINR > 0 {
			principal := math.Min(loan.PrincipalINR, c.UpfrontINR)
			pc.cashUp = c.UpfrontINR - principal
			payments, outstanding := loanSchedule(principal, loan.AnnualRate, loan.TenureMonths, p.horizon)
			for t := 1; t <= p.horizon; t++ {
				pc.financePV += p.discount[t] * payments[t]
			}
			pc.financePV += p.discount[p.horizon] * outstanding
		}

		pc.idvNorm = 1
		if c.Used {
			pc.idvNorm = scheduleAt(w.IDVSchedule, c.AgeYears)
		}
		pc.resaleBase = curveAt(c.ResaleCurve, c.AgeYears)
		p.cars[i] = pc
	}

	p.prefKeys = make([]uint64, len(in.Prefs))
	for a, pref := range in.Prefs {
		p.prefKeys[a] = streamKey("pref:" + pref.Key)
	}
	return p
}

// growth is the yearly price multiplier exp(μ + σZ) for one energy.
func (p *prepared) growth(e Energy, z float64) float64 {
	tr := p.trend[e]
	return math.Exp(tr.Mu + tr.Sigma*z)
}
