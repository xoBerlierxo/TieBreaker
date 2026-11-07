package interview

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
)

func TestUsageQuestions_FixedOrder(t *testing.T) {
	want := []string{QKmPerMonth, QCityShare, QOwnershipYears, QFinancing, QParking}
	answered := map[string]bool{}
	for _, id := range want {
		q := NextUsageQuestion(answered)
		if q == nil || q.ID != id {
			t.Fatalf("next question = %v, want %s", q, id)
		}
		answered[id] = true
	}
	if q := NextUsageQuestion(answered); q != nil {
		t.Fatalf("expected no more usage questions, got %s", q.ID)
	}
}

func TestValidateUsageAnswer_Ranges(t *testing.T) {
	tests := []struct {
		q, body string
		ok      bool
	}{
		{QKmPerMonth, `{"km_per_month": [800, 1200]}`, true},
		{QKmPerMonth, `{"km_per_month": [0, 20000]}`, true},
		{QKmPerMonth, `{"km_per_month": [1200, 800]}`, false},
		{QKmPerMonth, `{"km_per_month": [0, 20001]}`, false},
		{QKmPerMonth, `{"km_per_month": [-1, 100]}`, false},
		{QKmPerMonth, `{"km_per_month": [100]}`, false},
		{QKmPerMonth, `{"km": [1, 2]}`, false},
		{QCityShare, `{"city_share_pct": [40, 70]}`, true},
		{QCityShare, `{"city_share_pct": [40, 101]}`, false},
		{QOwnershipYears, `{"ownership_years": 5}`, true},
		{QOwnershipYears, `{"ownership_years": 0}`, false},
		{QOwnershipYears, `{"ownership_years": 16}`, false},
		{QFinancing, `{"mode": "cash"}`, true},
		{QFinancing, `{"mode": "loan", "principal_inr": 500000, "annual_rate_pct": 9.5, "tenure_months": 48}`, true},
		{QFinancing, `{"mode": "loan", "principal_inr": 500000, "annual_rate_pct": 31, "tenure_months": 48}`, false},
		{QFinancing, `{"mode": "loan", "principal_inr": 500000, "annual_rate_pct": 9, "tenure_months": 3}`, false},
		{QFinancing, `{"mode": "loan", "principal_inr": 500000}`, false},
		{QFinancing, `{"mode": "cash", "tenure_months": 12}`, false},
		{QFinancing, `{"mode": "lease"}`, false},
		{QParking, `{"parking": "covered"}`, true},
		{QParking, `{"skipped": true}`, true},
		{QParking, `{"parking": "roof"}`, false},
		{"usage:state", `{}`, false},
	}
	for _, tt := range tests {
		_, err := ValidateUsageAnswer(tt.q, json.RawMessage(tt.body))
		if tt.ok && err != nil {
			t.Errorf("%s %s: unexpected error %v", tt.q, tt.body, err)
		}
		if !tt.ok && !errors.Is(err, apperr.ErrValidation) {
			t.Errorf("%s %s: err = %v, want validation error", tt.q, tt.body, err)
		}
	}
}

func TestParseUsage_RoundTrip(t *testing.T) {
	store := map[string]json.RawMessage{}
	for q, body := range map[string]string{
		QKmPerMonth:     `{"km_per_month": [800, 1200]}`,
		QCityShare:      `{"city_share_pct": [40, 70]}`,
		QOwnershipYears: `{"ownership_years": 6}`,
		QFinancing:      `{"mode": "loan", "principal_inr": 400000, "annual_rate_pct": 9, "tenure_months": 36}`,
	} {
		norm, err := ValidateUsageAnswer(q, json.RawMessage(body))
		if err != nil {
			t.Fatal(err)
		}
		store[q] = norm
	}
	u, complete, err := ParseUsage(store)
	if err != nil || !complete {
		t.Fatalf("ParseUsage = complete %v, err %v", complete, err)
	}
	if u.KmPerMonth != [2]float64{800, 1200} || u.OwnershipYears != 6 || u.Loan == nil || u.Loan.TenureMonths != 36 {
		t.Errorf("parsed usage = %+v", u)
	}
	delete(store, QFinancing)
	if _, complete, _ := ParseUsage(store); complete {
		t.Error("usage without financing reported complete")
	}
}
