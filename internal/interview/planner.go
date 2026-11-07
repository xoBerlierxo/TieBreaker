package interview

import (
	"context"
	"errors"
	"runtime"
	"slices"

	"golang.org/x/sync/errgroup"

	"github.com/xoBerlierxo/TieBreaker/internal/decision"
	"github.com/xoBerlierxo/TieBreaker/internal/sim"
)

// Config holds the interview thresholds (SPEC §8.5, all configurable).
type Config struct {
	MaxQuestions int
	StopPBest    float64
	MinInfoGain  float64
	Ladder       Ladder
}

// Stop reasons, returned to the client with {"done": true}.
const (
	ReasonMaxQuestions = "max_questions"
	ReasonLeaderSure   = "leader_confident"
	ReasonLowInfoGain  = "low_information_gain"
	ReasonNoCandidates = "no_candidates"
	ReasonSingleCar    = "single_car"
)

// Planner chooses the next trade-off question.
type Planner struct {
	Cfg Config
	Sim QuickSimulator
}

// Gain is one candidate's information gain, reported for transparency.
type Gain struct {
	Attribute string  `json:"attribute"`
	Bits      float64 `json:"info_gain_bits"`
}

// Next is the planner's decision.
type Next struct {
	Done     bool      `json:"done"`
	Reason   string    `json:"reason,omitempty"`
	Question *Question `json:"question,omitempty"`
	PBest    []float64 `json:"p_best,omitempty"` // current state (answered brackets; unasked = ₹0)
	Gains    []Gain    `json:"gains,omitempty"`
	FellBack bool      `json:"fell_back,omitempty"` // quick sims timed out; fixed priority order used
}

// NextTradeoff decides the next trade-off question (SPEC §8.4, §8.5).
//
//   - base: the simulation inputs for the remaining cars with no preferences.
//   - attrs: Candidates(remaining cars), in the fixed priority order.
//   - s: the answers so far.
//
// The stop rules are checked in this order: question limit, leader confident,
// ladder in progress (continue it), no candidates left, best gain too small.
// If ctx expires while comparing candidates, the first candidate in priority
// order is asked instead (FellBack), so a slow machine never blocks the user.
func (p Planner) NextTradeoff(ctx context.Context, base sim.Inputs, attrs []Attribute, carNames []string, s TradeoffState, seed uint64) (Next, error) {
	if len(base.Cars) < 2 {
		return Next{Done: true, Reason: ReasonSingleCar}, nil
	}
	if s.Asked() >= p.Cfg.MaxQuestions {
		return Next{Done: true, Reason: ReasonMaxQuestions}, nil
	}

	current := base
	current.Prefs = PrefsFor(attrs, s, p.Cfg.Ladder)
	pBest, err := p.Sim.PBest(ctx, current, seed)
	timedOut := false
	switch {
	case isTimeout(err):
		timedOut = true
	case err != nil:
		return Next{}, err
	case pBest[decision.Leader(pBest)] >= p.Cfg.StopPBest:
		return Next{Done: true, Reason: ReasonLeaderSure, PBest: pBest}, nil
	}

	// A ladder in progress is always finished before switching attribute.
	if attr, st, ok := s.InProgress(p.Cfg.Ladder); ok {
		a, _ := findAttribute(attrs, attr)
		return Next{Question: p.ladderQuestion(a, st, len(s.Steps[attr])+1, carNames, base.HorizonYears, 0), PBest: pBest}, nil
	}

	open := openCandidates(attrs, s)
	if len(open) == 0 {
		return Next{Done: true, Reason: ReasonNoCandidates, PBest: pBest}, nil
	}
	if timedOut {
		return Next{Question: p.ladderQuestion(open[0], p.Cfg.Ladder.Start(), 1, carNames, base.HorizonYears, 0), FellBack: true}, nil
	}

	gains, err := p.informationGains(ctx, current, open, seed)
	if isTimeout(err) {
		return Next{Question: p.ladderQuestion(open[0], p.Cfg.Ladder.Start(), 1, carNames, base.HorizonYears, 0), PBest: pBest, FellBack: true}, nil
	}
	if err != nil {
		return Next{}, err
	}
	best := 0
	for i := range gains {
		if gains[i].Bits > gains[best].Bits { // ties keep the higher-priority candidate
			best = i
		}
	}
	if gains[best].Bits < p.Cfg.MinInfoGain {
		return Next{Done: true, Reason: ReasonLowInfoGain, PBest: pBest, Gains: gains}, nil
	}
	return Next{
		Question: p.ladderQuestion(open[best], p.Cfg.Ladder.Start(), 1, carNames, base.HorizonYears, gains[best].Bits),
		PBest:    pBest,
		Gains:    gains,
	}, nil
}

// informationGains computes, for each open candidate (SPEC §8.4):
//
//	H_prior   = entropy of P(best) with the attribute's value drawn from the
//	            equal-weight mixture of all brackets (DECISIONS.md D-P-7)
//	H_b       = entropy with the value restricted to bracket b
//	gain      = H_prior − mean_b(H_b)
//
// All quick sims share one seed, so the brackets are compared on identical
// futures. Candidates run in parallel, one goroutine each, at most GOMAXPROCS
// at a time.
func (p Planner) informationGains(ctx context.Context, current sim.Inputs, open []Attribute, seed uint64) ([]Gain, error) {
	brackets := p.Cfg.Ladder.AllBrackets()
	mixture := make([]sim.Range, len(brackets))
	for i, b := range brackets {
		mixture[i] = sim.Range{Lo: float64(b.Min), Hi: float64(b.Max)}
	}

	gains := make([]Gain, len(open))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(runtime.GOMAXPROCS(0))
	for i, a := range open {
		g.Go(func() error {
			with := func(r []sim.Range) sim.Inputs {
				in := current
				// Clone before appending: goroutines share current.Prefs'
				// backing array, and append could otherwise write into it.
				in.Prefs = append(slices.Clone(current.Prefs), sim.PrefAttribute{Key: a.Key, Shares: a.Shares, Brackets: r})
				return in
			}
			prior, err := p.Sim.PBest(gctx, with(mixture), seed)
			if err != nil {
				return err
			}
			expected := 0.0
			for _, b := range mixture {
				pb, err := p.Sim.PBest(gctx, with([]sim.Range{b}), seed)
				if err != nil {
					return err
				}
				expected += decision.Entropy(pb) / float64(len(mixture))
			}
			gains[i] = Gain{Attribute: a.Key, Bits: decision.Entropy(prior) - expected}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return gains, nil
}

// openCandidates are the attributes not asked yet, respecting the feature cap.
func openCandidates(attrs []Attribute, s TradeoffState) []Attribute {
	var out []Attribute
	featuresFull := s.FeaturesAsked() >= MaxFeatureQuestions
	for _, a := range attrs {
		if len(s.Steps[a.Key]) > 0 || (featuresFull && IsFeature(a.Key)) {
			continue
		}
		out = append(out, a)
	}
	return out
}

func (p Planner) ladderQuestion(a Attribute, st LadderState, step int, carNames []string, horizon int, gain float64) *Question {
	threshold, _ := p.Cfg.Ladder.NextThreshold(st)
	return &Question{
		ID:           TradeoffQuestionID(a.Key, step),
		Phase:        "tradeoff",
		Kind:         "yes_no",
		Text:         QuestionText(a, carNames, threshold, horizon),
		Attribute:    a.Key,
		Step:         step,
		ThresholdINR: threshold,
		InfoGain:     gain,
		Warning:      a.Warning,
		Example:      `{"yes": true}`,
	}
}

func isTimeout(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}
