// Command seed loads the curated car data and assumptions from data/seed into
// the database (SPEC §7). It is safe to run repeatedly: rows are upserted.
//
//	go run ./scripts/seed                    # local docker compose database
//	DATABASE_URL=postgres://... go run ./scripts/seed
//	go run ./scripts/seed --check            # validate the files without a database
//
// It works from any folder inside the repository (e.g. web/): the seed
// directory is found by walking up to the folder that contains go.mod.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/xoBerlierxo/TieBreaker/internal/catalog"
	"github.com/xoBerlierxo/TieBreaker/internal/store"
)

// localDSN is the docker-compose development database (see docker-compose.yml).
// It is only used when DATABASE_URL is unset, and it only ever points at
// localhost, so it can't seed a remote database by accident.
const localDSN = "postgres://tiebreaker:tiebreaker_dev_only@localhost:5432/tiebreaker?sslmode=disable" // #nosec G101 -- published dev-only password, localhost only

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

func run() error {
	dir := flag.String("dir", "", "seed directory (default: <repo root>/data/seed)")
	check := flag.Bool("check", false, "only validate the seed files; don't touch the database")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	seedDir := *dir
	if seedDir == "" {
		root, err := repoRoot()
		if err != nil {
			return err
		}
		seedDir = filepath.Join(root, "data", "seed")
	}

	if *check {
		variants, files, err := catalog.ReadSeedDir(seedDir)
		if err != nil {
			return err
		}
		n := 0
		for _, f := range files {
			n += len(f.Assumptions)
		}
		logger.Info("seed files are valid", "dir", seedDir, "variants", len(variants), "assumptions", n)
		return nil
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = localDSN
		logger.Info("DATABASE_URL not set: using the local docker compose database (localhost:5432)")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	db, err := store.OpenPostgres(ctx, dsn, logger)
	if err != nil {
		return fmt.Errorf("%w (is `docker compose up -d` running?)", err)
	}
	defer func() { _ = store.ClosePostgres(db) }()

	res, err := catalog.LoadDir(ctx, db, seedDir)
	if err != nil {
		return err
	}
	logger.Info("seed loaded", "dir", seedDir, "variants", res.Variants, "sources", res.Sources, "assumptions", res.Assumptions)
	return nil
}

// repoRoot walks up from the current folder to the one containing go.mod.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("not inside the Tiebreaker repository (no go.mod found); pass --dir")
		}
		dir = parent
	}
}
