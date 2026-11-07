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
