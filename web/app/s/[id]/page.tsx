"use client";

// Screen 1: shortlist and hard constraints (SPEC §6.1, §6.2).

import { useParams, useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { Button, Card, ErrorBanner, Eyebrow, inputClass, OptionCard, PageTitle, StepBar, UnverifiedBadge } from "@/components/ui";
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
      <PageTitle muted="then set your hard limits." sub="Add the 2–4 cars you're deciding between. Anything that breaks a hard limit is removed, and you'll see exactly why.">
        Build your shortlist,
      </PageTitle>
      <StepBar step={1} percent={session.cars.length >= 2 ? 25 : 10} />
      <ErrorBanner message={error} />

      <Card eyebrow={`Your shortlist · ${session.cars.length}/4`} aside={session.cars.length < 2 ? "add at least 2 cars" : "ready"}>
        {session.cars.length === 0 && <p className="text-sm text-zinc-500">No cars yet: search below to add one.</p>}
        <div className="grid gap-3 sm:grid-cols-2">
          {session.cars.map((c, i) => (
            <div key={c.id} className="flex items-center justify-between rounded-xl border border-white/10 bg-zinc-900/60 px-4 py-3">
              <div>
                <p className="font-mono text-xs text-emerald-400">0{i + 1}</p>
                <p className="text-sm font-semibold text-white">
                  {c.label}
                  <UnverifiedBadge verified={c.verified} />
                </p>
                {c.price && (
                  <p className="font-mono text-xs text-zinc-500">
                    {inr(c.price.budget_inr)} · {c.condition} ·{" "}
                    {c.price.source === "catalogue" ? (
                      <span className="text-emerald-400">sourced on-road price</span>
                    ) : c.price.source === "estimate" ? (
                      <span className="text-amber-300">estimated on-road</span>
                    ) : (
                      c.price.source
                    )}
                  </p>
                )}
              </div>
              <button className="text-xs text-zinc-500 hover:text-red-300" onClick={() => mutate("DELETE", `/v1/sessions/${id}/cars/${c.id}`)}>
                Remove
              </button>
            </div>
          ))}
        </div>
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
    <Card eyebrow="1 · Add a car" aside="search make, model or variant">
      <input className={`${inputClass} mb-3`} placeholder="Search…" value={q} onChange={(e) => setQ(e.target.value)} />
      <div className="mb-4 grid max-h-64 gap-2 overflow-auto sm:grid-cols-2">
        {results.map((v) => (
          <OptionCard
            key={v.id}
            selected={picked?.id === v.id}
            onClick={() => setPicked(v)}
            title={
              <>
                {v.display_name}
                <UnverifiedBadge verified={v.verified} />
              </>
            }
            subtitle={`${v.fuel_type} · ${v.transmission} · ${v.seats} seats`}
          />
        ))}
      </div>
      {picked && (
        <div className="space-y-4 border-t border-white/5 pt-4">
          <div className="grid grid-cols-2 gap-3 sm:max-w-md">
            <OptionCard selected={!used} onClick={() => setUsed(false)} title="New" subtitle="on-road price" />
            <OptionCard selected={used} onClick={() => setUsed(true)} title="Used" subtitle="asking price" />
          </div>
          {used && (
            <div className="grid gap-3 sm:grid-cols-3">
              <input className={inputClass} placeholder="Asking price ₹" value={asking} onChange={(e) => setAsking(e.target.value)} />
              <input className={inputClass} placeholder="Year" value={year} onChange={(e) => setYear(e.target.value)} />
              <input className={inputClass} placeholder="Odometer km" value={odometer} onChange={(e) => setOdometer(e.target.value)} />
            </div>
          )}
          <Button onClick={add}>Add {picked.display_name} →</Button>
        </div>
      )}
    </Card>
  );
}

// Budget segments: the ceiling each one sets, in lakh (null = no limit).
const SEGMENTS: [string, string, number | null][] = [
  ["< ₹10 Lakh", "City & entry", 10],
  ["₹10 – 15 Lakh", "Most popular", 15],
  ["₹15 – 25 Lakh", "Midsize & turbo", 25],
  ["₹25 – 45 Lakh", "Executive & SUV", 45],
  ["₹45 – 80 Lakh", "Premium & EV", 80],
  ["No limit", "Skip budget", null],
];

function budgetLabel(lakh: number) {
  return lakh >= 100 ? `₹${(lakh / 100).toFixed(2)} Crore` : `₹${lakh.toFixed(1)} Lakh`;
}

function Constraints({ hasEV, onSubmit }: { hasEV: boolean; onSubmit: (c: Record<string, unknown>) => Promise<void> }) {
  const [budget, setBudget] = useState<number | null>(15); // lakh; null = no limit
  const [seats, setSeats] = useState<number | null>(null);
  const [home, setHome] = useState(true);
  const [transmission, setTransmission] = useState<string | null>(null);

  return (
    <Card eyebrow="2 · Hard limits" aside="cars that fail these are removed, with the reason">
      <div className="rounded-2xl border border-emerald-500/20 bg-emerald-500/[0.03] p-5">
        <div className="flex flex-wrap items-end justify-between gap-2">
          <div>
            <p className="font-mono text-xs uppercase tracking-[0.2em] text-zinc-400">Maximum on-road budget</p>
            <p className="mt-1 text-4xl font-bold text-white">{budget === null ? "No limit" : budgetLabel(budget)}</p>
          </div>
          <p className="font-mono text-xs text-zinc-500">on-road for new · asking price for used</p>
        </div>
        <input
          type="range"
          className="tb-range mt-6"
          min={3}
          max={100}
          step={0.5}
          value={budget ?? 100}
          onChange={(e) => setBudget(Number(e.target.value))}
          aria-label="Maximum budget in lakh"
        />
        <div className="mt-2 flex justify-between font-mono text-xs text-zinc-600">
          <span>₹3 Lakh</span>
          <span>₹25 Lakh</span>
          <span>₹50 Lakh</span>
          <span>₹75 Lakh</span>
          <span>₹1 Crore</span>
        </div>
      </div>

      <p className="mb-3 mt-6 font-mono text-xs uppercase tracking-[0.2em] text-zinc-400">Or pick a quick segment</p>
      <div className="grid grid-cols-2 gap-3 md:grid-cols-6">
        {SEGMENTS.map(([title, sub, ceiling]) => (
          <OptionCard key={title} selected={budget === ceiling} onClick={() => setBudget(ceiling)} title={title} subtitle={sub} />
        ))}
      </div>

      <div className="mt-6 grid gap-6 md:grid-cols-2">
        <div>
          <Eyebrow>Minimum seats</Eyebrow>
          <div className="mt-3 grid grid-cols-3 gap-3">
            {([null, 5, 7] as const).map((s) => (
              <OptionCard key={String(s)} selected={seats === s} onClick={() => setSeats(s)} title={s === null ? "Any" : `${s}+`} />
            ))}
          </div>
        </div>
        <div>
          <Eyebrow>Gearbox</Eyebrow>
          <div className="mt-3 grid grid-cols-3 gap-3">
            {([
              [null, "Either"],
              ["automatic", "Automatic"],
              ["manual", "Manual"],
            ] as const).map(([v, label]) => (
              <OptionCard key={label} selected={transmission === v} onClick={() => setTransmission(v)} title={label} />
            ))}
          </div>
        </div>
        {hasEV && (
          <div>
            <Eyebrow>Can you charge an EV at home?</Eyebrow>
            <div className="mt-3 grid grid-cols-2 gap-3">
              <OptionCard selected={home} onClick={() => setHome(true)} title="Yes" subtitle="home tariff" />
              <OptionCard selected={!home} onClick={() => setHome(false)} title="No" subtitle="public charging" />
            </div>
          </div>
        )}
      </div>

      <Button
        className="mt-8"
        onClick={() =>
          onSubmit({
            max_budget_inr: budget === null ? null : Math.round(budget * 100_000),
            min_seats: seats,
            transmission,
            home_charging: hasEV ? home : null,
          })
        }
      >
        Continue to questions →
      </Button>
    </Card>
  );
}
