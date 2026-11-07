package sim

// worldDraws holds the shared random numbers for one future. Every future
// draws exactly the same quantity, in exactly this order, whatever the cars,
// fuels or horizon (SPEC §10.1 "shared futures"):
//
//	for each year 1..MaxHorizonYears: one N(0,1) per energy, then one for km variation
//	then: u for km/month, u for city share, one u per fuel (efficiency factor),
//	      one N(0,1) per fuel (resale market shock)
//
// Drawing unused values (e.g. the electricity path when no EV is shortlisted)
// keeps the stream aligned, so the same seed gives the same world for any input.
type worldDraws struct {
	priceZ   [MaxHorizonYears + 1][numEnergies]float64 // index 1..MaxHorizonYears
	kmZ      [MaxHorizonYears + 1]float64
	kmU      float64
	cityU    float64
	effU     [numFuels]float64
	marketZ  [numFuels]float64
	numDraws int // for tests: how many values were drawn
}

// drawWorld fills w from the world stream.
func drawWorld(s stream, w *worldDraws) {
	r := s.rng
	n := 0
	for t := 1; t <= MaxHorizonYears; t++ {
		for e := range numEnergies {
			w.priceZ[t][e] = r.NormFloat64()
			n++
		}
		w.kmZ[t] = r.NormFloat64()
		n++
	}
	w.kmU, w.cityU = r.Float64(), r.Float64()
	n += 2
	for f := range numFuels {
		w.effU[f] = r.Float64()
		n++
	}
	for f := range numFuels {
		w.marketZ[f] = r.NormFloat64()
		n++
	}
	w.numDraws = n
}

// worldPaths is the world for one future, turned into usable numbers.
type worldPaths struct {
	km        [MaxHorizonYears + 1]float64              // km driven in year t
	priceMult [MaxHorizonYears + 1][numEnergies]float64 // price in year t ÷ today's price
	cityShare float64
}

// buildPaths turns the draws into km and price multipliers for years 1..horizon.
//
// Price path (SPEC §10): year 1 pays today's price; then p(t+1) = p(t)·exp(μ + σZ).
// Usage: base km/year = 12 × (km/month drawn uniformly from the user's range),
// varied each year by a mean-one log-normal factor.
func buildPaths(p *prepared, w *worldDraws, out *worldPaths) {
	baseKm := 12 * p.in.Usage.KmPerMonth.at(w.kmU)
	sigma := p.in.World.KmYearSigma
	out.cityShare = p.in.Usage.CityShare.at(w.cityU)

	var mult [numEnergies]float64
	for e := range numEnergies {
		mult[e] = 1
	}
	for t := 1; t <= p.horizon; t++ {
		out.km[t] = baseKm * logNormalMeanOne(sigma, w.kmZ[t])
		for e := range numEnergies {
			out.priceMult[t][e] = mult[e]
			mult[e] *= p.growth(e, w.priceZ[t][e])
		}
	}
}
