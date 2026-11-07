package session

import (
	"fmt"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
)

// State is where a session is in the flow (SPEC §5.2).
type State string

const (
	StateShortlisting State = "SHORTLISTING"
	StateUsage        State = "USAGE"
	StateInterview    State = "INTERVIEW"
	StateSimulating   State = "SIMULATING"
	StateDone         State = "DONE"
)

// AllStates lists every state in flow order.
func AllStates() []State {
	return []State{StateShortlisting, StateUsage, StateInterview, StateSimulating, StateDone}
}

// allowed is the transition table (DECISIONS.md D-P-15):
//
//	SHORTLISTING → USAGE       constraints set, at least one car survives
//	SHORTLISTING → DONE        constraints eliminate every car (none_fit report)
//	USAGE        → INTERVIEW   last usage question answered
//	INTERVIEW    → SIMULATING  full simulation started
//	SIMULATING   → DONE        simulation finished, report written
//	SIMULATING   → INTERVIEW   simulation cancelled or failed: the user can start again
//
// Anything else is refused with 409 (SPEC §5.2).
func allowed(from, to State) bool {
	switch from {
	case StateShortlisting:
		return to == StateUsage || to == StateDone
	case StateUsage:
		return to == StateInterview
	case StateInterview:
		return to == StateSimulating
	case StateSimulating:
		return to == StateDone || to == StateInterview
	}
	return false
}

// CheckTransition returns an illegal-transition error if from → to is not allowed.
func CheckTransition(from, to State) error {
	if !allowed(from, to) {
		return apperr.New(apperr.ErrIllegalTransition, fmt.Sprintf("cannot go from %s to %s", from, to)).
			WithDetails("from", string(from)).WithDetails("to", string(to))
	}
	return nil
}

// RequireState returns an illegal-transition error unless the session is in want.
func RequireState(current, want State, action string) error {
	if current != want {
		return apperr.New(apperr.ErrIllegalTransition, fmt.Sprintf("%s is only possible in state %s (session is %s)", action, want, current)).
			WithDetails("state", string(current))
	}
	return nil
}
