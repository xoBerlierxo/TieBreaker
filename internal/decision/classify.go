package decision

// Thresholds decide between a winner and a deadlock (SPEC §9.1, configurable).
type Thresholds struct {
	WinPBest float64 `json:"win_p_best"` // default 0.55
	WinGap   float64 `json:"win_gap"`    // default 0.15
}

// Kind is the report type.
type Kind string

const (
	KindWinner   Kind = "winner"
	KindDeadlock Kind = "deadlock"
	KindNoneFit  Kind = "none_fit"
)

// Classification is the verdict for a finished run.
type Classification struct {
	Kind        Kind    `json:"kind"`
	Leader      int     `json:"leader"`
	LeaderPBest float64 `json:"leader_p_best"`
	Gap         float64 `json:"gap"`    // leader minus runner-up
	Reason      string  `json:"reason"` // which rule decided
}

// Classify applies SPEC §9.1: winner if the leader's P(best) ≥ WinPBest OR the
// gap to the runner-up ≥ WinGap; otherwise deadlock. A single car always wins.
func Classify(pBest []float64, t Thresholds) Classification {
	leader := Leader(pBest)
	c := Classification{Leader: leader, LeaderPBest: pBest[leader], Gap: TopTwoGap(pBest)}
	switch {
	case len(pBest) == 1:
		c.Kind, c.Reason = KindWinner, "only_car"
	case c.LeaderPBest >= t.WinPBest:
		c.Kind, c.Reason = KindWinner, "p_best_threshold"
	case c.Gap >= t.WinGap:
		c.Kind, c.Reason = KindWinner, "gap_threshold"
	default:
		c.Kind, c.Reason = KindDeadlock, "too_close"
	}
	return c
}
