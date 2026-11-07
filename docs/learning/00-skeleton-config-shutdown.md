# 00 — Skeleton: config, errors, HTTP server, graceful shutdown

Milestone **M0**. This note explains the Go concepts used in the skeleton and why each one was chosen. Read it alongside the code it points to.

---

## 1. Modules and the `internal/` directory

- `go.mod` declares the module path `github.com/xoBerlierxo/TieBreaker`. Every import inside the project starts with it, e.g. `github.com/xoBerlierxo/TieBreaker/internal/config`.
- Anything under `internal/` can only be imported by code inside this module; the compiler enforces it. That is how we say "these packages are private implementation, not a library".
- A package's name doesn't have to match its directory. `internal/http` is `package httpapi`, so it doesn't clash with `net/http` (DECISIONS D-M0-5).

## 2. A thin `main`, a testable `run`

[cmd/tiebreaker/main.go](../../cmd/tiebreaker/main.go)

```go
func main() {
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    err := run(ctx, os.Args[1:], os.Getenv, logger)
    ...
}
```

`main` only gathers the real world (signals, args, environment, stdout) and hands it to `run`. Tests call `run` with fake args and a fake `getenv`. Only `main` may call `os.Exit`. `os.Exit` skips deferred functions, so calling it deep inside the code would skip cleanup such as closing the database.

## 3. `context.Context`: cancellation flows downward

- `signal.NotifyContext` returns a context that is **cancelled** when SIGINT or SIGTERM arrives. Docker and Railway send SIGTERM on stop.
- That one context is passed down to `serve`/`worker` and into every I/O call (`OpenPostgres(ctx, …)`, `Ping(ctx)`). When it is cancelled, everything listening to it stops. SPEC §4: every function that does I/O takes `ctx` as its first parameter.
- `context.WithTimeout(ctx, 2*time.Second)` makes a child context with a deadline. `/readyz` uses it so a hanging database can't hang the request.
- `context.WithoutCancel(ctx)` (Go 1.21+) keeps the values but drops the cancellation. We need it at shutdown: the root context is *already* cancelled, but `Shutdown` still needs a context that lives for the grace period.

## 4. Goroutines and channels in `runHTTPServer`

[cmd/tiebreaker/serve.go](../../cmd/tiebreaker/serve.go)

```go
serveErr := make(chan error, 1)        // buffered: the goroutine never blocks on send
go func() { serveErr <- srv.Serve(ln) }()

select {
case err := <-serveErr:                // Serve failed on its own
    return err
case <-ctx.Done():                     // shutdown was requested
}
srv.Shutdown(shutdownCtx)              // stop accepting; wait for in-flight requests
<-serveErr                             // wait until the goroutine has really exited
```

- `srv.Serve` blocks forever, so it runs in its own goroutine.
- `select` waits on several channels at once and continues with whichever is ready first.
- The **buffered** channel (capacity 1) matters. If we returned early, the goroutine could still send its result without blocking forever, which would be a goroutine leak.
- The final `<-serveErr` guarantees the goroutine has finished before the function returns. No goroutine outlives its owner.

## 5. `http.Server` timeouts and graceful shutdown

- `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout` and `IdleTimeout` stop slow or malicious clients holding connections open (the "Slowloris" attack). The zero value means *no timeout*, which is unsafe on the internet.
- `Shutdown` stops accepting new connections and waits for active requests to finish, up to the context deadline. `TestServe_InFlightRequestFinishesDuringShutdown` proves that a slow request started before shutdown still completes.

## 6. Configuration without globals

[internal/config/config.go](../../internal/config/config.go)

- `Load(getenv func(string) string)` takes a function, not the environment, so tests pass `func(k string) string { return m[k] }`. This is *dependency injection* in its simplest form.
- The small `parser` type collects every error, and `errors.Join` returns them together, so a misconfigured deploy shows all its problems at once.
- `Config` implements `slog.LogValuer`. Whenever a `Config` is logged, slog calls `LogValue()`, which leaves out secrets and redacts URL passwords with `url.URL.Redacted()`. It is impossible to log the API key by accident.

## 7. Errors: sentinels, wrapping, `errors.Is` and `errors.As`

[internal/apperr/errors.go](../../internal/apperr/errors.go), [internal/http/errors.go](../../internal/http/errors.go)

```go
var ErrNotFound = errors.New("not found")                     // a sentinel
return fmt.Errorf("loading session: %w", apperr.ErrNotFound)  // %w wraps it
errors.Is(err, apperr.ErrNotFound)                            // true through any wrapping
var appErr *apperr.Error; errors.As(err, &appErr)             // pull out the typed error
```

- `%w` keeps the original error inside the new one, and `errors.Is` walks that chain. Never compare errors with `==`, because wrapping breaks it.
- `*apperr.Error` has an `Unwrap()` method returning its `Kind`, which is how `errors.Is(err, ErrNotFound)` also works on it.
- `statusFor` is the **one place** where errors become HTTP status codes (SPEC §18). Handlers never pick status codes themselves.

## 8. Small interfaces make code testable

```go
type Pinger interface { Ping(ctx context.Context) error }
```

The health handler depends on this one-method interface, not on GORM or go-redis. Production passes `store.PostgresPinger`; tests pass `fakePinger{err: errors.New("db down")}`. Go interfaces are satisfied *implicitly*: a type implements an interface just by having the methods, so the fake needs no `implements` declaration. The Go convention is to define the interface where it is *used* (in `httpapi`), not where it is implemented.

## 9. Gin middleware

[internal/http/middleware.go](../../internal/http/middleware.go)

A middleware is a `gin.HandlerFunc` that does something, calls `c.Next()` to run the rest of the chain, and can do more afterwards:

```go
func accessLog(logger *slog.Logger) gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()
        c.Next()                       // run the handler
        logger.Info(..., time.Since(start))
    }
}
```

- `accessLog` is a function returning a function. The inner function is a *closure* that captures `logger`.
- `c.Abort…` stops the chain, which `bodyLimit` uses to reject a request before the handler runs.
- `recovery` uses `defer` + `recover()`. A deferred function runs even while a panic is unwinding the stack, and `recover()` stops the panic. One buggy handler then returns a 500 instead of crashing the whole server.

## 10. Liveness vs. readiness

- `/healthz` (liveness): "is the process alive?". It checks nothing external. If it did, a database blip would make the orchestrator restart healthy containers.
- `/readyz` (readiness): "can I serve traffic right now?". It pings Postgres and Redis and returns 503 with details if either is down.

## 11. Docker multi-stage builds

[Dockerfile](../../Dockerfile)

- **Stage 1** (`golang:1.26-alpine`) compiles with `CGO_ENABLED=0`, producing a *static* binary that needs no C library.
- **Stage 2** (`distroless/static:nonroot`) contains only that binary: no shell, no package manager, and it runs as a non-root user. The image is small and has very little to attack.
- `COPY go.mod go.sum` + `go mod download` before `COPY . .` means editing code doesn't re-download dependencies, because Docker caches each layer.

## 12. Tests: table-driven, build tags, `TestMain`

- **Table-driven tests** (`TestLoad_InvalidValues`, `TestMapError_DomainErrorsToStatus`) list cases in a slice of structs and loop with `t.Run(name, …)`. Adding a case is one line, and each case shows up by name in the output.
- **`//go:build integration`** at the top of a file means it is compiled only with `go test -tags=integration`. Plain `go test ./...` stays fast and needs no Docker.
- **`TestMain(m *testing.M)`** runs once per package, around all its tests. We start the Postgres and Redis containers there. `TestMain` hands off to `runWithContainers` because `os.Exit` would skip the deferred container shutdown.
- **`go test -race`** compiles the race detector in, so it reports any memory accessed by two goroutines without synchronisation. It needs cgo, which is why a C compiler is installed on Windows.

---

## Questions you should be able to answer

1. **Why does `runHTTPServer` use a *buffered* channel for `serveErr`, and why does it read from that channel again after `Shutdown`?**
   *Hint:* think about which goroutine could block forever if nobody receives, and about the guarantee "no goroutine outlives its owner".

2. **`/readyz` returns 503 when Redis is down, but `/healthz` still returns 200. Why is that the correct design, and what would go wrong if liveness also checked the database?**
   *Hint:* what an orchestrator does when liveness fails, and what happens to the database when every container restarts at once.

3. **A service returns `fmt.Errorf("saving answer: %w", apperr.ErrVersionConflict)`. Walk through exactly how this becomes `409 {"error":{"code":"SESSION_VERSION_CONFLICT",…}}`, and explain why comparing with `err == apperr.ErrVersionConflict` would fail.**
   *Hint:* `%w`, `errors.Is` walking the `Unwrap` chain, `statusFor`, `publicMessage`.
