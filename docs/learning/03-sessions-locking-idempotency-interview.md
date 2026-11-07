# 03 — Sessions: optimistic locking, idempotency, the adaptive interview

Milestone **M3**. The code is in [internal/session](../../internal/session/), [internal/interview](../../internal/interview/) and [internal/http/sessions.go](../../internal/http/sessions.go).

---

## 1. Optimistic locking with a version number

Two browser tabs open the same session. Both read version 3. Tab A adds a car; tab B, unaware, adds a different car. Without protection, B's change is based on stale data.

**Optimistic locking:** every write says "I last saw version 3", and the database refuses the write unless that is still true:

```sql
UPDATE sessions SET version = version + 1
WHERE id = $1 AND version = $2        -- $2 = the If-Match header
RETURNING …;
```

- 1 row updated → you win, and the version is now 4.
- 0 rows → somebody changed it first → **409 Conflict**. The client reloads and retries.

It's "optimistic" because we don't lock while the user is thinking. We only check at the moment of writing.

**Concurrency in Postgres (READ COMMITTED):** if A and B run that UPDATE at the same instant, the row lock makes B *wait* until A commits. Postgres then re-evaluates B's `WHERE version = 3` against the new row (version 4), finds no match, and B gets 0 rows. That's why `TestConcurrentAnswers_ExactlyOneSucceeds` always sees exactly one 201 and one 409.

**HTTP side:** the server sends `ETag: "3"`; the client sends `If-Match: "3"`. A missing `If-Match` gets **412 Precondition Required**.

## 2. Idempotency keys: safe retries

A phone on a train sends "answer = yes", the response is lost, and the app retries. Without care, the answer is stored twice.

The client sends `Idempotency-Key: <random UUID>` and reuses it on retry. The server:

```go
tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&IdempotencyKey{SessionID: id, Key: key, RequestHash: hash})
// RowsAffected == 0  →  this key was seen before
```

- Same key, same body → return the **stored** response (status + exact bytes).
- Same key, different body → **422**: the client is confused, so refuse loudly.
- **The key is checked before the version.** The retry carries the *old* If-Match; it must get the original 201, not a 409.
- **Same transaction as the answer.** If two identical requests race, the second INSERT waits on the unique index until the first commits, then replays its response. If the work fails, everything rolls back, the key included, so the client can retry.

A gotcha we hit: storing the response in a `jsonb` column changed the bytes (`jsonb` re-orders keys), so replays didn't match. The fix was to store it as `text`.

## 3. Transactions with GORM

```go
store.WithTx(ctx, db, func(tx *gorm.DB) error {
    sess, err := bumpVersion(tx, id, version)   // uses tx, not db
    if err != nil { return err }                // → ROLLBACK
    return applyAnswer(ctx, tx, sess, req)      // nil → COMMIT
})
```

Everything inside must use `tx`. If you accidentally use the outer `db`, that query runs on another connection, outside the transaction, and can deadlock waiting for the row lock your own transaction holds.

## 4. State machines in code

[internal/session/state.go](../../internal/session/state.go) has a single `allowed(from, to)` switch. Every state change goes through `CheckTransition`; anything else is 409 `ILLEGAL_STATE_TRANSITION`. One small, exhaustively tested function (`TestTransition_EveryOtherPairIsIllegal` checks all 25 pairs) is easier to trust than state checks scattered through handlers.

## 5. Answers as an event log

No "interview progress" column exists. Each answer is a row (`usage:km_per_month`, `tradeoff:boot:2`, …), and the current state is rebuilt by **replaying** them (`TradeoffState.Add`, `Ladder.Replay`). This is a tiny version of event sourcing: one source of truth, nothing to keep in sync, and a full history for free.

## 6. The bracket ladder as binary search

[internal/interview/ladder.go](../../internal/interview/ladder.go): rungs `[0, 25k, 50k, 1L, 2L, 4L]` plus a cap of 8L give 6 brackets. The state is two indices `(lo, hi)`. Ask the middle rung, `m = (lo+hi)/2`; "yes" sets `lo = m`, "no" sets `hi = m`; stop when `hi − lo = 1`. Six brackets need at most ⌈log₂ 6⌉ = 3 questions, and the first question is ₹1L. `TestLadder_ConvergesInAtMost3Steps_AllPaths` walks every yes/no path recursively.

## 7. Information gain: picking the most useful question

- **Entropy** (in bits) of the winner distribution measures uncertainty: 0 = we know the winner; 1 = coin flip between two cars.
- For each candidate question: entropy *before* (value unknown, an equal mix of all brackets) minus the *average* entropy *after* (one sim per possible bracket). The question that removes the most uncertainty wins.
- **Common random numbers** make this cheap and stable: every quick sim uses the session's seed, so differences come from the answer, not from noise.
- **Parallelism:** one goroutine per candidate with `errgroup.SetLimit`. A subtle trap: `append(shared, x)` from several goroutines can write into the same backing array, a data race. `slices.Clone` first.

## 8. Timeouts as a product decision

```go
qctx, cancel := context.WithTimeout(ctx, 2*time.Second)
defer cancel()
next, err := planner.NextTradeoff(qctx, …)   // if the deadline hits, ask the next question in fixed priority order
```

The interview must never hang. `errors.Is(err, context.DeadlineExceeded)` detects the timeout, and the planner falls back to a sensible question instead of returning an error.

## 9. Integration tests that prove concurrency

`internal/http/integration_test.go` releases two goroutines from a closed channel so they hit the server at the same moment:

```go
start := make(chan struct{})
go func() { <-start; codes[0] = answer(…) }()
go func() { <-start; codes[1] = answer(…) }()
close(start)   // both unblock together
```

It then checks the database row counts, not just the HTTP codes.

---

## Questions you should be able to answer

1. **Two requests with the same `If-Match: "3"` arrive at the same millisecond. Walk through, at the Postgres level, why exactly one succeeds and what the other receives.**
   *Hint:* the row lock from `UPDATE`, waiting, re-evaluating `WHERE version = 3` under READ COMMITTED, 0 rows → 409.

2. **Why is the idempotency key checked *before* the version, and why in the *same* transaction as the answer insert? What breaks if you change either?**
   *Hint:* a retry carries a stale If-Match; a concurrent duplicate must wait on the unique index; a failure must release the key.

3. **Explain information gain for one candidate question in terms of the quick simulations it runs. Why do all of them use the same seed?**
   *Hint:* 1 mixture sim + 6 bracket sims; H(mixture) − mean H(bracket); common random numbers remove noise from the comparison.
