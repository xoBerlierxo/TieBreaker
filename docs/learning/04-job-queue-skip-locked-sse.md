# 04 — Background jobs: SKIP LOCKED, leases, fencing, SSE

Milestone **M4**. The code is in [internal/jobs](../../internal/jobs/), [internal/session/simulations.go](../../internal/session/simulations.go) and [internal/http/simulations.go](../../internal/http/simulations.go).

---

## 1. Why a background job at all?

A full simulation and its analysis must not run inside an HTTP request: requests time out, users close tabs, and an API server shouldn't be pinned at 100 % CPU. So the API **queues** the work (`202 Accepted` + a run ID) and a separate `worker` process does it.

## 2. A queue in Postgres with `SELECT … FOR UPDATE SKIP LOCKED`

```sql
SELECT * FROM sim_runs
WHERE status = 'queued' OR (status = 'running' AND lease_expires_at < now())
ORDER BY created_at LIMIT 1
FOR UPDATE SKIP LOCKED;
```

- `FOR UPDATE` locks the row until the transaction ends, so nobody else can claim it.
- `SKIP LOCKED` means "if another transaction already locked a row, pretend it isn't there". Without it, every worker would queue up behind the first; with it, each worker instantly gets a *different* row.
- In GORM: `tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})`.

## 3. Leases: handling crashed workers

When a worker claims a job it gets a **lease** (`lease_expires_at = now() + 60s`). A heartbeat goroutine renews it every second. If the worker crashes, the heartbeat stops, the lease runs out, and the `OR (status='running' AND lease_expires_at < now())` clause makes the job claimable again.

We use `now()` from **the database**, never `time.Now()` from the worker, so two machines with different clocks still agree on whose lease has expired.

## 4. Fencing: the slow worker problem

Worker A claims a job, then freezes for 70 seconds (a GC pause, a network blip). Its lease expires; worker B takes over and finishes. Then A wakes up and tries to write *its* result. **Fencing** prevents the damage: every write includes the owner check.

```sql
UPDATE sim_runs SET status = 'done', …
WHERE id = $1 AND worker_id = $2 AND status = 'running';   -- 0 rows → "lease lost"
```

A's update matches nothing, so it gets `ErrLeaseLost` and discards its work. Combined with doing the result insert in the *same* transaction, this gives an **exactly-once stored result** even though processing is at-least-once.

## 5. `context.WithCancelCause` and `context.Cause`

One job can be stopped for different reasons (a user cancel, a worker shutdown, a lost lease), and each needs different handling:

```go
jobCtx, cancel := context.WithCancelCause(context.WithoutCancel(parent))
cancel(errCancelRequested)          // from the heartbeat
…
switch cause := context.Cause(jobCtx); {
case errors.Is(cause, errCancelRequested): // mark cancelled
case errors.Is(cause, errShutdown):        // release back to the queue
case errors.Is(cause, ErrLeaseLost):       // drop silently
}
```

`context.WithoutCancel(parent)` lets the job outlive the worker's own context. On shutdown we give it a grace period instead of killing it immediately.

## 6. Goroutine lifecycle without leaks

`process` starts two helper goroutines (the shutdown watcher and the heartbeat). When the job ends:

```go
close(stopBackground)   // tell both goroutines to return
wg.Wait()               // and wait until they have
```

Closing a channel is a broadcast: every goroutine waiting on `<-stopBackground` wakes up. `goleak.VerifyNone(t, goleak.IgnoreCurrent())` in the worker tests proves nothing is left behind.

## 7. Redis pub/sub and its catch

`PUBLISH run:{id} {...}` sends to whoever is subscribed *right now*. Nobody listening means the message is lost. That's why progress is **also** stored in Postgres, and why the SSE handler:

1. **subscribes first** (and waits for the confirmation),
2. **then** reads the current state from Postgres,
3. then streams, and re-reads Postgres every 5 s just in case.

Reading first and subscribing second leaves a gap where an update can slip through unseen.

## 8. Server-sent events (SSE)

SSE is plain HTTP that never ends: the server keeps writing `event:`/`data:` blocks separated by blank lines, and the browser's `EventSource` parses them.

```
event: progress
data: {"run_id":"…","status":"running","progress":42}

: ping
```

- `c.Writer.Flush()` pushes each event out immediately instead of buffering it.
- The server's `WriteTimeout` (30 s) would kill the stream, so this one handler lifts it: `http.NewResponseController(w).SetWriteDeadline(time.Time{})`. That works because Gin's writer implements `Unwrap()`.
- `: ping` comment lines keep proxies from closing an idle connection.

## 9. Rate limiting with an atomic Lua script

```lua
local n = redis.call('INCR', KEYS[1])
if n == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end
return {n, redis.call('PTTL', KEYS[1])}
```

Redis runs a script without interruption, so "increment, and set the expiry the first time" can't race. Over the limit → 429 + `Retry-After`. If Redis is down we answer 503 (fail closed), because these routes start expensive work.

## 10. Keeping packages one-directional

`session` imports `jobs` (to insert runs). The worker in `jobs` must change the session state when a run ends, but `jobs` importing `session` would create a cycle. The fix is an interface owned by `jobs`:

```go
type Finalizer interface {
    Done(ctx, tx, run, out) error
    Stopped(ctx, tx, run, status) error
}
```

`session.Finalizer` implements it, and `cmd/tiebreaker/worker.go` connects the two. This is dependency inversion.

---

## Questions you should be able to answer

1. **Two workers poll the queue at the same instant and there is one queued job. Explain exactly what each one's `SELECT … FOR UPDATE SKIP LOCKED` returns, and what would happen without `SKIP LOCKED`.**
   *Hint:* the first locks the row; the second skips it and gets nothing (or the next row); without SKIP LOCKED the second blocks until the first commits.

2. **Worker A freezes after claiming a job, its lease expires, and worker B completes the job. A then wakes up and calls `Complete`. Walk through why A's result is never stored, naming the SQL condition responsible.**
   *Hint:* `WHERE worker_id = 'A' AND status = 'running'` matches 0 rows → `ErrLeaseLost` → the transaction rolls back.

3. **Why does the SSE handler subscribe to Redis *before* reading the run's state from Postgres? Describe the bug the opposite order would cause.**
   *Hint:* an update published between the read and the subscribe is lost forever, so the client would show stale progress until the 5 s poll.
