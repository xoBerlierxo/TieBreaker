"use client";

// The results view, shared by Screen 3 and the public share page.
// Everything shown comes from the report's facts (the single source of truth);
// the prose block is rendered from prose_md and labelled with its source.

import { Card, UnverifiedBadge } from "@/components/ui";
import { inr, lakh, pct } from "@/lib/format";
import { Markdown } from "@/lib/markdown";
import type { CarFacts, ReportView, ResultRow } from "@/lib/types";

const COLOURS = ["#4f46e5", "#059669", "#d97706", "#db2777"];

export function Report({ report, shareUrl }: { report: ReportView; shareUrl?: string }) {
  const f = report.facts;
  return (
    <div>
      {f.kind === "winner" && <h1 className="mb-2 text-3xl font-bold">🏆 {f.winner}</h1>}
      {f.kind === "deadlock" && <h1 className="mb-2 text-3xl font-bold">🤝 It&apos;s close</h1>}
      {f.kind === "none_fit" && <h1 className="mb-2 text-3xl font-bold">None of your cars fit</h1>}

      {f.placeholder_assumptions > 0 && (
        <div className="mb-4 rounded-lg border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900">
          ⚠ {f.placeholder_assumptions} model assumptions (fuel prices, tariffs, depreciation…) are placeholder values, not sourced data. Treat
          the numbers as a demonstration.
        </div>
      )}

      {f.cars.length > 0 && (
        <Card title="Probability each car is your best choice">
          <PBestBars cars={f.cars} />
          {report.results && report.results.length > 0 && <Histogram cars={f.cars} results={report.results} />}
        </Card>
      )}

      <Card title={report.prose_source === "llm" ? "Summary (written by AI, checked against the numbers)" : "Summary"}>
        <Markdown source={report.prose_md} />
      </Card>

      {f.cars.length > 0 && (
        <Card title="Costs over the whole ownership period (today's money)">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-slate-500">
                  <th className="py-1">Car</th>
                  <th>P(cheapest)</th>
                  <th>Net cost p10 / p50 / p90</th>
                  <th>Ownership cost p50</th>
                  <th>Safety</th>
                </tr>
              </thead>
              <tbody>
                {f.cars.map((c) => (
                  <tr key={c.item_id} className="border-t">
                    <td className="py-1">
                      {c.label}
                      <UnverifiedBadge verified={c.verified} />
                    </td>
                    <td>{pct(c.p_cheapest)}</td>
                    <td>
                      {lakh(c.net_cost.p10)} / <strong>{lakh(c.net_cost.p50)}</strong> / {lakh(c.net_cost.p90)}
                    </td>
                    <td>{lakh(c.ownership_cost.p50)}</td>
                    <td>{c.safety_rating}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
      )}

      {f.tipping_points.length > 0 && (
        <Card title="What could change the answer">
          <ul className="ml-5 list-disc space-y-1 text-sm">
            {f.tipping_points.map((t, i) => (
              <li key={i}>
                {t.sentence} {t.inside_user_range && <span className="text-amber-700">(within the range you gave)</span>}
              </li>
            ))}
          </ul>
        </Card>
      )}

      {f.eliminated.length > 0 && (
        <Card title="Removed by your constraints">
          <ul className="ml-5 list-disc text-sm">
            {f.eliminated.map((e) => (
              <li key={e.label}>
                <strong>{e.label}</strong>: {e.reasons.join("; ")}
              </li>
            ))}
          </ul>
        </Card>
      )}

      {f.run && (
        <p className="mt-2 text-xs text-slate-500">
          {f.run.futures.toLocaleString("en-IN")} simulated futures · model {f.run.model_version} · seed {f.run.seed} (re-running with this seed
          reproduces these numbers exactly)
        </p>
      )}
      {shareUrl && (
        <p className="mt-3 text-sm">
          Share this result: <a className="text-indigo-700 underline" href={shareUrl}>{shareUrl}</a>
        </p>
      )}
    </div>
  );
}

function PBestBars({ cars }: { cars: CarFacts[] }) {
  return (
    <ul className="space-y-2">
      {cars.map((c, i) => (
        <li key={c.item_id} className="text-sm">
          <div className="mb-0.5 flex justify-between">
            <span>{c.label}</span>
            <strong>{pct(c.p_best)}</strong>
          </div>
          <div className="h-3 rounded bg-slate-100">
            <div className="h-3 rounded" style={{ width: `${Math.max(1, c.p_best * 100)}%`, background: COLOURS[i % COLOURS.length] }} />
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
    <div className="mt-4">
      <p className="mb-1 text-xs text-slate-500">Net cost across simulated futures (lower is better)</p>
      <svg viewBox={`0 0 ${W} ${H}`} className="w-full" role="img" aria-label="Net cost histogram">
        <line x1={pad} y1={H - pad} x2={W - pad} y2={H - pad} stroke="#cbd5e1" />
        {cars.map((c, ci) => {
          const h = byItem.get(c.item_id);
          if (!h) return null;
          const pts = h.counts.map((n, i) => `${(x(i) + x(i + 1)) / 2},${y(n)}`).join(" ");
          return <polyline key={c.item_id} points={pts} fill="none" stroke={COLOURS[ci % COLOURS.length]} strokeWidth={2} />;
        })}
        <text x={pad} y={H - 6} fontSize="10" fill="#64748b">
          {inr(edges[0])}
        </text>
        <text x={W - pad} y={H - 6} fontSize="10" fill="#64748b" textAnchor="end">
          {inr(edges[edges.length - 1])}
        </text>
      </svg>
      <div className="flex flex-wrap gap-3 text-xs">
        {cars.map((c, i) => (
          <span key={c.item_id} className="flex items-center gap-1">
            <span className="inline-block h-2 w-4 rounded" style={{ background: COLOURS[i % COLOURS.length] }} />
            {c.label}
          </span>
        ))}
      </div>
    </div>
  );
}
