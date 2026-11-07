package interview

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
	"github.com/xoBerlierxo/TieBreaker/internal/sim"
)

// TradeoffStep is one stored yes/no ladder answer.
type TradeoffStep struct {
	Attribute    string `json:"attribute"`
	Step         int    `json:"step"`
	ThresholdINR int64  `json:"threshold_inr"`
	Yes          bool   `json:"yes"`
}

// TradeoffState is the interview's trade-off progress, rebuilt from the stored
// answers (no separate state column: the answers are the source of truth).
type TradeoffState struct {
	Order []string          // attributes, in the order they were first asked
	Steps map[string][]bool // attribute → yes/no answers, in order
}

// NewTradeoffState returns an empty state.
func NewTradeoffState() TradeoffState {
	return TradeoffState{Steps: map[string][]bool{}}
}

// TradeoffQuestionID is "tradeoff:<attribute>:<step>", e.g. "tradeoff:feature:esc:2".
func TradeoffQuestionID(attribute string, step int) string {
	return fmt.Sprintf("tradeoff:%s:%d", attribute, step)
}

// ParseTradeoffQuestionID splits a trade-off question ID. The attribute itself
// may contain ':' (features), so the step is the last segment.
func ParseTradeoffQuestionID(id string) (attribute string, step int, ok bool) {
	rest, found := strings.CutPrefix(id, "tradeoff:")
	if !found {
		return "", 0, false
	}
	i := strings.LastIndex(rest, ":")
	if i <= 0 {
		return "", 0, false
	}
	step, err := strconv.Atoi(rest[i+1:])
	if err != nil || step < 1 {
		return "", 0, false
	}
	return rest[:i], step, true
}

// Add appends a stored step (steps must arrive in order per attribute).
func (s *TradeoffState) Add(step TradeoffStep) error {
	if got := len(s.Steps[step.Attribute]); got != step.Step-1 {
		return fmt.Errorf("trade-off %s: step %d stored after %d steps", step.Attribute, step.Step, got)
	}
	if step.Step == 1 {
		s.Order = append(s.Order, step.Attribute)
	}
	s.Steps[step.Attribute] = append(s.Steps[step.Attribute], step.Yes)
	return nil
}

// Asked is the number of yes/no questions answered (each counts toward the limit).
func (s TradeoffState) Asked() int {
	n := 0
	for _, steps := range s.Steps {
		n += len(steps)
	}
	return n
}

// InProgress returns the attribute whose ladder is started but not finished.
func (s TradeoffState) InProgress(l Ladder) (string, LadderState, bool) {
	for _, attr := range s.Order {
		st, err := l.Replay(s.Steps[attr])
		if err == nil && !l.Done(st) {
			return attr, st, true
		}
	}
	return "", LadderState{}, false
}

// Brackets returns the current ₹ bracket for every attribute asked so far
// (partial ladders give wider brackets).
func (s TradeoffState) Brackets(l Ladder) map[string]Bracket {
	out := make(map[string]Bracket, len(s.Order))
	for _, attr := range s.Order {
		if st, err := l.Replay(s.Steps[attr]); err == nil {
			out[attr] = l.Bracket(st)
		}
	}
	return out
}

// FeaturesAsked counts feature attributes already asked about.
func (s TradeoffState) FeaturesAsked() int {
	n := 0
	for _, attr := range s.Order {
		if IsFeature(attr) {
			n++
		}
	}
	return n
}

// PrefsFor turns the answered brackets into the engine's preference inputs
// (SPEC §8.2), using the current attributes' shares. Attributes are emitted in
// the order they were asked, which keeps the inputs (and their hash) stable.
func PrefsFor(attrs []Attribute, s TradeoffState, l Ladder) []sim.PrefAttribute {
	brackets := s.Brackets(l)
	var out []sim.PrefAttribute
	for _, key := range s.Order {
		a, ok := findAttribute(attrs, key)
		if !ok {
			continue
		}
		b := brackets[key]
		out = append(out, sim.PrefAttribute{
			Key:      key,
			Shares:   a.Shares,
			Brackets: []sim.Range{{Lo: float64(b.Min), Hi: float64(b.Max)}},
		})
	}
	return out
}

func findAttribute(attrs []Attribute, key string) (Attribute, bool) {
	for _, a := range attrs {
		if a.Key == key {
			return a, true
		}
	}
	return Attribute{}, false
}

// tradeoffAnswerBody is the client's answer to a ladder step.
type tradeoffAnswerBody struct {
	Yes *bool `json:"yes"`
}

// ValidateTradeoffAnswer checks that questionID is a question the interview may
// ask right now, and returns the step to store. A ladder in progress must be
// finished first; otherwise step 1 of any not-yet-asked candidate is accepted.
func ValidateTradeoffAnswer(attrs []Attribute, s TradeoffState, l Ladder, maxQuestions int, questionID string, raw json.RawMessage) (TradeoffStep, error) {
	attr, step, ok := ParseTradeoffQuestionID(questionID)
	if !ok {
		return TradeoffStep{}, apperr.Validation("question_id", "malformed trade-off question id")
	}
	var body tradeoffAnswerBody
	if err := strictDecode(raw, &body); err != nil || body.Yes == nil {
		return TradeoffStep{}, apperr.Validation("answer", `expected {"yes": true} or {"yes": false}`)
	}
	if s.Asked() >= maxQuestions {
		return TradeoffStep{}, apperr.New(apperr.ErrIllegalTransition, "the question limit has been reached")
	}
	a, ok := findAttribute(attrs, attr)
	if !ok {
		return TradeoffStep{}, apperr.Validation("question_id", fmt.Sprintf("%q is not a difference between your cars", attr))
	}

	current, st, inProgress := s.InProgress(l)
	switch {
	case inProgress && attr != current:
		return TradeoffStep{}, apperr.New(apperr.ErrIllegalTransition, fmt.Sprintf("finish the %s question first", current))
	case !inProgress && len(s.Steps[attr]) > 0:
		return TradeoffStep{}, apperr.New(apperr.ErrIllegalTransition, fmt.Sprintf("%s has already been answered", attr))
	case !inProgress && IsFeature(attr) && s.FeaturesAsked() >= MaxFeatureQuestions:
		return TradeoffStep{}, apperr.New(apperr.ErrIllegalTransition, "at most two feature questions are asked")
	case !inProgress:
		st = l.Start()
	}
	if want := len(s.Steps[attr]) + 1; step != want {
		return TradeoffStep{}, apperr.New(apperr.ErrIllegalTransition, fmt.Sprintf("expected step %d of %s, got %d", want, attr, step))
	}
	threshold, done := l.NextThreshold(st)
	if done {
		return TradeoffStep{}, apperr.New(apperr.ErrIllegalTransition, fmt.Sprintf("%s is already finished", a.Key))
	}
	return TradeoffStep{Attribute: attr, Step: step, ThresholdINR: threshold, Yes: *body.Yes}, nil
}
