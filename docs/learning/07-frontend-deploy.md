# 07 — A thin frontend and deployment

Milestone **M7**. The code is in [web/](../../web/), [internal/http/cors.go](../../internal/http/cors.go), [.railway/railway.ts](../../.railway/railway.ts) and [docs/deploy.md](../deploy.md).

---

## 1. Next.js App Router: files are routes

```
web/app/page.tsx                    → /                    (start)
web/app/s/[id]/page.tsx             → /s/:id               (screen 1: shortlist)
web/app/s/[id]/interview/page.tsx   → /s/:id/interview     (screen 2)
web/app/s/[id]/results/page.tsx     → /s/:id/results       (screen 3)
web/app/r/[shareId]/page.tsx        → /r/:shareId          (public share page)
```

Every page starts with `"use client"`: it runs in the browser, keeps state with `useState`, and loads data with `useEffect`. That suits a thin UI over a JSON API. `useParams()` reads `[id]` from the URL. `useSearchParams()` must sit inside a `<Suspense>` boundary, which is why the results page wraps itself.

## 2. Speaking the API's protocol from the browser

[web/lib/api.ts](../../web/lib/api.ts) wraps `fetch` once:

- **Errors:** a non-2xx response is parsed into `ApiError{status, code, message, details}`, matching the Go error shape.
- **Optimistic locking:** each response's `ETag` is the session version. The next change sends it back as `If-Match`. On 409 `SESSION_VERSION_CONFLICT` the page reloads the session and tells the user ("changed in another tab").
- **Idempotency:** `submitIdempotent` makes **one** key per answer (`crypto.randomUUID()`) and reuses it for a retry after a network error, so a flaky connection can't record an answer twice. The vitest test proves the key is reused.

## 3. CORS: letting another origin call the API

The UI runs on Vercel (`https://….vercel.app`) and the API on Railway, two different **origins**, so the browser enforces CORS:

- For every response, the API echoes `Access-Control-Allow-Origin` **only** for origins in `CORS_ALLOWED_ORIGINS`.
- `Access-Control-Expose-Headers: ETag, …` lets JavaScript *read* the ETag (hidden by default).
- Before a request with custom headers (`If-Match`, `Idempotency-Key`) the browser sends a **preflight** `OPTIONS`. The middleware answers 204 with the allowed methods and headers.

## 4. Server-sent events in the browser

```ts
const es = new EventSource(`${API_BASE}/v1/simulations/${runId}/events`);
es.addEventListener("progress", (m) => setProgress(JSON.parse(m.data)));
```

`EventSource` reconnects by itself after a dropped connection, and the server re-sends the current state from Postgres on every connect (M4). So the UI needs no reconnect logic, only `es.close()` when the run is done.

## 5. Rendering untrusted text safely

Report prose may come from an LLM. [web/lib/markdown.tsx](../../web/lib/markdown.tsx) turns a tiny Markdown subset into **React elements**: never `dangerouslySetInnerHTML`, so prose can't inject HTML or scripts. The histogram is plain inline SVG built from the stored bins, with no chart library to keep up to date.

## 6. One image, two services, no shell

The runtime image is `distroless/static`: just the binary, with no `/bin/sh`. Platforms that wrap start commands in a shell would fail, so the binary itself picks its mode:

```go
if len(args) == 0 { if mode := getenv("TIEBREAKER_MODE"); mode != "" { args = []string{mode} } }
```

Railway sets `TIEBREAKER_MODE=serve` on the api service and `worker` on the worker service. The same idea covers migrations (`MIGRATE_ON_START=true`, safe thanks to golang-migrate's advisory lock) and the port (`$PORT` used when `HTTP_ADDR` is unset). That is twelve-factor configuration: behaviour comes from the environment, not from rebuilt images.

## 7. Pinning and reproducible installs

Every dependency (Go and npm) is pinned to an exact version at least two weeks old. `package-lock.json` makes `npm ci` reproducible in CI. One npm 10 bug (a crash while auto-installing vitest's optional peers) is worked around with `legacy-peer-deps=true` in `web/.npmrc`. That's safe because the one required peer, `vite`, is declared explicitly.

---

## Questions you should be able to answer

1. **The Vercel app sends `POST /answers` with `If-Match` and `Idempotency-Key` to the Railway API. Describe both HTTP exchanges the browser makes and which response headers make each succeed.**
   *Hint:* preflight `OPTIONS` → 204 with Allow-Origin/Methods/Headers; then the POST, whose response carries Allow-Origin and Expose-Headers so JS can read the ETag.

2. **A user double-clicks "Yes" on a flaky train connection. Walk through why exactly one answer is stored, covering both the frontend and the backend.**
   *Hint:* the button is disabled while busy; one idempotency key per answer, reused on retry; the server's unique key row plus the replayed response; If-Match as a second guard.

3. **Why does the image use `TIEBREAKER_MODE` instead of a Railway start command like `/tiebreaker worker`?**
   *Hint:* distroless has no shell; a start command may be run through `/bin/sh -c`; environment-driven configuration works with any runner.
