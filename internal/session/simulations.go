package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"gorm.io/gorm"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
	"github.com/xoBerlierxo/TieBreaker/internal/catalog"
	"github.com/xoBerlierxo/TieBreaker/internal/interview"
	"github.com/xoBerlierxo/TieBreaker/internal/jobs"
	"github.com/xoBerlierxo/TieBreaker/internal/sim"
	"github.com/xoBerlierxo/TieBreaker/internal/store"
)

// RunCar is one simulated car, as recorded in the run's context.
type RunCar struct {
	ItemID    string          `json:"item_id"`
	VariantID string          `json:"variant_id"`
	Label     string          `json:"label"`
	Condition Condition       `json:"condition"`
	Variant   catalog.Variant `json:"variant"` // catalogue data at the time of the run
	Price     Price           `json:"price"`
}

// RunContext is everything the report needs besides the samples. It is stored
// inside the run's inputs_json (jobs.Payload.Context), so a report can be
// rebuilt from the run row alone, even after the catalogue changes.
type RunContext struct {
	SessionID      string                  `json:"session_id"`
	UserState      string                  `json:"user_state"`
	Cars           []RunCar                `json:"cars"` // same order as the engine inputs
	Eliminated     []CarView               `json:"eliminated"`
	Attributes     []interview.Attribute   `json:"attributes"`
	Tradeoffs      []TradeoffView          `json:"tradeoffs"`
	Usage          interview.Usage         `json:"usage"`
	Constraints    Constraints             `json:"constraints"`
	Warnings       []CarWarning            `json:"warnings"`
	Assumptions    []catalog.AssumptionRef `json:"assumptions"`
	PlaceholderCnt int                     `json:"placeholder_assumptions"`
	HorizonYears   int                     `json:"horizon_years"`
	Ladder         interview.Ladder        `json:"ladder"`
}

// StartSimulation queues a full simulation (SPEC §11, §12): in one
// transaction, bump the version (If-Match), check the state, build the inputs
// with the assumptions snapshotted into them, insert a queued run, and move the
// session to SIMULATING. A double submit gets 409 (stale version, wrong state,
// or the partial unique index on active runs).
func (s *Service) StartSimulation(ctx context.Context, id string, version int) (jobs.Run, error) {
	var run jobs.Run
	err := s.mutate(ctx, id, version, func(tx *gorm.DB, sess Session) error {
		if err := RequireState(sess.State, StateInterview, "starting a simulation"); err != nil {
			return err
		}
		snap, err := s.load(ctx, tx, id)
		if err != nil {
			return err
		}
		snap.sess = sess
		b, err := s.buildFromSnapshot(ctx, snap)
		if err != nil {
			return err
		}
		set, err := s.cat.LoadAssumptionSet(ctx, sess.UserState, sim.ModelVersion)
		if err != nil {
			return err
		}
		rc := s.runContext(ctx, snap, b, set)
		payload, inputsHash, err := encodePayload(b.Inputs, rc)
		if err != nil {
			return err
		}
		run = jobs.Run{
			ID: NewID(), SessionID: id, Kind: jobs.KindFull,
			MasterSeed: jobs.SeedToDB(randomSeed()),
			Futures:    s.cfg.FullFutures, ChunkSize: s.cfg.ChunkSize,
			InputsHash: inputsHash, InputsJSON: payload, ModelVersion: sim.ModelVersion,
		}
		if err := jobs.Insert(tx, &run); err != nil {
			return err
		}
		return setState(tx, sess, StateSimulating)
	})
	return run, err
}

func (s *Service) runContext(ctx context.Context, snap snapshot, b InputsBuild, set catalog.AssumptionSet) RunContext {
	usage, _, _ := interview.ParseUsage(snap.answerMap())
	c, _ := snap.constraints()
	view := s.view(ctx, snap)
	rc := RunContext{
		SessionID: snap.sess.ID, UserState: snap.sess.UserState,
		Attributes: b.Attributes, Tradeoffs: view.Tradeoffs, Usage: usage, Constraints: c,
		Warnings: b.Warnings, Assumptions: set.Provenance, PlaceholderCnt: len(set.UnverifiedKeys()),
		HorizonYears: b.Inputs.HorizonYears, Ladder: s.cfg.Interview.Ladder,
		Eliminated: []CarView{},
	}
	for _, sc := range b.Cars {
		v := sc.Variant
		v.Sources = nil
		rc.Cars = append(rc.Cars, RunCar{ItemID: sc.Item.ID, VariantID: v.ID, Label: sc.Label, Condition: sc.Item.Condition, Variant: v, Price: sc.Price})
	}
	for _, cv := range view.Cars {
		if cv.Eliminated {
			rc.Eliminated = append(rc.Eliminated, cv)
		}
	}
	return rc
}

// encodePayload serialises the inputs and context and hashes the inputs.
func encodePayload(in sim.Inputs, rc RunContext) (json.RawMessage, string, error) {
	ctxJSON, err := json.Marshal(rc)
	if err != nil {
		return nil, "", fmt.Errorf("encoding run context: %w", err)
	}
	inputsJSON, err := json.Marshal(in)
	if err != nil {
		return nil, "", fmt.Errorf("encoding inputs: %w", err)
	}
	payload, err := json.Marshal(jobs.Payload{Inputs: in, Context: ctxJSON})
	if err != nil {
		return nil, "", fmt.Errorf("encoding payload: %w", err)
	}
	sum := sha256.Sum256(append(inputsJSON, []byte(sim.ModelVersion)...))
	return payload, hex.EncodeToString(sum[:]), nil
}

// randomSeed draws the run's master seed from crypto/rand.
func randomSeed() uint64 {
	var b [8]byte
	_, _ = rand.Read(b[:]) // never fails on supported platforms (Go 1.24+)
	return binary.LittleEndian.Uint64(b[:])
}

// RunView is GET /v1/simulations/:id: the run plus, when done, its results.
type RunView struct {
	jobs.Run
	Seed    string           `json:"master_seed"` // decimal string: a uint64 doesn't fit in a JSON number safely
	Results []jobs.ResultRow `json:"results,omitempty"`
}

// GetRun returns a run's status, progress and (when done) results.
func (s *Service) GetRun(ctx context.Context, runID string) (RunView, error) {
	run, err := jobs.GetRun(s.db.WithContext(ctx), runID)
	if err != nil {
		return RunView{}, err
	}
	v := RunView{Run: run, Seed: fmt.Sprintf("%d", run.Seed())}
	if run.Status == jobs.StatusDone {
		if v.Results, err = jobs.ResultsFor(s.db.WithContext(ctx), runID); err != nil {
			return RunView{}, err
		}
	}
	return v, nil
}

// CancelRun cancels a run (SPEC §11). A queued run is cancelled at once and
// the session goes back to INTERVIEW; a running run gets cancel_requested,
// which the worker's heartbeat picks up within about a second.
func (s *Service) CancelRun(ctx context.Context, runID string) (jobs.Run, error) {
	var out jobs.Run
	err := store.WithTx(ctx, s.db, func(tx *gorm.DB) error {
		run, err := jobs.LockRun(tx, runID)
		if err != nil {
			return err
		}
		switch run.Status {
		case jobs.StatusQueued:
			if err := tx.Model(&jobs.Run{}).Where("id = ?", runID).Updates(map[string]any{
				"status": jobs.StatusCancelled, "finished_at": gorm.Expr("now()"), "updated_at": gorm.Expr("now()"),
			}).Error; err != nil {
				return fmt.Errorf("cancelling run: %w", err)
			}
			if err := FinishRun(tx, run.SessionID, StateInterview); err != nil {
				return err
			}
		case jobs.StatusRunning:
			if err := tx.Model(&jobs.Run{}).Where("id = ?", runID).
				Updates(map[string]any{"cancel_requested": true, "updated_at": gorm.Expr("now()")}).Error; err != nil {
				return fmt.Errorf("requesting cancel: %w", err)
			}
		default:
			return apperr.New(apperr.ErrConflict, fmt.Sprintf("the simulation has already finished (%s)", run.Status))
		}
		out, err = jobs.GetRun(tx, runID)
		return err
	})
	return out, err
}

// FinishRun moves a session out of SIMULATING (to DONE, or back to INTERVIEW
// after a cancel or failure), bumping its version so open tabs see the change.
func FinishRun(tx *gorm.DB, sessionID string, to State) error {
	if err := CheckTransition(StateSimulating, to); err != nil {
		return err
	}
	res := tx.Model(&Session{}).Where("id = ? AND state = ?", sessionID, StateSimulating).
		Updates(map[string]any{"state": to, "version": gorm.Expr("version + 1"), "updated_at": gorm.Expr("now()")})
	if res.Error != nil {
		return fmt.Errorf("finishing simulation state: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return apperr.New(apperr.ErrIllegalTransition, "the session is not simulating")
	}
	return nil
}

// DoneHook runs extra work in the completion transaction (M5: write the report).
type DoneHook func(ctx context.Context, tx *gorm.DB, run jobs.Run, out jobs.Outcome) error

// Finalizer implements jobs.Finalizer: it moves the session on when a run
// ends, in the same transaction as the run's final status.
type Finalizer struct {
	OnDone DoneHook
}

// Done is called when a run finished successfully.
func (f Finalizer) Done(ctx context.Context, tx *gorm.DB, run jobs.Run, out jobs.Outcome) error {
	if f.OnDone != nil {
		if err := f.OnDone(ctx, tx, run, out); err != nil {
			return err
		}
	}
	return FinishRun(tx, run.SessionID, StateDone)
}

// Stopped is called when a run was cancelled or failed.
func (f Finalizer) Stopped(_ context.Context, tx *gorm.DB, run jobs.Run, _ jobs.Status) error {
	return FinishRun(tx, run.SessionID, StateInterview)
}
