package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/xoBerlierxo/TieBreaker/internal/config"
	"github.com/xoBerlierxo/TieBreaker/internal/decision"
	"github.com/xoBerlierxo/TieBreaker/internal/jobs"
	"github.com/xoBerlierxo/TieBreaker/internal/report"
	"github.com/xoBerlierxo/TieBreaker/internal/session"
	"github.com/xoBerlierxo/TieBreaker/internal/store"
)

// worker runs background simulation jobs until ctx is cancelled (SPEC §11).
// On shutdown, the job in progress gets SHUTDOWN_GRACE_SECONDS to finish;
// otherwise it is released back to the queue for another worker.
func worker(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	db, err := store.OpenPostgres(ctx, cfg.DatabaseURL, logger)
	if err != nil {
		return err
	}
	defer func() {
		if err := store.ClosePostgres(db); err != nil {
			logger.Warn("closing postgres", slog.Any("error", err))
		}
	}()

	rdb, err := store.OpenRedis(ctx, cfg.RedisURL)
	if err != nil {
		return err
	}
	defer func() {
		if err := rdb.Close(); err != nil {
			logger.Warn("closing redis", slog.Any("error", err))
		}
	}()

	w := &jobs.Worker{
		ID:    workerID(),
		DB:    db,
		Queue: jobs.NewQueue(db, cfg.Jobs.Lease, cfg.Jobs.MaxAttempts),
		Runner: jobs.SimRunner{Workers: cfg.Sim.Workers, Analyze: report.Analyzer{
			Thresholds:   decision.Thresholds{WinPBest: cfg.Decision.WinPBest, WinGap: cfg.Decision.WinGap},
			QuickFutures: cfg.Sim.QuickFutures,
			ChunkSize:    cfg.Sim.ChunkSize,
		}.Analyze},
		Finalizer:     session.Finalizer{OnDone: report.OnRunDone},
		Publisher:     jobs.RedisPublisher{Client: rdb},
		Logger:        logger,
		PollInterval:  cfg.Jobs.PollInterval,
		Heartbeat:     cfg.Jobs.Heartbeat,
		ShutdownGrace: cfg.HTTP.ShutdownGrace,
	}
	return w.Run(ctx)
}

// workerID identifies this worker in sim_runs.worker_id: the host name plus a
// random suffix, so two workers on one host never share an identity.
func workerID() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "worker"
	}
	return host + "-" + session.NewID()[:8]
}
