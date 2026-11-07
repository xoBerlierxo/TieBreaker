# FLOW.md — how execution moves through the code

This file follows the path a program run or a request takes through the files and functions. Each milestone adds its flows. Function names are written `package.Function`, with the file in brackets.

Status: **M7 (complete)**. Backend, frontend, and deployment entry points.

---

## 1. Process startup (both `serve` and `worker`)

```
main()                                         [cmd/tiebreaker/main.go]
 ├─ signal.NotifyContext(SIGINT, SIGTERM)      → root ctx; cancelled on Ctrl+C or `docker stop`
 ├─ slog.New(JSONHandler(stdout))              → the one logger, passed down explicitly
 └─ run(ctx, os.Args[1:], os.Getenv, logger)   [main.go]
     ├─ pick the subcommand: "serve" → serve, "worker" → worker, otherwise errUsage (exit 2)
     ├─ config.Load(getenv)                    [internal/config/config.go]
     │    ├─ parser.str / required / intMin / probability / ladder …   (defaults from SPEC §17)
     │    ├─ cross-field checks (APP_ENV, ladder cap, LLM key/model, no GOOGLE_API_KEY)
     │    └─ errors.Join(all problems) → returned as one error
     ├─ gin.SetMode(Release) when APP_ENV=production
     ├─ logger.Info("starting", config)        → Config.LogValue() redacts secrets
     └─ serve(ctx, cfg, logger)  or  worker(ctx, cfg, logger)
```

If `run` returns an error, `main` logs it and exits with code 1. `main` is the only place that calls `os.Exit`.

## 2. `serve`: the HTTP API

```
serve(ctx, cfg, logger)                        [cmd/tiebreaker/serve.go]
 ├─ store.OpenPostgres(ctx, DATABASE_URL)      [internal/store/postgres.go]
 │    gorm.Open (slog logger, TranslateError) → pool limits → PingContext (5 s timeout)
 ├─ store.OpenRedis(ctx, REDIS_URL)            [internal/store/redis.go]
 │    redis.ParseURL → timeouts → Ping (5 s timeout)
 ├─ httpapi.NewRouter(Deps{Logger, Postgres pinger, Redis pinger, MaxBodyBytes})
 │                                             [internal/http/router.go]
 │    middleware, in order: requestID → accessLog → recovery → bodyLimit   [internal/http/middleware.go]
 │    routes: GET /healthz, GET /readyz (healthHandler)                     [internal/http/health.go]
 │    NoRoute → WriteError(ErrNotFound)
 └─ runHTTPServer(ctx, addr, router, grace)    [serve.go]
      ├─ net.ListenConfig.Listen
      ├─ go srv.Serve(ln)                      → serving goroutine; its result goes to the serveErr channel
      ├─ select: serveErr (a real failure) | ctx.Done() (shutdown requested)
      ├─ srv.Shutdown(WithTimeout(WithoutCancel(ctx), grace))  → in-flight requests finish
      └─ <-serveErr                            → wait until the serving goroutine has exited
 deferred: redis Close, then ClosePostgres
```

## 3. One HTTP request (e.g. `GET /readyz`)

```
net/http server goroutine
 └─ gin.Engine.ServeHTTP
     ├─ requestID()   keep a valid X-Request-ID or generate one; c.Set("request_id"); echo the header
     ├─ accessLog()   start a timer → c.Next() → one slog line {request_id, method, route template, status, duration_ms}
     ├─ recovery()    defer recover() → on panic: log the stack, WriteError(500)
     ├─ bodyLimit()   Content-Length > max → WriteError(413); otherwise wrap the body in http.MaxBytesReader
     └─ healthHandler.readyz                   [internal/http/health.go]
          ├─ context.WithTimeout(request ctx, 2 s)
          ├─ for each of postgres, redis (fixed order): pinger.Ping(ctx)
          │     store.PostgresPinger.Ping → sql.DB.PingContext
          │     store.RedisPinger.Ping    → redis PING
          ├─ all ok  → 200 {"status":"ok","checks":{...}}
          └─ any down → WriteError(apperr.New(ErrUnavailable).WithDetails("checks", …))
```

### How errors leave a handler

```
handler returns or detects err
 └─ httpapi.WriteError(c, logger, err)         [internal/http/errors.go]
     ├─ statusFor(err)        errors.Is against the apperr sentinels → (status, CODE)   ← the ONLY mapping
     ├─ publicMessage(err)    4xx: the domain message; 500: "internal error" (the cause stays out of the response)
     ├─ details               copied from *apperr.Error (never for 500)
     ├─ status ≥ 500 → logger.Error with request_id and the real cause
     └─ c.AbortWithStatusJSON(status, {"error":{code,message,details}})
```

## 4. `worker` (M0 placeholder)

```
worker(ctx, cfg, logger)                       [cmd/tiebreaker/worker.go]
 ├─ store.OpenPostgres / store.OpenRedis       → fail fast if misconfigured
 ├─ log "worker started"
 └─ <-ctx.Done()                               → wait for SIGTERM; the job loop is added in M4
```

## 5. Shutdown (`docker compose stop`, Railway redeploy, Ctrl+C)

```
SIGTERM → root ctx cancelled
 ├─ serve:  runHTTPServer's select wakes → Shutdown(grace) → in-flight requests finish → Serve returns → close Redis, Postgres
 └─ worker: <-ctx.Done() returns → close Redis, Postgres
main: stop() releases the signal handler; returns normally (exit code 0)
```

## 6. Migrations (`tiebreaker migrate up`, the Compose `migrate` service)

```
run → migrateCmd(ctx, ["up"], getenv)          [cmd/tiebreaker/migrate.go]   (only DATABASE_URL needed)
 └─ store.MigrateUp(ctx, dsn)                  [internal/store/migrate.go]
     └─ withMigrate
         ├─ sql.Open("pgx", dsn) + PingContext
         ├─ pgxmigrate.WithInstance(db)         golang-migrate's pgx/v5 driver (takes an advisory lock)
         ├─ iofs.New(migrations.FS, ".")        the *.sql files embedded by migrations/embed.go
         ├─ context.AfterFunc(ctx → m.GracefulStop)
         └─ m.Up()   (ErrNoChange counts as success)
```

## 7. Seeding (`go run ./scripts/seed --dir data/seed`)

```
scripts/seed/main.go run()
 ├─ --check → catalog.ReadSeedDir(dir) only (no database)
 └─ store.OpenPostgres → catalog.LoadDir(ctx, db, dir)          [internal/catalog/seed.go]
     ├─ ReadSeedDir
     │   ├─ data/seed/*.json             → ModelFile{variants} (strict JSON) → checkSeedVariant (Validate + CanVerify if verified)
     │   └─ data/seed/assumptions/*.json → AssumptionFile (verified requires a real source and checked_at)
     └─ store.WithTx (all or nothing)
         ├─ each variant: lookup by slug → insertVariant | replaceVariant   [service.go]   (sources replaced)
         └─ each assumption: INSERT … ON CONFLICT (key, model_version) DO UPDATE
```

## 8. Catalogue requests

```
GET /v1/variants?q=&fuel=&limit=
 └─ variantHandler.search                       [internal/http/variants.go]
     ├─ queryInt("limit")                        [internal/http/bind.go]   bad integer → 400
     ├─ CatalogService.Search → catalog.Service.Search   [internal/catalog/service.go]
     │    validate q/fuel/limit → WHERE (make||' '||model||' '||variant_name) ILIKE %escaped% → ORDER BY name
     └─ toVariantDTO: + display_name, is_automatic, efficiency, PS/tonne, warnings [unverified_data, safety_not_rated]

GET /v1/variants/:id
 └─ variantHandler.get → catalog.Service.Get (malformed UUID → 404; Preload Sources) → toVariantDTO

POST|PUT|DELETE /admin/v1/variants[/:id]
 └─ requireAdmin(JWT_SECRET)                     [internal/http/auth.go]
      Bearer token → jwt parser (HS256 only, iss, aud, exp required, ≤ 1 h lifetime) → role == admin (else 403)
 └─ variantHandler.create|update|remove
      bindJSON (strict, single value) → catalog.Service.Create|Update|Delete
        checkWritable: Validate → sources complete → verified needs CanVerify (no placeholder sources)
        WithTx: insertVariant / lock row FOR UPDATE + replaceVariant / Delete (FK RESTRICT → 409 CONFLICT)
```

## 9. Assumptions → typed set (used from M3 onwards)

```
catalog.Service.LoadAssumptionSet(ctx, state, "sim-v1")      [internal/catalog/assumptions.go]
 ├─ SELECT rows WHERE model_version AND key IN RequiredAssumptionKeys(state)
 └─ BuildAssumptionSet → for each specsFor(state): strict decode into its typed field → collect provenance
                       → validate() (positive prices, every fuel/segment present, ranges valid)
                       → AssumptionSet (+ UnverifiedKeys for warnings, Snapshot for runs)
```

## 10. The simulation engine (`sim.Run`): pure; called by the interview (M3) and the worker (M4)

```
sim.Run(ctx, inputs, options)                       [internal/sim/run.go]
 ├─ inputs.Validate()                                [validate.go]   → ErrInvalidInputs
 ├─ prepare(&inputs)                                 [prepare.go]
 │    discount[t], inflation[t], price trends; per car: stream key, unit price (home or public tariff),
 │    cash upfront + financing PV (loanSchedule [finance.go]), IDV and resale normalisers
 ├─ newResult: pre-allocate every sample slice (one slot per future)
 ├─ errgroup.WithContext + SetLimit(workers)
 │   for chunk := range numChunks  (stop dispatching once gctx is cancelled)
 │     g.Go → runChunk(gctx, chunk)
 │             chunkRNG = rand.New(rand.NewPCG(masterSeed, chunk))
 │             for each future k in the chunk:
 │               every 256 futures: ctx.Err() → return
 │               futureSeed = chunkRNG.Uint64()
 │               world stream → drawWorld (87 fixed draws) [world.go] → buildPaths (km, price multipliers)
 │               prefValues: per attribute, pref stream → bracket + value → Σ share × value  [future.go]
 │               for each car: car stream → evaluateCar → 8 discounted categories  [future.go]
 │                             write slot k: categories, ownership (fixed-order sum), pref, net
 │           → progress callback under a mutex (monotonic)
 ├─ g.Wait()  → first error (e.g. context.Canceled)
 └─ ctx.Err() check → Result{Cars: per-car samples}

decision.Summarize(result)                          [internal/decision/summary.go]
 └─ PBest (ties split) · PCheapest · percentiles (type 7) · median per category · Leader · Entropy (bits)
```

## 11. Session lifecycle (HTTP → `session.Service`)

```
POST /v1/sessions {"user_state":"TN"}
 └─ sessionHandler.create [internal/http/sessions.go] → Service.Create [internal/session/service.go]
      NewID() (128 random bits, base64url) → INSERT sessions (state SHORTLISTING, version 1) → View; ETag: "1"

POST /v1/sessions/:id/cars        (If-Match)     → Service.AddCar
PUT  /v1/sessions/:id/constraints (If-Match)     → Service.SetConstraints
DELETE /v1/sessions/:id/cars/:itemId (If-Match)  → Service.RemoveCar
 └─ parseIfMatch ("3", 3, W/"3"; missing → 412)
 └─ Service.mutate(ctx, id, version, fn)
     └─ store.WithTx
         ├─ bumpVersion: UPDATE sessions SET version=version+1 WHERE id=? AND version=? RETURNING …
         │     0 rows → 404 (missing) or 409 SESSION_VERSION_CONFLICT (stale)
         └─ fn(tx, session):
              AddCar:         RequireState(SHORTLISTING) → count < 4 → BuildItem [pricing.go] → INSERT (duplicate → 409)
              SetConstraints: 2–4 cars → LoadAssumptionSet → PriceOf each car [pricing.go]
                              → Constraints.Validate(hasEV) → Evaluate [constraints.go] → write eliminations
                              → survivors? setState(USAGE) : setState(DONE) (+ none-fit hook, M5)
 └─ Service.Get → load (session, items, answers, variants) → view [view.go] → ETag
```

## 12. Answers (idempotent + optimistic locking)

```
POST /v1/sessions/:id/answers   (Idempotency-Key + If-Match)
 └─ sessionHandler.submitAnswer → Service.SubmitAnswer [internal/session/answers.go]
     RequestHash(body) (canonical JSON → SHA-256)
     store.WithTx:
       1. INSERT idempotency_keys … ON CONFLICT DO NOTHING
            existing key: different hash → 422 | same hash → replay stored status + exact bytes (Idempotent-Replayed: true)
       2. bumpVersion (stale If-Match → 409)
       3. applyAnswer by state:
            USAGE:     must be NextUsageQuestion → interview.ValidateUsageAnswer → INSERT answer
                       → all usage answered? setState(INTERVIEW)
            INTERVIEW: snapshot.tradeoffState() → interview.Candidates(survivors)
                       → interview.ValidateTradeoffAnswer (in-progress ladder first, feature cap, 10-question limit)
                       → INSERT answer {attribute, step, threshold_inr, yes}
       4. UPDATE idempotency_keys SET response_status, response_json (text, exact bytes)
     any error → rollback (including the key)
```

## 13. Next question (adaptive interview)

```
GET /v1/sessions/:id/next-question
 └─ Service.NextQuestion
     SHORTLISTING → 409 | USAGE → interview.NextUsageQuestion (fixed order) | SIMULATING/DONE → done
     INTERVIEW:
       buildFromSnapshot → BuildInputs [internal/session/inputs.go]
            survivors + AssumptionSet + usage + constraints → sim.Inputs; Candidates(survivors); warnings
       base = inputs without preferences; seed = interview.SeedFor(sessionID)
       context.WithTimeout(QuickTimeout = 2 s)
       interview.Planner.NextTradeoff [internal/interview/planner.go]
         ├─ limit reached? → done(max_questions)
         ├─ QuickSimulator.PBest(current state) [quicksim.go] (Redis cache → sim.Run(workers=1) → decision.PBest)
         │     leader ≥ 0.65 → done(leader_confident)
         ├─ ladder in progress → next step (Ladder.NextThreshold) [ladder.go]
         ├─ open candidates (unasked, feature cap) empty → done(no_candidates)
         ├─ informationGains: errgroup over candidates; per candidate 1 mixture sim + 6 bracket sims;
         │     gain = H(mixture) − mean H(bracket) (bits)
         ├─ best gain < 0.02 → done(low_information_gain)
         └─ question = step 1 of the best candidate at ₹1L (QuestionText with car names and horizon)
       deadline hit → first open candidate in priority order (fell_back: true, logged)
```

## 14. Starting a full simulation

```
POST /v1/sessions/:id/simulations  (If-Match)
 └─ rateLimit(sim-session, sim-ip) [internal/http/ratelimit.go] → store.RateLimiter.Allow (Lua INCR+PEXPIRE)
      over the limit → 429 + Retry-After; Redis down → 503
 └─ simulationHandler.start [internal/http/simulations.go]
 └─ session.Service.StartSimulation [internal/session/simulations.go]
     mutate (version bump) → RequireState(INTERVIEW) → buildFromSnapshot → BuildInputs (sim.Inputs with prefs)
     → runContext (car labels, catalogue data, eliminated cars, attributes, trade-offs, warnings, assumption provenance)
     → encodePayload {inputs, context} + inputs hash → seed from crypto/rand
     → jobs.Insert(tx, run{status queued}) (second active run → 409) → setState(SIMULATING)
 → 202 {run_id, status_url, events_url}
```

## 15. The worker (`tiebreaker worker`)

```
cmd/tiebreaker/worker.go → jobs.Worker{Queue, Runner: SimRunner, Finalizer: session.Finalizer, Publisher: Redis}.Run(ctx)
 loop:
   Queue.Claim [internal/jobs/queue.go]
     WithTx: SELECT … WHERE queued OR (running AND lease expired) ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED
             attempts ≥ max → failed;  else UPDATE running, worker_id, lease = now()+lease, attempts+1
   none → sleep(poll interval ± 20 %)
   process(run) [internal/jobs/worker.go]
     jobCtx = WithCancelCause(WithoutCancel(workerCtx))
     goroutine: shutdown watcher → after ShutdownGrace: cancel(errShutdown)
     goroutine: heartbeat ticker → ExtendLease (UPDATE … RETURNING cancel_requested)
                   lease lost → cancel(ErrLeaseLost); cancel requested → cancel(errCancelRequested)
     SimRunner.Run [internal/jobs/runner.go]
        decode inputs_json → sim.Run(seed, futures, chunk; progress → throttle → SetProgress + PUBLISH run:{id})
        → decision.Summarize → ResultRows (+ shared histograms) → decision.Fingerprint
     stop goroutines (WaitGroup) → switch on result / context.Cause:
        ok            → WithTx: Complete (fenced) + INSERT sim_results + Finalizer.Done (session → DONE)
        cancel        → WithTx: MarkCancelled (fenced) + Finalizer.Stopped (session → INTERVIEW)
        shutdown      → Queue.Release (back to queued, attempt not counted)
        lease lost    → drop the result (another worker owns the run)
        runner error  → MarkFailed + Finalizer.Stopped
     → PUBLISH the final event
```

## 16. Progress (SSE), status and cancel

```
GET /v1/simulations/:id/events
 └─ simulationHandler.events
     GetRun (404 if unknown) → jobs.Subscribe (Redis SUBSCRIBE run:{id}, wait for confirmation)
     → ResponseController.SetWriteDeadline(none) → headers text/event-stream
     → send current state from Postgres → loop: Redis event | 5 s Postgres poll | 15 s ": ping"
     → close after a terminal status (or 10 min)

GET  /v1/simulations/:id        → Service.GetRun (+ sim_results when done; seed as a decimal string)
POST /v1/simulations/:id/cancel → Service.CancelRun: WithTx + LockRun (FOR UPDATE)
      queued → cancelled + FinishRun(session → INTERVIEW) | running → cancel_requested = true | finished → 409
```

## 17. From finished simulation to report (inside the worker)

```
SimRunner.Run [internal/jobs/runner.go]  (after sim.Run + Summarize, progress 0–80 %)
 └─ Analyze = report.Analyzer.Analyze [internal/report/analyze.go]   (outside any transaction)
     ├─ decode RunContext from payload.context
     ├─ decision.Classify(P(best), thresholds) [internal/decision/classify.go]   → winner | deadlock (+ reason)
     ├─ decision.BuildSweeps(inputs, fuel-trend range, ladder cap) [tipping.go]
     ├─ decision.FindTippingPoints (errgroup over sweeps; progress 80–99 %)
     │     per sweep: 9 grid points (quick sims on the run's seed) → leader changes → bisection → crossover
     ├─ report.BuildFacts [facts.go]
     │     per car: P(best), P(cheapest), percentiles, breakdown, safety ("not rated" if none),
     │     pros/cons in ₹ (preferences × bracket midpoint; cost-category medians), not-asked list,
     │     tipping sentences involving the car, warnings
     └─ draft: RenderTemplate(facts) [template.go + templates/*.md.tmpl]  (M6: LLM writer + validator first)
→ Outcome.Extra = RunOutput{…, Analysis: report.Draft}
worker.complete → WithTx: Complete (fenced) + INSERT sim_results
                          + session.Finalizer.Done → report.OnRunDone (INSERT reports, new share_id) [store.go]
                          + FinishRun(session → DONE)
```

## 18. none_fit (no simulation)

```
Service.SetConstraints → every car eliminated → setState(DONE)
 └─ NoneFitHook = report.OnNoneFit (same transaction)
     BuildNoneFit(eliminated cars with their shortfall messages) → RenderTemplate(none_fit) → INSERT reports (run_id NULL)
```

## 19. Reading reports, share links, reproduction

```
GET /v1/sessions/:id/report → reportHandler.forSession [internal/http/reports.go] → report.Service.ForSession
      latest report | none + SIMULATING → 409 | none → 404;  View = facts + prose + results (histograms), no session ID
GET /r/:shareId             → report.Service.ByShareID (public, read-only, Cache-Control: public)

tiebreaker verify-run -run-id X [cmd/tiebreaker/verifyrun.go]
 └─ jobs.VerifyRun [internal/jobs/verify.go]: decode inputs_json → sim.Run(stored seed) → decision.Fingerprint == results_hash?

go run ./scripts/demo [scripts/demo/main.go]: the whole flow over HTTP, including SSE, printing the report
```

## 20. Optional LLM prose (LLM_ENABLED=true)

```
cmd/tiebreaker/worker.go proseWriter()
 └─ LLM disabled → nil (templates only; Gemini never contacted)
 └─ enabled → report.LLMWriter{Client: llm.Cached{Budget{Gemini}}, Names: catalog.AllNames}

report.Analyzer.draft [internal/report/analyze.go]
 ├─ RenderTemplate(facts)                          → always rendered (the fallback)
 └─ LLMWriter.Write(ctx, facts) [internal/report/llm.go]
     ├─ toLLMFacts: labels, probabilities, costs, pros/cons, tipping sentences (no IDs, seed, state, usage)
     ├─ log only sha256(prompt)[:8] + its size
     ├─ Client.GenerateText
     │    llm.Cached [wrappers.go]: Redis GET llm:<sha256(kind|model|request)> → hit? return
     │    llm.Budget: INCR llm:budget:<UTC date> > daily max → ErrQuotaExceeded
     │    llm.Gemini [gemini.go]: context.WithTimeout(20 s) → client.Models.GenerateContent(model, genai.Text(prompt),
     │         {SystemInstruction, MaxOutputTokens, Temperature 0.2, ThinkingLevelMinimal, RetryOptions{Attempts: 2, 429/5xx}})
     │         FinishReasonMaxTokens → ErrOutputTruncated;  genai.APIError → wrapped
     ├─ any error → ok=false (template)
     └─ ValidateProse(text, facts, catalogue names) [validate.go]
          remove report car names → other catalogue names? → every number matches a fact (precision-aware)?
          → verdict matches (deadlock: no winner claims; winner: named)?
          any violation → ok=false (template)
 → Draft{ProseMD, ProseSource: "llm" | "template"}
```

## 21. The frontend (`web/`, Next.js App Router, client components)

```
/                          app/page.tsx                 POST /v1/sessions {user_state} → router.push(/s/:id)
/s/:id                     app/s/[id]/page.tsx          GET session (ETag → version)
  search box               AddCar → GET /v1/variants?q=  (UnverifiedBadge on unverified data)
  add / remove car         mutate(POST|DELETE, If-Match: version) → 409 version conflict → reload + message
  constraints              PUT /constraints (If-Match) → USAGE → /s/:id/interview | DONE (none_fit) → /s/:id/results
/s/:id/interview           app/s/[id]/interview/page.tsx
  loop: GET /next-question → QuestionCard (range | number | financing | choice | yes_no)
        → submitIdempotent(POST /answers, one Idempotency-Key, If-Match) [lib/api.ts] → ETag → next
  done → POST /simulations (If-Match) → /s/:id/results?run=<id>
/s/:id/results             app/s/[id]/results/page.tsx
  EventSource(/v1/simulations/:run/events) → progress bar → "done" → GET /sessions/:id/report
  (reload without ?run → poll the report every 2 s while it returns 409)
  → <Report> [components/Report.tsx]: P(best) bars, SVG histogram (results[].histogram), summary
    (Markdown → React elements), cost table, tipping points, eliminations, seed, share link
/r/:shareId                app/r/[shareId]/page.tsx     GET /r/:shareId → <Report>
```

## 22. Deployment entry points

```
container start, no args → main.run: TIEBREAKER_MODE=serve|worker → the subcommand   [cmd/tiebreaker/main.go]
serve: MIGRATE_ON_START=true → store.MigrateUp (advisory lock) → listen on HTTP_ADDR or :$PORT
CORS: cors(CORS_ALLOWED_ORIGINS) runs first in the middleware chain                   [internal/http/cors.go]
```

## 23. Tests and where they plug in

| Flow | How it is tested without real infrastructure |
|---|---|
| config | `config.Load(mapGetenv)`, so the real environment is never read |
| router and middleware | `httpapi.NewRouter(Deps{fakePinger…})` + `httptest.NewRecorder` |
| server lifecycle | `runHTTPServer(ctx, "127.0.0.1:0", …, onListen)` gets a free port, then the test cancels ctx |
| store (integration) | `testutil.Postgres/Redis` start real containers once per package in `TestMain` |
| catalogue (integration) | `TestMain` starts Postgres, runs `store.MigrateUp`; each test `TRUNCATE`s the catalogue tables |
| catalogue handlers | `fakeCatalog` implements `httpapi.CatalogService` |
| engine | plain `sim.Run` calls on fixtures (`handInputs`, `stochasticInputs`); goleak in `TestMain` |
| interview | `Planner` with real quick sims on fictional inputs; `countingCache`; an expired deadline for the fallback |
| sessions over HTTP (integration) | `internal/http/integration_test.go`: real router + services + Postgres + Redis + seed data |
| job queue and worker (integration) | `internal/jobs/jobs_integration_test.go`: fake runners (counting, blocking), recording finalizer, goleak |
| simulations over HTTP (integration) | `internal/http/simulations_integration_test.go`: a real worker in a goroutine, SSE over `httptest.NewServer` |
| reports | `facts_test.go` with a hand-built fixture; golden files in `internal/report/testdata` (`-update` to rewrite) |
| reports over HTTP (integration) | `internal/http/reports_integration_test.go`: worker with the real Analyzer; share links; `VerifyRun` |
| Gemini adapter | `internal/llm/gemini_test.go`: the real adapter against an `httptest` server (`BaseURL`) |
| LLM cache and budget (integration) | `internal/llm/wrappers_integration_test.go`: real Redis, `llm.Fake` inside |
| fact validator and LLM writer | `validate_test.go` (table), `llm_test.go` with `llm.Fake` |
| frontend API client | `web/lib/api.test.ts` (vitest, fake `fetch`) |
