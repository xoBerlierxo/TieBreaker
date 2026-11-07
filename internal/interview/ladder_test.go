package interview

import (
	"testing"
)

func defaultLadder() Ladder {
	return Ladder{Rungs: []int64{0, 25_000, 50_000, 100_000, 200_000, 400_000}, Cap: 800_000}
}

func TestLadder_StartsAt1L(t *testing.T) {
	st := defaultLadder().Start()
	thr, done := defaultLadder().NextThreshold(st)
	if done || thr != 100_000 {
		t.Fatalf("first question threshold = %d (done=%v), want ₹1,00,000", thr, done)
	}
}

// TestLadder_ConvergesInAtMost3Steps_AllPaths walks every possible sequence of
// yes/no answers and checks SPEC §8.3: at most 3 steps, and the result is one
// of the six brackets.
func TestLadder_ConvergesInAtMost3Steps_AllPaths(t *testing.T) {
	l := defaultLadder()
	seen := map[Bracket]bool{}
	var walk func(st LadderState, steps int)
	walk = func(st LadderState, steps int) {
		if _, done := l.NextThreshold(st); done {
			seen[l.Bracket(st)] = true
			if steps > 3 {
				t.Errorf("converged after %d steps, want ≤ 3", steps)
			}
			return
		}
		if steps >= 3 {
			t.Fatalf("not converged after 3 steps: %+v", st)
		}
		walk(l.Apply(st, true), steps+1)
		walk(l.Apply(st, false), steps+1)
	}
	walk(l.Start(), 0)

	want := []Bracket{{0, 25_000}, {25_000, 50_000}, {50_000, 100_000}, {100_000, 200_000}, {200_000, 400_000}, {400_000, 800_000}}
	for _, b := range want {
		if !seen[b] {
			t.Errorf("bracket %+v is unreachable", b)
		}
	}
	if len(seen) != len(want) {
		t.Errorf("reached %d brackets, want %d: %v", len(seen), len(want), seen)
	}
}

func TestLadder_AllYesGives4Lto8L(t *testing.T) {
	l := defaultLadder()
	st := l.Start()
	for range 3 {
		st = l.Apply(st, true)
	}
	if b := l.Bracket(st); b != (Bracket{400_000, 800_000}) {
		t.Fatalf("all yes → %+v, want [4L, 8L]", b)
	}
}

func TestLadder_AllNoGives0to25k(t *testing.T) {
	l := defaultLadder()
	st := l.Start()
	for {
		if _, done := l.NextThreshold(st); done {
			break
		}
		st = l.Apply(st, false)
	}
	if b := l.Bracket(st); b != (Bracket{0, 25_000}) {
		t.Fatalf("all no → %+v, want [0, 25k]", b)
	}
}

func TestLadder_PartialStopGivesWideBracket(t *testing.T) {
	l := defaultLadder()
	st := l.Apply(l.Start(), true) // "yes" to ₹1L, then stopped
	if b := l.Bracket(st); b != (Bracket{100_000, 800_000}) {
		t.Fatalf("partial bracket = %+v, want [1L, 8L]", b)
	}
	if !l.Started(st) || l.Done(st) {
		t.Error("partial ladder should be started and not done")
	}
}

func TestLadder_CustomRungsAndCap(t *testing.T) {
	l := Ladder{Rungs: []int64{0, 10, 20}, Cap: 40}
	thr, _ := l.NextThreshold(l.Start())
	if thr != 10 {
		t.Fatalf("first threshold = %d, want 10", thr)
	}
	if got := len(l.AllBrackets()); got != 3 {
		t.Fatalf("AllBrackets = %d, want 3", got)
	}
}

func TestLadder_ReplayFromAnswers(t *testing.T) {
	l := defaultLadder()
	st, err := l.Replay([]bool{false, true}) // no at 1L → yes at 25k → asks 50k next
	if err != nil {
		t.Fatal(err)
	}
	if thr, done := l.NextThreshold(st); done || thr != 50_000 {
		t.Fatalf("next threshold = %d (done %v), want 50k", thr, done)
	}
	if _, err := l.Replay([]bool{true, true, true, true}); err == nil {
		t.Error("replaying more answers than the ladder allows should fail")
	}
}
