package sim

import "math"

// monthlyEMI is the standard equated monthly instalment for a loan of
// principal p at annualRate over n months:
//
//	EMI = p · i · (1+i)^n / ((1+i)^n − 1),  i = annualRate / 12
//
// With a zero rate it is simply p / n.
func monthlyEMI(p, annualRate float64, n int) float64 {
	if p <= 0 || n <= 0 {
		return 0
	}
	i := annualRate / 12
	if i == 0 {
		return p / float64(n)
	}
	g := math.Pow(1+i, float64(n))
	return p * i * g / (g - 1)
}

// balanceAfter is the outstanding principal after m monthly payments:
//
//	B_m = p·(1+i)^m − EMI·((1+i)^m − 1)/i
func balanceAfter(p, annualRate float64, n, m int) float64 {
	if m >= n || p <= 0 {
		return 0
	}
	emi := monthlyEMI(p, annualRate, n)
	i := annualRate / 12
	if i == 0 {
		return p - emi*float64(m)
	}
	g := math.Pow(1+i, float64(m))
	return math.Max(0, p*g-emi*(g-1)/i)
}

// loanSchedule splits the EMIs into yearly totals for years 1..horizon
// (payments[t] is year t; payments[0] is unused) and returns the balance still
// owed after the horizon, which the buyer pays off at the end (DECISIONS.md:
// a loan longer than the ownership horizon must still be repaid).
func loanSchedule(p, annualRate float64, n, horizon int) (payments []float64, outstanding float64) {
	payments = make([]float64, horizon+1)
	emi := monthlyEMI(p, annualRate, n)
	for t := 1; t <= horizon; t++ {
		first, last := (t-1)*12+1, t*12 // months paid in year t
		if last > n {
			last = n
		}
		if first <= last {
			payments[t] = emi * float64(last-first+1)
		}
	}
	return payments, balanceAfter(p, annualRate, n, horizon*12)
}

// discountFactor converts money paid at the end of year t into today's money
// at a yearly rate r: 1 / (1+r)^t.
func discountFactor(r float64, t int) float64 {
	return math.Pow(1+r, -float64(t))
}

// curveAt reads a by-age curve where curve[k] is the value at age k+1. Age 0
// is 1 (the new value) and ages beyond the curve repeat its last value.
func curveAt(curve []float64, age int) float64 {
	switch {
	case age <= 0:
		return 1
	case age > len(curve):
		return curve[len(curve)-1]
	default:
		return curve[age-1] // #nosec G602 -- 1 <= age <= len(curve) in this branch
	}
}

// scheduleAt reads a schedule indexed from age 0; ages beyond it repeat the last value.
func scheduleAt(schedule []float64, age int) float64 {
	if age < 0 {
		age = 0
	}
	if age >= len(schedule) {
		return schedule[len(schedule)-1]
	}
	return schedule[age] // #nosec G602 -- 0 <= age < len(schedule) here
}
