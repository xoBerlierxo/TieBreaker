package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/xoBerlierxo/TieBreaker/internal/apperr"
	"github.com/xoBerlierxo/TieBreaker/internal/store"
)

// Queue is the Postgres-backed job queue over sim_runs.
//
// All lease arithmetic uses the database clock (now()), never the worker's,
// so workers with skewed clocks still agree on whose lease has expired.
type Queue struct {
	db          *gorm.DB
	lease       time.Duration
	maxAttempts int
}

// NewQueue creates a queue. lease is how long a claim lasts without a
// heartbeat; maxAttempts caps how often a run is retried after worker crashes.
func NewQueue(db *gorm.DB, lease time.Duration, maxAttempts int) *Queue {
	return &Queue{db: db, lease: lease, maxAttempts: maxAttempts}
}

// Lease returns the configured lease duration.
func (q *Queue) Lease() time.Duration { return q.lease }

func (q *Queue) leaseSeconds() float64 { return q.lease.Seconds() }

// Insert adds a queued run. It must be called inside the caller's transaction,
// so the run and the session's state change commit together. A second active
// run for the same session violates a partial unique index → 409.
func Insert(tx *gorm.DB, run *Run) error {
	run.Status = StatusQueued
	if err := tx.Create(run).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return apperr.New(apperr.ErrConflict, "a simulation is already running for this session")
		}
		return fmt.Errorf("queueing simulation: %w", err)
	}
	return nil
}

// Claim takes the oldest available run for workerID, or returns (nil, nil) if
// there is none (SPEC §11). Available means queued, or running with an expired
// lease (its worker crashed).
//
//	SELECT … FOR UPDATE SKIP LOCKED  — rows another worker is claiming right now
//	                                   are skipped instead of waited for, so many
//	                                   workers can poll without blocking each other.
func (q *Queue) Claim(ctx context.Context, workerID string) (*Run, error) {
	var claimed *Run
	err := store.WithTx(ctx, q.db, func(tx *gorm.DB) error {
		var candidates []Run
		err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("kind = ? AND (status = ? OR (status = ? AND lease_expires_at < now()))", KindFull, StatusQueued, StatusRunning).
			Order("created_at, id").Limit(1).
			Find(&candidates).Error
		if err != nil {
			return fmt.Errorf("claiming run: %w", err)
		}
		if len(candidates) == 0 {
			return nil
		}
		run := candidates[0]

		// A run whose worker keeps dying is not retried forever.
		if run.Attempts >= q.maxAttempts {
			msg := fmt.Sprintf("gave up after %d attempts (the worker stopped responding)", run.Attempts)
			return tx.Model(&Run{}).Where("id = ?", run.ID).Updates(map[string]any{
				"status": StatusFailed, "error": msg, "worker_id": nil, "lease_expires_at": nil,
				"finished_at": gorm.Expr("now()"), "updated_at": gorm.Expr("now()"),
			}).Error
		}

		err = tx.Model(&Run{}).Where("id = ?", run.ID).Updates(map[string]any{
			"status":           StatusRunning,
			"worker_id":        workerID,
			"lease_expires_at": gorm.Expr("now() + make_interval(secs => ?)", q.leaseSeconds()),
			"attempts":         gorm.Expr("attempts + 1"),
			"started_at":       gorm.Expr("COALESCE(started_at, now())"),
			"updated_at":       gorm.Expr("now()"),
		}).Error
		if err != nil {
			return fmt.Errorf("marking run running: %w", err)
		}
		var fresh Run
		if err := tx.First(&fresh, "id = ?", run.ID).Error; err != nil {
			return fmt.Errorf("reloading claimed run: %w", err)
		}
		claimed = &fresh
		return nil
	})
	return claimed, err
}

// ExtendLease is the heartbeat: it pushes the lease forward and reports whether
// a cancel was requested. If the worker no longer owns the run, ErrLeaseLost.
func (q *Queue) ExtendLease(ctx context.Context, runID, workerID string) (cancelRequested bool, err error) {
	var rows []struct{ CancelRequested bool }
	err = q.db.WithContext(ctx).Raw(`UPDATE sim_runs
		SET lease_expires_at = now() + make_interval(secs => ?), updated_at = now()
		WHERE id = ? AND worker_id = ? AND status = ?
		RETURNING cancel_requested`, q.leaseSeconds(), runID, workerID, StatusRunning).Scan(&rows).Error
	if err != nil {
		return false, fmt.Errorf("extending lease: %w", err)
	}
	if len(rows) == 0 {
		return false, ErrLeaseLost
	}
	return rows[0].CancelRequested, nil
}

// SetProgress records progress (fenced on worker_id; best effort).
func (q *Queue) SetProgress(ctx context.Context, runID, workerID string, progress int) error {
	res := q.db.WithContext(ctx).Model(&Run{}).
		Where("id = ? AND worker_id = ? AND status = ?", runID, workerID, StatusRunning).
		Updates(map[string]any{"progress": progress, "updated_at": gorm.Expr("now()")})
	if res.Error != nil {
		return fmt.Errorf("saving progress: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrLeaseLost
	}
	return nil
}

// finish moves a running run to a terminal status, fenced on worker_id.
func finish(tx *gorm.DB, runID, workerID string, status Status, fields map[string]any) error {
	updates := map[string]any{
		"status": status, "worker_id": nil, "lease_expires_at": nil,
		"finished_at": gorm.Expr("now()"), "updated_at": gorm.Expr("now()"),
	}
	for k, v := range fields {
		updates[k] = v
	}
	res := tx.Model(&Run{}).Where("id = ? AND worker_id = ? AND status = ?", runID, workerID, StatusRunning).Updates(updates)
	if res.Error != nil {
		return fmt.Errorf("finishing run: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrLeaseLost
	}
	return nil
}

// Complete marks the run done. It must run in the same transaction that writes
// the results, so results are only ever stored by the worker that owns the run.
func Complete(tx *gorm.DB, runID, workerID, resultsHash string) error {
	return finish(tx, runID, workerID, StatusDone, map[string]any{"progress": 100, "results_hash": resultsHash})
}

// MarkCancelled marks a running run cancelled (fenced).
func MarkCancelled(tx *gorm.DB, runID, workerID string) error {
	return finish(tx, runID, workerID, StatusCancelled, nil)
}

// MarkFailed marks a running run failed with a message (fenced).
func MarkFailed(tx *gorm.DB, runID, workerID, msg string) error {
	return finish(tx, runID, workerID, StatusFailed, map[string]any{"error": msg})
}

// Release hands a running run back to the queue without counting the attempt,
// e.g. when the worker shuts down before finishing.
func (q *Queue) Release(ctx context.Context, runID, workerID string) error {
	res := q.db.WithContext(ctx).Model(&Run{}).
		Where("id = ? AND worker_id = ? AND status = ?", runID, workerID, StatusRunning).
		Updates(map[string]any{
			"status": StatusQueued, "worker_id": nil, "lease_expires_at": nil, "progress": 0,
			"attempts": gorm.Expr("GREATEST(attempts - 1, 0)"), "updated_at": gorm.Expr("now()"),
		})
	if res.Error != nil {
		return fmt.Errorf("releasing run: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrLeaseLost
	}
	return nil
}

// Get loads a run by ID.
func (q *Queue) Get(ctx context.Context, runID string) (Run, error) {
	return GetRun(q.db.WithContext(ctx), runID)
}

// GetRun loads a run by ID using db (which may be a transaction).
func GetRun(db *gorm.DB, runID string) (Run, error) {
	var run Run
	err := db.First(&run, "id = ?", runID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Run{}, apperr.New(apperr.ErrNotFound, "simulation not found")
	}
	if err != nil {
		return Run{}, fmt.Errorf("loading simulation: %w", err)
	}
	return run, nil
}

// LockRun loads a run with a row lock, inside a transaction.
func LockRun(tx *gorm.DB, runID string) (Run, error) {
	return GetRun(tx.Clauses(clause.Locking{Strength: "UPDATE"}), runID)
}

// ResultsFor returns a run's per-car results.
func ResultsFor(db *gorm.DB, runID string) ([]ResultRow, error) {
	var rows []ResultRow
	if err := db.Where("run_id = ?", runID).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("loading results: %w", err)
	}
	return rows, nil
}
