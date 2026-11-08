# Deploying Tiebreaker

**Target:** the API and the worker on **Railway** (one Docker image, two services, plus managed Postgres and Redis), and the frontend on **Vercel** (SPEC §3).

> Claude Code prepared these files and instructions but cannot run the deploy: it needs your Railway and Vercel accounts. Secrets are set in the dashboards only, never committed.

---

## 1. Railway: API, worker, Postgres, Redis

### Option A: dashboard (always works)

1. **New project** → *Deploy from GitHub repo* → `xoBerlierxo/TieBreaker`. Railway detects the root `Dockerfile`. Rename this service **api**.
2. **+ New → Database → PostgreSQL**, and **+ New → Database → Redis**.
3. In the **api** service → *Variables*:

   | Variable | Value |
   |---|---|
   | `TIEBREAKER_MODE` | `serve` |
   | `MIGRATE_ON_START` | `true` |
   | `APP_ENV` | `production` |
   | `DATABASE_URL` | reference → Postgres `DATABASE_URL` |
   | `REDIS_URL` | reference → Redis `REDIS_URL` |
   | `JWT_SECRET` | a long random value (`openssl rand -base64 48`); ≥ 32 characters is required in production |
   | `CORS_ALLOWED_ORIGINS` | your Vercel URL, e.g. `https://tiebreaker.vercel.app` (comma-separate several) |
   | `LLM_ENABLED` | `false` (or `true` with the next two) |
   | `GEMINI_API_KEY` | only if `LLM_ENABLED=true`. **Do not** set `GOOGLE_API_KEY`. |
   | `GEMINI_MODEL` | e.g. `gemini-3.5-flash-lite` (checked 2026-11-07; see `.env.example`) |

   You don't need `HTTP_ADDR`: the API listens on Railway's `$PORT` automatically.

   **Real client IPs:** behind Railway's proxy, every request reaches the API from the proxy's address, so per-IP rate limits would be shared by all users. Set `CLIENT_IP_HEADER` to the header Railway's proxy fills with the client IP (commonly `X-Real-IP`; confirm in Railway's docs or by logging a request's headers). Only set it to a header the proxy always overwrites, otherwise clients could spoof it.
4. *Settings → Networking → Generate domain* for **api**. *Settings → Healthcheck path:* `/readyz`.
5. **+ New → GitHub repo** (the same repo) again, and name it **worker**. Give it the same variables as **api** except: `TIEBREAKER_MODE=worker`, and no `MIGRATE_ON_START` or `CORS_ALLOWED_ORIGINS`. It needs no domain.
6. Deploy. The API applies migrations at start. Check:

   ```bash
   curl https://<api-domain>/readyz     # {"status":"ok","checks":{"postgres":"ok","redis":"ok"}}
   ```

7. **Load the catalogue** (once, and whenever you edit `data/seed`), from your machine:

   ```bash
   DATABASE_URL="<Railway Postgres public URL>" go run ./scripts/seed
   ```

### Option B: Infrastructure as Code

[.railway/railway.ts](../.railway/railway.ts) describes the same setup. It was written against Railway's IaC reference (checked 2026-11-07); Railway's older `railway.toml` format is deprecated as of 2026-12-01. **Preview before applying:**

```bash
npm install railway        # in the repo root, for the CLI's TypeScript types
railway login && railway link
railway config plan        # review every change
railway config apply
```

Then set the `preserve()` variables (`JWT_SECRET`, `CORS_ALLOWED_ORIGINS`, the LLM settings) in the dashboard and run the seed loader as in step 7.

## 2. Vercel: frontend

1. **Add New → Project** → import `xoBerlierxo/TieBreaker`.
2. **Root Directory:** `web`. The framework is detected as Next.js; keep the default build command.
3. **Environment variable:** `NEXT_PUBLIC_API_BASE_URL = https://<api-domain>` (no trailing slash).
4. Deploy, then put the Vercel URL into the API's `CORS_ALLOWED_ORIGINS` and redeploy the API.

## 3. Smoke test the deployment

```bash
bash scripts/demo.sh https://<api-domain>
```

This walks the whole flow (session → shortlist → constraints → interview → simulation over SSE → report → share link). Then open the Vercel URL and click through the three screens.

Reproduce a run against production:

```bash
DATABASE_URL="<Railway Postgres public URL>" go run ./cmd/tiebreaker verify-run -run-id <run id from the report>
```

## 4. Operational notes

- **Scaling:** run more **worker** replicas freely. Jobs are claimed with `FOR UPDATE SKIP LOCKED`, and leases recover jobs from crashed workers. The API is stateless and can also run several replicas; migrations take an advisory lock.
- **Redeploys:** on SIGTERM the API drains requests (`SHUTDOWN_GRACE_SECONDS`), and a worker finishes or releases its current job.
- **Gemini quota:** `LLM_DAILY_MAX_CALLS` caps calls per UTC day. Over the cap, reports use the template.
