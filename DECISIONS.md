# DECISIONS.md — why the code is the way it is

This file records every meaningful decision in the project and the reason for it. Each entry says **what** was decided, **why**, and **what else was considered**. Newer entries go at the bottom of each section. Entries are never deleted: if a decision changes, the old entry is marked *Superseded by D-xx*.

The ID format is `D-<milestone>-<n>`. `D-P-*` entries were taken during planning, before any code was written.

---

## Planning decisions (before M0)

### D-P-1 — Follow the fixed stack from SPEC §3
- **Decision:** Go, Gin, GORM, golang-migrate, PostgreSQL 16, Redis 7, `google.golang.org/genai`, `log/slog`, testcontainers-go, golangci-lint, Docker Compose, GitHub Actions, Next.js.
- **Why:** the spec fixes the stack. Swapping anything requires asking first.

### D-P-2 — Used cars ship in M3 (developer's answer to SPEC §21 Q1)
- **Decision:** the engine supports used cars from M2, and the API supports them from M3. A used car's resale value at the end of ownership = asking price × curve(current age + T) / curve(current age) × shocks.
- **Why:** to the engine, a used car is just different inputs. The spec's own example includes a used Nano.

### D-P-3 — Claude builds the thin frontend in M7 (§21 Q2)

### D-P-4 — Launch states: TN, KA, MH, DL (§21 Q3)

### D-P-5 — Gemini P2 uses are deferred to an optional M8
- **Decision:** `GenerateJSON` and `llm.Fake` are built and tested in M6. Free-text usage parsing and brochure import come after M7.
- **Why:** they are explicitly v2 or P2 in the spec, and the core value (P1: report prose) doesn't depend on them.

### D-P-6 — Assumption values are clearly marked placeholders
- **Decision:** every assumption key for the 4 states is seeded with a value marked `PLACEHOLDER`, `verified=false` and `source_url="TODO"`. The API and UI show a "placeholder assumptions" banner. The developer replaces them with sourced values.
- **Why:** SPEC §7 forbids inventing data, but the engine needs numbers to run. Marking them visibly as placeholders is honest. Silently plausible numbers would not be.
- **Alternative rejected:** researching and citing real public values now. That risks wrong data that *looks* verified.

### D-P-7 — Preferences the user wasn't asked about count as ₹0 in the final simulation
- **Decision:**
  - The final simulation only prices what the user actually told us. The report lists unasked differences as "not asked".
  - To compute an attribute's information gain, its "before" state is an equal-weight mix of the 6 ladder brackets.
- **Why:**
  - A broad prior (up to ₹8L) on unasked attributes would swamp the ownership costs and produce noise-driven deadlocks.
  - Using the bracket mix for information gain makes the gain mathematically non-negative: entropy is concave, so the entropy of a mix is at least the average of the parts' entropies.
  - The interview's stop rule looks at the same state the final simulation uses, so "the interview stopped because Car A leads" stays true in the final report.

### D-P-8 — Safety is compared on stars only
- **Decision:** the safety question uses NCAP stars and is asked only when every remaining car has a rating. If the rating bodies differ, a warning is shown. Airbag and ESC differences come up through the `features` attribute.
- **Why:** stars, airbag counts and ESC can't be interpolated on one linear scale without making up weights. An unrated car must never count as 0 stars (§7).

### D-P-9 — `DISCOUNT_RATE` is a nominal opportunity-cost rate
- **Why:** fuel-price paths and EMIs are in nominal rupees. Discounting nominal cash flows with a *real* rate would be inconsistent.

### D-P-10 — Answer shapes
- **Decision:** km/month and the city/highway split are ranges. Ownership years and loan terms are single values.
- **Why:** the engine steps one year at a time and needs a whole-number horizon. Loan terms come from a quote, so a range adds nothing.

### D-P-11 — The on-road price excludes first-year insurance
- **Why:** insurance is counted every year from year 1. Including it in the on-road price as well would count it twice. The override field's label says so.

### D-P-12 — One car survives the constraints → skip the trade-off interview
- **Decision:** the usage questions and the full simulation still run, so the cost breakdown is still shown. The result is a winner report saying "the only car that meets your constraints".

### D-P-13 — How the bracket ladder works with information gain
- **Decision:**
  - The next attribute is chosen by information gain over its 6 final brackets. Its ladder then runs to the end (at most 3 yes/no steps).
  - Stop rules are checked between steps. A ladder cut short leaves a wider bracket (e.g. "yes at ₹1L" → `[1L, 8L]`).
  - Mechanics: state is a pair of rung indices `(lo, hi)`, starting at `(0, 6)`. Ask rung `m = (lo+hi)/2`; "yes" sets `lo = m`, "no" sets `hi = m`; stop when `hi − lo = 1`.
- **Why:** information gain is defined over whole answers (brackets). Changing attribute in the middle of a ladder would confuse users. The index arithmetic asks ₹1L first, never asks about ₹0, and finishes in at most 3 steps.

### D-P-14 — Common random numbers (CRN) in the engine
- **Decision:**
  - Each future draws a *fixed* number of shared "world" values, in a fixed order, even when some are unused.
  - Car-specific randomness comes from `PCG(futureSeed, FNV64a(shortlistItemID))`.
  - The quick sims for one interview decision all use the same seed.
- **Why:** comparing brackets on identical futures cancels most of the Monte Carlo noise, so 2,000 futures are enough to rank questions. Keying car streams by stable ID means adding or removing a car never changes another car's randomness.

### D-P-15 — Session state machine
- **Decision:**
  - SHORTLISTING → USAGE via `PUT /constraints`, or → DONE if no car passes the constraints.
  - USAGE → INTERVIEW after the last usage answer.
  - INTERVIEW → SIMULATING via `POST /simulations`.
  - SIMULATING → DONE when the job finishes, or → INTERVIEW if the run is cancelled or fails.
  - Anything else returns 409.
- **Why:** the spec lists the states but not the edges. A cancelled run must not leave the session stuck.

### D-P-16 — Sample catalogue cars are fictional
- **Decision:** 3 variants from "Sample Motors", all `verified=false`, with sources pointing at `https://example.invalid/placeholder`.
- **Why:** SPEC §7 forbids invented real specifications. Made-up numbers on a real model name would be exactly that.

### D-P-17 — IDs: 128 random bits, base64url (22 characters)
- **Why:** §14 requires ≥128 bits of randomness. UUIDv4 has only 122 random bits; §5.2 also allows "random 128-bit".

### D-P-18 — Gemini: `Models.GenerateContent` on genai v1.76.0, model `gemini-3.5-flash-lite` (checked 2026-11-07)
- **Why:**
  - These were checked in the official docs and in the SDK source, not from memory.
  - v1.77.0 was less than 2 weeks old, so v1.76.0 is pinned.
  - `gemini-3.5-flash-lite` is labelled Stable and "fastest, most cost-effective".
  - The newer Interactions API also exists, but `GenerateContent` matches the spec's client setup and is the established surface.
- **Watch-outs found in the SDK source:**
  - If both `GOOGLE_API_KEY` and `GEMINI_API_KEY` are set, the SDK uses `GOOGLE_API_KEY`, so config fails fast in that case.
  - `genai.APIError` is returned as a *value*, so the check is `errors.As(err, &genai.APIError{})` with a value target.
  - `HTTPRetryOptions.Attempts` counts the first request, so "1 retry" means `Attempts=2`.

### D-P-19 — Integration tests use the `integration` build tag
- **Why:** `go test -race ./...` stays fast and works without Docker. CI runs `go test -race -tags=integration ./...` as a separate job.

---

## M0 — Skeleton

### D-M0-1 — Module path `github.com/xoBerlierxo/TieBreaker`
- **Why:** this is the GitHub account `gh` is logged in as; the developer confirmed it. Go module paths may contain capital letters; the proxy escapes them (`!tie!breaker`), so mixed case only looks odd, it doesn't break anything.

### D-M0-2 — Dependency versions: newest release that is at least 2 weeks old (checked 2026-11-07 on proxy.golang.org)
| Module | Version | Note |
|---|---|---|
| github.com/gin-gonic/gin | v1.12.0 | |
| gorm.io/gorm | v1.31.2 | |
| gorm.io/driver/postgres | v1.6.3 | uses pgx v5 underneath |
| github.com/jackc/pgx/v5 | v5.11.0 | v5.12.0 was less than 2 weeks old |
| github.com/golang-migrate/migrate/v4 | v4.20.1 | |
| github.com/redis/go-redis/v9 | v9.23.0 | |
| github.com/testcontainers/testcontainers-go (+ modules) | v0.44.0 | v0.45.0 was 2 days old |
| go.uber.org/goleak | v1.3.0 | |
| golang.org/x/sync | v0.24.0 | errgroup |
| github.com/golang-jwt/jwt/v5 | v5.3.1 | used from M1 |
| google.golang.org/genai | v1.76.0 | used from M6 |
| golangci-lint | v2.15.0 | |
- **Why:** a brand-new release hasn't had time for regressions to surface. Two weeks is a cheap safety margin.

### D-M0-3 — `go 1.26` in go.mod
- **Why:** it matches the installed toolchain (1.26.1) and satisfies the spec's "≥ 1.24" (the genai SDK's minimum). CI reads the version from `go.mod`, so local and CI stay in step.

### D-M0-4 — Package boundaries are enforced by golangci-lint `depguard`
- **Decision:** `internal/sim` and `internal/decision` may not import `internal/{http,store,llm,jobs}`, GORM, Redis, Gin, or the old `math/rand`.
- **Why:** SPEC §4 makes this a rule. A lint failure in CI enforces it; a comment would only state it.

### D-M0-5 — The HTTP package is named `httpapi` (directory `internal/http`)
- **Why:** SPEC §4 fixes the directory name. A package literally named `http` would clash with `net/http`, which almost every file in it imports, forcing an alias in each file. Go allows a package name to differ from its directory; the package doc comment explains this.

### D-M0-6 — CI action versions
- **Decision:** `actions/checkout@v7.0.1`, `actions/setup-go@v7.0.0` (reads the Go version from `go.mod`), `golangci/golangci-lint-action@v9.3.0` running golangci-lint v2.15.0. All are pinned to exact tags.
- **Jobs:** `lint`, `unit` (also checks that `go mod tidy` leaves no diff), `race`, `integration` (`-tags=integration`, Docker on the hosted runner).
- **Why:** these are exactly the four CI jobs SPEC §3 asks for. Exact tags mean CI can't change underneath us.

### D-M0-7 — Domain errors: one `apperr` package, mapped to HTTP in one function
- **Decision:** `internal/apperr` holds the sentinel errors (`ErrNotFound`, `ErrVersionConflict`, …) and an `*apperr.Error{Kind, Message, Details}` type. `httpapi.statusFor` is the only place they become status codes.
- **Why:** SPEC §18 asks for sentinel/typed errors mapped in one place. A separate package with no dependencies lets services and the HTTP layer share it without import cycles.
- **Client-facing messages:** for 4xx the message comes from the domain error. For 500 the client only sees `"internal error"`; the cause is logged, because it may contain SQL, hostnames or credentials (`TestErrorResponse_InternalErrorsHideTheCause`).

### D-M0-8 — Liveness (`/healthz`) never touches dependencies; readiness (`/readyz`) checks both
- **Why:** if liveness depended on Postgres, a short database outage would make the orchestrator restart every healthy API container, which makes the outage worse. Readiness returns 503 with per-dependency details, so traffic is routed away until the dependency is back.

### D-M0-9 — Configuration takes `getenv` as a parameter; `Config` implements `slog.LogValuer`
- **Why:**
  - No globals, and tests pass a map, so the real environment never leaks into tests.
  - All errors are collected with `errors.Join`, so a bad deployment is fixed in one pass instead of one variable at a time.
  - `LogValue` means logging the config can never print the Gemini key or the JWT secret. Passwords inside `DATABASE_URL`/`REDIS_URL` are redacted with `url.URL.Redacted()`.
  - Durations are logged as strings, because slog's JSON handler prints nanoseconds.

### D-M0-10 — Graceful shutdown with `signal.NotifyContext` and `http.Server.Shutdown`
- **Decision:** one root context is cancelled on SIGINT/SIGTERM.
  - The server stops accepting connections and waits up to `SHUTDOWN_GRACE_SECONDS` (default 20 s) for in-flight requests.
  - The shutdown deadline uses `context.WithoutCancel(ctx)`, because the root context is already cancelled at that point.
  - `runHTTPServer` waits for `Serve` to return before returning, so no goroutine outlives it.
- **Tests:** `TestServe_InFlightRequestFinishesDuringShutdown` checks that a request started before shutdown still completes.

### D-M0-11 — Server timeouts
- **Decision:** `ReadHeaderTimeout` 5 s, `ReadTimeout` 15 s, `WriteTimeout` 30 s, `IdleTimeout` 60 s.
- **Why:** they protect against slow clients holding connections open (Slowloris). The SSE route (M4) will extend its own write deadline with `http.ResponseController` instead of disabling the timeout for everyone.

### D-M0-12 — Request IDs and access logs
- **Decision:** a valid incoming `X-Request-ID` (`[A-Za-z0-9._-]{1,128}`) is kept so requests can be traced through a proxy; otherwise 128 random bits are used, hex-encoded. Access logs record the **route template** (`/v1/sessions/:id`), not the raw path.
- **Why:** restricting the ID prevents log injection (newlines). Logging the template keeps session IDs, which act like bearer secrets, out of the logs.

### D-M0-13 — Body-size limit in two layers
- **Decision:** a declared `Content-Length` over the limit is rejected immediately with 413. Otherwise the body is wrapped in `http.MaxBytesReader`, and reading past the limit returns `*http.MaxBytesError`, which `statusFor` maps to 413.
- **Why:** chunked requests have no `Content-Length`, so checking the header alone would not be enough.

### D-M0-14 — Docker: static binary on `distroless/static:nonroot`; Compose uses development-only defaults
- **Why:**
  - `CGO_ENABLED=0` produces a fully static binary, so the runtime image needs no libc, shell or package manager: smaller and with less to attack.
  - Running as non-root limits the damage if the process is ever compromised.
  - Compose uses `${VAR:-dev-default}`, so `docker compose up` works on a fresh checkout without any setup. The defaults are clearly named `dev_only`/`insecure` and are never used in production, where Railway variables are set.
  - `migrate` becomes a one-shot Compose service in M1, once there are migrations to run.

### D-M0-15 — Integration helpers live behind the `integration` tag
- **Decision:** `internal/testutil` starts one Postgres 16 and one Redis 7 container per test package, from `TestMain`. Images match Compose and production.
- **Why:** starting containers costs seconds, so sharing them per package keeps the suite fast. Tests isolate themselves with their own tables or keys. `TestMain` delegates to a function so the deferred terminate calls run before `os.Exit`.

### D-M0-16 — GORM settings: slog logger, parameterised query logging, `TranslateError`
- **Why:**
  - Only slow queries (> 200 ms) and errors are logged, through slog, with placeholders instead of values (`ParameterizedQueries`), because values may contain user answers (SPEC §13).
  - `TranslateError` turns driver errors into `gorm.ErrDuplicatedKey` etc., so repositories can use `errors.Is`.
  - Timestamps are UTC.

---

## M1 — Catalogue

### D-M1-1 — Migrations are embedded and run by `tiebreaker migrate`, plus a one-shot Compose service
- **Decision:** `migrations/embed.go` embeds `*.sql`. `store.MigrateUp` uses golang-migrate with the `iofs` source and the `pgx/v5` database driver, over a dedicated `database/sql` connection. Compose runs `migrate up` once; `api` and `worker` wait for `service_completed_successfully`.
- **Why:** containers and Railway don't need the migrate CLI or the files on disk. golang-migrate's advisory lock makes concurrent runs safe. The `pgx/v5` driver avoids pulling in `lib/pq` next to pgx. The `migrate` subcommand reads only `DATABASE_URL`, so it can run before the rest of the config exists.

### D-M1-2 — Database CHECK constraints *and* Go validation
- **Decision:** every rule on `variants` is enforced twice: as a SQL `CHECK` (the last line of defence, which also covers manual SQL) and in `Variant.Validate()`, which gives friendly field-level errors.
- **Why:** a constraint violation from Postgres is a poor API error. Validation without constraints lets bad data in through any other path.

### D-M1-3 — One `Variant` struct for the GORM model, the seed format and the admin API body
- **Why:** there is one source of truth for field names (explicit `gorm:"column:…"` and `json:"…"` tags on every field). The API adds derived fields and warnings through `httpapi.VariantDTO`, which embeds `Variant`. Nullable data is a pointer field, so "unknown" is never confused with zero.

### D-M1-4 — `store.TextArray` for `text[]`, built on pgx's own codec
- **Why:** GORM has no array type, and `database/sql` can't scan an array into `[]string`. Delegating to `pgtype.Map.SQLScanner` reuses pgx's tested array parser instead of hand-writing one. Writing works because pgx's `database/sql` driver accepts a `[]string` value.

### D-M1-5 — `catalog.Date` for "checked on" dates
- **Why:** seed files are typed by hand, and `"2026-11-07"` is much friendlier than an RFC 3339 timestamp. The type implements JSON and SQL (un)marshalling for a Postgres `date`.

### D-M1-6 — Placeholders are detectable by rule, not by convention
- **Decision:** `catalog.IsPlaceholderURL` treats `TODO`, empty, non-http(s), `example.com` and any `.invalid` host as a placeholder. Sample sources use `https://example.invalid/placeholder`; `.invalid` is reserved by RFC 2606 and can never resolve.
- **Enforcement:** `CanVerify` refuses placeholder sources. The seed loader refuses `verified: true` without real sources. A guard test (`TestSeedRepo_SamplesAreFictionalAndUnverified`) runs over the real `data/seed` and enforces SPEC §7: at most 3 samples, all unverified.

### D-M1-7 — The feature vocabulary lives in Go (`catalog.KnownFeatures`), not in `data/seed/features.json`
- **Why:** the interview needs it at runtime inside the API binary, and a Go function is simpler than embedding or loading a file. It also can't be mutated (it returns a new slice). *(Deviation from Plan.md M1.4, which listed `features.json`.)*

### D-M1-8 — Assumption keys and typed decoding
- **Decision:**
  - The keys follow SPEC §5.1's examples (`fuel_price.petrol.TN`, `electricity_tariff.TN`, `road_tax.TN.rule`, `real_efficiency_factor.petrol`). `docs/model.md` lists all of them.
  - Each key has its own decode step (`specsFor`), using strict JSON (unknown fields rejected, so `"sigam"` is an error), followed by value validation.
  - All missing or invalid keys are reported together.
- **Why:** the engine must never receive a silent zero from a typo. A strongly typed `AssumptionSet` is also exactly what gets snapshotted into a run.

### D-M1-9 — Seeding is an idempotent upsert in one transaction
- **Decision:** variants are matched by `slug` (their sources replaced), assumptions by `(key, model_version)` with `ON CONFLICT DO UPDATE`. Everything happens in a single `WithTx`.
- **Why:** re-running the loader after editing a file must converge, never duplicate. A failure half-way leaves the database unchanged.

### D-M1-10 — Admin JWT details
- **Decision:**
  - HS256 only (`WithValidMethods`, which blocks `alg: none` and algorithm confusion).
  - Issuer `tiebreaker`, audience `tiebreaker-admin`, `exp` and `iat` required, 30 s leeway, lifetime ≤ 1 h (enforced both when minting and when checking).
  - 401 for any token problem; 403 for a valid token without `role=admin`.
  - Production requires `JWT_SECRET` to be at least 32 characters.
- **Why:** these are the standard JWT pitfalls. Rejecting long-lived tokens even when they are correctly signed limits the damage from a leaked token.

### D-M1-11 — New sentinels `ErrConflict` (409), `ErrUnauthorized` (401), `ErrForbidden` (403)
- **Why:** deleting a variant that is used in a shortlist is a generic conflict, not a session-version conflict. Admin auth needs 401 and 403. All three are still mapped only in `statusFor`.

### D-M1-12 — Search details
- **Decision:** `ILIKE '%q%'` on exactly the expression the trigram index uses. `%` and `_` in the query are escaped. `limit` defaults to 20, and values outside 1–50 are a 400 (not silently clamped). A malformed UUID returns 404, not a Postgres syntax error.

---

## M2 — Simulation engine

### D-M2-1 — The engine takes fully resolved inputs
- **Decision:** `sim.Inputs` holds per-car numbers (upfront price, value basis, service cost per visit, repair rate, resale curve…) and shared `World` numbers. The session package does the mapping from the catalogue and assumptions to these numbers (M3).
- **Why:** the engine stays a pure function with no lookups, and `sim.Inputs` is exactly the JSON stored in a run (`inputs_json`). That makes a run reproducible from its database row alone.

### D-M2-2 — One seed per future, then streams keyed by name
- **Decision:** the chunk RNG is exactly as the spec requires: `rand.New(rand.NewPCG(masterSeed, uint64(i)))`. Each future takes one `Uint64()` from it as `futureSeed`. Then:
  - the world uses `PCG(futureSeed, const)`;
  - each car uses `PCG(futureSeed, FNV64a("car:"+ID))`;
  - each preference uses `PCG(futureSeed, FNV64a("pref:"+key))`.
- **Why:** if everything were drawn sequentially from the chunk stream, adding a car, a preference or a year would shift every later number, which destroys common random numbers. With keyed streams, every comparison the interview and tipping-point code makes (same seed, one input changed) is on identical futures. The tests prove it: `TestCarStream_UnaffectedByAddingAnotherCar` and `TestPrefStream_AddingAttributeDoesNotShiftOthers`.
- **Fixed draw count:** the world stream always draws 87 values (15 years even if T is shorter, every energy even with no EV).

### D-M2-3 — "Merge in chunk order" is done by index, not by a copy step
- **Decision:** results are pre-allocated, and future k writes slot k. Chunks write only their own disjoint ranges, so the assembled result is in chunk order no matter which goroutine finishes first. No mutex, no channel, no second copy.
- **Why:** this meets SPEC §10.1's goal (identical output for any worker count) with less memory and code. The race detector and `TestRun_IdenticalAcrossWorkerCounts_1_4_16` (bit-exact) confirm it.

### D-M2-4 — Concurrency with `errgroup.WithContext` + `SetLimit`
- **Decision:** `g.SetLimit(workers)` bounds parallelism (`g.Go` blocks when it is full). The first error cancels `gctx`, and the dispatch loop stops starting new chunks once `gctx` is cancelled.
- **Progress:** the callback runs under a mutex, so callers see 1, 2, 3… in order.
- **Workers** = `runtime.GOMAXPROCS(0)` by default, capped at the number of chunks.

### D-M2-5 — Cancellation is checked every 256 futures
- **Decision:** each chunk checks `ctx.Err()` every 256 futures (SPEC §10.1). `Run` returns `ctx.Err()` (`context.Canceled` or `DeadlineExceeded`) and no partial result.
- **Tests:** `TestRun_CancelMidRunReturnsCanceledPromptly` measures how fast it returns. goleak in `TestMain` fails the package if a goroutine survives.

### D-M2-6 — Mean-one log-normal shocks
- **Decision:** km variation and resale shocks are `exp(σZ − σ²/2)`.
- **Why:** `E[exp(σZ)] = exp(σ²/2) > 1`, so a plain `exp(σZ)` would quietly inflate costs and resale values as σ grows. Subtracting σ²/2 keeps the mean exactly equal to the deterministic value. Price paths keep the spec's `exp(μ + σZ)`, with μ as given in the assumptions.

### D-M2-7 — Year conventions
- **Decision:** year 1 pays today's price, and costs are discounted at the end of their year. Age in year t = age at purchase + t − 1. Insurance IDV follows the schedule from age 0. For used cars, IDV and resale are scaled relative to the car's age at purchase.
- **Why:** these conventions make the hand-checkable test unambiguous. Each one is written out in `docs/model.md` §3.

### D-M2-8 — Financing is deterministic and precomputed once per run
- **Why:** loan payments don't depend on the future, so they are computed once in `prepare`. Any balance still owed at the horizon is paid off then, and the principal is capped at each car's price.

### D-M2-9 — P(best): exact ties split equally; entropy in bits
- **Why:** two identical cars must come out 50/50 for the deadlock sanity test, not 100/0 in favour of whichever is listed first. Entropy in bits (log₂) makes the 0.02 information-gain threshold easy to read: 1 bit = a fair coin between two cars.

### D-M2-10 — Percentiles use type-7 linear interpolation
- **Why:** this is the most common definition (NumPy's default, Excel's `PERCENTILE.INC`), so p10/p50/p90 match what a reviewer computes in a spreadsheet.

### D-M2-11 — Golden-hash regression test
- **Decision:** `TestRun_GoldenHash` stores the SHA-256 of all samples for a fixed input and seed. Any change in output fails it; the fix is to bump `sim.ModelVersion` and update the hash.
- **Why:** reproducible share links depend on the model not changing silently.
- **Limit:** the Go spec allows fused multiply-add on some architectures, so bit-exactness is per architecture (amd64 in CI and on Railway).

### D-M2-12 — Benchmarks set the worker count explicitly
- **Why:** `-cpu=1,4` changes GOMAXPROCS, but the first measurements didn't reflect it the way expected (a "1 CPU" run reported 12-thread speed). Sub-benchmarks with `workers=1/4/NumCPU` give unambiguous numbers for the README. Results: 50k futures in 33 ms on 4 workers; quick sim p95 7.4 ms (`docs/benchmarks.md`).

### D-M2-13 — Assumption key `service.default_interval_km` added
- **Why:** a variant with no service interval still needs one to simulate. The fallback value belongs in the assumptions table (SPEC §10: every assumption lives there), not hard-coded, and a data warning is shown when it is used.

---

## M3 — Sessions and interview

### D-M3-1 — Optimistic locking with one `UPDATE … WHERE version = ? RETURNING`
- **Decision:** every change to a session starts with `UPDATE sessions SET version = version + 1 WHERE id = ? AND version = ? RETURNING …` inside the transaction (`Service.mutate`). If no row comes back, a follow-up read tells 404 (session missing) apart from 409 (`SESSION_VERSION_CONFLICT`, with `current_version` in the details).
- **Why:** the UPDATE both checks the version and takes the row lock in one statement, so two concurrent writers are serialised. Under READ COMMITTED, the second writer waits for the first to commit, then re-evaluates its `WHERE` clause against the new row, finds the version has changed, and gets 409. Because the bump happens *first*, the rest of the work runs only for the winner. Any later error rolls the bump back.

### D-M3-2 — Idempotency key is claimed in the same transaction, before the version check
- **Decision:** `INSERT INTO idempotency_keys … ON CONFLICT DO NOTHING`. If the key already exists, the request hash is compared: a different body gets 422, the same body gets the stored response (status and exact bytes) with `Idempotent-Replayed: true`. Only then does the version bump run.
- **Why:**
  - **Before the version check:** a client retrying after a lost response still has the old `If-Match`; it must get the original 201, not a 409. `TestIdempotency_ReplayAfterVersionMovedReturnsStored` covers this.
  - **Same transaction:** if a second request with the same key arrives while the first is in flight, Postgres makes its INSERT wait on the unique index until the first commits. It then replays the committed response (`TestConcurrentSameKey_OneRowBothSameResponse`).
  - **Failures roll back the key**, so a 400 or 409 never gets stuck on a key; the client can fix the request and retry with it.
- **Request hash:** SHA-256 of canonical JSON (decoded with `UseNumber`, re-encoded with sorted keys), so whitespace and key order don't matter.

### D-M3-3 — The stored idempotent response is `text`, not `jsonb`
- **Why:** found by `TestIdempotency_SameKeySameBodyReturnsSameResponseOneRow`. `jsonb` normalises key order and spacing, so a replay returned different bytes than the original response. "Same response" should mean byte-identical, so `response_json` is a `text` column holding the exact bytes. (A change to migration 0007 before any release.)

### D-M3-4 — Answers are the source of truth for interview progress
- **Decision:** there is no interview-state column. Usage answers are rows like `usage:km_per_month`; each ladder step is its own row `tradeoff:<attribute>:<step>`, holding `{attribute, step, threshold_inr, yes}`. `TradeoffState` and the brackets are rebuilt by replaying these rows in insertion order.
- **Why:** there is nothing to keep in sync. The unique `(session_id, question_id)` is a second guard against double answers. The SPEC §5.3 bracket format (`{attribute, min_inr, max_inr}`) is computed for the session view.

### D-M3-5 — Answer validation follows the interview's rules, but doesn't recompute the question choice
- **Decision:**
  - Usage questions must be answered in their fixed order.
  - A trade-off answer is accepted for the next step of the ladder in progress, or, when no ladder is in progress, for step 1 of any not-yet-asked candidate (respecting the feature cap and the 10-question limit).
- **Why:** re-running the information-gain selection on every POST would double the cost for no safety gain. A client choosing a different valid candidate harms nobody, and "real differences only" is still enforced.

### D-M3-6 — The user's state is collected when the session is created (Plan.md D2)
- **Why:** the budget constraint uses the on-road price, which needs state road tax, and constraints come before the usage questions. The usage "state" question is therefore not asked again.

### D-M3-7 — Prices for the budget check vs. the engine
- **Decision:**
  - **New car:** on-road = ex-showroom + road tax (state slab) + registration/misc, excluding insurance (D-P-11). An on-road override replaces it.
  - **Used car:** the budget compares the asking price (SPEC §6.2); the engine's upfront cost adds transfer costs (rate × price + fixed).
  - The value basis for insurance and resale is ex-showroom (new) or asking price (used).

### D-M3-8 — Interview seed per session and a 2 s quick-sim timeout with a fallback
- **Decision:** every quick simulation in one interview uses `FNV64a("interview:"+sessionID)` as its seed, so all comparisons are on the same futures. `next-question` runs under `context.WithTimeout(QuickTimeout)`. If the deadline expires, the first open candidate in fixed priority order is asked and `fell_back: true` is returned and logged.
- **Why:** SPEC §11 requires a strict timeout. The user should never be blocked by a slow machine; a sensible question beats an error.

### D-M3-9 — Quick-sim cache in Redis; errors count as misses
- **Decision:** the key is `quicksim:` + SHA-256(inputs JSON, seed, futures, model version), with a 1 h TTL. A Redis error is logged and treated as a cache miss.
- **Why:** the cache only speeds things up, so it must never fail a request. The model version in the key means an engine change can't serve stale results.

### D-M3-10 — Parallel candidate evaluation; quick sims single-threaded
- **Decision:** `informationGains` runs one goroutine per candidate (`errgroup.SetLimit(GOMAXPROCS)`), and each quick sim runs with `Workers=1`. Each goroutine clones the preference slice before appending to it.
- **Why:** this keeps every core busy without oversubscribing it. The clone avoids a data race: `append` on a slice shared between goroutines can write into the same backing array.
- **Measured:** about 57 ms per call (p95 70 ms) with no cache for 3 cars × 9 candidates (64 quick sims). The target is 1.5 s.

### D-M3-11 — Shared rupee formatting in `internal/money`
- **Why:** interview questions and reports must show the same number the same way (₹1,02,340 and "₹1.02 lakh"). Plan.md put this in `report/format.go`; it moved to its own package because `interview` needs it first.

### D-M3-12 — The spec's session integration scenarios run over real HTTP
- **Decision:** `internal/http/integration_test.go` wires the real router and services to real Postgres and Redis. It checks stale `If-Match` → 409, missing → 412, idempotency replay and 422, concurrent submissions, the shortlist rules, none-fit, and a full interview. *(Plan.md listed these under the session package; testing them through HTTP also covers the status-code mapping and headers.)*
- **Test plumbing:** the unit-test `TestMain` lives in `main_unit_test.go` (`//go:build !integration`), because a package can have only one `TestMain` per build.
