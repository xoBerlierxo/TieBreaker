// Command seed loads the curated car data and assumptions from data/seed into
// the database (SPEC §7). It is safe to run repeatedly: rows are upserted.
//
//	DATABASE_URL=postgres://... go run ./scripts/seed --dir data/seed
//	go run ./scripts/seed --check     # validate the files without a database
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"

	"github.com/xoBerlierxo/TieBreaker/internal/catalog"
	"github.com/xoBerlierxo/TieBreaker/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

func run() error {
	dir := flag.String("dir", "data/seed", "seed directory")
	check := flag.Bool("check", false, "only validate the seed files; don't touch the database")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if *check {
		variants, files, err := catalog.ReadSeedDir(*dir)
		if err != nil {
			return err
		}
		n := 0
		for _, f := range files {
			n += len(f.Assumptions)
		}
		logger.Info("seed files are valid", "variants", len(variants), "assumptions", n)
		return nil
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	db, err := store.OpenPostgres(ctx, dsn, logger)
	if err != nil {
		return err
	}
	defer func() { _ = store.ClosePostgres(db) }()

	res, err := catalog.LoadDir(ctx, db, *dir)
	if err != nil {
		return err
	}
	logger.Info("seed loaded", "variants", res.Variants, "sources", res.Sources, "assumptions", res.Assumptions)
	return nil
}
