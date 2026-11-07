package main

import (
	"context"
	"log/slog"

	"github.com/xoBerlierxo/TieBreaker/internal/config"
	"github.com/xoBerlierxo/TieBreaker/internal/store"
)

// worker runs background simulation jobs until ctx is cancelled.
//
// M0: it only connects to Postgres and Redis (so a misconfigured worker fails
// fast at startup) and then waits. The job loop arrives in M4.
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

	logger.Info("worker started; job loop not implemented yet (M4)")
	<-ctx.Done()
	logger.Info("worker stopping")
	return nil
}
