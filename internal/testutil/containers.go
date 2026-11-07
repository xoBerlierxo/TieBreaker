//go:build integration

// Package testutil starts real Postgres 16 and Redis 7 containers for
// integration tests (SPEC §15), using testcontainers-go.
//
// Containers are expensive to start (a few seconds each), so each test package
// starts them once in TestMain and shares them across its tests. Tests that
// need isolation create their own tables or keys.
package testutil

import (
	"context"
	"fmt"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

// Same major versions as docker-compose.yml and production (SPEC §3).
const (
	postgresImage = "postgres:16-alpine"
	redisImage    = "redis:7-alpine"
)

// Postgres starts a Postgres container and returns its DSN and a function that
// stops it.
func Postgres(ctx context.Context) (dsn string, terminate func(), err error) {
	ctr, err := tcpostgres.Run(ctx, postgresImage,
		tcpostgres.WithDatabase("tiebreaker_test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		return "", nil, fmt.Errorf("starting postgres container: %w", err)
	}
	terminate = func() { _ = testcontainers.TerminateContainer(ctr) }

	dsn, err = ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		terminate()
		return "", nil, fmt.Errorf("postgres connection string: %w", err)
	}
	return dsn, terminate, nil
}

// Redis starts a Redis container and returns its redis:// URL and a function
// that stops it.
func Redis(ctx context.Context) (url string, terminate func(), err error) {
	ctr, err := tcredis.Run(ctx, redisImage)
	if err != nil {
		return "", nil, fmt.Errorf("starting redis container: %w", err)
	}
	terminate = func() { _ = testcontainers.TerminateContainer(ctr) }

	url, err = ctr.ConnectionString(ctx)
	if err != nil {
		terminate()
		return "", nil, fmt.Errorf("redis connection string: %w", err)
	}
	return url, terminate, nil
}
