# Tiebreaker

A car-shortlist decision engine. Give it 2–4 shortlisted cars (new or used). It asks a short adaptive interview, runs a Monte Carlo simulation of ownership, and reports **the probability that each car is the best choice for you**, with everything expressed in rupees:

```
NetCost(car, future) = OwnershipCost(car, future) − PreferenceValue(car, future)
```

> **Status:** under construction. Milestone **M0 (skeleton)** is done. See [Plan.md](Plan.md) for the roadmap and [SPEC.md](SPEC.md) for the specification. The full README (architecture diagram, benchmarks, demo script) arrives in M7.

## Quick start (local)

Requirements: Docker Desktop, Go 1.26+.

```bash
docker compose up -d --build
curl localhost:8080/healthz    # liveness  → {"status":"ok"}
curl localhost:8080/readyz     # readiness → checks Postgres and Redis
```

## Development

```bash
go test -race ./...                       # unit tests (no Docker needed)
go test -race -tags=integration ./...     # + integration tests (real Postgres/Redis via testcontainers)
golangci-lint run ./...                   # lint (v2.15.0)
```

## Documentation

- [Plan.md](Plan.md): the build plan and progress checkboxes
- [DECISIONS.md](DECISIONS.md): every significant decision and why
- [FLOW.md](FLOW.md): how execution moves through the code
- [docs/learning/](docs/learning/): Go concepts used, one note per milestone
