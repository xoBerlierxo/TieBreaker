"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { Button, Card, ErrorBanner, inputClass } from "@/components/ui";
import { errorMessage, request } from "@/lib/api";
import type { SessionView } from "@/lib/types";

const STATES = [
  ["TN", "Tamil Nadu"],
  ["KA", "Karnataka"],
  ["MH", "Maharashtra"],
  ["DL", "Delhi"],
];

export default function Home() {
  const router = useRouter();
  const [state, setState] = useState("TN");
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);

  async function start() {
    setBusy(true);
    setError(undefined);
    try {
      const { data } = await request<SessionView>("POST", "/v1/sessions", { body: { user_state: state } });
      router.push(`/s/${data.id}`);
    } catch (e) {
      setError(errorMessage(e));
      setBusy(false);
    }
  }

  return (
    <div>
      <h1 className="mb-2 text-3xl font-bold">Can&apos;t decide between cars?</h1>
      <p className="mb-6 text-slate-600">
        Shortlist 2–4 cars. Tiebreaker asks a few questions, prices your trade-offs in rupees, simulates thousands of possible futures of
        owning each car, and tells you the probability that each one is the best choice <em>for you</em>.
      </p>
      <ErrorBanner message={error} />
      <Card title="Start">
        <label className="mb-1 block text-sm font-medium">Where will you register the car?</label>
        <select className={`${inputClass} mb-4 max-w-xs`} value={state} onChange={(e) => setState(e.target.value)}>
          {STATES.map(([code, name]) => (
            <option key={code} value={code}>
              {name}
            </option>
          ))}
        </select>
        <div>
          <Button onClick={start} disabled={busy}>
            {busy ? "Starting…" : "Start comparing"}
          </Button>
        </div>
      </Card>
    </div>
  );
}
