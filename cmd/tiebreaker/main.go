// Command tiebreaker is the single binary for the whole backend (SPEC §4).
//
//	tiebreaker serve    run the HTTP API
//	tiebreaker worker   run background simulation jobs
//
// More subcommands (migrate, admin-token, verify-run) are added in later
// milestones. main is the only function allowed to call os.Exit (SPEC §18:
// no panics outside main); everything else returns errors.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"

	"github.com/xoBerlierxo/TieBreaker/internal/config"
)

// errUsage means the command line was wrong; main exits with code 2 for it.
var errUsage = errors.New("usage: tiebreaker <serve|worker|migrate up|down|version|admin-token|verify-run>")

func main() {
	// ctx is cancelled on Ctrl+C (SIGINT) or SIGTERM (what Docker and Railway
	// send on stop). Cancelling it starts a graceful shutdown everywhere.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	err := run(ctx, os.Args[1:], os.Getenv, logger)
	stop()

	switch {
	case err == nil:
		return
	case errors.Is(err, errUsage):
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	default:
		logger.Error("exiting with error", slog.Any("error", err))
		os.Exit(1)
	}
}

// run dispatches to a subcommand. It takes everything from the outside world
// (args, environment, logger) as parameters, so tests can call it directly.
func run(ctx context.Context, args []string, getenv func(string) string, logger *slog.Logger) error {
	if len(args) == 0 {
		return errUsage
	}

	// Subcommands that don't need the full app config.
	switch args[0] {
	case "migrate":
		return migrateCmd(ctx, args[1:], getenv, logger)
	case "admin-token":
		return adminTokenCmd(args[1:], getenv, os.Stdout)
	}

	var cmd func(context.Context, config.Config, *slog.Logger) error
	switch args[0] {
	case "serve":
		cmd = serve
	case "worker":
		cmd = worker
	default:
		return fmt.Errorf("unknown subcommand %q: %w", args[0], errUsage)
	}

	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	if cfg.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	logger.Info("starting", slog.String("subcommand", args[0]), slog.Any("config", cfg))
	return cmd(ctx, cfg, logger)
}
