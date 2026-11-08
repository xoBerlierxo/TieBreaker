"use client";

// Screen 2: usage questions, then the adaptive trade-off interview (SPEC §6.3, §8).

import { useParams, useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { Button, Card, ErrorBanner, inputClass, OptionCard, PageTitle, StepBar } from "@/components/ui";
import { errorMessage, isVersionConflict, request, submitIdempotent } from "@/lib/api";
import { inr, pct } from "@/lib/format";
import type { NextResponse, Question, SessionView } from "@/lib/types";

export default function InterviewPage() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const [version, setVersion] = useState("");
  const [next, setNext] = useState<NextResponse>();
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);

  const refresh = useCallback(async () => {
    const s = await request<SessionView>("GET", `/v1/sessions/${id}`);
    setVersion(s.etag ?? String(s.data.version));
    if (s.data.state === "SHORTLISTING") return router.replace(`/s/${id}`);
    if (s.data.state === "SIMULATING" || s.data.state === "DONE") return router.replace(`/s/${id}/results`);
    const n = await request<NextResponse>("GET", `/v1/sessions/${id}/next-question`);
    setNext(n.data);
  }, [id, router]);

  useEffect(() => {
    refresh().catch((e) => setError(errorMessage(e)));
  }, [refresh]);

  async function answer(questionId: string, body: unknown) {
    setBusy(true);
    setError(undefined);
    try {
      const res = await submitIdempotent(`/v1/sessions/${id}/answers`, { question_id: questionId, answer: body }, version);
      if (res.etag) setVersion(res.etag);
      await refresh();
    } catch (e) {
      if (isVersionConflict(e)) await refresh();
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  async function simulate() {
    setBusy(true);
    try {
      const res = await request<{ run_id: string }>("POST", `/v1/sessions/${id}/simulations`, { ifMatch: version });
      router.push(`/s/${id}/results?run=${res.data.run_id}`);
    } catch (e) {
      setError(errorMessage(e));
      setBusy(false);
    }
  }

  const tradeoff = next?.question?.phase === "tradeoff";
  const progress = next?.done ? 66 : tradeoff ? 45 + Math.min(20, (next?.questions_asked ?? 0) * 2) : 40;

  return (
    <div>
      <PageTitle muted="only the questions that matter." sub="First how you'll use the car, then a few rupee trade-offs. Each trade-off question is picked because it's the one most likely to change the answer.">
        A few questions,
      </PageTitle>
      <StepBar step={2} percent={progress} />
      <ErrorBanner message={error} />

      <div className="grid gap-5 lg:grid-cols-[1.6fr_1fr]">
        <div>
          {next && !next.done && next.question && <QuestionCard key={next.question.question_id} q={next.question} busy={busy} onAnswer={answer} />}
          {next?.done && (
            <Card eyebrow="Interview complete" title="That's all we need.">
              <p className="mb-5 text-sm text-zinc-400">
                {next.reason === "leader_confident" && "One car already leads clearly. "}
                {next.reason === "low_information_gain" && "More questions wouldn't change the answer much. "}
                {next.reason === "single_car" && "Only one car meets your hard limits. "}
                Now the full simulation runs: 50,000 futures of owning each car.
              </p>
              <Button onClick={simulate} disabled={busy}>
                Run the full simulation →
              </Button>
            </Card>
          )}
        </div>

        <Card eyebrow="Right now" aside={next ? `${next.questions_asked} asked · ${next.questions_left} left` : undefined}>
          {next?.p_best && next.p_best.length > 0 ? (
            <ul className="space-y-4">
              {next.p_best.map((p) => (
                <li key={p.item_id}>
                  <div className="mb-1 flex justify-between gap-2 text-sm">
                    <span className="truncate text-zinc-300">{p.label}</span>
                    <span className="font-mono text-emerald-400">{pct(p.p_best)}</span>
                  </div>
                  <div className="h-2 rounded-full bg-zinc-800">
                    <div className="h-2 rounded-full bg-gradient-to-r from-emerald-400 to-teal-500 transition-all" style={{ width: `${Math.max(2, p.p_best * 100)}%` }} />
                  </div>
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-sm text-zinc-500">Live odds appear once your usage is known.</p>
          )}
          <p className="mt-5 font-mono text-xs text-zinc-600">Probability of being your best choice, from quick simulations.</p>
        </Card>
      </div>
    </div>
  );
}

function QuestionCard({ q, busy, onAnswer }: { q: Question; busy: boolean; onAnswer: (id: string, body: unknown) => void }) {
  const [lo, setLo] = useState("");
  const [hi, setHi] = useState("");
  const [value, setValue] = useState("");
  const [loan, setLoan] = useState(false);
  const [principal, setPrincipal] = useState("");
  const [rate, setRate] = useState("");
  const [tenure, setTenure] = useState("");

  const rangeKey = q.question_id === "usage:km_per_month" ? "km_per_month" : "city_share_pct";
  const isTradeoff = q.phase === "tradeoff";

  return (
    <Card eyebrow={isTradeoff ? `Trade-off · would you pay ${q.threshold_inr ? inr(q.threshold_inr) : ""}?` : "Your usage"}>
      <p className="mb-5 text-lg text-white">{q.text}</p>
      {q.warning && <p className="mb-4 rounded-lg border border-amber-500/30 bg-amber-500/10 p-2 text-xs text-amber-200">Note: {q.warning}</p>}

      {q.kind === "range" && (
        <div className="flex flex-wrap items-center gap-3">
          <input className={`${inputClass} max-w-32`} placeholder="from" value={lo} onChange={(e) => setLo(e.target.value)} />
          <span className="text-zinc-500">to</span>
          <input className={`${inputClass} max-w-32`} placeholder="to" value={hi} onChange={(e) => setHi(e.target.value)} />
          <span className="font-mono text-xs text-zinc-500">{q.unit}</span>
          <Button disabled={busy} onClick={() => onAnswer(q.question_id, { [rangeKey]: [Number(lo), Number(hi)] })}>
            Next →
          </Button>
        </div>
      )}

      {q.kind === "number" && (
        <div className="flex flex-wrap items-center gap-3">
          <input className={`${inputClass} max-w-32`} value={value} onChange={(e) => setValue(e.target.value)} />
          <span className="font-mono text-xs text-zinc-500">{q.unit}</span>
          <Button disabled={busy} onClick={() => onAnswer(q.question_id, { ownership_years: Number(value) })}>
            Next →
          </Button>
        </div>
      )}

      {q.kind === "financing" && (
        <div className="space-y-4">
          <div className="grid max-w-md grid-cols-2 gap-3">
            <OptionCard selected={!loan} onClick={() => setLoan(false)} title="Cash" subtitle="pay in full" />
            <OptionCard selected={loan} onClick={() => setLoan(true)} title="Loan" subtitle="EMI" />
          </div>
          {loan && (
            <div className="grid gap-3 sm:grid-cols-3">
              <input className={inputClass} placeholder="Amount ₹" value={principal} onChange={(e) => setPrincipal(e.target.value)} />
              <input className={inputClass} placeholder="Rate % a year" value={rate} onChange={(e) => setRate(e.target.value)} />
              <input className={inputClass} placeholder="Tenure months" value={tenure} onChange={(e) => setTenure(e.target.value)} />
            </div>
          )}
          <Button
            disabled={busy}
            onClick={() =>
              onAnswer(
                q.question_id,
                loan ? { mode: "loan", principal_inr: Number(principal), annual_rate_pct: Number(rate), tenure_months: Number(tenure) } : { mode: "cash" },
              )
            }
          >
            Next →
          </Button>
        </div>
      )}

      {q.kind === "choice" && (
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          {q.options?.map((o) => (
            <OptionCard key={o} selected={false} disabled={busy} onClick={() => onAnswer(q.question_id, { parking: o })} title={o[0].toUpperCase() + o.slice(1)} />
          ))}
          {q.optional && <OptionCard selected={false} disabled={busy} onClick={() => onAnswer(q.question_id, { skipped: true })} title="Skip" subtitle="optional" />}
        </div>
      )}

      {q.kind === "yes_no" && (
        <div className="grid max-w-lg grid-cols-2 gap-3">
          <OptionCard selected={false} disabled={busy} onClick={() => onAnswer(q.question_id, { yes: true })} title="Yes, I would" subtitle="worth it to me" />
          <OptionCard selected={false} disabled={busy} onClick={() => onAnswer(q.question_id, { yes: false })} title="No" subtitle="not for that much" />
        </div>
      )}
    </Card>
  );
}
