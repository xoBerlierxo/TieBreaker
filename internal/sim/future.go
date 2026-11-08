package sim

import "math"

// evaluateCar computes one car's discounted ownership cost in one future
// (SPEC §10):
//
//	OwnershipCost = Upfront
//	              + Σ_t discount(t) × (Financing_t + Energy_t + Insurance_t + Service_t + Repairs_t + Tyres_t)
//	              − discount(T) × Resale_T
//
// Financing is precomputed in prepare (it is the same in every future). The
// car's own stream (cs) supplies its idiosyncratic randomness: first the resale
// shock (always exactly one draw), then the repair events.
func evaluateCar(p *prepared, pc *preparedCar, w *worldDraws, wp *worldPaths, cs stream) (cats [NumCategories]float64) {
	world := &p.in.World
	r := cs.rng
	idioZ := r.NormFloat64() // fixed first draw in the car stream

	cats[CatUpfront] = pc.cashUp
	cats[CatFinancing] = pc.financePV

	// Real-world efficiency: claimed × a factor drawn per fuel (shared by all
	// cars of that fuel in this future), blended by the city share.
	u := w.effU[pc.fuelIdx]
	factor := wp.cityShare*pc.EfficiencyFactor.City.at(u) + (1-wp.cityShare)*pc.EfficiencyFactor.Highway.at(u)
	kmPerUnit := pc.ClaimedEfficiency * factor
	if pc.Fuel == FuelEV {
		kmPerUnit *= world.ChargingEfficiency // energy lost between the socket and the battery
	}

	odometer := pc.OdometerKm
	for t := 1; t <= p.horizon; t++ {
		km := wp.km[t]
		d := p.discount[t]
		infl := p.inflation[t]
		age := pc.AgeYears + t - 1 // car age at the start of year t

		// Energy: litres (or kg, kWh) used × this year's price.
		energy := km / kmPerUnit * pc.unitPrice * wp.priceMult[t][pc.energy]

		// Insurance: premium = rate × IDV, IDV falling with age per the schedule.
		idv := pc.ValueBasisINR * scheduleAt(world.IDVSchedule, age) / pc.idvNorm
		insurance := pc.InsuranceRate * idv
		if t == 1 && pc.FirstYearInsuranceIncluded && !pc.Used {
			insurance = 0 // already paid inside the on-road price
		}

		// Service: one visit per service interval, at least the minimum per year.
		visits := math.Max(km/pc.ServiceIntervalKm, world.ServiceMinVisits)
		service := visits * pc.ServiceCostPerVisit * infl

		// Tyres: the share of a tyre set worn out this year.
		tyres := km / world.TyreLifeKm * pc.TyreSetCostINR * infl

		// Repairs: Poisson number of events, rate rising with age, odometer,
		// condition and used status; log-normal cost per event.
		lambda := pc.RepairBaseRate *
			(1 + world.RepairAgeSlope*float64(age)) *
			(1 + world.RepairOdoSlopePer100k*odometer/100_000) *
			pc.RepairGradeMultiplier
		if pc.Used {
			lambda *= world.RepairUsedMultiplier
		}
		repairs := 0.0
		for range poisson(r, lambda) {
			repairs += math.Exp(world.RepairCostMu + world.RepairCostSigma*r.NormFloat64())
		}
		repairs *= infl
		if float64(age) < pc.WarrantyYears && odometer < pc.WarrantyKm {
			repairs *= 1 - world.WarrantyCoverage
		}

		cats[CatEnergy] += d * energy
		cats[CatInsurance] += d * insurance
		cats[CatService] += d * service
		cats[CatTyres] += d * tyres
		cats[CatRepairs] += d * repairs
		odometer += km
	}

	// Resale at the end of the horizon: baseline curve × market shock (shared by
	// cars of this fuel) × the car's own shock × EV battery wear, clamped to
	// [0, value basis].
	resale := pc.ValueBasisINR * curveAt(pc.ResaleCurve, pc.AgeYears+p.horizon) / pc.resaleBase
	resale *= logNormalMeanOne(world.ResaleMarketSigma, w.marketZ[pc.fuelIdx])
	resale *= logNormalMeanOne(world.ResaleIdioSigma, idioZ)
	if pc.Fuel == FuelEV {
		resale *= math.Pow(1-world.EVBatteryWearPerYear, float64(p.horizon))
	}
	resale = math.Min(math.Max(resale, 0), pc.ValueBasisINR)
	cats[CatResale] = -p.discount[p.horizon] * resale

	return cats
}

// prefValues samples the ₹ value of each preference attribute for one future
// and returns each car's total preference value (SPEC §8.2).
func prefValues(p *prepared, futureSeed uint64, ps stream, out []float64) {
	for c := range out {
		out[c] = 0
	}
	for a, pref := range p.in.Prefs {
		ps.reseed(futureSeed, p.prefKeys[a])
		r := ps.rng
		uBracket, uValue := r.Float64(), r.Float64() // always two draws per attribute
		b := pref.Brackets[min(int(uBracket*float64(len(pref.Brackets))), len(pref.Brackets)-1)]
		value := b.at(uValue)
		for c, share := range pref.Shares {
			out[c] += share * value
		}
	}
}
