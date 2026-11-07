// Railway Infrastructure as Code for Tiebreaker (api + worker + Postgres + Redis).
//
// Written against https://docs.railway.com/infrastructure-as-code/reference
// (checked 2026-11-07). Preview before applying:
//   npm install railway && railway login && railway link
//   railway config plan     # review
//   railway config apply    # apply
// If anything here disagrees with your Railway project, follow the dashboard
// steps in docs/deploy.md instead; they set exactly the same things.
//
// One Docker image serves both services: TIEBREAKER_MODE picks `serve` or
// `worker` (the runtime image has no shell, so no start-command wrapping is
// needed), and the API applies migrations on start (MIGRATE_ON_START).
import { defineRailway, github, postgres, preserve, project, redis, service } from "railway/iac";

export default defineRailway(() => {
  const db = postgres("postgres");
  const cache = redis("redis");
  const source = github("xoBerlierxo/TieBreaker", { branch: "main" }); // builds the root Dockerfile

  const shared = {
    APP_ENV: "production",
    DATABASE_URL: db.env.DATABASE_URL,
    REDIS_URL: cache.env.REDIS_URL,
    JWT_SECRET: preserve(), // set in the dashboard; never committed
    LLM_ENABLED: preserve(), // "false" until you add a Gemini key
    GEMINI_API_KEY: preserve(),
    GEMINI_MODEL: preserve(),
  };

  const api = service("api", {
    source,
    healthcheck: "/readyz",
    healthcheckTimeout: 60,
    env: {
      ...shared,
      TIEBREAKER_MODE: "serve",
      MIGRATE_ON_START: "true",
      CORS_ALLOWED_ORIGINS: preserve(), // your Vercel URL, e.g. https://tiebreaker.vercel.app
    },
  });

  const worker = service("worker", {
    source,
    env: { ...shared, TIEBREAKER_MODE: "worker" },
  });

  return project("tiebreaker", { resources: [db, cache, api, worker] });
});
