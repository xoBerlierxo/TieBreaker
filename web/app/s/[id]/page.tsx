"use client";

// Screen 1: shortlist and hard constraints (SPEC §6.1, §6.2).

import { useParams, useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { Button, Card, ErrorBanner, inputClass, UnverifiedBadge } from "@/components/ui";
import { errorMessage, isVersionConflict, request } from "@/lib/api";
import { inr } from "@/lib/format";
import type { SessionView, Variant } from "@/lib/types";

export default function ShortlistPage() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const [session, setSession] = useState<SessionView>();
  const [version, setVersion] = useState<string>("");
  const [error, setError] = useState<string>();

  const load = useCallback(async () => {
    const res = await request<SessionView>("GET", `/v1/sessions/${id}`);
    setSession(res.data);
    setVersion(res.etag ?? String(res.data.version));
    return res.data;
  }, [id]);

  useEffect(() => {
    load()
      .then((s) => {
        if (s.state === "USAGE" || s.state === "INTERVIEW") router.replace(`/s/${id}/interview`);
        if (s.state === "SIMULATING" || s.state === "DONE") router.replace(`/s/${id}/results`);
      })
      .catch((e) => setError(errorMessage(e)));
  }, [id, load, router]);

  // mutate runs a change with If-Match; on a version conflict it reloads the
  // session (another tab changed it) and asks the user to try again.
  async function mutate(method: string, path: string, body?: unknown) {
    setError(undefined);
    try {
      const res = await request<SessionView>(method, path, { body, ifMatch: version });
      setSession(res.data);
      setVersion(res.etag ?? String(res.data.version));
      return res.data;
    } catch (e) {
      if (isVersionConflict(e)) {
        await load();
        setError("This session was changed in another tab. It has been reloaded; please try again.");
      } else {
        setError(errorMessage(e));
      }
      return undefined;
    }
  }

  if (!session) return <ErrorBanner message={error} />;
  const hasEV = session.cars.some((c) => c.fuel_type === "ev");

  return (
    <div>
      <h1 className="mb-4 text-2xl font-bold">1 · Your shortlist</h1>
      <ErrorBanner message={error} />
      <Card title={`Cars (${session.cars.length}/4)`}>
        {session.cars.length === 0 && <p className="text-sm text-slate-500">Add 2–4 cars you are deciding between.</p>}
        <ul className="divide-y">
          {session.cars.map((c) => (
            <li key={c.id} className="flex items-center justify-between py-2 text-sm">
              <span>
                {c.label}
                <UnverifiedBadge verified={c.verified} />
                {c.price && <span className="ml-2 text-slate-500">{inr(c.price.budget_inr)}</span>}
              </span>
              <Button variant="secondary" onClick={() => mutate("DELETE", `/v1/sessions/${id}/cars/${c.id}`)}>
                Remove
              </Button>
            </li>
          ))}
        </ul>
      </Card>
      {session.cars.length < 4 && <AddCar onAdd={(body) => mutate("POST", `/v1/sessions/${id}/cars`, body)} />}
      {session.cars.length >= 2 && (
        <Constraints
          hasEV={hasEV}
          onSubmit={async (c) => {
            const s = await mutate("PUT", `/v1/sessions/${id}/constraints`, c);
            if (s) router.push(s.state === "DONE" ? `/s/${id}/results` : `/s/${id}/interview`);
          }}
        />
      )}
    </div>
  );
}

function AddCar({ onAdd }: { onAdd: (body: Record<string, unknown>) => Promise<unknown> }) {
  const [q, setQ] = useState("");
  const [results, setResults] = useState<Variant[]>([]);
  const [picked, setPicked] = useState<Variant>();
  const [used, setUsed] = useState(false);
  const [asking, setAsking] = useState("");
  const [year, setYear] = useState("");
  const [odometer, setOdometer] = useState("");

  useEffect(() => {
    const t = setTimeout(() => {
      request<{ variants: Variant[] }>("GET", `/v1/variants?q=${encodeURIComponent(q)}&limit=10`)
        .then((r) => setResults(r.data.variants))
        .catch(() => setResults([]));
    }, 250);
    return () => clearTimeout(t);
  }, [q]);

  async function add() {
    if (!picked) return;
    const body: Record<string, unknown> = { variant_id: picked.id, condition: used ? "used" : "new" };
    if (used) Object.assign(body, { asking_price_inr: Number(asking), year: Number(year), odometer_km: Number(odometer) });
    await onAdd(body);
    setPicked(undefined);
    setUsed(false);
  }

  return (
    <Card title="Add a car">
      <input className={`${inputClass} mb-2`} placeholder="Search make, model or variant…" value={q} onChange={(e) => setQ(e.target.value)} />
      <ul className="mb-3 max-h-48 overflow-auto rounded border">
        {results.map((v) => (
          <li key={v.id}>
            <button className={`w-full px-3 py-2 text-left text-sm hover:bg-slate-50 ${picked?.id === v.id ? "bg-indigo-50" : ""}`} onClick={() => setPicked(v)}>
              {v.display_name} <span className="text-slate-500">· {v.fuel_type} · {v.transmission}</span>
              <UnverifiedBadge verified={v.verified} />
            </button>
          </li>
        ))}
      </ul>
      {picked && (
        <div className="space-y-2">
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={used} onChange={(e) => setUsed(e.target.checked)} /> Used car
          </label>
          {used && (
            <div className="grid grid-cols-3 gap-2">
              <input className={inputClass} placeholder="Asking price ₹" value={asking} onChange={(e) => setAsking(e.target.value)} />
              <input className={inputClass} placeholder="Year" value={year} onChange={(e) => setYear(e.target.value)} />
              <input className={inputClass} placeholder="Odometer km" value={odometer} onChange={(e) => setOdometer(e.target.value)} />
            </div>
          )}
          <Button onClick={add}>Add {picked.display_name}</Button>
        </div>
      )}
    </Card>
  );
}

function Constraints({ hasEV, onSubmit }: { hasEV: boolean; onSubmit: (c: Record<string, unknown>) => Promise<void> }) {
  const [budget, setBudget] = useState("");
  const [seats, setSeats] = useState("");
  const [home, setHome] = useState(true);
  const [transmission, setTransmission] = useState("");

  return (
    <Card title="Hard constraints">
      <p className="mb-3 text-sm text-slate-500">Cars that fail these are removed, and you&apos;ll see why and by how much.</p>
      <div className="grid gap-3 sm:grid-cols-2">
        <label className="text-sm">
          Max budget (₹, on-road or asking)
          <input className={inputClass} value={budget} onChange={(e) => setBudget(e.target.value)} placeholder="e.g. 1500000" />
        </label>
        <label className="text-sm">
          Minimum seats
          <input className={inputClass} value={seats} onChange={(e) => setSeats(e.target.value)} placeholder="e.g. 5" />
        </label>
        <label className="text-sm">
          Transmission
          <select className={inputClass} value={transmission} onChange={(e) => setTransmission(e.target.value)}>
            <option value="">Either</option>
            <option value="automatic">Automatic only</option>
            <option value="manual">Manual only</option>
          </select>
        </label>
        {hasEV && (
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={home} onChange={(e) => setHome(e.target.checked)} /> I can charge an EV at home
          </label>
        )}
      </div>
      <Button
        className="mt-4"
        onClick={() =>
          onSubmit({
            max_budget_inr: budget ? Number(budget) : null,
            min_seats: seats ? Number(seats) : null,
            transmission: transmission || null,
            home_charging: hasEV ? home : null,
          })
        }
      >
        Continue
      </Button>
    </Card>
  );
}
