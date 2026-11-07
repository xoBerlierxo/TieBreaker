# FLOW.md — how execution moves through the code

This file follows the path a program run or a request takes through the files and functions. Each milestone adds its flows. Function names are written `package.Function`, with the file in brackets.

Status: **M0 (skeleton)**. Startup, health checks and shutdown are covered.

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

## 6. Tests and where they plug in

| Flow | How it is tested without real infrastructure |
|---|---|
| config | `config.Load(mapGetenv)`, so the real environment is never read |
| router and middleware | `httpapi.NewRouter(Deps{fakePinger…})` + `httptest.NewRecorder` |
| server lifecycle | `runHTTPServer(ctx, "127.0.0.1:0", …, onListen)` gets a free port, then the test cancels ctx |
| store (integration) | `testutil.Postgres/Redis` start real containers once per package in `TestMain` |
