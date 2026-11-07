package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"

	"github.com/xoBerlierxo/TieBreaker/internal/jobs"
	"github.com/xoBerlierxo/TieBreaker/internal/store"
)

// verifyRunCmd re-runs a stored simulation and checks it reproduces exactly:
//
//	tiebreaker verify-run -run-id <id>
//
// It needs only DATABASE_URL. Exit status 1 if the results differ.
func verifyRunCmd(ctx context.Context, args []string, getenv func(string) string, out io.Writer, logger *slog.Logger) error {
	fs := flag.NewFlagSet("verify-run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	runID := fs.String("run-id", "", "the simulation run to verify")
	if err := fs.Parse(args); err != nil || *runID == "" {
		return fmt.Errorf("verify-run needs -run-id: %w", errUsage)
	}
	dsn := getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}
	db, err := store.OpenPostgres(ctx, dsn, logger)
	if err != nil {
		return err
	}
	defer func() { _ = store.ClosePostgres(db) }()

	v, err := jobs.VerifyRun(ctx, db, *runID)
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	_, _ = fmt.Fprintln(out, string(b))
	if !v.Match {
		return fmt.Errorf("run %s did NOT reproduce", *runID)
	}
	return nil
}
