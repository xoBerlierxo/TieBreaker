// Package store owns the database and Redis connections and the shared helpers
// every repository uses, such as WithTx (SPEC §4, §18).
package store

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Pool limits. A single API or worker process rarely needs more than a handful
// of connections; Postgres' default max_connections is 100 for everyone.
const (
	maxOpenConns    = 20
	maxIdleConns    = 5
	connMaxLifetime = 30 * time.Minute
	connMaxIdleTime = 5 * time.Minute
	pingTimeout     = 5 * time.Second
)

// OpenPostgres opens a GORM connection pool and checks that the database
// answers. GORM's own logging goes through slog, and only slow queries
// (> 200 ms) and errors are logged. SQL parameters are never logged, because
// they may contain user answers.
func OpenPostgres(ctx context.Context, dsn string, log *slog.Logger) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.NewSlogLogger(log, logger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true, // "not found" is a normal result, not a problem
			ParameterizedQueries:      true, // log "WHERE id = $1", never the value
		}),
		// Map driver errors (e.g. unique violations) to gorm.ErrDuplicatedKey and
		// friends, so repositories can check them with errors.Is.
		TranslateError: true,
		// Timestamps are always stored in UTC.
		NowFunc: func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return nil, fmt.Errorf("opening postgres: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("getting sql.DB from gorm: %w", err)
	}
	sqlDB.SetMaxOpenConns(maxOpenConns)
	sqlDB.SetMaxIdleConns(maxIdleConns)
	sqlDB.SetConnMaxLifetime(connMaxLifetime)
	sqlDB.SetConnMaxIdleTime(connMaxIdleTime)

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("pinging postgres: %w", err)
	}
	return db, nil
}

// ClosePostgres closes the underlying connection pool.
func ClosePostgres(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("getting sql.DB from gorm: %w", err)
	}
	return sqlDB.Close()
}

// PostgresPinger adapts a *gorm.DB to the httpapi.Pinger interface.
type PostgresPinger struct{ DB *gorm.DB }

// Ping checks that the database answers within the context's deadline.
func (p PostgresPinger) Ping(ctx context.Context) error {
	sqlDB, err := p.DB.DB()
	if err != nil {
		return fmt.Errorf("getting sql.DB from gorm: %w", err)
	}
	return sqlDB.PingContext(ctx)
}
