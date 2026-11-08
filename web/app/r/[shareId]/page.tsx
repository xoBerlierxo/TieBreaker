"use client";

// The public, read-only share page (SPEC §6.7): GET /r/:shareId.

import { useParams } from "next/navigation";
import { useEffect, useState } from "react";
import { Report } from "@/components/Report";
import { ErrorBanner, Eyebrow } from "@/components/ui";
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
  if (!report) return <p className="font-mono text-sm text-zinc-500">Loading…</p>;
  return (
    <div>
      <div className="mb-4">
        <Eyebrow dot>Shared Tiebreaker result</Eyebrow>
      </div>
      <Report report={report} />
    </div>
  );
}
