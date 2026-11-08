# Setup guide

Everything you need to do yourself to run Tiebreaker, fill it with real car data, switch on Gemini, and put it online. Work through the parts in order; each one ends with a check that tells you it worked.

| Part | What you get | Time |
|---|---|---|
| [1. Install the tools](#1-install-the-tools) | a machine that can build and run everything | 20 min, once |
| [2. Run it locally](#2-run-it-locally) | the full app on your laptop with sample data | 5 min |
| [3. Add real car data](#3-add-real-car-data) | real, sourced cars and assumptions instead of placeholders | an evening per few cars |
| [4. Switch on Gemini](#4-switch-on-gemini-optional) | AI-written report summaries, checked against the numbers | 10 min |
| [5. Put it online](#5-put-it-online) | a public URL for your resume and video | 30 min |
| [6. Troubleshooting](#6-troubleshooting) | fixes for the problems you're most likely to hit | — |

---

## 1. Install the tools

| Tool | Version | Check |
|---|---|---|
| Git | any recent | `git --version` |
| Docker Desktop | any recent, **running** | `docker info` shows a server version |
| Go | 1.26 or newer | `go version` |
| Node.js | 22 (npm 10) | `node --version` |
| *Optional:* golangci-lint | v2.15.0 | `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0` |
| *Optional:* a C compiler | MSYS2 gcc on Windows | only needed for `go test -race` |

Then get the code and create your local settings file:

```powershell
git clone https://github.com/xoBerlierxo/TieBreaker.git
cd TieBreaker
copy .env.example .env      # Git Bash / macOS / Linux: cp .env.example .env
```

`.env` is ignored by git. It's the only place your secrets go on your machine.

## 2. Run it locally

```powershell
docker compose up -d --build
```

This starts Postgres and Redis, runs the database migrations once, then starts the API on port 8080 and the worker.

Load the catalogue and the assumptions (once, and again after every change in `data/seed/`):

```powershell
$env:DATABASE_URL="postgres://tiebreaker:tiebreaker_dev_only@localhost:5432/tiebreaker?sslmode=disable"
go run ./scripts/seed
```

Start the frontend:

```powershell
cd web
npm ci          # first time only
npm run dev     # http://localhost:3000
```

**Check:**
- `curl localhost:8080/readyz` returns `"status":"ok"`.
- `bash scripts/demo.sh` runs the whole flow in the terminal and prints a report.
- http://localhost:3000 lets you click through shortlist → interview → results.

`docker compose down` stops everything; your data stays in a Docker volume. `docker compose down -v` also wipes the database, so you start empty.

---

## 3. Add real car data

Right now the app holds **3 fictional cars** ("Sample Motors") and **59 placeholder assumptions** (fuel prices, tariffs, depreciation…). That's why the UI shows "unverified data" and "placeholder assumptions" warnings. This part replaces them with real values.

> **The one rule:** every value needs a real source (a URL you can open) and the date you checked it. If you can't find a source for a value, leave the field out (`null`) or keep the car `"verified": false`. The app is designed to show "unverified" or "not rated" rather than a guessed number.

The tests use their own frozen copy of the sample data (`testdata/seed/`), so you can change `data/seed/` freely without breaking CI.

### 3.1 Where to find sources

| Data | Where to look |
|---|---|
| Specs and ex-showroom price | the manufacturer's official model page and its brochure PDF (state-wise prices are often on the price page) |
| Safety rating | the official Bharat NCAP results (bncap.in) or Global NCAP results (globalncap.org) for that exact model and year |
| Fuel prices (petrol, diesel, CNG) | the retail selling prices published by the oil marketing companies (e.g. Indian Oil) and city gas distributors, for each state's capital |
| Electricity tariff | the state electricity regulatory commission's current tariff order (domestic category) |
| Public charging tariff | the published rates of the main charging networks in that state |
| Road tax and registration | the state transport department's tax schedule |
| Insurance, depreciation, service and repair costs | published schedules where they exist (e.g. insurer IDV depreciation tables); otherwise keep these `verified: false` and say so |

Save the exact page URL (or the PDF URL) and today's date for each value.

### 3.2 Add a car

Create one file per model in `data/seed/`, e.g. `data/seed/<make>-<model>.json`. A model file holds one or more variants:

```jsonc
{
  "variants": [
    {
      "slug": "<make>-<model>-<variant>",          // lowercase-with-hyphens, unique
      "make": "<Make>",
      "model": "<Model>",
      "variant_name": "<Variant name as sold>",
      "body_type": "hatchback",                    // hatchback | sedan | compact_suv | suv | muv
      "fuel_type": "petrol",                       // petrol | diesel | cng | ev | strong_hybrid
      "ex_showroom_price_inr": <number>,           // null if unknown
      "launch_year": <year>,
      "discontinued_year": null,                   // a year for discontinued models (they can then only be added as used)
      "claimed_efficiency": <number>,              // ARAI/claimed figure
      "efficiency_unit": "km/l",                   // km/l (petrol, diesel, hybrid) | km/kg (CNG) | km/kWh (EV)
      "battery_kwh": null,                         // EVs only (required for EVs)
      "claimed_range_km": null,                    // EVs only; with battery_kwh it can replace claimed_efficiency
      "safety_rating_stars": <0-5 or null>,        // null = "not rated" (never use 0 for unknown)
      "safety_rating_body": "<e.g. Bharat NCAP>",  // must be null exactly when stars are null
      "airbags": <number or null>,
      "boot_litres": <number or null>,
      "ground_clearance_mm": <number or null>,
      "power_ps": <number or null>,
      "kerb_weight_kg": <number or null>,
      "transmission": "manual",                    // manual | amt | cvt | dct | torque_converter | single_speed
      "seats": <number>,
      "service_interval_km": <number or null>,
      "warranty_years": <number or null>,
      "warranty_km": <number or null>,
      "key_features": ["rear_camera", "esc"],      // only keys from the list below count in the interview
      "verified": true,
      "sources": [
        { "field_name": "ex_showroom_price_inr", "source_url": "https://…", "checked_at": "2026-11-08" },
        { "field_name": "safety_rating_stars",   "source_url": "https://…", "checked_at": "2026-11-08" }
        // … one entry per field that has a value (see below)
      ]
    }
  ]
}
```

(The `//` comments and `<…>` markers are explanations only: the real file must be plain JSON with real values.)

**Feature keys** (the only ones the interview asks about): `6_airbags`, `esc`, `rear_camera`, `connected_car`, `sunroof`, `ventilated_seats`, `adas`, `auto_climate`, `cruise_control`, `360_camera`. Other keys are stored but ignored.

**What `"verified": true` requires.** The loader refuses the file unless **every field that has a value** has at least one source with a real `http(s)` URL (not `TODO`, not `example.invalid`). That means:
- always: `make`, `model`, `variant_name`, `body_type`, `fuel_type`, `launch_year`, `transmission`, `seats`;
- plus every optional field you filled in (`ex_showroom_price_inr`, `claimed_efficiency`, `safety_rating_stars`, `airbags`, `boot_litres`, …), and `key_features` if it isn't empty.

One URL can back several fields: add one `sources` entry per field, all pointing at the same brochure. If you're still collecting sources, set `"verified": false`. The car works in the app with an "unverified data" badge.

**Used cars** need nothing in the catalogue beyond the variant itself. The buyer enters asking price, year and odometer in the app.

### 3.3 Replace the assumptions

Edit `data/seed/assumptions/common.json` and the four state files (`TN.json`, `KA.json`, `MH.json`, `DL.json`). Every entry looks like this:

```json
{ "key": "fuel_price.petrol.TN", "value": { "inr_per_litre": <number> },
  "source_url": "https://…", "checked_at": "2026-11-08", "verified": true, "note": "…" }
```

- The value shapes and units for every key are listed in [docs/model.md](model.md) §1–2. Keep the units exactly: rates are fractions (`0.10` for 10 %), prices are per litre / kg / kWh, and resale curves are the share of the new price at ages 1, 2, 3…
- `"verified": true` needs a real `source_url` and a `checked_at` date, or the loader refuses it.
- Values you can't source (repair rates, depreciation curves are hard) can stay as they are with `verified: false`. The report then still shows how many assumptions are placeholders, which is the honest result.

### 3.4 Remove the sample cars

1. Delete `data/seed/sample-motors-*.json`. (Keep `testdata/seed/` as it is: the tests use it.)
2. The seed loader only adds and updates; it never deletes. Remove the old rows from your **local** database with one of:
   - wipe everything and start fresh: `docker compose down -v`, then `docker compose up -d --build` and seed again; or
   - delete just the samples:

     ```powershell
     docker compose exec postgres psql -U tiebreaker -d tiebreaker -c "DELETE FROM sessions; DELETE FROM variants WHERE slug LIKE 'sample-motors-%';"
     ```

     The `DELETE FROM sessions` is needed because a car that's on someone's shortlist can't be deleted.

### 3.5 Load and check

```powershell
go run ./scripts/seed --check     # validates every file without touching the database
go run ./scripts/seed             # loads it (DATABASE_URL set as in part 2)
go test ./internal/catalog/...    # the guard tests on data/seed must still pass
```

**Check:**
- `curl "localhost:8080/v1/variants?q=<model>"` shows your car with `"verified": true` and an empty `warnings` list. `safety_not_rated` appears only if it really isn't rated.
- In a new report, the "placeholder assumptions" warning is gone, or shows how many are still unsourced.
- `bash scripts/demo.sh http://localhost:8080 <search term>` runs the demo with your cars (the second argument is the catalogue search).

**Alternative to files:** admins can add or edit cars through the API. Get a 15-minute token with `docker compose exec -T api /tiebreaker admin-token` (in Git Bash, prefix it with `MSYS_NO_PATHCONV=1`). Then `POST /admin/v1/variants` with the same JSON as one variant above and the header `Authorization: Bearer <token>`. Files are better for real data, because the sources live in git.

---

## 4. Switch on Gemini (optional)

Without Gemini every report uses a template, and everything works. With it, the worker asks Gemini to rewrite the report facts as a short summary. Any summary that mentions a number or car that isn't in the facts is thrown away and the template is used instead.

### 4.1 Get a key

1. Go to **Google AI Studio** (aistudio.google.com), sign in, and choose **Get API key → Create API key**.
2. Copy the key. Treat it like a password: never commit it, paste it in chat, or put it in a screenshot.

### 4.2 Check the key works

The SDK prefers `GOOGLE_API_KEY` over `GEMINI_API_KEY` when both are set, so make sure the first one is **not** set. The app refuses to start if it is.

```powershell
echo $env:GOOGLE_API_KEY                 # must print nothing; if it does: Remove-Item Env:GOOGLE_API_KEY
$env:GEMINI_API_KEY="<your key>"
$env:GEMINI_MODEL="gemini-3.5-flash-lite"
go run ./scripts/llm-smoke
```

You should see `RESULT: LLM prose accepted by the fact validator.` followed by a short summary.

About the model: `gemini-3.5-flash-lite` was the stable, fastest and cheapest model on Google's model page on 2026-11-07. Models change often, so check https://aistudio.google.com/docs/models and pick a current **Stable** "flash-lite" model if that one has been retired. There is no default in the code on purpose.

### 4.3 Turn it on in the app

Edit `.env`:

```dotenv
LLM_ENABLED=true
GEMINI_API_KEY=<your key>
GEMINI_MODEL=gemini-3.5-flash-lite
```

Then recreate the containers so they pick up the new values:

```powershell
docker compose up -d
docker compose logs worker | Select-String "LLM"      # Git Bash: docker compose logs worker | grep LLM
```

The log should say `LLM enabled for report prose`.

**Check:** finish one interview and run a simulation. The results page should show the heading **"Summary (written by AI, checked against the numbers)"**, and the report's `prose_source` should be `llm`. If you see the plain "Summary" instead, the worker log says why: no key, quota reached, a timeout, or `failed fact validation`. In every case the template was used, which is the designed behaviour.

### 4.4 Limits that protect your quota

| Setting (`.env`) | Default | Meaning |
|---|---|---|
| `LLM_DAILY_MAX_CALLS` | 500 | Gemini calls per UTC day; after that, reports use the template |
| `LLM_CACHE_TTL_HOURS` | 24 | identical facts reuse the stored summary, so no new call |
| `LLM_TIMEOUT_SECONDS` | 20 | per call; one retry on 429/5xx |
| `LLM_MAX_OUTPUT_TOKENS` | 1024 | cap on summary length |
| `RATE_LIMIT_SIM_PER_SESSION_PER_HOUR` / `…_PER_IP_PER_HOUR` | 10 / 30 | simulations, which trigger the summaries |

Docker Compose only passes `LLM_ENABLED`, `GEMINI_API_KEY` and `GEMINI_MODEL` into the containers. To change any of the others locally, add them to the `x-app-env` block in `docker-compose.yml`. On Railway you just set them as variables.

Only car data and numeric results are sent to Gemini: never session IDs, your state, or your usage answers. Logs record a short fingerprint of the prompt, never its content or the key.

---

## 5. Put it online

Full step-by-step: [docs/deploy.md](deploy.md). In short:

1. **Make the repo public** (your resume links to it):
   `gh repo edit xoBerlierxo/TieBreaker --visibility public --accept-visibility-change-consequences`
   Before that, confirm no secrets were ever committed: `git log -p | Select-String "GEMINI_API_KEY="` should show only the empty placeholder from `.env.example`.
2. **Railway:** create a project from the GitHub repo, then add Postgres and Redis. Create two services from the same repo: **api** (`TIEBREAKER_MODE=serve`, `MIGRATE_ON_START=true`, plus a public domain and the `/readyz` health check) and **worker** (`TIEBREAKER_MODE=worker`). Both need `DATABASE_URL`, `REDIS_URL`, `APP_ENV=production` and `JWT_SECRET` (`openssl rand -base64 48`; at least 32 characters). Add the Gemini variables from part 4 if you want AI summaries.
3. **Seed production** from your machine, using the Postgres public URL from Railway:
   `$env:DATABASE_URL="<Railway Postgres public URL>"; go run ./scripts/seed`
4. **Vercel:** import the repo, set **Root Directory** to `web`, and set `NEXT_PUBLIC_API_BASE_URL=https://<api domain>`.
5. Put the Vercel URL into the API's `CORS_ALLOWED_ORIGINS` and redeploy the API.

**Check:**
- `bash scripts/demo.sh https://<api domain> <search term>` passes.
- The Vercel URL works end to end in a private browser window.

---

## 6. Troubleshooting

| Symptom | Cause and fix |
|---|---|
| `npm ci` fails with **EPERM … unlink …next-swc** | a Next.js server is still running and holds the file. Stop it (Ctrl+C in its terminal, or `Get-Process node \| Stop-Process`) and retry. |
| The page says **Failed to fetch** | the API isn't running or CORS blocks it. Check `curl localhost:8080/readyz`. In production, check `CORS_ALLOWED_ORIGINS` matches the frontend URL exactly (scheme included, no trailing slash). |
| Search finds no cars | the catalogue isn't seeded: run part 2's seed command. |
| `/readyz` returns **503** | Postgres or Redis is down: `docker compose ps`, then `docker compose up -d`. |
| Integration tests fail with **rootless Docker is not supported** or **cannot connect** | Docker Desktop isn't running. Start it and wait until it says "running". |
| The seed loader says **cannot mark verified: fields without a real source: …** | add a `sources` entry for each listed field, or set `"verified": false` for now. |
| The seed loader says **unknown field** | a typo in a field name: the loader is strict on purpose. Compare with the template in 3.2. |
| Report is still **template** with Gemini on | read `docker compose logs worker`: the reason (quota, timeout, validation) is logged. `GOOGLE_API_KEY` set in the environment also stops the app at startup. |
| **409 "changed in another tab"** | expected: two tabs edited the same session. The page reloads; just click again. |
| Git Bash turns `/tiebreaker` into `C:/Program Files/Git/tiebreaker` | prefix the command with `MSYS_NO_PATHCONV=1`. |
