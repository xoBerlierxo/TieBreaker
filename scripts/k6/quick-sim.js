// k6 load test for the interview's next-question endpoint (quick simulations).
// SPEC §16 target: p95 < 1.5 s. Run against a seeded API with one prepared
// session in INTERVIEW state:
//   k6 run -e BASE=http://localhost:8080 -e SESSION=<id> scripts/k6/quick-sim.js
import http from "k6/http";
import { check } from "k6";

export const options = {
  vus: 10,
  duration: "30s",
  thresholds: { http_req_duration: ["p(95)<1500"] },
};

export default function () {
  const res = http.get(`${__ENV.BASE}/v1/sessions/${__ENV.SESSION}/next-question`);
  check(res, { "status is 200": (r) => r.status === 200 });
}
