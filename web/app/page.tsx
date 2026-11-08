"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { Button, ErrorBanner, Eyebrow, OptionCard, Pill } from "@/components/ui";
import { errorMessage, request } from "@/lib/api";
import type { SessionView } from "@/lib/types";

const STATES: [string, string, string][] = [
  ["TN", "Tamil Nadu", "Chennai"],
  ["KA", "Karnataka", "Bengaluru"],
  ["MH", "Maharashtra", "Mumbai · Pune"],
  ["DL", "Delhi", "Delhi-NCR"],
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
      <section className="grid items-center gap-10 rounded-3xl border border-white/5 bg-black/40 p-6 sm:p-10 lg:grid-cols-[1.15fr_1fr]">
        <div>
          <Pill>Tiebreaker decision engine</Pill>
          <h1 className="mt-6 text-4xl font-bold leading-[1.05] tracking-tight text-white sm:text-6xl">
            Pick the right car, <span className="font-normal text-zinc-500">with odds instead of opinions.</span>
          </h1>
          <p className="mt-6 max-w-xl text-lg text-zinc-400">
            Shortlist 2–4 cars. Tiebreaker prices your trade-offs in rupees, simulates thousands of possible futures of owning each one, and
            tells you the probability that each car is the best choice <em className="text-zinc-200">for you</em>.
          </p>

          <ErrorBanner message={error} />
          <div className="mt-8">
            <Eyebrow>Where will you register the car?</Eyebrow>
            <div className="mt-3 grid grid-cols-2 gap-3 sm:grid-cols-4">
              {STATES.map(([code, name, city]) => (
                <OptionCard key={code} selected={state === code} onClick={() => setState(code)} title={name} subtitle={city} />
              ))}
            </div>
          </div>

          <div className="mt-8 flex flex-wrap gap-3">
            <Button onClick={start} disabled={busy}>
              {busy ? "Starting…" : "Start comparing →"}
            </Button>
            <a href="#how">
              <Button variant="secondary" type="button">
                See how it decides
              </Button>
            </a>
          </div>
          <p className="mt-6 font-mono text-xs text-zinc-500">
            <span className="text-emerald-400">● Deterministic engine active</span> · 50,000 futures per verdict · reproducible by seed
          </p>
        </div>

        <HeroPanel />
      </section>

      <section id="how" className="mt-16 scroll-mt-24">
        <Eyebrow dot>How it decides</Eyebrow>
        <h2 className="mt-3 text-3xl font-bold tracking-tight text-white">
          Three steps. <span className="font-normal text-zinc-500">No spec-sheet guesswork.</span>
        </h2>
        <div className="mt-6 grid gap-4 md:grid-cols-3">
          {[
            ["01", "Shortlist & budget", "Add the cars you're torn between, new or used. Hard limits like budget and seats remove cars and show by how much they miss."],
            ["02", "A short, adaptive interview", "It asks only about real differences, and always the question that would change the answer most. Each answer is a rupee value."],
            ["03", "Simulate & decide", "Fuel prices, mileage, repairs and resale vary across 50,000 futures. You get P(best), costs in rupees, and the single factor that would tip it."],
          ].map(([n, title, text]) => (
            <div key={n} className="rounded-2xl border border-white/10 bg-white/[0.02] p-5">
              <p className="font-mono text-xs text-emerald-400">{n}</p>
              <h3 className="mt-2 font-semibold text-white">{title}</h3>
              <p className="mt-2 text-sm text-zinc-400">{text}</p>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}

/** HeroPanel is an illustrative preview of a verdict (example numbers, not real data). */
function HeroPanel() {
  const rows: [string, number][] = [
    ["Car A", 0.62],
    ["Car B", 0.27],
    ["Car C", 0.11],
  ];
  return (
    <div className="rounded-3xl border border-white/10 bg-zinc-900/40 p-3">
      <div className="rounded-2xl border border-white/5 bg-black/60 p-6">
        <p className="font-mono text-xs uppercase tracking-[0.2em] text-zinc-500">Example verdict</p>
        <p className="mt-3 text-2xl font-bold text-white">
          Car A <span className="font-normal text-zinc-500">wins in 62% of futures</span>
        </p>
        <ul className="mt-6 space-y-4">
          {rows.map(([label, p], i) => (
            <li key={label}>
              <div className="mb-1 flex justify-between text-sm">
                <span className="text-zinc-300">{label}</span>
                <span className="font-mono text-emerald-400">{Math.round(p * 100)}%</span>
              </div>
              <div className="h-2 rounded-full bg-zinc-800">
                <div className={`h-2 rounded-full ${i === 0 ? "bg-gradient-to-r from-emerald-400 to-teal-500" : "bg-zinc-600"}`} style={{ width: `${p * 100}%` }} />
              </div>
            </li>
          ))}
        </ul>
        <p className="mt-6 rounded-xl border border-emerald-500/20 bg-emerald-500/5 p-3 text-sm text-zinc-300">
          <span className="font-mono text-xs text-emerald-400">TIPPING POINT · </span>
          drive more than ~1,500 km a month and Car B takes over.
        </p>
      </div>
      <div className="mt-3 rounded-full border border-white/10 px-4 py-2 font-mono text-xs text-emerald-400">
        ● Shortlist → Interview → Simulation → Verdict
      </div>
    </div>
  );
}
