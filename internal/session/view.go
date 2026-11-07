package session

import (
	"context"
	"encoding/json"
	"time"

	"github.com/xoBerlierxo/TieBreaker/internal/catalog"
	"github.com/xoBerlierxo/TieBreaker/internal/interview"
	"github.com/xoBerlierxo/TieBreaker/internal/sim"
)

// View is the full session state returned by GET /v1/sessions/:id.
type View struct {
	ID             string           `json:"id"`
	Version        int              `json:"version"`
	State          State            `json:"state"`
	UserState      string           `json:"user_state"`
	CreatedAt      time.Time        `json:"created_at"`
	Cars           []CarView        `json:"cars"`
	Constraints    *Constraints     `json:"constraints"`
	Usage          *interview.Usage `json:"usage,omitempty"`
	UsageComplete  bool             `json:"usage_complete"`
	Tradeoffs      []TradeoffView   `json:"tradeoffs"`
	QuestionsAsked int              `json:"questions_asked"`
}

// CarView is one shortlisted car with its catalogue summary, price and eliminations.
type CarView struct {
	Item
	Label        string           `json:"label"`
	FuelType     catalog.FuelType `json:"fuel_type"`
	Verified     bool             `json:"verified"`
	Warnings     []string         `json:"warnings"`
	Price        *Price           `json:"price,omitempty"`
	Eliminations []Elimination    `json:"eliminations,omitempty"`
}

// TradeoffView is one priced trade-off: its ₹ bracket so far (SPEC §5.3 format).
type TradeoffView struct {
	Attribute string            `json:"attribute"`
	Steps     int               `json:"steps"`
	Done      bool              `json:"done"`
	Bracket   interview.Bracket `json:"bracket"`
}

func (s *Service) view(ctx context.Context, snap snapshot) View {
	v := View{
		ID: snap.sess.ID, Version: snap.sess.Version, State: snap.sess.State,
		UserState: snap.sess.UserState, CreatedAt: snap.sess.CreatedAt,
		Cars: []CarView{}, Tradeoffs: []TradeoffView{},
	}
	set, setErr := s.cat.LoadAssumptionSet(ctx, snap.sess.UserState, sim.ModelVersion)

	for _, it := range snap.items {
		variant := snap.variants[it.VariantID]
		cv := CarView{Item: it, Label: CarLabel(it, variant), FuelType: variant.FuelType, Verified: variant.Verified, Warnings: []string{}}
		if !variant.Verified {
			cv.Warnings = append(cv.Warnings, WarnUnverifiedCar)
		}
		if variant.SafetyRatingStars == nil {
			cv.Warnings = append(cv.Warnings, WarnSafetyNotRated)
		}
		if setErr == nil {
			if p, err := PriceOf(it, variant, set); err == nil {
				cv.Price = &p
			}
		}
		if len(it.EliminationJSON) > 0 {
			_ = json.Unmarshal(it.EliminationJSON, &cv.Eliminations)
		}
		v.Cars = append(v.Cars, cv)
	}
	if c, err := snap.constraints(); err == nil && len(snap.sess.ConstraintsJSON) > 0 {
		v.Constraints = &c
	}
	if u, complete, err := interview.ParseUsage(snap.answerMap()); err == nil {
		v.UsageComplete = complete
		if complete {
			v.Usage = &u
		}
	}
	if st, err := snap.tradeoffState(); err == nil {
		l := s.cfg.Interview.Ladder
		brackets := st.Brackets(l)
		for _, attr := range st.Order {
			ls, _ := l.Replay(st.Steps[attr])
			v.Tradeoffs = append(v.Tradeoffs, TradeoffView{Attribute: attr, Steps: len(st.Steps[attr]), Done: l.Done(ls), Bracket: brackets[attr]})
		}
		v.QuestionsAsked = st.Asked()
	}
	return v
}
