"use client";

// The public, read-only share page (SPEC §6.7): GET /r/:shareId.

import { useParams } from "next/navigation";
import { useEffect, useState } from "react";
import { Report } from "@/components/Report";
import { ErrorBanner } from "@/components/ui";
import { errorMessage, request } from "@/lib/api";
import type { ReportView } from "@/lib/types";

export default function SharedReportPage() {
  const { shareId } = useParams<{ shareId: string }>();
  const [report, setReport] = useState<ReportView>();
  const [error, setError] = useState<string>();

  useEffect(() => {
    request<ReportView>("GET", `/r/${shareId}`)
      .then((r) => setReport(r.data))
      .catch((e) => setError(errorMessage(e)));
  }, [shareId]);

  if (error) return <ErrorBanner message={error} />;
  if (!report) return <p className="text-sm text-slate-500">Loading…</p>;
  return (
    <div>
      <p className="mb-3 text-xs uppercase tracking-wide text-slate-500">Shared Tiebreaker result</p>
      <Report report={report} />
    </div>
  );
}
