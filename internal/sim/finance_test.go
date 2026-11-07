package sim

import (
	"math"
	"testing"
)

func near(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %.6f, want %.6f (±%g)", name, got, want, tol)
	}
}

func TestEMI_HandCalculated(t *testing.T) {
	// ₹1,20,000 at 12 % a year for 12 months: i = 0.01, (1.01)^12 = 1.12682503,
	// EMI = 120000 × 0.01 × 1.12682503 / 0.12682503 = ₹10,661.85.
	near(t, "EMI", monthlyEMI(120_000, 0.12, 12), 10_661.85, 0.01)
	// Zero interest: principal / months.
	near(t, "EMI at 0%", monthlyEMI(120_000, 0, 12), 10_000, 1e-9)
	near(t, "EMI no loan", monthlyEMI(0, 0.1, 12), 0, 0)
}

func TestLoanSchedule_PaymentsSumToEMITimesTenure(t *testing.T) {
	p, rate, n := 500_000.0, 0.09, 30
	payments, outstanding := loanSchedule(p, rate, n, 5)
	sum := 0.0
	for _, x := range payments {
		sum += x
	}
	near(t, "sum of payments", sum, monthlyEMI(p, rate, n)*30, 1e-6)
	near(t, "outstanding after full tenure", outstanding, 0, 0)
	// 30 months: 12 + 12 + 6.
	near(t, "year 3 share", payments[3], monthlyEMI(p, rate, n)*6, 1e-6)
	near(t, "year 4", payments[4], 0, 0)
}

func TestOutstandingAtHorizon_TenureLongerThanHorizon(t *testing.T) {
	// Zero-interest loan of ₹1,20,000 over 24 months, horizon 1 year:
	// 12 payments of ₹5,000 made, ₹60,000 still owed.
	payments, outstanding := loanSchedule(120_000, 0, 24, 1)
	near(t, "year 1 payments", payments[1], 60_000, 1e-9)
	near(t, "outstanding", outstanding, 60_000, 1e-9)

	// With interest, payments made + outstanding exceed the principal.
	payments, outstanding = loanSchedule(120_000, 0.12, 24, 1)
	if payments[1]+outstanding <= 120_000 {
		t.Errorf("paid %.2f + owed %.2f should exceed principal", payments[1], outstanding)
	}
}

func TestDiscountFactor_EndOfYear(t *testing.T) {
	near(t, "t=0", discountFactor(0.1, 0), 1, 1e-12)
	near(t, "t=1", discountFactor(0.1, 1), 1/1.1, 1e-12)
	near(t, "t=2", discountFactor(0.1, 2), 1/1.21, 1e-12)
}

func TestCurveAt_EdgesAndExtrapolation(t *testing.T) {
	curve := []float64{0.8, 0.7, 0.6}
	tests := []struct {
		age  int
		want float64
	}{{0, 1}, {1, 0.8}, {3, 0.6}, {10, 0.6}}
	for _, tt := range tests {
		near(t, "curveAt", curveAt(curve, tt.age), tt.want, 0)
	}
	near(t, "scheduleAt beyond end", scheduleAt([]float64{0.95, 0.85}, 7), 0.85, 0)
}
