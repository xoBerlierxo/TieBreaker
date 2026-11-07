package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver

	"github.com/xoBerlierxo/TieBreaker/migrations"
)

// MigrateUp applies every pending migration from the embedded migrations/*.sql
// files (SPEC §3: golang-migrate with plain SQL, never GORM AutoMigrate).
// Running it when the schema is already current is a no-op.
//
// golang-migrate takes a Postgres advisory lock, so two processes running this
// at the same moment (e.g. two containers starting) can't both apply a migration.
func MigrateUp(ctx context.Context, dsn string) error {
	return withMigrate(ctx, dsn, func(m *migrate.Migrate) error { return m.Up() })
}

// MigrateDown rolls back `steps` migrations, or all of them if steps <= 0.
func MigrateDown(ctx context.Context, dsn string, steps int) error {
	return withMigrate(ctx, dsn, func(m *migrate.Migrate) error {
		if steps <= 0 {
			return m.Down()
		}
		return m.Steps(-steps)
	})
}

// MigrationVersion reports the current schema version and whether a previous
// migration failed half-way ("dirty", which needs manual repair).
func MigrationVersion(ctx context.Context, dsn string) (version uint, dirty bool, err error) {
	err = withMigrate(ctx, dsn, func(m *migrate.Migrate) error {
		version, dirty, err = m.Version()
		if errors.Is(err, migrate.ErrNilVersion) {
			return nil // no migrations applied yet: version 0
		}
		return err
	})
	return version, dirty, err
}

// withMigrate opens a dedicated connection, builds a migrator over the
// embedded files, runs fn, and always closes everything. "No change" is not an
// error. If ctx is cancelled, the migrator is asked to stop after the current
// migration (it never stops half-way through one).
func withMigrate(ctx context.Context, dsn string, fn func(*migrate.Migrate) error) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("opening migration connection: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("pinging database for migrations: %w", err)
	}

	driver, err := pgxmigrate.WithInstance(db, &pgxmigrate.Config{})
	if err != nil {
		return fmt.Errorf("creating migration driver: %w", err)
	}
	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("reading embedded migrations: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", source, "pgx5", driver)
	if err != nil {
		return fmt.Errorf("creating migrator: %w", err)
	}
	defer m.Close()

	stop := context.AfterFunc(ctx, func() {
		select {
		case m.GracefulStop <- true:
		default:
		}
	})
	defer stop()

	if err := fn(m); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("running migrations: %w", err)
	}
	return nil
}
