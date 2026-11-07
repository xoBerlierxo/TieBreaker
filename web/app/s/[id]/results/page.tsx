"use client";

// Screen 3: live progress over SSE, then the report (SPEC §11, §9).

import { useParams, useRouter, useSearchParams } from "next/navigation";
import { Suspense, useCallback, useEffect, useState } from "react";
import { Report } from "@/components/Report";
import { Button, Card, ErrorBanner } from "@/components/ui";
import { API_BASE, ApiError, errorMessage, request } from "@/lib/api";
import type { ProgressEvent, ReportView } from "@/lib/types";

export default function ResultsPage() {
  return (
    <Suspense>
      <Results />
    </Suspense>
  );
}

function Results() {
  const { id } = useParams<{ id: string }>();
  const runId = useSearchParams().get("run");
  const router = useRouter();
  const [progress, setProgress] = useState<ProgressEvent>();
  const [report, setReport] = useState<ReportView>();
  const [error, setError] = useState<string>();

  const loadReport = useCallback(async () => {
    try {
      const r = await request<ReportView>("GET", `/v1/sessions/${id}/report`);
      setReport(r.data);
      return true;
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) return false; // still simulating
      setError(errorMessage(e));
      return true;
    }
  }, [id]);

  // With a run ID: follow its progress live; without one (e.g. a reload), poll the report.
  useEffect(() => {
    if (!runId) {
      let stop = false;
      const tick = async () => {
        if (!stop && !(await loadReport())) setTimeout(tick, 2000);
      };
      tick();
      return () => {
        stop = true;
      };
    }
    const es = new EventSource(`${API_BASE}/v1/simulations/${runId}/events`);
    es.addEventListener("progress", (msg) => {
      const ev = JSON.parse((msg as MessageEvent).data) as ProgressEvent;
      setProgress(ev);
      if (ev.status === "done") {
        es.close();
        loadReport();
      } else if (ev.status === "failed" || ev.status === "cancelled") {
        es.close();
        if (ev.status === "failed") setError(`The simulation failed: ${ev.error ?? "unknown error"}`);
      }
    });
    es.onerror = () => {
      // EventSource reconnects by itself; the server re-sends the current state.
    };
    return () => es.close();
  }, [runId, loadReport]);

  async function cancel() {
    if (!runId) return;
    await request("POST", `/v1/simulations/${runId}/cancel`).catch(() => undefined);
    router.push(`/s/${id}/interview`);
  }

  if (report) {
    const shareUrl = typeof window !== "undefined" ? `${window.location.origin}/r/${report.share_id}` : undefined;
    return <Report report={report} shareUrl={shareUrl} />;
  }
  return (
    <div>
      <h1 className="mb-4 text-2xl font-bold">3 · Simulating your futures</h1>
      <ErrorBanner message={error} />
      <Card>
        <p className="mb-2 text-sm text-slate-600">
          {progress ? `${progress.status}… ${progress.progress}%` : "Waiting for a worker…"}
        </p>
        <div className="mb-4 h-3 rounded bg-slate-100">
          <div className="h-3 rounded bg-indigo-500 transition-all" style={{ width: `${progress?.progress ?? 2}%` }} />
        </div>
        {runId && progress?.status !== "done" && (
          <Button variant="secondary" onClick={cancel}>
            Cancel
          </Button>
        )}
      </Card>
    </div>
  );
}
