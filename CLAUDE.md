# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Tiebreaker is a Go backend (plus a thin Next.js frontend from M7) that tells a car buyer the probability that each of 2–4 shortlisted cars is the best choice for them. It does this with a deterministic Monte Carlo simulation of ownership cost minus rupee-priced preferences. `SPEC.md` is the authoritative spec. `Plan.md` is the build plan with progress checkboxes; where they disagree, SPEC.md wins, except for the deviations listed in Plan.md §7.

## How work is done here (the developer's standing instructions)

- Work **one milestone (M0–M7) at a time** from `Plan.md`. Write the listed tests **first**, then the code. Tick each task's checkbox when it is done, and **stop at the end of every phase for review**. Don't start the next phase until the developer says so.
- At the end of each phase, also:
  - add entries to `DECISIONS.md` (`D-<milestone>-<n>`: decision, why, alternatives considered). This is a MUST-DO from SPEC.md, not optional.
  - update `FLOW.md` with how execution moves between files and functions for the new code.
  - write `docs/learning/NN-<topic>.md`: the Go concepts used and why, plus 3 interview questions. The developer is learning Go and must be able to explain every line, so prefer clear code over clever code.
- Commits are small and focused, one per finished task.
- If something in the spec is ambiguous, ask rather than invent. Record any deviation in Plan.md §7.

## Commands

The environment is Windows with Git Bash or PowerShell. Docker Desktop must be running for Compose and integration tests. cgo plus MSYS2 gcc are installed, so `-race` works locally.

```bash
go build ./...
golangci-lint run ./...                          # v2.15.0; config also lints files behind the integration tag
go test -race ./...                              # unit tests, no Docker
go test -race -tags=integration ./...            # adds testcontainers Postgres 16 / Redis 7 tests
go test -race -run TestLoad_InvalidValues ./internal/config/...          # single test
go test -race -run 'TestLoad_InvalidValues/zero_futures' ./internal/config/...   # single table case (spaces become _)
go test -race -tags=integration -run TestWithTx ./internal/store/...     # single integration test

docker compose up -d --build                     # postgres + redis → one-shot migrate → api + worker
DATABASE_URL="postgres://tiebreaker:tiebreaker_dev_only@localhost:5432/tiebreaker?sslmode=disable" go run ./scripts/seed
go run ./scripts/seed --check                    # validate data/seed without a database
MSYS_NO_PATHCONV=1 docker compose exec -T api /tiebreaker admin-token   # Git Bash rewrites /paths without MSYS_NO_PATHCONV
curl localhost:8080/healthz ; curl localhost:8080/readyz
go run ./cmd/tiebreaker serve                    # needs DATABASE_URL, REDIS_URL, JWT_SECRET in the env
bash scripts/demo.sh [api-url]                    # end-to-end flow over HTTP (Go program in scripts/demo)
go run ./cmd/tiebreaker verify-run -run-id <id>  # re-run a stored simulation and compare its results hash
go test ./internal/report -run Golden -update    # rewrite report template golden files after a deliberate change
cd web && npm ci && npm run lint && npm test && npm run build   # frontend (lint = tsc --noEmit)
```

CI (`.github/workflows/ci.yml`) runs five jobs: lint, unit (also fails if `go mod tidy` changes `go.mod`/`go.sum`), race, integration, and web. Run `go mod tidy` before committing dependency changes.

## Architecture (big picture)

- **One binary, subcommands.** `cmd/tiebreaker` holds `main.go` (`run()` dispatches; only `main` calls `os.Exit`), `serve.go` and `worker.go`. Planned subcommands are `migrate`, `admin-token` and `verify-run`. Root `ctx` comes from `signal.NotifyContext`. `runHTTPServer` handles graceful shutdown with `Shutdown` under `context.WithoutCancel(ctx)`.
- **Config.** `config.Load(getenv)` is called once. It takes `getenv` as a parameter, so there are no globals and tests pass a map. All errors are joined into one. `Config.LogValue()` redacts secrets. New settings go here and in `.env.example`.
- **Errors.** Services return sentinels and `*apperr.Error` from `internal/apperr`, wrapped with `%w`. The **only** error-to-HTTP mapping is `statusFor` in `internal/http/errors.go`. Handlers call `httpapi.WriteError(c, logger, err)`, which always produces `{"error":{"code","message","details"}}`; 500s hide the cause.
- **The HTTP package is `package httpapi`** in directory `internal/http`, to avoid clashing with `net/http`. `NewRouter(Deps)` takes every dependency explicitly. The middleware order is requestID → accessLog (logs the route template, never raw paths with IDs) → recovery → bodyLimit.
- **Store.** `store.OpenPostgres` (GORM with the slog logger, `TranslateError`, parameterised query logging) and `store.OpenRedis`. **All transactions go through `store.WithTx(ctx, db, fn)`**, and `fn` must use the `tx` it is given.
- **Package boundaries enforced by lint.** `depguard` blocks `internal/sim` and `internal/decision` from importing `internal/{http,store,llm,jobs}`, GORM, Redis, Gin, or `math/rand` (use `math/rand/v2`). They are pure functions: plain structs in, plain structs out.
- **Request and job flow.** See FLOW.md for the full call paths.
  - The API runs quick sims in-process for the adaptive interview.
  - Full runs go into `sim_runs`, which workers claim with `FOR UPDATE SKIP LOCKED`, using a lease and `worker_id` fencing.
  - Progress is published on Redis `run:{id}` and read through SSE.
  - The worker produces `decision` → `report.BuildFacts` → template, with optional Gemini prose that passes through a fact validator.

## Non-negotiable rules from SPEC.md (easy to violate by accident)

- **`data/seed/` is the developer's real catalogue; tests must use the fictional fixture in `testdata/seed/`** (only the two guard tests in `internal/catalog/seed_test.go` read `data/seed`).
- **Never invent real car specs, prices or safety ratings**, in code, seed data or tests. Sample variants are fictional ("Sample Motors"), `verified=false`, with placeholder sources. Assumption values are `PLACEHOLDER` / `verified=false` / `source_url="TODO"`. A missing safety rating is "not rated", never 0.
- **The engine decides; the LLM only writes prose.** No number or ranking may come from Gemini. LLM output must pass the fact validator, otherwise the template is used.
- **Determinism.** Chunk `i` uses `rand.New(rand.NewPCG(masterSeed, uint64(i)))`. Merge chunks in chunk order. Each future draws a fixed number of world values in a fixed order. Car streams are keyed by the stable shortlist-item ID. No map iteration, clock or global rand in result paths. Results must be bit-identical for 1, 4 and 16 workers.
- **Migrations are plain `.sql` files run by golang-migrate. Never use GORM AutoMigrate.**
- Use `google.golang.org/genai` (pinned v1.76.0), **never** `github.com/google/generative-ai-go`. There is no default Gemini model in code (env `GEMINI_MODEL`). Config refuses `GOOGLE_API_KEY` when `LLM_ENABLED=true`. Never send personal data to Gemini, and never log prompts or the key.
- Every I/O or long-running function takes `context.Context` first. No `panic` outside `main`. No package-level mutable globals. Handlers stay thin: parse → service → map the result.
- `-race` must stay green everywhere. Integration tests carry `//go:build integration` and share containers per package via `TestMain` (`internal/testutil`).

## Dependencies

Pin each new dependency to its newest release that is **at least 2 weeks old** (check `proxy.golang.org/<module>/@v/list` and `.info`). Record it in DECISIONS.md D-M0-2. Before using a library API, check its signatures in the module source under `$(go env GOMODCACHE)` rather than relying on memory.
