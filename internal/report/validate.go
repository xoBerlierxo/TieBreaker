package report

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/xoBerlierxo/TieBreaker/internal/decision"
)

// The fact validator (SPEC §9.4): every number (₹ amounts, percentages, km,
// years) and every car name in LLM prose must match the facts, allowing for
// rounding such as ₹1,02,340 → "about ₹1 lakh". Any mismatch → the prose is
// discarded and the template is used.

// numberPattern finds a number with its context:
//
//	group 1: an optional hedge ("about", "~", …)
//	group 2: an optional currency marker (₹, Rs, INR)
//	group 3: the digits, with Indian or Western grouping and decimals
//	group 4: an optional unit right after it (lakh, crore, %, percent, k, L, th…)
var numberPattern = regexp.MustCompile(`(?i)(~|about|around|roughly|approximately|approx\.?|nearly|almost|some)?\s*(₹|rs\.?|inr)?\s*(\d[\d,]*(?:\.\d+)?)\s*(lakhs?|crores?|cr\b|%|percent|k\b|l\b|st\b|nd\b|rd\b|th\b)?`)

// numberWord converts a spelled-out number ("three") to its value.
func numberWord(w string) float64 {
	words := []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten", "eleven", "twelve"}
	w = strings.ToLower(w)
	if i := slices.Index(words, w); i >= 0 {
		return float64(i)
	}
	switch w {
	case "fifteen":
		return 15
	case "twenty":
		return 20
	}
	return -1
}

var numberWordPattern = regexp.MustCompile(`(?i)\b(zero|one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve|fifteen|twenty)\b`)

// kind of a stated number, which decides which fact values it may match.
type numKind int

const (
	kindPlain numKind = iota
	kindMoney
	kindPercent
)

type stated struct {
	text      string
	value     float64
	precision float64 // one unit of the last written digit, e.g. "1.02 lakh" → 1,000
	hedged    bool
	kind      numKind
}

// tolerance is half a unit of the last stated digit, or a full unit when the
// number is hedged ("about"). Never below ₹1 / 0.5 %.
func (s stated) tolerance() float64 {
	t := s.precision / 2
	if s.hedged {
		t = s.precision
	}
	return math.Max(t, 0.5)
}

// extractNumbers finds every number in text with its kind and precision.
func extractNumbers(text string) []stated {
	var out []stated
	for _, m := range numberPattern.FindAllStringSubmatch(text, -1) {
		digits := strings.ReplaceAll(m[3], ",", "")
		v, err := strconv.ParseFloat(digits, 64)
		if err != nil {
			continue
		}
		s := stated{text: strings.TrimSpace(m[0]), value: v, hedged: m[1] != "", precision: digitPrecision(digits)}
		currency := m[2] != ""
		switch unit := strings.ToLower(m[4]); {
		case strings.HasPrefix(unit, "lakh") || (unit == "l" && currency):
			s.value, s.precision, s.kind = v*1e5, s.precision*1e5, kindMoney
		case strings.HasPrefix(unit, "crore") || unit == "cr":
			s.value, s.precision, s.kind = v*1e7, s.precision*1e7, kindMoney
		case unit == "k" && currency:
			s.value, s.precision, s.kind = v*1e3, s.precision*1e3, kindMoney
		case unit == "%" || unit == "percent":
			s.kind = kindPercent
		case currency:
			s.kind = kindMoney
		}
		out = append(out, s)
	}
	for _, m := range numberWordPattern.FindAllString(text, -1) {
		out = append(out, stated{text: m, value: numberWord(m), precision: 1})
	}
	return out
}

// digitPrecision is one unit of the last significant written digit:
// "1.02" → 0.01; "102340" → 1; "100000" → 100000 (trailing zeros = rounding).
func digitPrecision(digits string) float64 {
	if i := strings.IndexByte(digits, '.'); i >= 0 {
		return math.Pow(10, -float64(len(digits)-i-1))
	}
	zeros := len(digits) - len(strings.TrimRight(digits, "0"))
	if zeros == len(digits) {
		return 1 // the number 0
	}
	return math.Pow(10, float64(zeros))
}

// allowedValues collects every number the facts contain, by kind.
type allowedValues struct {
	money, percent, plain []float64
}

func (a *allowedValues) addMoney(vs ...float64) {
	for _, v := range vs {
		a.money = append(a.money, math.Abs(v))
		a.plain = append(a.plain, math.Abs(v))
	}
}

func (a *allowedValues) addPercent(ps ...float64) { // probabilities 0–1
	for _, p := range ps {
		a.percent = append(a.percent, p*100)
		a.plain = append(a.plain, p*100)
	}
}

func (a *allowedValues) addPlain(vs ...float64) { a.plain = append(a.plain, vs...) }

// addFromText allows every number already written in a fact string (pros,
// tipping sentences…), exactly as the template would show it.
func (a *allowedValues) addFromText(s string) {
	for _, n := range extractNumbers(s) {
		switch n.kind {
		case kindMoney:
			a.addMoney(n.value)
		case kindPercent:
			a.percent = append(a.percent, n.value)
			a.plain = append(a.plain, n.value)
		default:
			a.addPlain(n.value)
		}
	}
}

func factValues(f Facts) allowedValues {
	var a allowedValues
	a.addPlain(10, 50, 90) // percentile names: 10th / 50th / 90th
	a.addPlain(float64(len(f.Cars)), float64(len(f.Tipping)), float64(len(f.Eliminated)), float64(f.Placeholders), float64(f.HorizonYears))
	if f.Decision != nil {
		a.addPercent(f.Decision.LeaderPBest, f.Decision.Gap)
	}
	a.addPercent(f.Thresholds.WinPBest, f.Thresholds.WinGap)
	if f.Run != nil {
		a.addPlain(float64(f.Run.Futures))
	}
	if f.Usage != nil {
		a.addPlain(f.Usage.KmPerMonth[0], f.Usage.KmPerMonth[1], f.Usage.CitySharePct[0], f.Usage.CitySharePct[1])
		a.addFromText(f.Usage.Financing)
	}
	for _, c := range f.Cars {
		a.addPercent(c.PBest, c.PCheapest)
		a.addMoney(c.NetCost.P10, c.NetCost.P50, c.NetCost.P90, c.OwnershipCost.P10, c.OwnershipCost.P50, c.OwnershipCost.P90)
		for _, b := range c.Breakdown {
			a.addMoney(b.Median)
		}
		for _, p := range append(slices.Clone(c.Pros), c.Cons...) {
			a.addMoney(p.ValueINR)
			a.addFromText(p.Text)
		}
		a.addFromText(c.SafetyRating)
	}
	for _, t := range f.Tipping {
		a.addPlain(t.Crossover)
		a.addFromText(t.CrossoverText)
		a.addFromText(t.Sentence)
	}
	for _, e := range f.Eliminated {
		for _, r := range e.Reasons {
			a.addFromText(r)
		}
	}
	return a
}

func matchesAny(n stated, values []float64) bool {
	tol := n.tolerance()
	for _, v := range values {
		if math.Abs(v-n.value) <= tol {
			return true
		}
	}
	return false
}

// Violation is one problem found in LLM prose.
type Violation struct {
	Text   string `json:"text"`
	Reason string `json:"reason"`
}

// winnerClaims are phrases that declare a winner; they must not appear when
// the facts say deadlock (the LLM may never decide, SPEC §1).
func winnerClaims() []string {
	return []string{"clear winner", "is the best choice", "the best car", "is the winner", "comes out on top", "you should buy", "we recommend"}
}

// ValidateProse checks LLM prose against the facts. catalogNames are the
// makes, models and display names of every car in the catalogue, so a car
// that isn't part of this report is caught. It returns all violations; an
// empty result means the prose may be used.
func ValidateProse(prose string, f Facts, catalogNames []string) []Violation {
	var v []Violation
	lower := strings.ToLower(prose)

	// 1. Remove the names of the cars in the report (their labels contain
	// numbers like "1.2" or "40 kWh" that are names, not facts).
	allowedNames := map[string]bool{}
	var labels []string
	for _, c := range f.Cars {
		labels = append(labels, c.Label)
		for _, n := range []string{c.Label, c.Make, c.Model, strings.TrimSpace(c.Make + " " + c.Model)} {
			if n != "" {
				allowedNames[strings.ToLower(n)] = true
			}
		}
	}
	for _, e := range f.Eliminated {
		labels = append(labels, e.Label)
		allowedNames[strings.ToLower(e.Label)] = true
	}
	slices.SortFunc(labels, func(a, b string) int { return len(b) - len(a) }) // longest first
	scrubbed := prose
	for _, l := range labels {
		scrubbed = replaceFold(scrubbed, l, " CAR ")
	}
	for _, c := range f.Cars {
		for _, n := range []string{c.Make, c.Model} {
			if len(n) >= 3 {
				scrubbed = replaceFold(scrubbed, n, " CAR ")
			}
		}
	}

	// 2. No other catalogue car may be mentioned.
	for _, name := range catalogNames {
		ln := strings.ToLower(strings.TrimSpace(name))
		if len(ln) < 3 || allowedNames[ln] {
			continue
		}
		if containsWord(strings.ToLower(scrubbed), ln) {
			v = append(v, Violation{Text: name, Reason: "names a car that is not in this report"})
		}
	}

	// 3. Every number must match a fact.
	allowed := factValues(f)
	for _, n := range extractNumbers(scrubbed) {
		var ok bool
		switch n.kind {
		case kindMoney:
			ok = matchesAny(n, allowed.money)
		case kindPercent:
			ok = matchesAny(n, allowed.percent)
		default:
			ok = matchesAny(n, allowed.plain)
		}
		if !ok {
			v = append(v, Violation{Text: n.text, Reason: fmt.Sprintf("the number %g is not in the facts", n.value)})
		}
	}

	// 4. The verdict must match the facts.
	switch f.Kind {
	case decision.KindDeadlock:
		for _, claim := range winnerClaims() {
			if strings.Contains(lower, claim) {
				v = append(v, Violation{Text: claim, Reason: "claims a winner, but the result is a deadlock"})
			}
		}
	case decision.KindWinner:
		if f.Winner != "" && !strings.Contains(lower, strings.ToLower(f.Winner)) {
			v = append(v, Violation{Text: f.Winner, Reason: "does not name the winning car"})
		}
	}
	return v
}

func replaceFold(s, old, repl string) string {
	if old == "" {
		return s
	}
	re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(old))
	return re.ReplaceAllString(s, repl)
}

func containsWord(haystack, word string) bool {
	re := regexp.MustCompile(`(^|[^\p{L}\p{N}])` + regexp.QuoteMeta(word) + `($|[^\p{L}\p{N}])`)
	return re.MatchString(haystack)
}
