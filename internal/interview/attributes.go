package interview

import (
	"fmt"
	"strings"

	"github.com/xoBerlierxo/TieBreaker/internal/catalog"
	"github.com/xoBerlierxo/TieBreaker/internal/money"
)

// Trade-off attribute keys (SPEC §8.1). Feature attributes are "feature:<key>".
const (
	KeySafety          = "safety"
	KeyBoot            = "boot"
	KeyGroundClearance = "ground_clearance"
	KeyPerformance     = "performance"
	KeyAutomatic       = "automatic"
	KeyWarranty        = "warranty"
	featurePrefix      = "feature:"
)

// MaxFeatureQuestions is how many feature differences may be asked about (SPEC §8.1).
const MaxFeatureQuestions = 2

// Attribute is one trade-off the buyer could be asked about: a real difference
// between the remaining cars on one numeric scale (higher = better).
type Attribute struct {
	Key     string    `json:"key"`
	Label   string    `json:"label"`
	Values  []float64 `json:"values"` // per car, on the attribute's scale
	Shares  []float64 `json:"shares"` // per car: 1 = best, 0 = worst, linear in between (SPEC §8.2)
	Best    int       `json:"best"`   // index of the best car
	Worst   int       `json:"worst"`  // index of the worst car
	Warning string    `json:"warning,omitempty"`
	// ValueLabels describes each car's value in words ("5★", "350 L of boot
	// space"), so stored runs and reports can show it without the catalogue.
	ValueLabels []string `json:"value_labels"`
	unit        func(v float64) string
}

// IsFeature reports whether an attribute key is a feature difference.
func IsFeature(key string) bool { return strings.HasPrefix(key, featurePrefix) }

// Candidates lists every attribute on which the cars really differ, in a fixed
// order (safety, boot, ground clearance, performance, automatic, features in
// vocabulary order, warranty). An attribute is skipped if any car is missing
// the value or if all cars are equal (SPEC §8.1: never ask about it).
func Candidates(cars []catalog.Variant) []Attribute {
	var out []Attribute
	add := func(a Attribute, ok bool) {
		if ok {
			out = append(out, a)
		}
	}

	// Safety: NCAP stars, only when every car is rated (DECISIONS.md D-P-8).
	if stars, ok := collectInt(cars, func(v catalog.Variant) *int { return v.SafetyRatingStars }); ok {
		a, ok := newAttribute(KeySafety, "safety rating", stars, func(v float64) string { return fmt.Sprintf("%.0f★", v) })
		if ok {
			bodies := map[string]bool{}
			for _, c := range cars {
				bodies[*c.SafetyRatingBody] = true
			}
			if len(bodies) > 1 {
				a.Warning = "the cars were rated by different bodies, so the stars are not strictly comparable"
			}
		}
		add(a, ok)
	}
	if boot, ok := collectInt(cars, func(v catalog.Variant) *int { return v.BootLitres }); ok {
		add(newAttribute(KeyBoot, "boot space", boot, func(v float64) string { return fmt.Sprintf("%.0f L of boot space", v) }))
	}
	if gc, ok := collectInt(cars, func(v catalog.Variant) *int { return v.GroundClearanceMM }); ok {
		add(newAttribute(KeyGroundClearance, "ground clearance", gc, func(v float64) string { return fmt.Sprintf("%.0f mm ground clearance", v) }))
	}
	if ptw, ok := collect(cars, func(v catalog.Variant) (float64, bool) { return v.PowerToWeight() }); ok {
		add(newAttribute(KeyPerformance, "performance", ptw, func(v float64) string { return fmt.Sprintf("%.0f PS per tonne", v) }))
	}
	auto, _ := collect(cars, func(v catalog.Variant) (float64, bool) { return boolTo01(v.IsAutomatic()), true })
	add(newAttribute(KeyAutomatic, "automatic gearbox", auto, func(v float64) string {
		if v == 1 {
			return "an automatic gearbox"
		}
		return "a manual gearbox"
	}))
	for _, f := range catalog.KnownFeatures() {
		feature := f
		has, known := collect(cars, func(v catalog.Variant) (float64, bool) { return hasFeature(v, feature.Key) })
		if !known {
			continue // a car's equipment is unknown: never present a guess as a difference
		}
		add(newAttribute(featurePrefix+feature.Key, feature.Label, has, func(v float64) string {
			if v == 1 {
				return feature.Label
			}
			return "no " + strings.TrimPrefix(strings.TrimPrefix(feature.Label, "a "), "an ")
		}))
	}
	// Warranty: years if they differ, otherwise km.
	if years, ok := collectInt(cars, func(v catalog.Variant) *int { return v.WarrantyYears }); ok {
		a, ok := newAttribute(KeyWarranty, "warranty", years, func(v float64) string { return fmt.Sprintf("a %.0f-year warranty", v) })
		if !ok {
			if km, okKm := collectInt(cars, func(v catalog.Variant) *int { return v.WarrantyKm }); okKm {
				a, ok = newAttribute(KeyWarranty, "warranty", km, func(v float64) string {
					return "a " + money.FormatNumber(v) + " km warranty"
				})
			}
		}
		add(a, ok)
	}
	return out
}

// newAttribute computes shares and best/worst; ok is false if all values are equal.
func newAttribute(key, label string, values []float64, unit func(float64) string) (Attribute, bool) {
	lo, hi := values[0], values[0]
	best, worst := 0, 0
	for i, v := range values {
		if v > hi {
			hi, best = v, i
		}
		if v < lo {
			lo, worst = v, i
		}
	}
	if hi == lo {
		return Attribute{}, false
	}
	shares := make([]float64, len(values))
	labels := make([]string, len(values))
	for i, v := range values {
		shares[i] = (v - lo) / (hi - lo)
		labels[i] = unit(v)
	}
	return Attribute{Key: key, Label: label, Values: values, Shares: shares, Best: best, Worst: worst, ValueLabels: labels, unit: unit}, true
}

func collectInt(cars []catalog.Variant, get func(catalog.Variant) *int) ([]float64, bool) {
	return collect(cars, func(v catalog.Variant) (float64, bool) {
		p := get(v)
		if p == nil {
			return 0, false
		}
		return float64(*p), true
	})
}

func collect(cars []catalog.Variant, get func(catalog.Variant) (float64, bool)) ([]float64, bool) {
	out := make([]float64, len(cars))
	for i, c := range cars {
		v, ok := get(c)
		if !ok {
			return nil, false
		}
		out[i] = v
	}
	return out, true
}

// hasFeature reports whether a car has a feature, and whether that is known.
// "6_airbags" is derived from the airbag count when it is known; if the count
// is unknown and the feature isn't listed, the answer is unknown (we must not
// claim the car lacks airbags just because the data is missing).
func hasFeature(v catalog.Variant, key string) (float64, bool) {
	if v.HasFeature(key) {
		return 1, true
	}
	if key == "6_airbags" {
		if v.Airbags == nil {
			return 0, false
		}
		return boolTo01(*v.Airbags >= 6), true
	}
	return 0, true
}

func boolTo01(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// Describe returns a human description of car i's value, e.g. "5★" or "350 L of boot space".
func (a Attribute) Describe(i int) string {
	switch {
	case a.unit != nil:
		return a.unit(a.Values[i])
	case i < len(a.ValueLabels):
		return a.ValueLabels[i]
	default:
		return fmt.Sprintf("%g", a.Values[i])
	}
}

// QuestionText is the yes/no ladder question for one step (SPEC §8.1 examples).
func QuestionText(a Attribute, carNames []string, thresholdINR int64, horizonYears int) string {
	best, worst := carNames[a.Best], carNames[a.Worst]
	return fmt.Sprintf("%s has %s; %s has %s. Would you pay %s more over %d years for %s's %s?",
		best, a.Describe(a.Best), worst, a.Describe(a.Worst),
		money.FormatINR(float64(thresholdINR)), horizonYears, best, a.Label)
}
