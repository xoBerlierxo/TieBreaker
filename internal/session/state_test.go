package session

import (
	"errors"
	"regexp"
	"testing"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
)

func TestNewID_22CharsURLSafe(t *testing.T) {
	re := regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`)
	for range 100 {
		if id := NewID(); !re.MatchString(id) {
			t.Fatalf("NewID() = %q, want 22 base64url characters", id)
		}
	}
}

func TestNewID_NoCollisionsIn100k(t *testing.T) {
	seen := make(map[string]bool, 100_000)
	for range 100_000 {
		id := NewID()
		if seen[id] {
			t.Fatalf("collision on %q", id)
		}
		seen[id] = true
	}
}

func TestTransition_AllowedPairs(t *testing.T) {
	ok := [][2]State{
		{StateShortlisting, StateUsage},
		{StateShortlisting, StateDone},
		{StateUsage, StateInterview},
		{StateInterview, StateSimulating},
		{StateSimulating, StateDone},
		{StateSimulating, StateInterview},
	}
	for _, p := range ok {
		if err := CheckTransition(p[0], p[1]); err != nil {
			t.Errorf("%s → %s rejected: %v", p[0], p[1], err)
		}
	}
}

func TestTransition_EveryOtherPairIsIllegal(t *testing.T) {
	allowedSet := map[[2]State]bool{
		{StateShortlisting, StateUsage}: true, {StateShortlisting, StateDone}: true,
		{StateUsage, StateInterview}: true, {StateInterview, StateSimulating}: true,
		{StateSimulating, StateDone}: true, {StateSimulating, StateInterview}: true,
	}
	for _, from := range AllStates() {
		for _, to := range AllStates() {
			if allowedSet[[2]State{from, to}] {
				continue
			}
			if err := CheckTransition(from, to); !errors.Is(err, apperr.ErrIllegalTransition) {
				t.Errorf("%s → %s: err = %v, want illegal transition", from, to, err)
			}
		}
	}
}
