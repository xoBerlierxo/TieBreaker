// Package interview builds the buyer interview (SPEC §6, §8): the fixed usage
// questions, the trade-off candidates that come from real differences between
// cars, the ₹ bracket ladder, and the adaptive choice of the next question.
package interview

import "fmt"

// Ladder is the ₹ bracket ladder (SPEC §8.3). Rungs must start at 0 and be
// strictly increasing; Cap is the upper bound for "more than the top rung".
// With the default rungs [0, 25k, 50k, 1L, 2L, 4L] and cap 8L, the six possible
// answers are [0,25k] [25k,50k] [50k,1L] [1L,2L] [2L,4L] [4L,8L].
type Ladder struct {
	Rungs []int64
	Cap   int64
}

// LadderState is a binary search over rung indices. Index len(Rungs) stands
// for Cap. The answer is known to lie in [rung(Lo), rung(Hi)].
type LadderState struct {
	Lo, Hi int
}

// Bracket is a ₹ range [Min, Max].
type Bracket struct {
	Min int64 `json:"min_inr"`
	Max int64 `json:"max_inr"`
}

// Start is the state before any question: anywhere between 0 and the cap.
func (l Ladder) Start() LadderState { return LadderState{Lo: 0, Hi: len(l.Rungs)} }

func (l Ladder) rung(i int) int64 {
	if i >= len(l.Rungs) {
		return l.Cap
	}
	return l.Rungs[i]
}

// NextThreshold returns the ₹ amount for the next yes/no question ("would you
// pay ₹X more?"), or done = true when the bracket is down to one step.
//
// It asks the middle rung m = (Lo+Hi)/2. Starting from (0, 6) that is rung 3
// (₹1L), as SPEC §8.3 requires, and it never asks about ₹0. Each answer halves
// the remaining brackets, so 6 brackets need at most 3 questions.
func (l Ladder) NextThreshold(s LadderState) (thresholdINR int64, done bool) {
	if s.Hi-s.Lo <= 1 {
		return 0, true
	}
	return l.rung((s.Lo + s.Hi) / 2), false
}

// Apply records a yes/no answer to the current threshold question.
func (l Ladder) Apply(s LadderState, yes bool) LadderState {
	if s.Hi-s.Lo <= 1 {
		return s
	}
	m := (s.Lo + s.Hi) / 2
	if yes {
		s.Lo = m // they'd pay at least rung(m)
	} else {
		s.Hi = m // they wouldn't pay rung(m)
	}
	return s
}

// Bracket is the current ₹ range. If the ladder was stopped early it is wider,
// e.g. "yes at ₹1L" then stop → [1L, 8L] (DECISIONS.md D-P-13).
func (l Ladder) Bracket(s LadderState) Bracket {
	return Bracket{Min: l.rung(s.Lo), Max: l.rung(s.Hi)}
}

// Done reports whether the ladder has converged to a single bracket.
func (l Ladder) Done(s LadderState) bool { return s.Hi-s.Lo <= 1 }

// Started reports whether at least one question has been answered.
func (l Ladder) Started(s LadderState) bool { return s != l.Start() }

// AllBrackets lists every final answer, in ascending order. The interview uses
// it to evaluate a question before asking it (SPEC §8.4).
func (l Ladder) AllBrackets() []Bracket {
	out := make([]Bracket, 0, len(l.Rungs))
	for i := range l.Rungs {
		out = append(out, Bracket{Min: l.rung(i), Max: l.rung(i + 1)})
	}
	return out
}

// MaxSteps is the most yes/no questions one attribute can take.
func (l Ladder) MaxSteps() int {
	steps, n := 0, len(l.Rungs)
	for n > 1 {
		n = (n + 1) / 2
		steps++
	}
	return steps
}

// Replay rebuilds a ladder state from the yes/no answers given so far, in order.
func (l Ladder) Replay(answers []bool) (LadderState, error) {
	s := l.Start()
	for i, yes := range answers {
		if l.Done(s) {
			return s, fmt.Errorf("ladder already finished after %d answers, got %d", i, len(answers))
		}
		s = l.Apply(s, yes)
	}
	return s, nil
}
