// k6 load test for starting full simulations and polling them to completion.
// Each iteration needs a session in INTERVIEW state; pass a comma-separated
// list prepared with scripts/demo (stop it before "Start the full simulation"):
//   k6 run -e BASE=http://localhost:8080 -e SESSIONS=<id1>:<version1>,<id2>:<version2> scripts/k6/full-sim.js
// Note: simulation starts are rate limited (RATE_LIMIT_SIM_PER_IP_PER_HOUR).
import http from "k6/http";
import { check, sleep } from "k6";

const sessions = (__ENV.SESSIONS || "").split(",").filter(Boolean);
export const options = { iterations: sessions.length, vus: Math.min(5, Math.max(1, sessions.length)) };

export default function () {
  const [id, version] = sessions[__ITER % sessions.length].split(":");
  const start = http.post(`${__ENV.BASE}/v1/sessions/${id}/simulations`, null, { headers: { "If-Match": `"${version}"` } });
  check(start, { "202 accepted": (r) => r.status === 202 });
  if (start.status !== 202) return;
  const runId = start.json("run_id");
  for (let i = 0; i < 60; i++) {
    const s = http.get(`${__ENV.BASE}/v1/simulations/${runId}`).json("status");
    if (s === "done" || s === "failed" || s === "cancelled") {
      check(s, { "run done": (v) => v === "done" });
      return;
    }
    sleep(0.5);
  }
}
