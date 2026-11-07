package report

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
	"github.com/xoBerlierxo/TieBreaker/internal/jobs"
	"github.com/xoBerlierxo/TieBreaker/internal/session"
)

// Report is one row of the reports table.
type Report struct {
	ID          int64           `gorm:"column:id;primaryKey"`
	SessionID   string          `gorm:"column:session_id"`
	RunID       *string         `gorm:"column:run_id"`
	Kind        string          `gorm:"column:kind"`
	FactsJSON   json.RawMessage `gorm:"column:facts_json;type:jsonb"`
	ProseMD     string          `gorm:"column:prose_md"`
	ProseSource string          `gorm:"column:prose_source"`
	ShareID     string          `gorm:"column:share_id"`
	CreatedAt   time.Time       `gorm:"column:created_at"`
}

// TableName tells GORM the table name explicitly.
func (Report) TableName() string { return "reports" }

// Insert stores a report with a fresh share ID (128 random bits, SPEC §14).
// Call it inside the transaction that finishes the run (or applies the
// constraints, for none_fit), so the report and the state change commit together.
func Insert(tx *gorm.DB, sessionID string, runID *string, d Draft) (Report, error) {
	facts, err := json.Marshal(d.Facts)
	if err != nil {
		return Report{}, fmt.Errorf("encoding facts: %w", err)
	}
	r := Report{
		SessionID: sessionID, RunID: runID, Kind: string(d.Facts.Kind), FactsJSON: facts,
		ProseMD: d.ProseMD, ProseSource: d.ProseSource, ShareID: session.NewID(),
	}
	if err := tx.Create(&r).Error; err != nil {
		return Report{}, fmt.Errorf("storing report: %w", err)
	}
	return r, nil
}

// OnRunDone is the session finalizer hook for finished runs: it stores the
// report drafted by the Analyzer (jobs.RunOutput.Analysis).
func OnRunDone(_ context.Context, tx *gorm.DB, run jobs.Run, out jobs.Outcome) error {
	ro, ok := out.Extra.(jobs.RunOutput)
	if !ok {
		return errors.New("run output missing")
	}
	d, ok := ro.Analysis.(Draft)
	if !ok {
		return errors.New("report draft missing from run output")
	}
	_, err := Insert(tx, run.SessionID, &run.ID, d)
	return err
}

// OnNoneFit is the session hook for "every car eliminated": it stores the
// none_fit report in the constraints transaction.
func OnNoneFit(_ context.Context, tx *gorm.DB, sessionID string, cars []session.CarView) error {
	var userState string
	if err := tx.Model(&session.Session{}).Select("user_state").Where("id = ?", sessionID).Scan(&userState).Error; err != nil {
		return fmt.Errorf("loading session for none_fit report: %w", err)
	}
	f := BuildNoneFit(userState, cars)
	prose, err := RenderTemplate(f)
	if err != nil {
		return err
	}
	_, err = Insert(tx, sessionID, nil, Draft{Facts: f, ProseMD: prose, ProseSource: SourceTemplate})
	return err
}

// View is the API representation of a report. It deliberately has no session
// ID: the same view is served on the public share link (SPEC §6.7).
type View struct {
	Kind        string           `json:"kind"`
	ShareID     string           `json:"share_id"`
	SharePath   string           `json:"share_path"`
	RunID       *string          `json:"run_id,omitempty"`
	Facts       json.RawMessage  `json:"facts"`
	ProseMD     string           `json:"prose_md"`
	ProseSource string           `json:"prose_source"`
	CreatedAt   time.Time        `json:"created_at"`
	Results     []jobs.ResultRow `json:"results,omitempty"` // includes the net-cost histograms
}

// Service serves stored reports.
type Service struct{ db *gorm.DB }

// NewService creates a report service.
func NewService(db *gorm.DB) *Service { return &Service{db: db} }

// ForSession returns the latest report of a session (SPEC §12).
func (s *Service) ForSession(ctx context.Context, sessionID string) (View, error) {
	var r Report
	err := s.db.WithContext(ctx).Where("session_id = ?", sessionID).Order("created_at DESC, id DESC").First(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		var sess session.Session
		switch err := s.db.WithContext(ctx).Select("state").First(&sess, "id = ?", sessionID).Error; {
		case errors.Is(err, gorm.ErrRecordNotFound):
			return View{}, apperr.New(apperr.ErrNotFound, "session not found")
		case err != nil:
			return View{}, fmt.Errorf("loading session: %w", err)
		case sess.State == session.StateSimulating:
			return View{}, apperr.New(apperr.ErrIllegalTransition, "the simulation is still running")
		default:
			return View{}, apperr.New(apperr.ErrNotFound, "no report yet: finish the interview and run the simulation")
		}
	}
	if err != nil {
		return View{}, fmt.Errorf("loading report: %w", err)
	}
	return s.view(ctx, r)
}

// ByShareID returns a report by its public share ID (SPEC §6.7).
func (s *Service) ByShareID(ctx context.Context, shareID string) (View, error) {
	var r Report
	err := s.db.WithContext(ctx).First(&r, "share_id = ?", shareID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return View{}, apperr.New(apperr.ErrNotFound, "report not found")
	}
	if err != nil {
		return View{}, fmt.Errorf("loading report: %w", err)
	}
	return s.view(ctx, r)
}

func (s *Service) view(ctx context.Context, r Report) (View, error) {
	v := View{
		Kind: r.Kind, ShareID: r.ShareID, SharePath: "/r/" + r.ShareID, RunID: r.RunID,
		Facts: r.FactsJSON, ProseMD: r.ProseMD, ProseSource: r.ProseSource, CreatedAt: r.CreatedAt,
	}
	if r.RunID != nil {
		rows, err := jobs.ResultsFor(s.db.WithContext(ctx), *r.RunID)
		if err != nil {
			return View{}, err
		}
		v.Results = rows
	}
	return v, nil
}
