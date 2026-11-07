# spec.md — Tiebreaker: a car-shortlist decision engine

> This is the specification for the project. Claude Code: read the whole file before planning or writing code.
> If something here is ambiguous or seems wrong, ask before inventing an answer.

---

## 1. What we are building

A car buyer has shortlisted 2–4 cars (e.g. Nexon, Alto K10, a used Nano). Tiebreaker:

1. Collects structured data for each shortlisted car (new or used).
2. Interviews the buyer with a short, adaptive set of questions, where trade-off answers are priced in rupees.
3. Runs a Monte Carlo simulation of 5-year ownership (or the user's horizon) across tens of thousands of simulated futures.
4. Reports **the probability that each car is the best choice for this specific buyer**.
5. If no car wins clearly (a **deadlock**), produces a short report of each car's pros and cons, plus the **tipping points**: which single factor would decide it.

**Core principle:** everything is expressed in rupees.

```
NetCost(car, future) = OwnershipCost(car, future) − PreferenceValue(car, future)
```

The car with the lowest NetCost wins that future. Across all futures we get P(best) per car.

**The engine decides; the LLM only writes prose.** No ranking, score or number may come from the LLM.

## 2. Goals and non-goals

### Goals (v1)
- Backend in Go that a reviewer can read, run and trust.
- Results are deterministic and reproducible (same inputs + same seed → same output, regardless of worker count).
- Correct under concurrency (two browser tabs, double submits, cancelled runs).
- Every car data value has a source and a "checked on" date.
- Works fully **without** a Gemini key (falls back to templates). Gemini only improves the prose.

### Non-goals (v1)
- No scraping of car aggregator sites (CarDekho, CarWale, etc.). Car data is hand-curated or comes from user-uploaded brochures.
- No user accounts. Sessions are anonymous with unguessable IDs.
- No payments, no dealer integrations, no mobile app.
- No pixel-perfect UI. The frontend is a thin demo layer.

## 3. Tech stack (fixed — do not substitute without asking)

| Layer | Choice |
|---|---|
| Language | Go ≥ 1.24 (required by the Gemini SDK) |
| HTTP | Gin |
| ORM | GORM (with Postgres driver) |
| Migrations | golang-migrate with plain `.sql` files. **Do not use GORM AutoMigrate** |
| Database | PostgreSQL 16 |
| Cache / pub-sub / rate limiting | Redis 7 |
| LLM | Gemini via `google.golang.org/genai`. **Do NOT use the deprecated `github.com/google/generative-ai-go`** |
| Logging | `log/slog` (structured JSON) |
| Config | Environment variables, loaded once at startup into a typed `Config` struct |
| Tests | standard `testing`, table-driven tests, `testcontainers-go` for Postgres/Redis integration tests, `go test -race` |
| Lint | golangci-lint |
| Local dev | Docker Compose (api, worker, postgres, redis) |
| CI | GitHub Actions: lint, unit tests, race tests, integration tests |
| Frontend | Next.js (App Router) + Tailwind in `/web`, kept thin |
| Deploy | API + worker on Railway (Docker); frontend on Vercel |

## 4. Repository layout

```
/cmd/tiebreaker/main.go     single binary; `serve` or `worker` mode via flag/subcommand
/internal/config            typed config from env
/internal/http              Gin router, handlers, middleware, request/response DTOs
/internal/catalog           variants, sources, assumptions (domain + repo)
/internal/session           sessions, shortlist, answers, versioning
/internal/interview         question generation, bracket ladder, adaptive selection
/internal/sim               PURE Monte Carlo engine: no DB, no HTTP, no globals
/internal/decision          P(best), deadlock detection, tipping points
/internal/report            facts → report; template renderer; LLM writer + fact validator
/internal/llm               Gemini client behind an interface + fake for tests
/internal/jobs              simulation job queue (Postgres SKIP LOCKED) + progress pub/sub
/internal/store             GORM setup, transactions helper, shared repo code
/migrations                 NNNN_name.up.sql / .down.sql
/data/seed                  curated car data as JSON (see §7)
/web                        Next.js frontend
/docs                       architecture.md, model.md (simulation assumptions), learning/
/scripts                    seed loader, k6 load test
```

Rules:
- `internal/sim` and `internal/decision` must not import anything from `http`, `store`, `llm` or `jobs`. They take plain structs in and return plain structs out.
- Every function that does I/O or long computation takes `context.Context` as its first parameter.

## 5. Domain model

### 5.1 Car data

**Variant** (a specific version of a model, e.g. "Nexon EV Creative 45"):
- `id`, `make`, `model`, `variant_name`, `body_type`, `fuel_type` (petrol | diesel | cng | ev | strong_hybrid)
- `ex_showroom_price_inr` (new cars), `launch_year`, `discontinued_year` (nullable)
- `claimed_efficiency` + `efficiency_unit` (km/l, km/kg, or km/kWh), `battery_kwh` (EV only), `claimed_range_km` (EV only)
- `safety_rating_stars` (nullable), `safety_rating_body` (e.g. Bharat NCAP / Global NCAP), `airbags`
- `boot_litres`, `ground_clearance_mm`, `power_ps`, `kerb_weight_kg`, `transmission` (manual | amt | cvt | dct | torque_converter | single_speed), `seats`
- `service_interval_km`, `warranty_years`, `warranty_km`
- `key_features` (text[] — e.g. "rear_camera", "6_airbags", "esc", "connected_car")
- `verified` (bool), `created_at`, `updated_at`

**VariantSource:** `variant_id`, `field_name`, `source_url`, `checked_at`, `note`. Each data field must have at least one source before it can be marked `verified`.

**ShortlistItem** (a car within a session):
- `variant_id`, `condition` (new | used)
- if used: `asking_price_inr`, `year`, `odometer_km`, `owners_count`, `condition_grade` (good | fair | poor)
- if new: `on_road_price_override_inr` (nullable; otherwise computed from ex-showroom + state road tax + estimates)

**Assumption** (versioned, editable, sourced):
- `key` (e.g. `fuel_price.petrol.TN`, `electricity_tariff.TN`, `road_tax.TN.rule`, `real_efficiency_factor.petrol`), `value_json`, `source_url`, `checked_at`, `model_version`.
- Assumptions are snapshotted into each simulation run so old results stay reproducible after assumptions change.

### 5.2 Session
- `id` (UUIDv4 or random 128-bit, URL-safe), `version` (int, for optimistic locking), `state`, `user_state` (Indian state code), `created_at`
- Session states: `SHORTLISTING → USAGE → INTERVIEW → SIMULATING → DONE`. Allowed transitions are enforced in code; anything else returns `409`.

### 5.3 Answers
- `session_id`, `question_id`, `answer_json`, `created_at`
- Usage answers are **ranges**, e.g. `{ "km_per_month": [800, 1200] }`.
- Trade-off answers are **₹ brackets**, e.g. `{ "attribute": "safety", "min_inr": 50000, "max_inr": 100000 }`.

### 5.4 Simulation runs
- `sim_runs`: `id`, `session_id`, `kind` (quick | full), `master_seed` (uint64), `futures`, `inputs_hash`, `model_version`, `status` (queued | running | done | failed | cancelled), `progress` (0–100), `error`, timestamps
- `sim_results`: `run_id`, `shortlist_item_id`, `p_best`, `p_cheapest`, `netcost_p10/p50/p90`, `ownership_cost_p10/p50/p90`, `breakdown_json` (median cost by category)
- `reports`: `session_id`, `kind` (winner | deadlock | none_fit), `facts_json`, `prose_md`, `prose_source` (template | llm), `share_id`

### 5.5 Idempotency
- `idempotency_keys`: `session_id`, `key`, `request_hash`, `response_json`, `created_at`. Unique on `(session_id, key)`.
- Same key + same body → return the stored response. Same key + different body → `422`.

## 6. User flow

1. **Shortlist:** search variants, add 2–4 cars, mark each new or used (used cars need asking price, year and odometer). Reject duplicates and fewer than 2 or more than 4 cars.
2. **Hard constraints:** max budget (on-road or asking price), minimum seats, home charging available (asked only if an EV is in the list), transmission requirement (optional). Eliminate cars that fail and record why. If every car is eliminated → `none_fit` report showing which constraint each car failed and by how much.
3. **Usage questions** (fixed order, all answered as ranges): state, km per month, city/highway split, years of ownership, financing (loan amount, rate, tenure — or cash), parking type (optional).
4. **Trade-off interview** (adaptive, §8): at most 10 questions.
5. **Full simulation** runs as a background job with live progress.
6. **Report:** a winner report or a deadlock report (§9).
7. **Share link:** `GET /r/:share_id` shows the stored result; re-running with the stored seed reproduces it exactly.

## 7. Car data: rules for the catalogue

- Seed data lives in `/data/seed/*.json`, one file per model, and is loaded by `scripts/seed`.
- **Claude Code must not invent real specifications, prices or safety ratings.** Create the schema, the loader and **at most 3 sample variants clearly marked `"verified": false` with placeholder sources**. The developer will fill in real data with real source URLs.
- The API and UI must show an "unverified data" warning for any car with `verified = false`.
- Missing optional fields (e.g. no safety rating) are allowed. The report must say "not rated" instead of treating it as zero.

**v2 — Brochure import (Gemini):** an admin uploads a brochure PDF. Gemini extracts a draft Variant as JSON (structured output with a response schema), plus a confidence value for each field. The draft goes into a review queue and is **never** published without admin approval.

## 8. The interview engine

### 8.1 Which trade-off questions exist
Generate candidate questions **only from real differences between the remaining shortlisted cars**. Supported attributes in v1:

| Attribute | Measured by | Example question |
|---|---|---|
| safety | stars, airbags, ESC | "Car A is rated 5 stars, Car B 3 stars. How much more would you pay over 5 years for Car A's safety?" |
| boot | litres | "Car A has 120 L more boot space…" |
| ground_clearance | mm | |
| performance | power-to-weight (PS/tonne) | |
| automatic | AT vs MT | |
| features | the set difference of `key_features` | one question per meaningful feature difference, max 2 |
| warranty | years/km | |

If every remaining car has the same value for an attribute, never ask about it.

### 8.2 Preference value model
- Each answer gives a ₹ range `[lo, hi]` for **the difference between the best and worst car on that attribute**.
- A car in between gets a linearly interpolated share of that value on the attribute's numeric scale.
- `PreferenceValue(car) = Σ over attributes of that car's share of the sampled value`.
- In each simulated future the value is sampled uniformly from `[lo, hi]`, so uncertainty in the answer becomes uncertainty in the result.

### 8.3 The bracket ladder
- Ladder: ₹0, ₹25k, ₹50k, ₹1L, ₹2L, ₹4L (configurable).
- Start in the middle (₹1L). Ask a yes/no question ("Would you pay ₹1L more for this?"), and move up or down like a binary search, taking at most 3 yes/no steps per attribute.
- The result is a bracket `[lo, hi]`. "More than ₹4L" becomes `[4L, 8L]` (cap configurable). "Not even ₹25k" becomes `[0, 25k]`.
- Each step counts as one question toward the limit of 10.

### 8.4 Choosing the next question
For each candidate attribute not yet answered:
1. For each possible bracket answer, run a **quick simulation** (default 2,000 futures) with that attribute's value restricted to the bracket.
2. Compute the entropy of the winner distribution (P(best) across cars) for each bracket.
3. Expected entropy = the average over brackets (assume each bracket is equally likely).
4. Information gain = current entropy − expected entropy.

Ask the question with the highest information gain. Cache quick-sim results by inputs hash.

### 8.5 When the interview stops (any of these)
- the leader's P(best) ≥ 0.65
- the highest remaining information gain < 0.02
- 10 questions have been asked
- no candidate questions remain

All thresholds are configurable.

## 9. Decision and reports

### 9.1 Winner
After the full simulation, if the leader's P(best) ≥ 0.55 **or** the gap between the top two ≥ 0.15 → **winner report**.

### 9.2 Deadlock
Otherwise → **deadlock report**. Classify it:
- **Tipping point:** sweep one input at a time across its plausible range (km/month, ownership years, fuel-price trend, home charging yes/no, each preference value) using 9 grid points with quick simulations. If the leader changes, find the crossover point by bisection and report it, e.g. "Above ~1,100 km/month Car A wins; below it Car B wins."
- **Genuinely close:** no single input changes the leader. Say so plainly. That is a valid and useful answer.

### 9.3 Report contents (all reports)
For each car:
- P(best), P(cheapest), net cost and ownership cost (10th / 50th / 90th percentiles), median cost breakdown by category
- **Top 3 pros and top 3 cons, ranked by the ₹ value this user gave each difference** (not by generic importance)
- Tipping points that involve this car
- Data warnings (unverified data, missing safety rating, assumptions used)

### 9.4 Facts first, prose second
1. `report.BuildFacts(...)` produces a `Facts` struct. This is the single source of truth.
2. `report.RenderTemplate(facts)` always works and is the fallback.
3. If the LLM is enabled, `report.RenderLLM(facts)` asks Gemini for a short, plain-English summary using **only** the facts given.
4. **Fact validator:** extract every number (₹ amounts, percentages, km, years) and every car name from the LLM output. Each must match a value in `Facts` (allowing for rounding such as ₹1,02,340 → "about ₹1 lakh"). Any mismatch → discard and use the template. Store `prose_source` so the UI can show which one was used.

## 10. Ownership cost model (`internal/sim`)

All money is in INR. Time step is 1 year. Horizon = the user's ownership years (default 5).

**Per car, per future:**
```
OwnershipCost = Upfront
              + Σ_t discount(t) × (Financing_t + Energy_t + Insurance_t + Service_t + Repairs_t + Tyres_t)
              − discount(T) × Resale_T
```
- **Upfront:** on-road price (new) or asking price + transfer costs (used), minus the loan principal if financed.
- **Financing:** standard EMI amortisation from the user's loan inputs.
- **Discounting:** real discount rate from config (default 7%) represents the opportunity cost of cash.
- **Energy:** `km_year / real_efficiency × price_t`.
  - `real_efficiency = claimed × factor`, with `factor` sampled from a range per fuel type (assumption).
  - EV: energy = `km / (km per kWh) / charging_efficiency × tariff_t`. If there is no home charging, use the public-charging tariff assumption.
- **Price paths:** `p_{t+1} = p_t × exp(μ + σ·Z)`, `Z ~ N(0,1)`, with μ and σ per energy type from assumptions.
- **Usage:** `km_year` sampled from the user's km/month range × 12, with year-to-year variation.
- **Insurance:** a share of the insured value (IDV), with IDV falling each year per the assumption schedule.
- **Service:** a scheduled cost per `service_interval_km` from assumptions per segment and fuel type.
- **Repairs:** Poisson-distributed number of events per year, with the rate rising with age and odometer reading (higher for used cars and for poorer condition grades). Cost per event is lognormal.
- **Resale:** a baseline depreciation curve by fuel type and age × a lognormal shock. EV battery wear adds an extra downward adjustment. Clamp to [0, purchase price].

**Every assumption** (ranges, μ/σ, curves, rates) lives in the `assumptions` table, is documented in `docs/model.md` with its source, and is shown on the results page.

### 10.1 Engine requirements (non-negotiable)
- **Shared futures:** within one future, all cars use the same sampled world (prices, usage, shocks where they apply). Car-specific randomness, like repairs, uses that car's own sub-stream of the future's RNG.
- **Determinism:** the run has a `master_seed`. Futures are split into chunks (default 1,000). Chunk `i` uses `rand.New(rand.NewPCG(masterSeed, uint64(i)))` from `math/rand/v2`. Chunk results are merged **in chunk order**, so the output is identical for any number of workers.
- **Concurrency:** worker count = `runtime.GOMAXPROCS(0)` by default. Use `errgroup` or a worker pool. No shared mutable state between workers except the final merge.
- **Cancellation:** check `ctx.Err()` at least every 256 futures, return promptly, and leak no goroutines.
- **Progress:** an optional callback reporting completed chunks.
- **Output:** for each car, all net-cost samples (or a mergeable sketch if memory becomes a problem), from which percentiles, P(best) and P(cheapest) are computed.
- The engine is a pure function: `Run(ctx, Inputs, Options) (Result, error)`.

## 11. Background jobs and progress

- Full simulations run in `worker` mode. The API inserts a `sim_runs` row (status `queued`) and returns `202` with the run ID.
- Workers claim jobs with `SELECT … FOR UPDATE SKIP LOCKED` (GORM `clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}`) inside a transaction, then set status `running`.
- A job left in `running` past a lease timeout is reclaimed by another worker (handles crashed workers).
- Progress is published on Redis pub/sub channel `run:{id}`. The API exposes it as server-sent events and also stores the latest progress in Postgres, so a client that reconnects can catch up.
- Cancel: `POST /v1/simulations/:id/cancel` sets a flag; the worker's context gets cancelled; status becomes `cancelled`.
- Quick simulations (interview) run in-process in the API with a strict timeout (default 2s).

## 12. HTTP API (v1)

All responses are JSON. Errors use one consistent shape:
```json
{ "error": { "code": "SESSION_VERSION_CONFLICT", "message": "…", "details": {} } }
```

| Method | Path | Notes |
|---|---|---|
| GET | `/healthz`, `/readyz` | readiness checks DB + Redis |
| GET | `/v1/variants?q=&fuel=&limit=` | catalogue search |
| GET | `/v1/variants/:id` | includes sources and `verified` |
| POST | `/v1/sessions` | creates a session |
| GET | `/v1/sessions/:id` | full state, includes `version` (also sent as an `ETag` header) |
| POST | `/v1/sessions/:id/cars` | add a shortlist item; requires `If-Match: <version>` |
| DELETE | `/v1/sessions/:id/cars/:itemId` | requires `If-Match` |
| PUT | `/v1/sessions/:id/constraints` | hard constraints; requires `If-Match` |
| GET | `/v1/sessions/:id/next-question` | next usage or trade-off question, or `{ "done": true }` |
| POST | `/v1/sessions/:id/answers` | requires `Idempotency-Key` and `If-Match` |
| POST | `/v1/sessions/:id/simulations` | starts a full run → `202 { run_id }` |
| GET | `/v1/simulations/:id` | status + progress |
| GET | `/v1/simulations/:id/events` | SSE progress stream |
| POST | `/v1/simulations/:id/cancel` | |
| GET | `/v1/sessions/:id/report` | winner / deadlock / none_fit |
| GET | `/r/:shareId` | public read-only report |
| POST | `/admin/v1/variants` (+ PUT, DELETE) | admin only |
| POST | `/admin/v1/brochures` | v2: upload PDF → draft variant |

**Status codes:** `400` validation, `404` not found, `409` illegal state transition or stale version, `412` missing `If-Match`, `422` idempotency-key reuse with a different body, `429` rate limited, `503` dependency down.

## 13. Gemini integration (`internal/llm`)

- SDK: `google.golang.org/genai`. Create the client with `genai.NewClient(ctx, &genai.ClientConfig{APIKey: cfg.GeminiAPIKey, Backend: genai.BackendGeminiAPI})`.
- Environment:
  - `GEMINI_API_KEY` — required only if `LLM_ENABLED=true`
  - `GEMINI_MODEL` — **no hard-coded default in code**; check the current Gemini docs for a suitable fast model and put it in `.env.example`
  - `LLM_ENABLED` — default `false`
  - Do **not** also set `GOOGLE_API_KEY`: the SDK reads both and prefers `GOOGLE_API_KEY` when both are set, which causes confusion.
- Wrap it behind an interface so tests never call the real API:
  ```go
  type Client interface {
      GenerateText(ctx context.Context, req TextRequest) (string, error)
      GenerateJSON(ctx context.Context, req JSONRequest, out any) error // uses response schema
  }
  ```
  Provide `llm.Fake` for tests.
- Every call: context timeout (default 20s), at most 1 retry with backoff on 429/5xx, token/output limits set, results cached by input hash.
- Rate-limit LLM-backed endpoints per session and per IP using Redis, to protect the API key's quota.
- **Never** send personal data to Gemini. Only car data and numeric results.
- **Never** log the API key or full prompts that contain user answers.

**Uses:**
| Priority | Use | Safeguard |
|---|---|---|
| P1 | Report prose (§9.4) | Fact validator + template fallback |
| P2 | Turn free-text usage ("30 km daily commute, Pondicherry trip every month") into structured ranges | JSON schema output; the user confirms the parsed ranges before they are used |
| P2 | Brochure PDF → draft Variant (§7) | Admin approval required; per-field confidence |

## 14. Security and robustness

- Session IDs and share IDs are unguessable (≥128 bits of randomness).
- Admin endpoints are protected by a JWT with an `admin` role (short-lived access token; the secret comes from env).
- Input validation on every handler (Gin binding + explicit range checks: km/month 0–20,000, prices > 0, years 1–15, and so on).
- Request body size limits; timeouts on the server and on every outgoing call.
- Rate limits (Redis): simulation starts per session and per IP, plus the LLM endpoints.
- Graceful shutdown: stop accepting work, finish or cancel in-flight jobs, close the DB and Redis.
- No secrets in the repo; provide `.env.example`.

## 15. Testing requirements

**Unit (`internal/sim`, `internal/decision`, `internal/interview`):**
- **Hand-checkable case:** with all randomness turned off (σ = 0, fixed factors), the ownership cost must equal a hand-calculated value written in the test.
- **Common-sense properties:** more km never lowers energy cost; a higher fuel price path never improves the petrol car relative to an EV; more ownership years never lowers total energy cost.
- **Determinism:** the same seed with 1, 4 and 16 workers gives identical results (compare exactly).
- **Cancellation:** cancel mid-run → returns `context.Canceled` promptly and no goroutines leak (use `go.uber.org/goleak`).
- **Deadlock sanity:** two identical cars must deadlock; a car that is cheaper and better on every attribute must win with P(best) ≈ 1.
- **Interview:** never asks about an attribute where all cars are equal; the bracket ladder converges in at most 3 steps; stops when the leader passes the threshold.
- **Fact validator:** rejects prose containing a number that is not in the facts; accepts correctly rounded numbers.

**Integration (testcontainers-go, real Postgres + Redis):**
- Stale `If-Match` → `409`; two concurrent answer submissions on one session → exactly one succeeds.
- Same `Idempotency-Key` twice → same response, one row; different body → `422`.
- Two workers, ten queued jobs → each job processed exactly once.
- Kill a worker mid-job → the lease expires and another worker completes it.

**Everything runs with `go test -race ./...`.**

**Benchmarks:** `BenchmarkRun_3cars_50k` — report futures/second in the README.

**Load (optional):** a k6 script for quick-sim and full-sim endpoints.

## 16. Performance targets (measure, then adjust)
- Quick simulation (3 cars × 2,000 futures): p95 < 150 ms on a 4-core machine.
- `next-question` (all candidate quick sims, cached where possible): p95 < 1.5 s.
- Full simulation (3 cars × 50,000 futures): < 5 s on 4 cores.

## 17. Configuration (env)

`APP_ENV`, `HTTP_ADDR`, `DATABASE_URL`, `REDIS_URL`, `JWT_SECRET`, `LLM_ENABLED`, `GEMINI_API_KEY`, `GEMINI_MODEL`, `SIM_FULL_FUTURES` (50000), `SIM_QUICK_FUTURES` (2000), `SIM_CHUNK_SIZE` (1000), `SIM_WORKERS` (0 = GOMAXPROCS), `INTERVIEW_MAX_QUESTIONS` (10), `INTERVIEW_STOP_PBEST` (0.65), `INTERVIEW_MIN_INFO_GAIN` (0.02), `DECISION_WIN_PBEST` (0.55), `DECISION_WIN_GAP` (0.15), `DISCOUNT_RATE` (0.07), `JOB_LEASE_SECONDS` (60).

## 18. Code conventions
- Idiomatic Go. Wrap errors with `fmt.Errorf("…: %w", err)`. Define sentinel or typed domain errors and map them to HTTP codes in one place.
- No `panic` outside `main`. No package-level mutable globals.
- Handlers are thin: parse → call a service → map the result. Business logic lives in services.
- Database transactions go through one helper (`store.WithTx(ctx, fn)`).
- Table-driven tests. Test names describe the behaviour.
- Small, focused commits with descriptive messages.

## 19. Collaboration mode (the developer must be able to explain every line)

The developer is learning Go through this project and will have to explain the code in technical interviews.
- After each milestone, write `docs/learning/NN-<topic>.md`: which Go concepts were used and why (e.g. contexts, errgroup, SKIP LOCKED, optimistic locking), plus 3 questions the developer should be able to answer about that code.
- Prefer clear code over clever code.

## 20. Milestones and acceptance criteria

| # | Milestone | Done when |
|---|---|---|
| M0 | Skeleton | `docker compose up` runs api + worker + postgres + redis; `/healthz` and `/readyz` work; CI is green |
| M1 | Catalogue | migrations, seed loader, variant search; unverified-data flag shown |
| M2 | Simulation engine | §10.1 met; hand-checkable, common-sense, determinism and cancellation tests pass; benchmark recorded |
| M3 | Sessions + interview | shortlist, constraints, usage questions, bracket ladder, adaptive selection; idempotency and `If-Match` integration tests pass |
| M4 | Jobs + progress | SKIP LOCKED queue, leases, SSE progress, cancel; worker-crash test passes |
| M5 | Decision + reports | winner / deadlock / none_fit; tipping points; template report; share links |
| M6 | Gemini | LLM prose with fact validator and fallback; rate limits; works with `LLM_ENABLED=false` |
| M7 | Frontend + deploy | three screens (shortlist, interview, results with histogram); deployed; README with architecture diagram, benchmark numbers and a demo script |

## 21. Open questions (answer before or during planning)
1. Should used-car support ship in M3, or move to after M7?
2. Is the frontend in scope for Claude Code, or will the developer build it?
3. Which Indian states should the assumptions cover at launch? (Suggested: Tamil Nadu, Karnataka, Maharashtra, Delhi.)

## MUST DO (tasks that are equally important as you are writing the code)
1. Create a "DECISIONS.md" file that basically a knowledge root of all the key decisions. Log all the meaningful decisions that you are making while coding and include all the key reasons behind that particular taken decision. An illustrative example would Be exaplaining why a particular library or why a particular tradeoff was accepted.
2. Create "FLOW.md" file that documents how the code Execution is moving between the files and functions throughout the Code base.