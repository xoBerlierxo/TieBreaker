package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"

	"github.com/xoBerlierxo/TieBreaker/internal/store"
)

// migrateCmd runs database migrations:
//
//	tiebreaker migrate up
//	tiebreaker migrate down [-steps N]     (default: roll back everything)
//	tiebreaker migrate version
//
// It needs only DATABASE_URL, not the full app config, so it can run as a
// one-shot job before the API and worker start.
func migrateCmd(ctx context.Context, args []string, getenv func(string) string, logger *slog.Logger) error {
	if len(args) == 0 {
		return fmt.Errorf("migrate needs a direction (up, down, version): %w", errUsage)
	}
	dsn := getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}

	switch args[0] {
	case "up":
		if err := store.MigrateUp(ctx, dsn); err != nil {
			return err
		}
	case "down":
		fs := flag.NewFlagSet("migrate down", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		steps := fs.Int("steps", 0, "number of migrations to roll back (0 = all)")
		if err := fs.Parse(args[1:]); err != nil {
			return fmt.Errorf("%w: %w", err, errUsage)
		}
		if err := store.MigrateDown(ctx, dsn, *steps); err != nil {
			return err
		}
	case "version":
	default:
		return fmt.Errorf("unknown migrate direction %q: %w", args[0], errUsage)
	}

	version, dirty, err := store.MigrationVersion(ctx, dsn)
	if err != nil {
		return err
	}
	logger.Info("migrations", slog.String("command", args[0]), slog.Uint64("version", uint64(version)), slog.Bool("dirty", dirty))
	return nil
}
