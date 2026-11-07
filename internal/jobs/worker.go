package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/xoBerlierxo/TieBreaker/internal/store"
)

// Runner does the actual work for one run. progress reports 0–100.
type Runner interface {
	Run(ctx context.Context, run Run, progress func(pct int)) (Outcome, error)
}

// Outcome is a successful run's output.
type Outcome struct {
	Results     []ResultRow
	ResultsHash string
	Extra       any // passed to the Finalizer (e.g. the decision and tipping points)
}

// Finalizer runs inside the completion transaction, so the session's state
// change (and in M5, the report) commits atomically with the run's status.
type Finalizer interface {
	// Done is called after the run is marked done and its results are stored.
	Done(ctx context.Context, tx *gorm.DB, run Run, out Outcome) error
	// Stopped is called after a run is cancelled or fails.
	Stopped(ctx context.Context, tx *gorm.DB, run Run, status Status) error
}

// Causes for cancelling a job's context.
var (
	errCancelRequested = errors.New("cancel requested")
	errShutdown        = errors.New("worker shutting down")
)

// Worker claims and runs jobs until its context is cancelled.
type Worker struct {
	ID            string
	DB            *gorm.DB
	Queue         *Queue
	Runner        Runner
	Finalizer     Finalizer
	Publisher     Publisher
	Logger        *slog.Logger
	PollInterval  time.Duration // how long to wait when the queue is empty
	Heartbeat     time.Duration // how often to extend the lease and check for cancel
	ShutdownGrace time.Duration // how long a running job may continue after shutdown starts
}

// Run is the worker loop. It returns nil when ctx is cancelled (graceful
// shutdown): the job in progress gets ShutdownGrace to finish, otherwise it is
// released back to the queue for another worker.
func (w *Worker) Run(ctx context.Context) error {
	w.Logger.Info("worker started", slog.String("worker_id", w.ID))
	for {
		if ctx.Err() != nil {
			w.Logger.Info("worker stopped", slog.String("worker_id", w.ID))
			return nil
		}
		run, err := w.Queue.Claim(ctx, w.ID)
		switch {
		case err != nil && ctx.Err() == nil:
			w.Logger.Error("claiming a job failed", slog.Any("error", err))
			w.sleep(ctx)
		case run == nil:
			w.sleep(ctx)
		default:
			w.process(ctx, *run)
		}
	}
}

// sleep waits one poll interval (with ±20 % jitter, so many idle workers don't
// hit the database in lockstep) or until ctx is cancelled.
func (w *Worker) sleep(ctx context.Context) {
	d := w.PollInterval
	if d > 0 {
		d = time.Duration(float64(d) * (0.8 + 0.4*rand.Float64())) // #nosec G404 -- jitter, not security
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// process runs one claimed job to a terminal state (or releases it).
func (w *Worker) process(parent context.Context, run Run) {
	log := w.Logger.With(slog.String("run_id", run.ID), slog.String("worker_id", w.ID), slog.Int("attempt", run.Attempts))
	log.Info("job started")

	// The job's context is independent of the worker's, so a shutdown doesn't
	// kill it instantly; it is cancelled with a cause instead.
	jobCtx, cancel := context.WithCancelCause(context.WithoutCancel(parent))
	defer cancel(nil)

	var wg sync.WaitGroup
	stopBackground := make(chan struct{})

	// Shutdown watcher: on worker shutdown, allow ShutdownGrace, then stop the job.
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-stopBackground:
			return
		case <-parent.Done():
		}
		t := time.NewTimer(w.ShutdownGrace)
		defer t.Stop()
		select {
		case <-stopBackground:
		case <-t.C:
			cancel(errShutdown)
		}
	}()

	// Heartbeat: extend the lease and pick up cancel requests (SPEC §11).
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(w.Heartbeat)
		defer t.Stop()
		for {
			select {
			case <-stopBackground:
				return
			case <-t.C:
				cancelReq, err := w.Queue.ExtendLease(jobCtx, run.ID, w.ID)
				switch {
				case errors.Is(err, ErrLeaseLost):
					cancel(ErrLeaseLost)
					return
				case err != nil:
					log.Warn("heartbeat failed", slog.Any("error", err))
				case cancelReq:
					cancel(errCancelRequested)
					return
				}
			}
		}
	}()

	th := newThrottle(2, 250*time.Millisecond)
	progress := func(pct int) {
		pct = max(0, min(pct, 99)) // 100 is reserved for "done"
		if !th.allow(pct) {
			return
		}
		if err := w.Queue.SetProgress(jobCtx, run.ID, w.ID, pct); err != nil && !errors.Is(err, ErrLeaseLost) {
			log.Warn("saving progress failed", slog.Any("error", err))
		}
		w.publish(jobCtx, ProgressEvent{RunID: run.ID, Status: StatusRunning, Progress: pct})
	}

	out, runErr := w.Runner.Run(jobCtx, run, progress)
	close(stopBackground)
	wg.Wait()
	cause := context.Cause(jobCtx)

	// Finishing uses a fresh context: the job context may already be cancelled.
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(parent), 30*time.Second)
	defer finishCancel()

	switch {
	case runErr == nil:
		w.complete(finishCtx, log, run, out)
	case errors.Is(cause, errCancelRequested):
		w.stop(finishCtx, log, run, StatusCancelled, "")
	case errors.Is(cause, errShutdown):
		if err := w.Queue.Release(finishCtx, run.ID, w.ID); err != nil {
			log.Warn("releasing job on shutdown failed", slog.Any("error", err))
		} else {
			log.Info("job released back to the queue (shutdown)")
			w.publish(finishCtx, ProgressEvent{RunID: run.ID, Status: StatusQueued})
		}
	case errors.Is(cause, ErrLeaseLost):
		log.Warn("lease lost; another worker owns this job now")
	default:
		log.Error("job failed", slog.Any("error", runErr))
		w.stop(finishCtx, log, run, StatusFailed, runErr.Error())
	}
}

func (w *Worker) complete(ctx context.Context, log *slog.Logger, run Run, out Outcome) {
	err := store.WithTx(ctx, w.DB, func(tx *gorm.DB) error {
		if err := Complete(tx, run.ID, w.ID, out.ResultsHash); err != nil {
			return err
		}
		if len(out.Results) > 0 {
			for i := range out.Results {
				out.Results[i].RunID = run.ID
			}
			if err := tx.Create(&out.Results).Error; err != nil {
				return fmt.Errorf("storing results: %w", err)
			}
		}
		if w.Finalizer != nil {
			return w.Finalizer.Done(ctx, tx, run, out)
		}
		return nil
	})
	switch {
	case errors.Is(err, ErrLeaseLost):
		log.Warn("finished, but the lease was lost; results discarded (another worker owns the job)")
		return
	case err != nil:
		log.Error("storing the job's results failed", slog.Any("error", err))
		w.stop(ctx, log, run, StatusFailed, "storing results failed")
		return
	}
	log.Info("job done")
	w.publish(ctx, ProgressEvent{RunID: run.ID, Status: StatusDone, Progress: 100})
}

func (w *Worker) stop(ctx context.Context, log *slog.Logger, run Run, status Status, msg string) {
	err := store.WithTx(ctx, w.DB, func(tx *gorm.DB) error {
		var err error
		if status == StatusCancelled {
			err = MarkCancelled(tx, run.ID, w.ID)
		} else {
			err = MarkFailed(tx, run.ID, w.ID, msg)
		}
		if err != nil {
			return err
		}
		if w.Finalizer != nil {
			return w.Finalizer.Stopped(ctx, tx, run, status)
		}
		return nil
	})
	if err != nil {
		log.Warn("recording the job's end failed", slog.String("status", string(status)), slog.Any("error", err))
		return
	}
	log.Info("job stopped", slog.String("status", string(status)))
	w.publish(ctx, ProgressEvent{RunID: run.ID, Status: status, Error: msg})
}

func (w *Worker) publish(ctx context.Context, ev ProgressEvent) {
	if w.Publisher == nil {
		return
	}
	if err := w.Publisher.Publish(ctx, ev); err != nil {
		w.Logger.Warn("publishing progress failed", slog.String("run_id", ev.RunID), slog.Any("error", err))
	}
}
