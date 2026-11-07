// Package report turns decisions into reports (SPEC §9): facts first, prose
// second. BuildFacts produces the Facts struct, the single source of truth;
// RenderTemplate always works; an optional LLM writer (M6) may rephrase the
// facts, but its output is checked against them before it is used.
package report

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/xoBerlierxo/TieBreaker/internal/decision"
	"github.com/xoBerlierxo/TieBreaker/internal/interview"
	"github.com/xoBerlierxo/TieBreaker/internal/money"
	"github.com/xoBerlierxo/TieBreaker/internal/session"
	"github.com/xoBerlierxo/TieBreaker/internal/sim"
)

// Deadlock types (SPEC §9.2).
const (
	DeadlockTippingPoint   = "tipping_point"
	DeadlockGenuinelyClose = "genuinely_close"
)

// minPointINR hides pros and cons worth less than this (noise, not a difference).
const minPointINR = 500

// Facts is everything a report says, as numbers and short phrases. Prose is
// generated from it and may only restate it.
type Facts struct {
	Kind         decision.Kind            `json:"kind"`
	DeadlockType string                   `json:"deadlock_type,omitempty"`
	Decision     *decision.Classification `json:"decision,omitempty"`
	Thresholds   decision.Thresholds      `json:"thresholds"`
	Winner       string                   `json:"winner,omitempty"`
	Run          *RunFacts                `json:"run,omitempty"`
	UserState    string                   `json:"user_state"`
	HorizonYears int                      `json:"horizon_years,omitempty"`
	Usage        *UsageFacts              `json:"usage,omitempty"`
	Cars         []CarFacts               `json:"cars"`
	Tipping      []TippingFact            `json:"tipping_points"`
	Eliminated   []EliminatedFact         `json:"eliminated"`
	Warnings     []string                 `json:"warnings"`
	Placeholders int                      `json:"placeholder_assumptions"`
}

// RunFacts identifies the simulation behind the report, for reproduction.
type RunFacts struct {
	ID           string `json:"id"`
	ModelVersion string `json:"model_version"`
	Seed         string `json:"seed"`
	Futures      int    `json:"futures"`
	ResultsHash  string `json:"results_hash,omitempty"`
}

// UsageFacts is the buyer's usage, as the report describes it.
type UsageFacts struct {
	KmPerMonth   [2]float64 `json:"km_per_month"`
	CitySharePct [2]float64 `json:"city_share_pct"`
	Financing    string     `json:"financing"`
}

// CarFacts is one car in the report (SPEC §9.3).
type CarFacts struct {
	ItemID        string                    `json:"item_id"`
	Label         string                    `json:"label"`
	Condition     string                    `json:"condition"`
	Fuel          string                    `json:"fuel"`
	Verified      bool                      `json:"verified"`
	SafetyRating  string                    `json:"safety_rating"` // e.g. "5★ (Bharat NCAP)" or "not rated"
	PBest         float64                   `json:"p_best"`
	PCheapest     float64                   `json:"p_cheapest"`
	NetCost       decision.Percentiles      `json:"net_cost"`
	OwnershipCost decision.Percentiles      `json:"ownership_cost"`
	Breakdown     []decision.CategoryMedian `json:"breakdown"`
	Pros          []Point                   `json:"pros"`
	Cons          []Point                   `json:"cons"`
	NotAsked      []string                  `json:"not_asked"` // differences the buyer was not asked to price
	Tipping       []string                  `json:"tipping_points"`
	Warnings      []string                  `json:"warnings"`
}

// Point is one pro or con, worth ValueINR to this buyer.
type Point struct {
	Source   string  `json:"source"` // "preference" (from the interview) or "cost" (from the simulation)
	Key      string  `json:"key"`
	Text     string  `json:"text"`
	ValueINR float64 `json:"value_inr"` // positive for pros, negative for cons
}

// TippingFact is one tipping point in words.
type TippingFact struct {
	Input           string  `json:"input"`
	Crossover       float64 `json:"crossover"`
	CrossoverText   string  `json:"crossover_text"`
	Below           string  `json:"below"` // label of the car leading below the crossover
	Above           string  `json:"above"`
	InsideUserRange bool    `json:"inside_user_range"`
	Sentence        string  `json:"sentence"`
}

// EliminatedFact is a car the hard constraints removed, and why.
type EliminatedFact struct {
	Label   string   `json:"label"`
	Reasons []string `json:"reasons"`
}

// Input is everything BuildFacts needs.
type Input struct {
	Inputs         sim.Inputs
	Context        session.RunContext
	Summary        decision.Summary
	Classification decision.Classification
	Tipping        decision.Tipping
	Thresholds     decision.Thresholds
	Run            RunFacts
}

// BuildFacts produces the report facts for a finished run (SPEC §9.3, §9.4).
func BuildFacts(in Input) Facts {
	rc := in.Context
	f := Facts{
		Kind:         in.Classification.Kind,
		Decision:     &in.Classification,
		Thresholds:   in.Thresholds,
		Run:          &in.Run,
		UserState:    rc.UserState,
		HorizonYears: rc.HorizonYears,
		Usage:        usageFacts(rc.Usage),
		Placeholders: rc.PlaceholderCnt,
		Cars:         []CarFacts{},
		Tipping:      []TippingFact{},
		Eliminated:   eliminatedFacts(rc.Eliminated),
		Warnings:     []string{},
	}
	if f.Kind == decision.KindWinner {
		f.Winner = rc.Cars[in.Classification.Leader].Label
	} else {
		f.DeadlockType = DeadlockGenuinelyClose
		if len(in.Tipping.Points) > 0 {
			f.DeadlockType = DeadlockTippingPoint
		}
	}

	labels := make([]string, len(rc.Cars))
	for i, c := range rc.Cars {
		labels[i] = c.Label
	}
	for _, tp := range in.Tipping.Points {
		f.Tipping = append(f.Tipping, tippingFact(tp, labels, rc))
	}

	for i, car := range rc.Cars {
		cs := in.Summary.Cars[i]
		cf := CarFacts{
			ItemID: car.ItemID, Label: car.Label, Condition: string(car.Condition), Fuel: string(car.Variant.FuelType),
			Verified: car.Variant.Verified, SafetyRating: safetyRating(car),
			PBest: cs.PBest, PCheapest: cs.PCheapest, NetCost: cs.NetCost, OwnershipCost: cs.OwnershipCost,
			Breakdown: cs.Breakdown, Pros: []Point{}, Cons: []Point{}, NotAsked: []string{}, Tipping: []string{}, Warnings: []string{},
		}
		cf.Pros, cf.Cons = prosAndCons(i, in, rc)
		cf.NotAsked = notAsked(rc)
		for _, t := range f.Tipping {
			if t.Below == car.Label || t.Above == car.Label {
				cf.Tipping = append(cf.Tipping, t.Sentence)
			}
		}
		for _, w := range rc.Warnings {
			if w.ItemID == car.ItemID {
				cf.Warnings = append(cf.Warnings, w.Text)
			}
		}
		f.Cars = append(f.Cars, cf)
	}
	for _, w := range rc.Warnings {
		// Placeholder assumptions are reported once, as a count (Placeholders).
		if w.ItemID == "" && w.Code != session.WarnPlaceholderAssumpt {
			f.Warnings = append(f.Warnings, w.Text)
		}
	}
	return f
}

// BuildNoneFit produces the facts for a session whose constraints eliminated
// every car (SPEC §6.2): which constraint each car failed and by how much.
func BuildNoneFit(userState string, cars []session.CarView) Facts {
	return Facts{
		Kind: decision.KindNoneFit, UserState: userState,
		Cars: []CarFacts{}, Tipping: []TippingFact{}, Warnings: []string{},
		Eliminated: eliminatedFacts(cars),
	}
}

func eliminatedFacts(cars []session.CarView) []EliminatedFact {
	out := []EliminatedFact{}
	for _, c := range cars {
		if !c.Eliminated {
			continue
		}
		ef := EliminatedFact{Label: c.Label}
		for _, e := range c.Eliminations {
			ef.Reasons = append(ef.Reasons, e.Message)
		}
		out = append(out, ef)
	}
	return out
}

func usageFacts(u interview.Usage) *UsageFacts {
	uf := &UsageFacts{KmPerMonth: u.KmPerMonth, CitySharePct: u.CitySharePct, Financing: "cash"}
	if u.Loan != nil {
		uf.Financing = fmt.Sprintf("a loan of %s at %.1f%% for %d months",
			money.FormatINR(u.Loan.PrincipalINR), u.Loan.AnnualRatePct, u.Loan.TenureMonths)
	}
	return uf
}

func safetyRating(c session.RunCar) string {
	v := c.Variant
	if v.SafetyRatingStars == nil {
		return "not rated" // never treated as zero (SPEC §7)
	}
	body := ""
	if v.SafetyRatingBody != nil {
		body = " (" + *v.SafetyRatingBody + ")"
	}
	return fmt.Sprintf("%d★%s", *v.SafetyRatingStars, body)
}

// prosAndCons ranks one car's advantages and disadvantages by what they are
// worth to THIS buyer in rupees (SPEC §9.3), from two sources:
//
//   - preferences: (car's share − average share of the other cars) × the
//     midpoint of the ₹ bracket the buyer gave for that difference;
//   - costs: the average median cost of the other cars minus this car's,
//     per category (positive = this car is cheaper there).
//
// The top 3 of each sign are kept.
func prosAndCons(car int, in Input, rc session.RunContext) (pros, cons []Point) {
	var points []Point
	brackets := map[string]interview.Bracket{}
	for _, t := range rc.Tradeoffs {
		brackets[t.Attribute] = t.Bracket
	}
	others := len(rc.Cars) - 1
	if others == 0 {
		return []Point{}, []Point{}
	}

	for _, a := range rc.Attributes {
		b, priced := brackets[a.Key]
		if !priced {
			continue
		}
		mean := 0.0
		for j, s := range a.Shares {
			if j != car {
				mean += s / float64(others)
			}
		}
		value := (a.Shares[car] - mean) * float64(b.Min+b.Max) / 2
		if math.Abs(value) < minPointINR {
			continue
		}
		points = append(points, Point{Source: "preference", Key: a.Key, ValueINR: value,
			Text: preferenceText(a, car, value)})
	}

	for k, cat := range in.Summary.Cars[car].Breakdown {
		mean := 0.0
		for j := range in.Summary.Cars {
			if j != car {
				mean += in.Summary.Cars[j].Breakdown[k].Median / float64(others)
			}
		}
		value := mean - cat.Median
		if math.Abs(value) < minPointINR {
			continue
		}
		points = append(points, Point{Source: "cost", Key: cat.Category, ValueINR: value,
			Text: costText(cat.Category, value, rc.HorizonYears)})
	}

	pros, cons = []Point{}, []Point{}
	for _, p := range points {
		if p.ValueINR > 0 {
			pros = append(pros, p)
		} else {
			cons = append(cons, p)
		}
	}
	slices.SortStableFunc(pros, func(a, b Point) int { return cmpDesc(a.ValueINR, b.ValueINR) })
	slices.SortStableFunc(cons, func(a, b Point) int { return cmpDesc(-a.ValueINR, -b.ValueINR) })
	return pros[:min(3, len(pros))], cons[:min(3, len(cons))]
}

func cmpDesc(a, b float64) int {
	switch {
	case a > b:
		return -1
	case a < b:
		return 1
	}
	return 0
}

func preferenceText(a interview.Attribute, car int, value float64) string {
	other := a.Worst
	if value < 0 {
		other = a.Best
	}
	return fmt.Sprintf("%s: %s vs %s; worth about %s to you", capitalise(a.Label), a.Describe(car), a.Describe(other), money.FormatLakh(math.Abs(value)))
}

// costName names a cost category in prose.
func costName(category string) string {
	switch category {
	case "upfront":
		return "upfront payment"
	case "financing":
		return "loan cost"
	case "energy":
		return "fuel/energy cost"
	case "tyres":
		return "tyre cost"
	case "repairs":
		return "repair cost"
	default:
		return category + " cost"
	}
}

func costText(category string, value float64, years int) string {
	amount := money.FormatLakh(math.Abs(value))
	if category == "resale" {
		if value > 0 {
			return fmt.Sprintf("Better resale value: about %s more back after %d years (median, today's money)", amount, years)
		}
		return fmt.Sprintf("Weaker resale value: about %s less back after %d years (median, today's money)", amount, years)
	}
	name := costName(category)
	if category == "upfront" { // paid once at purchase, not "over N years"
		if value > 0 {
			return fmt.Sprintf("Lower upfront payment: about %s less at purchase", amount)
		}
		return fmt.Sprintf("Higher upfront payment: about %s more at purchase", amount)
	}
	if value > 0 {
		return fmt.Sprintf("Lower %s: about %s less over %d years (median, today's money)", name, amount, years)
	}
	return fmt.Sprintf("Higher %s: about %s more over %d years (median, today's money)", name, amount, years)
}

func notAsked(rc session.RunContext) []string {
	asked := map[string]bool{}
	for _, t := range rc.Tradeoffs {
		asked[t.Attribute] = true
	}
	out := []string{}
	for _, a := range rc.Attributes {
		if !asked[a.Key] {
			out = append(out, a.Label)
		}
	}
	return out
}

// tippingFact puts a tipping point into words (SPEC §9.2 example: "Above
// ~1,100 km/month Car A wins; below it Car B wins").
func tippingFact(tp decision.TippingPoint, labels []string, rc session.RunContext) TippingFact {
	below, above := labels[tp.Below], labels[tp.Above]
	tf := TippingFact{Input: tp.Input, Crossover: tp.Crossover, Below: below, Above: above, InsideUserRange: tp.InsideUserRange}
	switch tp.Input {
	case "km_per_month":
		x := math.Round(tp.Crossover/50) * 50
		tf.CrossoverText = "~" + money.FormatNumber(x) + " km/month"
		tf.Sentence = fmt.Sprintf("Above %s, %s wins; below it, %s wins.", tf.CrossoverText, above, below)
	case "ownership_years":
		tf.CrossoverText = fmt.Sprintf("%.0f years", tp.Crossover)
		tf.Sentence = fmt.Sprintf("If you keep the car %s or more, %s wins; for a shorter time, %s wins.", tf.CrossoverText, above, below)
	case "fuel_trend":
		tf.CrossoverText = fmt.Sprintf("~%.1f%% a year", tp.Crossover*100)
		tf.Sentence = fmt.Sprintf("If fuel prices rise faster than %s, %s wins; if slower, %s wins.", tf.CrossoverText, above, below)
	case "home_charging":
		tf.CrossoverText = "home charging"
		tf.Sentence = fmt.Sprintf("With home charging, %s wins; without it, %s wins.", above, below)
	default:
		label := tp.Input
		if key, ok := decision.IsPrefSweep(tp.Input); ok {
			for _, a := range rc.Attributes {
				if a.Key == key {
					label = a.Label
				}
			}
		}
		x := math.Round(tp.Crossover/5000) * 5000
		tf.CrossoverText = "~" + money.FormatINR(x)
		tf.Sentence = fmt.Sprintf("If %s is worth more than %s to you, %s wins; if less, %s wins.", label, tf.CrossoverText, above, below)
	}
	return tf
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
