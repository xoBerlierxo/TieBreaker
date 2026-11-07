"use client";

// Screen 2: usage questions, then the adaptive trade-off interview (SPEC §6.3, §8).

import { useParams, useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { Button, Card, ErrorBanner, inputClass } from "@/components/ui";
import { errorMessage, isVersionConflict, request, submitIdempotent } from "@/lib/api";
import { pct } from "@/lib/format";
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

  return (
    <div>
      <h1 className="mb-4 text-2xl font-bold">2 · A few questions</h1>
      <ErrorBanner message={error} />
      {next?.p_best && next.p_best.length > 0 && (
        <Card title="Right now">
          <ul className="space-y-1 text-sm">
            {next.p_best.map((p) => (
              <li key={p.item_id} className="flex items-center gap-2">
                <span className="w-64 truncate">{p.label}</span>
                <span className="h-2 rounded bg-indigo-500" style={{ width: `${Math.max(2, p.p_best * 200)}px` }} />
                <span>{pct(p.p_best)}</span>
              </li>
            ))}
          </ul>
          <p className="mt-2 text-xs text-slate-500">Probability of being your best choice, from quick simulations.</p>
        </Card>
      )}
      {next && !next.done && next.question && <QuestionCard key={next.question.question_id} q={next.question} busy={busy} onAnswer={answer} />}
      {next?.done && (
        <Card title="That's all we need">
          <p className="mb-3 text-sm text-slate-600">
            {next.reason === "leader_confident" && "One car already leads clearly. "}
            {next.reason === "low_information_gain" && "Further questions wouldn't change the answer much. "}
            {next.reason === "single_car" && "Only one car meets your constraints. "}
            Now we run the full simulation (tens of thousands of futures).
          </p>
          <Button onClick={simulate} disabled={busy}>
            Run the full simulation
          </Button>
        </Card>
      )}
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

  return (
    <Card title={q.phase === "tradeoff" ? "Trade-off" : "Your usage"}>
      <p className="mb-3">{q.text}</p>
      {q.warning && <p className="mb-3 text-xs text-amber-700">Note: {q.warning}</p>}

      {q.kind === "range" && (
        <div className="flex items-center gap-2">
          <input className={inputClass} placeholder="from" value={lo} onChange={(e) => setLo(e.target.value)} />
          <span>–</span>
          <input className={inputClass} placeholder="to" value={hi} onChange={(e) => setHi(e.target.value)} />
          <span className="whitespace-nowrap text-sm text-slate-500">{q.unit}</span>
          <Button disabled={busy} onClick={() => onAnswer(q.question_id, { [rangeKey]: [Number(lo), Number(hi)] })}>
            Next
          </Button>
        </div>
      )}

      {q.kind === "number" && (
        <div className="flex items-center gap-2">
          <input className={`${inputClass} max-w-32`} value={value} onChange={(e) => setValue(e.target.value)} />
          <span className="text-sm text-slate-500">{q.unit}</span>
          <Button disabled={busy} onClick={() => onAnswer(q.question_id, { ownership_years: Number(value) })}>
            Next
          </Button>
        </div>
      )}

      {q.kind === "financing" && (
        <div className="space-y-2">
          <label className="mr-4 text-sm">
            <input type="radio" checked={!loan} onChange={() => setLoan(false)} /> Cash
          </label>
          <label className="text-sm">
            <input type="radio" checked={loan} onChange={() => setLoan(true)} /> Loan
          </label>
          {loan && (
            <div className="grid grid-cols-3 gap-2">
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
            Next
          </Button>
        </div>
      )}

      {q.kind === "choice" && (
        <div className="flex flex-wrap gap-2">
          {q.options?.map((o) => (
            <Button key={o} variant="secondary" disabled={busy} onClick={() => onAnswer(q.question_id, { parking: o })}>
              {o}
            </Button>
          ))}
          {q.optional && (
            <Button variant="secondary" disabled={busy} onClick={() => onAnswer(q.question_id, { skipped: true })}>
              Skip
            </Button>
          )}
        </div>
      )}

      {q.kind === "yes_no" && (
        <div className="flex gap-2">
          <Button disabled={busy} onClick={() => onAnswer(q.question_id, { yes: true })}>
            Yes, I would
          </Button>
          <Button variant="secondary" disabled={busy} onClick={() => onAnswer(q.question_id, { yes: false })}>
            No
          </Button>
        </div>
      )}
    </Card>
  );
}
