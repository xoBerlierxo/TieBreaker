# Architecture

> Stub for M0; it grows with each milestone. The target design is in [Plan.md §1](../Plan.md#1-summary-of-the-architecture-and-key-decisions).

## Processes

One binary, `tiebreaker`, runs in two modes:

| Mode | Command | Responsibility |
|---|---|---|
| API | `tiebreaker serve` | HTTP API (Gin), in-process quick simulations for the interview |
| Worker | `tiebreaker worker` | full simulations as background jobs (from M4) |

Both connect to **PostgreSQL 16** (source of truth) and **Redis 7** (pub/sub for progress, rate limits, caches).

## Packages (current)

| Package | Role |
|---|---|
| `cmd/tiebreaker` | entry point, subcommand dispatch, server lifecycle, graceful shutdown |
| `internal/config` | typed configuration from env vars, validated once at startup |
| `internal/apperr` | domain error sentinels shared by the services and the HTTP layer |
| `internal/http` (package `httpapi`) | router, middleware, error shape, health checks |
| `internal/store` | Postgres (GORM) and Redis connections, `WithTx` |
| `internal/testutil` | testcontainers helpers (integration tests only) |

Dependency direction: `cmd` → `http` → services → `store`. `sim` and `decision` stay pure; golangci-lint `depguard` enforces that.
