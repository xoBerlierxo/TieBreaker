# Tiebreaker

**A car-shortlist decision engine.** You shortlist 2–4 cars (new or used). Tiebreaker asks a short adaptive interview, prices your trade-offs in rupees, simulates tens of thousands of possible futures of owning each car, and reports **the probability that each car is the best choice for you**. If no car wins clearly, it tells you which single factor would decide it.

```
NetCost(car, future) = OwnershipCost(car, future) − PreferenceValue(car, future)
```

In each simulated future, the car with the lowest net cost wins. Across all futures you get P(best) per car. **The engine decides; an optional LLM only rewrites the facts as prose, and is checked against them.**

> ⚠️ **Data status:** the catalogue holds 3 **fictional** sample cars ("Sample Motors", `verified=false`), and every model assumption (fuel prices, tariffs, depreciation…) is a clearly labelled **placeholder**. The API and UI show these warnings everywhere. Replace them with sourced data before trusting any number; see [Data honesty](#data-honesty).

---

## Architecture

```mermaid
flowchart LR
  subgraph Browser
    W["Next.js app (web/)"]
  end
  subgraph "tiebreaker binary (Go)"
    API["serve: Gin API"]
    WK["worker"]
  end
  PG[(PostgreSQL 16)]
  RD[(Redis 7)]
  GM[[Gemini, optional]]
  W -- "JSON + ETag/If-Match, Idempotency-Key" --> API
  W -- "SSE progress" --> API
  API -- GORM --> PG
  API -- "quick-sim cache, rate limits, SSE subscribe" --> RD
  API -- "in-process quick sims" --> ENG
  WK -- "claim: FOR UPDATE SKIP LOCKED + lease" --> PG
  WK -- "PUBLISH run:{id}" --> RD
  WK -- "full sim + tipping points" --> ENG
  WK -. "prose only, fact-validated" .-> GM
  ENG["internal/sim + internal/decision (pure, deterministic)"]
```

| Package | Responsibility |
|---|---|
| `cmd/tiebreaker` | single binary: `serve`, `worker`, `migrate`, `admin-token`, `verify-run` |
| `internal/sim` | **pure** Monte Carlo engine: no I/O, no globals, deterministic for any worker count |
| `internal/decision` | P(best), percentiles, entropy, winner/deadlock, tipping points (pure) |
| `internal/interview` | usage questions, trade-off candidates, ₹ bracket ladder, information-gain planner |
| `internal/session` | sessions, optimistic locking, constraints, idempotent answers, starting runs |
| `internal/jobs` | Postgres job queue (SKIP LOCKED, leases, fencing), worker, progress pub/sub, verify |
| `internal/report` | facts → template prose; LLM writer + fact validator; report storage and share links |
| `internal/llm` | Gemini (`google.golang.org/genai`) behind an interface, fake, cache, daily budget |
| `internal/catalog` | variants, sources, verification gate, assumptions, seed loader |
| `internal/http` | Gin router, handlers, middleware, the one error→status mapping |
| `web/` | thin Next.js (App Router) + Tailwind frontend: shortlist, interview, results |

More: [docs/architecture.md](docs/architecture.md), [FLOW.md](FLOW.md) (how execution moves through the code), [DECISIONS.md](DECISIONS.md) (every decision and why), [docs/model.md](docs/model.md) (the cost model and every assumption).

## Quick start (local)

Requirements: Docker Desktop, Go 1.26+, Node 22 (for the frontend).

```bash
docker compose up -d --build        # postgres + redis → migrate → api (:8080) + worker
DATABASE_URL="postgres://tiebreaker:tiebreaker_dev_only@localhost:5432/tiebreaker?sslmode=disable" \
  go run ./scripts/seed             # load the sample catalogue and placeholder assumptions
bash scripts/demo.sh                # the whole flow end to end in the terminal

cd web && npm ci && npm run dev     # the UI on http://localhost:3000
```

### Demo script

`bash scripts/demo.sh [api-url]` (a Go program, `scripts/demo`, so it runs anywhere Go does):

1. checks `/readyz` and creates a session in Tamil Nadu;
2. searches the catalogue and shortlists the three sample cars (showing their `unverified_data` warnings);
3. sets hard constraints (budget, home charging);
4. answers the usage questions, then the adaptive yes/no ₹ questions until the engine stops, printing the live P(best);
5. starts the full simulation and streams its progress over server-sent events;
6. prints the report (winner or deadlock, pros and cons in ₹, tipping points), the share link, and the `verify-run` command that reproduces it bit for bit.

## API (v1)

All responses are JSON. Errors have one shape: `{"error": {"code": "SESSION_VERSION_CONFLICT", "message": "…", "details": {}}}`.

| Method | Path | Notes |
|---|---|---|
| GET | `/healthz`, `/readyz` | liveness; readiness checks Postgres and Redis |
| GET | `/v1/variants?q=&fuel=&limit=` | catalogue search (with `warnings`) |
| GET | `/v1/variants/:id` | includes sources and `verified` |
| POST | `/v1/sessions` | `{"user_state": "TN"}` → session, `ETag: "1"` |
| GET | `/v1/sessions/:id` | full state; version in `ETag` |
| POST/DELETE | `/v1/sessions/:id/cars[/:itemId]` | requires `If-Match` |
| PUT | `/v1/sessions/:id/constraints` | requires `If-Match`; eliminations with shortfalls |
| GET | `/v1/sessions/:id/next-question` | usage or trade-off question, or `{"done": true}` |
| POST | `/v1/sessions/:id/answers` | requires `Idempotency-Key` and `If-Match` |
| POST | `/v1/sessions/:id/simulations` | → `202 {run_id}`; rate limited |
| GET | `/v1/simulations/:id` | status, progress, results |
| GET | `/v1/simulations/:id/events` | SSE progress stream |
| POST | `/v1/simulations/:id/cancel` | |
| GET | `/v1/sessions/:id/report` | winner / deadlock / none_fit |
| GET | `/r/:shareId` | public, read-only report (no session ID) |
| POST/PUT/DELETE | `/admin/v1/variants[/:id]` | admin JWT (`tiebreaker admin-token`) |

Status codes: `400` validation · `401/403` auth · `404` · `409` illegal transition or stale version · `412` missing `If-Match` · `422` idempotency key reused with a different body · `429` rate limited · `503` dependency down.

## How it works, briefly

- **Interview (SPEC §8):** questions come only from real differences between your cars. Each trade-off is a yes/no ₹ ladder (₹1L first, at most 3 steps). The next question is the one with the highest **information gain** over the winner distribution, estimated with quick simulations on a fixed seed. The interview stops when a car leads with ≥ 65 %, gains drop below 0.02 bits, 10 questions are asked, or nothing is left to ask.
- **Engine (SPEC §10):** upfront price, EMIs, fuel or electricity on log-normal price paths, insurance on a falling IDV, service, tyres, Poisson repairs with log-normal costs (more for older, high-mileage and poorer-condition cars, less in warranty), and resale with market and own shocks. Everything is discounted to today's money. See [docs/model.md](docs/model.md).
- **Decision (SPEC §9):** winner if P(best) ≥ 55 % or the lead is ≥ 15 points; otherwise a deadlock, classified by **tipping points** (9-point sweeps of km/month, years, fuel-price trend, home charging and each preference, with bisection to the crossover) or as **genuinely close**.
- **Reports:** `BuildFacts` → template prose (always) → optional Gemini prose that must pass a **fact validator**: every number and car name must match the facts, with rounding allowed (₹1,02,340 → "about ₹1 lakh").

## Determinism and reproducing a result

Each run stores its master seed and its full inputs, including a snapshot of every assumption. Chunk *i* of the futures uses `rand.NewPCG(masterSeed, i)`. Each future derives named sub-streams for the world, each car and each preference, and writes its own sample slot, so results are **bit-identical for 1, 4 or 16 workers** (tested). To reproduce a share link's numbers:

```bash
tiebreaker verify-run -run-id <run id>      # re-runs and compares a SHA-256 over every sample
```

Bit-exactness is guaranteed per CPU architecture: Go may fuse multiply-adds differently on arm64.

## Benchmarks

AMD Ryzen 5 5600H (6C/12T), Windows 11, Go 1.26.1, amd64 · workload: 3 cars, 5 years, loan, 2 preferences, all randomness on. Details: [docs/benchmarks.md](docs/benchmarks.md).

| Benchmark | Result | SPEC §16 target |
|---|---|---|
| `BenchmarkRun_3cars_50k`, 4 workers | **33 ms** · ~1.49 M futures/s | < 5 s on 4 cores |
| `BenchmarkRun_3cars_50k`, 1 worker / 12 workers | 117 ms (~429 k futures/s) / 18 ms (~2.73 M futures/s) | — |
| Quick simulation (3 cars × 2,000 futures) | p95 **7.4 ms** | p95 < 150 ms |
| `next-question`, no cache (3 cars × 9 candidates = 64 quick sims) | p95 **70 ms** | p95 < 1.5 s |

```bash
go test -run='^$' -bench=. -benchmem ./internal/sim/ ./internal/interview/
```

## Testing

```bash
go test -race ./...                       # unit tests (no Docker)
go test -race -tags=integration ./...     # + real Postgres/Redis via testcontainers
golangci-lint run ./...                   # lint (incl. depguard: sim/decision stay pure)
cd web && npm run lint && npm test && npm run build
```

Highlights: the hand-checked ownership cost, common-sense property tests (checked future by future), bit-exact determinism across worker counts, cancellation with goroutine-leak checks (goleak), the deadlock sanity cases, stale `If-Match` → 409, concurrent answers (exactly one wins), idempotency replay and 422, two workers × ten jobs (each processed once), worker crash → lease expiry → another worker finishes, SSE end to end, the fact validator, the Gemini adapter against an `httptest` server, and template golden files. Optional k6 load scripts are in `scripts/k6/`.

## LLM (optional)

`LLM_ENABLED=false` by default: everything works with templates. With `LLM_ENABLED=true`, `GEMINI_API_KEY` and `GEMINI_MODEL` (no default in code; `gemini-3.5-flash-lite` was the recommended stable fast model on 2026-11-07), the worker asks Gemini to rephrase the report facts. Only car data and numeric results are sent; there is a 20 s timeout, one retry on 429/5xx, a Redis cache and a daily call cap. Prose that fails the fact validator is discarded, and `prose_source` tells the UI which text you see. Try a key with `go run ./scripts/llm-smoke`.

## Data honesty

- Every catalogue value needs a source (URL + checked date) before a variant can be marked verified; placeholder sources are rejected. A guard test enforces SPEC §7: at most 3 sample variants, all unverified.
- Every assumption lives in the `assumptions` table with a source and a `verified` flag; the seed values are placeholders. To add real data, edit `data/seed/*.json` and `data/seed/assumptions/*.json` with real `source_url`/`checked_at`, set `verified: true`, and run `go run ./scripts/seed`.
- A missing safety rating is shown as "not rated", never as zero.

## Deployment

API and worker on Railway (one image, `TIEBREAKER_MODE=serve|worker`), frontend on Vercel. Step by step: [docs/deploy.md](docs/deploy.md).

## Known limitations

- Placeholder assumptions and fictional sample cars until real, sourced data is added.
- Bit-exact reproduction is per CPU architecture.
- v2 items not built: brochure PDF import and free-text usage parsing (`GenerateJSON` with a response schema is ready for them).

## Learning notes

The project doubles as a Go learning log, one note per milestone, each ending with three interview questions:
[00 skeleton](docs/learning/00-skeleton-config-shutdown.md) ·
[01 catalogue](docs/learning/01-catalogue-migrations-gorm.md) ·
[02 engine](docs/learning/02-monte-carlo-engine-concurrency.md) ·
[03 sessions](docs/learning/03-sessions-locking-idempotency-interview.md) ·
[04 jobs](docs/learning/04-job-queue-skip-locked-sse.md) ·
[05 reports](docs/learning/05-decision-tipping-points-reports.md) ·
[06 Gemini](docs/learning/06-gemini-llm-fact-validation.md) ·
[07 frontend + deploy](docs/learning/07-frontend-deploy.md)
