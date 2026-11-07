package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
	"github.com/xoBerlierxo/TieBreaker/internal/catalog"
	"github.com/xoBerlierxo/TieBreaker/internal/interview"
	"github.com/xoBerlierxo/TieBreaker/internal/sim"
	"github.com/xoBerlierxo/TieBreaker/internal/store"
)

// Shortlist size limits (SPEC §6.1).
const (
	MinCars = 2
	MaxCars = 4
)

// Catalog is what the session service needs from the catalogue.
type Catalog interface {
	Get(ctx context.Context, id string) (catalog.Variant, error)
	GetMany(ctx context.Context, ids []string) (map[string]catalog.Variant, error)
	LoadAssumptionSet(ctx context.Context, state, modelVersion string) (catalog.AssumptionSet, error)
}

// NoneFitHook is called inside the constraints transaction when every car is
// eliminated, so the none_fit report (M5) is written atomically with the state change.
type NoneFitHook func(ctx context.Context, tx *gorm.DB, sessionID string, cars []CarView) error

// Config holds the settings the session service needs.
type Config struct {
	Interview    interview.Config
	QuickFutures int
	ChunkSize    int
	QuickTimeout time.Duration
	DiscountRate float64
	FullFutures  int
}

// Service is the session business logic. Handlers call it; it never knows about HTTP.
type Service struct {
	db      *gorm.DB
	cat     Catalog
	cfg     Config
	planner interview.Planner
	logger  *slog.Logger
	now     func() time.Time
	noneFit NoneFitHook
}

// NewService creates a session service. cache may be nil (no quick-sim caching).
func NewService(db *gorm.DB, cat Catalog, cfg Config, cache interview.Cache, logger *slog.Logger) *Service {
	if cache == nil {
		cache = interview.NoCache{}
	}
	return &Service{
		db: db, cat: cat, cfg: cfg, logger: logger, now: time.Now,
		planner: interview.Planner{
			Cfg: cfg.Interview,
			Sim: interview.QuickSimulator{Futures: cfg.QuickFutures, ChunkSize: cfg.ChunkSize, Cache: cache},
		},
	}
}

// SetNoneFitHook installs the hook that writes the none_fit report (M5).
func (s *Service) SetNoneFitHook(h NoneFitHook) { s.noneFit = h }

// SetClock replaces the clock (tests).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// Create starts a new session in SHORTLISTING (SPEC §12 POST /v1/sessions).
func (s *Service) Create(ctx context.Context, userState string) (View, error) {
	if !catalog.IsSupportedState(userState) {
		return View{}, apperr.Validation("user_state", fmt.Sprintf("user_state must be one of %v", catalog.SupportedStates()))
	}
	sess := Session{ID: NewID(), Version: 1, State: StateShortlisting, UserState: userState}
	if err := s.db.WithContext(ctx).Create(&sess).Error; err != nil {
		return View{}, fmt.Errorf("creating session: %w", err)
	}
	return s.Get(ctx, sess.ID)
}

// Get returns the full session view.
func (s *Service) Get(ctx context.Context, id string) (View, error) {
	snap, err := s.load(ctx, s.db, id)
	if err != nil {
		return View{}, err
	}
	return s.view(ctx, snap), nil
}

// snapshot is everything stored for one session, read in one go.
type snapshot struct {
	sess     Session
	items    []Item
	variants map[string]catalog.Variant
	answers  []Answer
}

func (s *Service) load(ctx context.Context, db *gorm.DB, id string) (snapshot, error) {
	var snap snapshot
	err := db.WithContext(ctx).First(&snap.sess, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return snapshot{}, apperr.New(apperr.ErrNotFound, "session not found")
	}
	if err != nil {
		return snapshot{}, fmt.Errorf("loading session: %w", err)
	}
	if err := db.WithContext(ctx).Where("session_id = ?", id).Order("position").Find(&snap.items).Error; err != nil {
		return snapshot{}, fmt.Errorf("loading shortlist: %w", err)
	}
	if err := db.WithContext(ctx).Where("session_id = ?", id).Order("id").Find(&snap.answers).Error; err != nil {
		return snapshot{}, fmt.Errorf("loading answers: %w", err)
	}
	ids := make([]string, len(snap.items))
	for i, it := range snap.items {
		ids[i] = it.VariantID
	}
	snap.variants, err = s.cat.GetMany(ctx, ids)
	if err != nil {
		return snapshot{}, err
	}
	return snap, nil
}

// mutate runs fn in a transaction after an optimistic-locking version bump:
//
//	UPDATE sessions SET version = version + 1 WHERE id = ? AND version = ? RETURNING *
//
// If no row matches, the session is missing (404) or the If-Match version is
// stale (409). The UPDATE also takes the row lock, so concurrent writers to
// one session are serialised: the second one waits, then finds the version has
// moved on and fails with 409. Any error from fn rolls the bump back.
func (s *Service) mutate(ctx context.Context, id string, expected int, fn func(tx *gorm.DB, sess Session) error) error {
	return store.WithTx(ctx, s.db, func(tx *gorm.DB) error {
		sess, err := bumpVersion(tx, id, expected)
		if err != nil {
			return err
		}
		return fn(tx, sess)
	})
}

func bumpVersion(tx *gorm.DB, id string, expected int) (Session, error) {
	var sess Session
	res := tx.Raw(`UPDATE sessions SET version = version + 1, updated_at = now()
		WHERE id = ? AND version = ?
		RETURNING id, version, state, user_state, constraints_json, created_at, updated_at`, id, expected).Scan(&sess)
	if res.Error != nil {
		return Session{}, fmt.Errorf("bumping session version: %w", res.Error)
	}
	if res.RowsAffected == 1 {
		return sess, nil
	}
	var current Session
	err := tx.Select("version").First(&current, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Session{}, apperr.New(apperr.ErrNotFound, "session not found")
	}
	if err != nil {
		return Session{}, fmt.Errorf("reading session version: %w", err)
	}
	return Session{}, apperr.New(apperr.ErrVersionConflict, "the session was changed by another request; reload it and retry").
		WithDetails("current_version", current.Version)
}

func setState(tx *gorm.DB, sess Session, to State) error {
	if err := CheckTransition(sess.State, to); err != nil {
		return err
	}
	if err := tx.Model(&Session{}).Where("id = ?", sess.ID).Update("state", to).Error; err != nil {
		return fmt.Errorf("updating session state: %w", err)
	}
	return nil
}

// AddCar adds a car to the shortlist (SPEC §6.1). Requires If-Match.
func (s *Service) AddCar(ctx context.Context, id string, version int, req AddCarRequest) (View, error) {
	v, err := s.cat.Get(ctx, req.VariantID)
	if errors.Is(err, apperr.ErrNotFound) {
		return View{}, apperr.Validation("variant_id", "unknown variant")
	}
	if err != nil {
		return View{}, err
	}
	item, err := BuildItem(req, v, s.now())
	if err != nil {
		return View{}, err
	}

	err = s.mutate(ctx, id, version, func(tx *gorm.DB, sess Session) error {
		if err := RequireState(sess.State, StateShortlisting, "adding a car"); err != nil {
			return err
		}
		var count int64
		var maxPos *int
		if err := tx.Model(&Item{}).Where("session_id = ?", id).Count(&count).Error; err != nil {
			return fmt.Errorf("counting shortlist: %w", err)
		}
		if count >= MaxCars {
			return apperr.Validation("shortlist", fmt.Sprintf("a shortlist holds at most %d cars", MaxCars))
		}
		if err := tx.Model(&Item{}).Where("session_id = ?", id).Select("MAX(position)").Scan(&maxPos).Error; err != nil {
			return fmt.Errorf("reading positions: %w", err)
		}
		item.SessionID, item.Position = id, 0
		if maxPos != nil {
			item.Position = *maxPos + 1
		}
		if err := tx.Create(&item).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return apperr.New(apperr.ErrConflict, "this car is already on the shortlist")
			}
			return fmt.Errorf("adding car: %w", err)
		}
		return nil
	})
	if err != nil {
		return View{}, err
	}
	return s.Get(ctx, id)
}

// RemoveCar removes a car from the shortlist. Requires If-Match.
func (s *Service) RemoveCar(ctx context.Context, id string, version int, itemID string) (View, error) {
	err := s.mutate(ctx, id, version, func(tx *gorm.DB, sess Session) error {
		if err := RequireState(sess.State, StateShortlisting, "removing a car"); err != nil {
			return err
		}
		res := tx.Where("id::text = ? AND session_id = ?", itemID, id).Delete(&Item{})
		if res.Error != nil {
			return fmt.Errorf("removing car: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return apperr.New(apperr.ErrNotFound, "car not found on this shortlist")
		}
		return nil
	})
	if err != nil {
		return View{}, err
	}
	return s.Get(ctx, id)
}

// SetConstraints applies the hard constraints (SPEC §6.2), records every
// elimination with its reason, and moves the session to USAGE, or to DONE if no
// car survives. Requires If-Match.
func (s *Service) SetConstraints(ctx context.Context, id string, version int, c Constraints) (View, error) {
	err := s.mutate(ctx, id, version, func(tx *gorm.DB, sess Session) error {
		if err := RequireState(sess.State, StateShortlisting, "setting constraints"); err != nil {
			return err
		}
		snap, err := s.load(ctx, tx, id)
		if err != nil {
			return err
		}
		if n := len(snap.items); n < MinCars || n > MaxCars {
			return apperr.New(apperr.ErrIllegalTransition, fmt.Sprintf("a shortlist needs %d–%d cars, it has %d", MinCars, MaxCars, n))
		}
		set, err := s.cat.LoadAssumptionSet(ctx, sess.UserState, sim.ModelVersion)
		if err != nil {
			return err
		}
		hasEV := false
		checks := make([]CarToCheck, len(snap.items))
		for i, it := range snap.items {
			v := snap.variants[it.VariantID]
			hasEV = hasEV || v.FuelType == catalog.FuelEV
			price, err := PriceOf(it, v, set)
			if err != nil {
				return err
			}
			checks[i] = CarToCheck{Item: it, Variant: v, Price: price}
		}
		if err := c.Validate(hasEV); err != nil {
			return err
		}

		results := c.Evaluate(checks)
		survivors := 0
		for i, fails := range results {
			var elim json.RawMessage
			if len(fails) > 0 {
				if elim, err = json.Marshal(fails); err != nil {
					return fmt.Errorf("encoding eliminations: %w", err)
				}
			} else {
				survivors++
			}
			if err := tx.Model(&Item{}).Where("id = ?", snap.items[i].ID).
				Updates(map[string]any{"eliminated": len(fails) > 0, "elimination_json": elim}).Error; err != nil {
				return fmt.Errorf("recording eliminations: %w", err)
			}
		}
		raw, err := json.Marshal(c)
		if err != nil {
			return fmt.Errorf("encoding constraints: %w", err)
		}
		if err := tx.Model(&Session{}).Where("id = ?", id).Update("constraints_json", raw).Error; err != nil {
			return fmt.Errorf("saving constraints: %w", err)
		}
		if survivors == 0 {
			if err := setState(tx, sess, StateDone); err != nil {
				return err
			}
			if s.noneFit != nil {
				after, err := s.load(ctx, tx, id)
				if err != nil {
					return err
				}
				return s.noneFit(ctx, tx, id, s.view(ctx, after).Cars)
			}
			return nil
		}
		return setState(tx, sess, StateUsage)
	})
	if err != nil {
		return View{}, err
	}
	return s.Get(ctx, id)
}

// survivors returns the cars that passed the constraints, in shortlist order.
func (snap snapshot) survivors() []Item {
	var out []Item
	for _, it := range snap.items {
		if !it.Eliminated {
			out = append(out, it)
		}
	}
	return out
}

func (snap snapshot) constraints() (Constraints, error) {
	var c Constraints
	if len(snap.sess.ConstraintsJSON) == 0 {
		return c, nil
	}
	if err := json.Unmarshal(snap.sess.ConstraintsJSON, &c); err != nil {
		return c, fmt.Errorf("parsing stored constraints: %w", err)
	}
	return c, nil
}

func (snap snapshot) answerMap() map[string]json.RawMessage {
	m := make(map[string]json.RawMessage, len(snap.answers))
	for _, a := range snap.answers {
		m[a.QuestionID] = a.AnswerJSON
	}
	return m
}

func (snap snapshot) tradeoffState() (interview.TradeoffState, error) {
	st := interview.NewTradeoffState()
	for _, a := range snap.answers {
		if _, _, ok := interview.ParseTradeoffQuestionID(a.QuestionID); !ok {
			continue
		}
		var step interview.TradeoffStep
		if err := json.Unmarshal(a.AnswerJSON, &step); err != nil {
			return st, fmt.Errorf("parsing stored trade-off answer: %w", err)
		}
		if err := st.Add(step); err != nil {
			return st, err
		}
	}
	return st, nil
}

func (snap snapshot) survivingVariants() []catalog.Variant {
	var out []catalog.Variant
	for _, it := range snap.survivors() {
		out = append(out, snap.variants[it.VariantID])
	}
	return out
}

// BuildSimulationInputs turns a session into engine inputs, with the answered
// trade-offs as preferences. It is used by the interview (with the
// preferences removed) and by the full simulation (M4).
func (s *Service) BuildSimulationInputs(ctx context.Context, db *gorm.DB, id string) (InputsBuild, Session, error) {
	snap, err := s.load(ctx, db, id)
	if err != nil {
		return InputsBuild{}, Session{}, err
	}
	b, err := s.buildFromSnapshot(ctx, snap)
	return b, snap.sess, err
}

func (s *Service) buildFromSnapshot(ctx context.Context, snap snapshot) (InputsBuild, error) {
	usage, complete, err := interview.ParseUsage(snap.answerMap())
	if err != nil {
		return InputsBuild{}, err
	}
	if !complete {
		return InputsBuild{}, apperr.New(apperr.ErrIllegalTransition, "answer the usage questions first")
	}
	c, err := snap.constraints()
	if err != nil {
		return InputsBuild{}, err
	}
	st, err := snap.tradeoffState()
	if err != nil {
		return InputsBuild{}, err
	}
	set, err := s.cat.LoadAssumptionSet(ctx, snap.sess.UserState, sim.ModelVersion)
	if err != nil {
		return InputsBuild{}, err
	}
	var cars []SurvivingCar
	for _, it := range snap.survivors() {
		v := snap.variants[it.VariantID]
		price, err := PriceOf(it, v, set)
		if err != nil {
			return InputsBuild{}, err
		}
		cars = append(cars, SurvivingCar{Item: it, Variant: v, Price: price, Label: CarLabel(it, v)})
	}
	return BuildInputs(cars, set, usage, c, st, s.cfg.Interview.Ladder, s.cfg.DiscountRate, s.now())
}

// CarProbability is one car's P(best) in the current interview state.
type CarProbability struct {
	ItemID string  `json:"item_id"`
	Label  string  `json:"label"`
	PBest  float64 `json:"p_best"`
}

// NextResponse is GET /next-question's result.
type NextResponse struct {
	Done           bool                `json:"done"`
	Reason         string              `json:"reason,omitempty"`
	Question       *interview.Question `json:"question,omitempty"`
	PBest          []CarProbability    `json:"p_best,omitempty"`
	Gains          []interview.Gain    `json:"gains,omitempty"`
	FellBack       bool                `json:"fell_back,omitempty"`
	QuestionsAsked int                 `json:"questions_asked"`
	QuestionsLeft  int                 `json:"questions_left"`
}

// NextQuestion returns the next usage or trade-off question, or done (SPEC §12).
// It never changes the session. Trade-off selection runs quick simulations
// with a strict timeout (SPEC §11: default 2 s).
func (s *Service) NextQuestion(ctx context.Context, id string) (NextResponse, error) {
	snap, err := s.load(ctx, s.db, id)
	if err != nil {
		return NextResponse{}, err
	}
	switch snap.sess.State {
	case StateShortlisting:
		return NextResponse{}, apperr.New(apperr.ErrIllegalTransition, "add 2–4 cars and set your constraints first")
	case StateUsage:
		answered := map[string]bool{}
		for _, a := range snap.answers {
			answered[a.QuestionID] = true
		}
		if q := interview.NextUsageQuestion(answered); q != nil {
			return NextResponse{Question: q, QuestionsLeft: s.cfg.Interview.MaxQuestions}, nil
		}
		return NextResponse{Done: true, Reason: "usage_complete"}, nil
	case StateSimulating, StateDone:
		return NextResponse{Done: true, Reason: "interview_complete"}, nil
	}

	b, err := s.buildFromSnapshot(ctx, snap)
	if err != nil {
		return NextResponse{}, err
	}
	st, err := snap.tradeoffState()
	if err != nil {
		return NextResponse{}, err
	}
	base := b.Inputs
	base.Prefs = nil
	names := make([]string, len(b.Cars))
	for i, c := range b.Cars {
		names[i] = c.Label
	}

	qctx, cancel := context.WithTimeout(ctx, s.cfg.QuickTimeout)
	defer cancel()
	next, err := s.planner.NextTradeoff(qctx, base, b.Attributes, names, st, interview.SeedFor(id))
	if err != nil {
		return NextResponse{}, err
	}
	if next.FellBack {
		s.logger.WarnContext(ctx, "quick simulations timed out; asked the next question in priority order",
			slog.Duration("timeout", s.cfg.QuickTimeout))
	}

	resp := NextResponse{
		Done: next.Done, Reason: next.Reason, Question: next.Question, Gains: next.Gains, FellBack: next.FellBack,
		QuestionsAsked: st.Asked(), QuestionsLeft: max(0, s.cfg.Interview.MaxQuestions-st.Asked()),
	}
	for i, p := range next.PBest {
		resp.PBest = append(resp.PBest, CarProbability{ItemID: b.Cars[i].Item.ID, Label: b.Cars[i].Label, PBest: p})
	}
	return resp, nil
}
