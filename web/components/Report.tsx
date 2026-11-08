"use client";

// The results view, shared by Screen 3 and the public share page.
// Everything shown comes from the report's facts (the single source of truth);
// the prose block is rendered from prose_md and labelled with its source.

import { Card, Eyebrow, Pill, UnverifiedBadge } from "@/components/ui";
import { inr, lakh, pct } from "@/lib/format";
import { Markdown } from "@/lib/markdown";
import type { CarFacts, ReportView, ResultRow } from "@/lib/types";

// Emerald first (the leader colour), then distinct hues that stay readable on black.
const COLOURS = ["#34d399", "#38bdf8", "#fbbf24", "#f472b6"];

export function Report({ report, shareUrl }: { report: ReportView; shareUrl?: string }) {
  const f = report.facts;
  const leader = [...f.cars].sort((a, b) => b.p_best - a.p_best)[0];

  return (
    <div>
      <section className="mb-6 rounded-3xl border border-white/5 bg-black/40 p-6 sm:p-10">
        <Pill>{f.kind === "winner" ? "Clear winner" : f.kind === "deadlock" ? "Too close to call" : "No car fits"}</Pill>
        <h1 className="mt-5 text-4xl font-bold leading-tight tracking-tight text-white sm:text-5xl">
          {f.kind === "winner" && (
            <>
              {f.winner} <span className="font-normal text-zinc-500">is your best choice.</span>
            </>
          )}
          {f.kind === "deadlock" && (
            <>
              It&apos;s close. <span className="font-normal text-zinc-500">{f.tipping_points.length > 0 ? "One factor decides it." : "Either is a sound pick."}</span>
            </>
          )}
          {f.kind === "none_fit" && (
            <>
              None of your cars fit <span className="font-normal text-zinc-500">your hard limits.</span>
            </>
          )}
        </h1>
        {leader && f.kind !== "none_fit" && (
          <p className="mt-4 text-lg text-zinc-400">
            {leader.label} had the lowest net cost for you in <span className="font-semibold text-emerald-400">{pct(leader.p_best)}</span> of{" "}
            {f.run ? f.run.futures.toLocaleString("en-IN") : "the"} simulated futures.
          </p>
        )}
      </section>

      {f.placeholder_assumptions > 0 && (
        <div className="mb-5 rounded-xl border border-amber-500/30 bg-amber-500/10 p-4 text-sm text-amber-200">
          <span className="font-mono text-xs uppercase tracking-wider text-amber-300">Placeholder data · </span>
          {f.placeholder_assumptions} model assumptions (fuel prices, tariffs, depreciation…) are placeholder values, not sourced data. Treat the
          numbers as a demonstration.
        </div>
      )}

      {f.cars.length > 0 && (
        <Card eyebrow="Probability each car is your best choice">
          <PBestBars cars={f.cars} />
          {report.results && report.results.length > 0 && <Histogram cars={f.cars} results={report.results} />}
        </Card>
      )}

      {f.tipping_points.length > 0 && (
        <Card eyebrow="What could change the answer">
          <ul className="space-y-2">
            {f.tipping_points.map((t, i) => (
              <li key={i} className="rounded-xl border border-emerald-500/20 bg-emerald-500/5 p-3 text-sm text-zinc-200">
                {t.sentence}
                {t.inside_user_range && <span className="ml-2 font-mono text-xs text-amber-300">within the range you gave</span>}
              </li>
            ))}
          </ul>
        </Card>
      )}

      {f.cars.length > 0 && (
        <div className="mb-5 grid gap-4 md:grid-cols-2">
          {f.cars.map((c, i) => (
            <CarPanel key={c.item_id} car={c} colour={COLOURS[i % COLOURS.length]} />
          ))}
        </div>
      )}

      <Card eyebrow={report.prose_source === "llm" ? "Summary · written by AI, checked against the numbers" : "Summary"}>
        <Markdown source={report.prose_md} />
      </Card>

      {f.eliminated.length > 0 && (
        <Card eyebrow="Removed by your hard limits">
          <ul className="space-y-2 text-sm">
            {f.eliminated.map((e) => (
              <li key={e.label} className="rounded-xl border border-white/10 bg-zinc-900/60 p-3">
                <span className="font-semibold text-white">{e.label}</span>
                <span className="text-zinc-400">: {e.reasons.join("; ")}</span>
              </li>
            ))}
          </ul>
        </Card>
      )}

      {f.run && (
        <p className="mt-2 font-mono text-xs text-zinc-600">
          {f.run.futures.toLocaleString("en-IN")} simulated futures · model {f.run.model_version} · seed {f.run.seed} · re-running with this seed
          reproduces these numbers exactly
        </p>
      )}
      {shareUrl && (
        <div className="mt-5 flex flex-wrap items-center gap-3 rounded-xl border border-white/10 bg-zinc-900/60 p-4">
          <Eyebrow>Share this result</Eyebrow>
          <a className="break-all font-mono text-sm text-emerald-400 underline-offset-4 hover:underline" href={shareUrl}>
            {shareUrl}
          </a>
        </div>
      )}
    </div>
  );
}

function CarPanel({ car, colour }: { car: CarFacts; colour: string }) {
  return (
    <section className="rounded-2xl border border-white/10 bg-white/[0.02] p-5">
      <div className="flex items-start justify-between gap-2">
        <h3 className="font-semibold text-white">
          <span className="mr-2 inline-block h-2.5 w-2.5 rounded-full" style={{ background: colour }} />
          {car.label}
          <UnverifiedBadge verified={car.verified} />
        </h3>
        <span className="font-mono text-sm text-emerald-400">{pct(car.p_best)}</span>
      </div>
      <dl className="mt-4 grid grid-cols-2 gap-3 font-mono text-xs">
        <Stat label="Net cost p50" value={lakh(car.net_cost.p50)} />
        <Stat label="Range p10–p90" value={`${lakh(car.net_cost.p10)} – ${lakh(car.net_cost.p90)}`} />
        <Stat label="Cheapest to own" value={pct(car.p_cheapest)} />
        <Stat label="Safety" value={car.safety_rating} />
      </dl>
      {car.pros.length > 0 && (
        <ul className="mt-4 space-y-1 text-sm">
          {car.pros.map((p) => (
            <li key={p.key} className="text-zinc-300">
              <span className="mr-1.5 text-emerald-400">+</span>
              {p.text}
            </li>
          ))}
        </ul>
      )}
      {car.cons.length > 0 && (
        <ul className="mt-2 space-y-1 text-sm">
          {car.cons.map((p) => (
            <li key={p.key} className="text-zinc-400">
              <span className="mr-1.5 text-red-400">−</span>
              {p.text}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border border-white/5 bg-black/40 p-2.5">
      <dt className="uppercase tracking-wider text-zinc-500">{label}</dt>
      <dd className="mt-1 text-sm text-white">{value}</dd>
    </div>
  );
}

function PBestBars({ cars }: { cars: CarFacts[] }) {
  return (
    <ul className="space-y-4">
      {cars.map((c, i) => (
        <li key={c.item_id} className="text-sm">
          <div className="mb-1 flex justify-between">
            <span className="text-zinc-300">{c.label}</span>
            <span className="font-mono font-semibold text-white">{pct(c.p_best)}</span>
          </div>
          <div className="h-2.5 rounded-full bg-zinc-800">
            <div className="h-2.5 rounded-full" style={{ width: `${Math.max(1, c.p_best * 100)}%`, background: COLOURS[i % COLOURS.length] }} />
          </div>
        </li>
      ))}
    </ul>
  );
}

/** Histogram draws each car's net-cost distribution on a shared axis (inline SVG, no chart library). */
function Histogram({ cars, results }: { cars: CarFacts[]; results: ResultRow[] }) {
  const byItem = new Map(results.map((r) => [r.shortlist_item_id, r.histogram]));
  const hists = cars.map((c) => byItem.get(c.item_id)).filter((h): h is ResultRow["histogram"] => !!h);
  if (hists.length === 0) return null;
  const edges = hists[0].edges_inr;
  const maxCount = Math.max(...hists.flatMap((h) => h.counts), 1);
  const W = 640,
    H = 180,
    pad = 24;
  const x = (i: number) => pad + (i / (edges.length - 1)) * (W - 2 * pad);
  const y = (n: number) => H - pad - (n / maxCount) * (H - 2 * pad);

  return (
    <div className="mt-6 border-t border-white/5 pt-5">
      <p className="mb-2 font-mono text-xs uppercase tracking-wider text-zinc-500">Net cost across simulated futures · lower is better</p>
      <svg viewBox={`0 0 ${W} ${H}`} className="w-full" role="img" aria-label="Net cost histogram">
        <line x1={pad} y1={H - pad} x2={W - pad} y2={H - pad} stroke="#3f3f46" />
        {cars.map((c, ci) => {
          const h = byItem.get(c.item_id);
          if (!h) return null;
          const pts = h.counts.map((n, i) => `${(x(i) + x(i + 1)) / 2},${y(n)}`).join(" ");
          return <polyline key={c.item_id} points={pts} fill="none" stroke={COLOURS[ci % COLOURS.length]} strokeWidth={2} />;
        })}
        <text x={pad} y={H - 6} fontSize="10" fill="#71717a">
          {inr(edges[0])}
        </text>
        <text x={W - pad} y={H - 6} fontSize="10" fill="#71717a" textAnchor="end">
          {inr(edges[edges.length - 1])}
        </text>
      </svg>
      <div className="flex flex-wrap gap-4 text-xs text-zinc-400">
        {cars.map((c, i) => (
          <span key={c.item_id} className="flex items-center gap-1.5">
            <span className="inline-block h-2 w-4 rounded" style={{ background: COLOURS[i % COLOURS.length] }} />
            {c.label}
          </span>
        ))}
      </div>
    </div>
  );
}
