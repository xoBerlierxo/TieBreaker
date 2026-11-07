package interview

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
)

// Question is what GET /next-question returns.
type Question struct {
	ID       string   `json:"question_id"`
	Phase    string   `json:"phase"` // "usage" or "tradeoff"
	Kind     string   `json:"kind"`  // "range", "number", "financing", "choice", "yes_no"
	Text     string   `json:"text"`
	Unit     string   `json:"unit,omitempty"`
	Min      *float64 `json:"min,omitempty"`
	Max      *float64 `json:"max,omitempty"`
	Options  []string `json:"options,omitempty"`
	Optional bool     `json:"optional,omitempty"`
	Example  string   `json:"example,omitempty"` // an example answer body

	// Trade-off questions only.
	Attribute    string  `json:"attribute,omitempty"`
	Step         int     `json:"step,omitempty"`
	ThresholdINR int64   `json:"threshold_inr,omitempty"`
	InfoGain     float64 `json:"info_gain_bits,omitempty"`
	Warning      string  `json:"warning,omitempty"`
}

// Usage question IDs, in the fixed order of SPEC §6.3. The user's state is
// taken from the session (DECISIONS.md / Plan.md D2), so it is not asked again.
const (
	QKmPerMonth     = "usage:km_per_month"
	QCityShare      = "usage:city_share"
	QOwnershipYears = "usage:ownership_years"
	QFinancing      = "usage:financing"
	QParking        = "usage:parking"
)

// parkingOptions are the parking answers (collected, not used by the v1 cost
// model; Plan.md D9). A function, so no caller can modify a shared slice.
func parkingOptions() []string { return []string{"covered", "open", "street"} }

func f(v float64) *float64 { return &v }

// UsageQuestions returns the usage questions in their fixed order.
func UsageQuestions() []Question {
	return []Question{
		{ID: QKmPerMonth, Phase: "usage", Kind: "range", Unit: "km/month", Min: f(0), Max: f(20_000),
			Text: "Roughly how many kilometres a month will you drive? Give a range.", Example: `{"km_per_month": [800, 1200]}`},
		{ID: QCityShare, Phase: "usage", Kind: "range", Unit: "% of km in the city", Min: f(0), Max: f(100),
			Text: "What share of that driving is in the city (rather than on highways)? Give a range in percent.", Example: `{"city_share_pct": [50, 70]}`},
		{ID: QOwnershipYears, Phase: "usage", Kind: "number", Unit: "years", Min: f(1), Max: f(15),
			Text: "How many years do you expect to keep the car?", Example: `{"ownership_years": 5}`},
		{ID: QFinancing, Phase: "usage", Kind: "financing",
			Text:    "Will you pay cash or take a loan? For a loan, give the amount, yearly interest rate and tenure.",
			Example: `{"mode": "loan", "principal_inr": 500000, "annual_rate_pct": 9.5, "tenure_months": 48}`},
		{ID: QParking, Phase: "usage", Kind: "choice", Options: parkingOptions(), Optional: true,
			Text: "Where will the car usually be parked? (optional)", Example: `{"parking": "covered"}`},
	}
}

// NextUsageQuestion returns the first usage question not yet answered, or nil.
func NextUsageQuestion(answered map[string]bool) *Question {
	for _, q := range UsageQuestions() {
		if !answered[q.ID] {
			q := q
			return &q
		}
	}
	return nil
}

// IsUsageQuestion reports whether id is one of the usage questions.
func IsUsageQuestion(id string) bool {
	for _, q := range UsageQuestions() {
		if q.ID == id {
			return true
		}
	}
	return false
}

// Usage is the parsed set of usage answers.
type Usage struct {
	KmPerMonth     [2]float64 `json:"km_per_month"`
	CitySharePct   [2]float64 `json:"city_share_pct"`
	OwnershipYears int        `json:"ownership_years"`
	Loan           *Loan      `json:"loan,omitempty"`
	Parking        string     `json:"parking,omitempty"`
}

// Loan is the financing answer.
type Loan struct {
	PrincipalINR  float64 `json:"principal_inr"`
	AnnualRatePct float64 `json:"annual_rate_pct"`
	TenureMonths  int     `json:"tenure_months"`
}

type kmAnswer struct {
	KmPerMonth []float64 `json:"km_per_month"`
}
type cityAnswer struct {
	CitySharePct []float64 `json:"city_share_pct"`
}
type yearsAnswer struct {
	OwnershipYears *int `json:"ownership_years"`
}
type financingAnswer struct {
	Mode          string   `json:"mode"`
	PrincipalINR  *float64 `json:"principal_inr,omitempty"`
	AnnualRatePct *float64 `json:"annual_rate_pct,omitempty"`
	TenureMonths  *int     `json:"tenure_months,omitempty"`
}
type parkingAnswer struct {
	Parking string `json:"parking,omitempty"`
	Skipped bool   `json:"skipped,omitempty"`
}

// ValidateUsageAnswer checks one usage answer against SPEC §14's ranges and
// returns it normalised (canonical JSON) for storage.
func ValidateUsageAnswer(questionID string, raw json.RawMessage) (json.RawMessage, error) {
	bad := func(msg string) error { return apperr.Validation("answer", questionID+": "+msg) }
	switch questionID {
	case QKmPerMonth:
		var a kmAnswer
		if err := strictDecode(raw, &a); err != nil || len(a.KmPerMonth) != 2 {
			return nil, bad(`expected {"km_per_month": [low, high]}`)
		}
		if lo, hi := a.KmPerMonth[0], a.KmPerMonth[1]; lo < 0 || hi > 20_000 || lo > hi {
			return nil, bad("km per month must satisfy 0 ≤ low ≤ high ≤ 20,000")
		}
		return json.Marshal(a)
	case QCityShare:
		var a cityAnswer
		if err := strictDecode(raw, &a); err != nil || len(a.CitySharePct) != 2 {
			return nil, bad(`expected {"city_share_pct": [low, high]}`)
		}
		if lo, hi := a.CitySharePct[0], a.CitySharePct[1]; lo < 0 || hi > 100 || lo > hi {
			return nil, bad("city share must satisfy 0 ≤ low ≤ high ≤ 100")
		}
		return json.Marshal(a)
	case QOwnershipYears:
		var a yearsAnswer
		if err := strictDecode(raw, &a); err != nil || a.OwnershipYears == nil {
			return nil, bad(`expected {"ownership_years": n}`)
		}
		if y := *a.OwnershipYears; y < 1 || y > 15 {
			return nil, bad("ownership years must be between 1 and 15")
		}
		return json.Marshal(a)
	case QFinancing:
		var a financingAnswer
		if err := strictDecode(raw, &a); err != nil {
			return nil, bad(`expected {"mode": "cash"} or {"mode": "loan", "principal_inr", "annual_rate_pct", "tenure_months"}`)
		}
		switch a.Mode {
		case "cash":
			if a.PrincipalINR != nil || a.AnnualRatePct != nil || a.TenureMonths != nil {
				return nil, bad("a cash answer has no loan fields")
			}
		case "loan":
			if a.PrincipalINR == nil || a.AnnualRatePct == nil || a.TenureMonths == nil {
				return nil, bad("a loan needs principal_inr, annual_rate_pct and tenure_months")
			}
			if *a.PrincipalINR <= 0 || *a.PrincipalINR > 1e8 {
				return nil, bad("loan principal must be positive")
			}
			if *a.AnnualRatePct < 0 || *a.AnnualRatePct > 30 {
				return nil, bad("annual rate must be between 0 and 30 %")
			}
			if *a.TenureMonths < 6 || *a.TenureMonths > 120 {
				return nil, bad("tenure must be between 6 and 120 months")
			}
		default:
			return nil, bad(`mode must be "cash" or "loan"`)
		}
		return json.Marshal(a)
	case QParking:
		var a parkingAnswer
		if err := strictDecode(raw, &a); err != nil {
			return nil, bad(`expected {"parking": "covered|open|street"} or {"skipped": true}`)
		}
		if a.Skipped == (a.Parking != "") {
			return nil, bad(`give either a parking type or {"skipped": true}`)
		}
		if a.Parking != "" && !contains(parkingOptions(), a.Parking) {
			return nil, bad(fmt.Sprintf("parking must be one of %v", parkingOptions()))
		}
		return json.Marshal(a)
	default:
		return nil, apperr.Validation("question_id", fmt.Sprintf("unknown usage question %q", questionID))
	}
}

// ParseUsage reads the stored (already validated) usage answers. complete is
// false until every required question has an answer.
func ParseUsage(answers map[string]json.RawMessage) (u Usage, complete bool, err error) {
	var km kmAnswer
	var city cityAnswer
	var years yearsAnswer
	var fin financingAnswer
	required := []struct {
		id  string
		dst any
	}{{QKmPerMonth, &km}, {QCityShare, &city}, {QOwnershipYears, &years}, {QFinancing, &fin}}
	complete = true
	for _, r := range required {
		raw, ok := answers[r.id]
		if !ok {
			complete = false
			continue
		}
		if err := json.Unmarshal(raw, r.dst); err != nil {
			return Usage{}, false, fmt.Errorf("parsing stored answer %s: %w", r.id, err)
		}
	}
	if len(km.KmPerMonth) == 2 {
		u.KmPerMonth = [2]float64{km.KmPerMonth[0], km.KmPerMonth[1]}
	}
	if len(city.CitySharePct) == 2 {
		u.CitySharePct = [2]float64{city.CitySharePct[0], city.CitySharePct[1]}
	}
	if years.OwnershipYears != nil {
		u.OwnershipYears = *years.OwnershipYears
	}
	if fin.Mode == "loan" && fin.PrincipalINR != nil && fin.AnnualRatePct != nil && fin.TenureMonths != nil {
		u.Loan = &Loan{PrincipalINR: *fin.PrincipalINR, AnnualRatePct: *fin.AnnualRatePct, TenureMonths: *fin.TenureMonths}
	}
	if raw, ok := answers[QParking]; ok {
		var p parkingAnswer
		if err := json.Unmarshal(raw, &p); err == nil {
			u.Parking = p.Parking
		}
	}
	return u, complete, nil
}

func strictDecode(raw json.RawMessage, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if dec.More() {
		return fmt.Errorf("trailing data")
	}
	return nil
}

func contains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}
