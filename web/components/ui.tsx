import type { ReactNode } from "react";

export function Card({ title, children }: { title?: string; children: ReactNode }) {
  return (
    <section className="mb-4 rounded-xl border bg-white p-4 shadow-sm">
      {title && <h2 className="mb-3 text-lg font-semibold">{title}</h2>}
      {children}
    </section>
  );
}

export function ErrorBanner({ message }: { message?: string }) {
  if (!message) return null;
  return <div className="mb-4 rounded-lg border border-red-300 bg-red-50 p-3 text-sm text-red-800">{message}</div>;
}

/** UnverifiedBadge marks catalogue data that has no verified sources (SPEC §7). */
export function UnverifiedBadge({ verified }: { verified: boolean }) {
  if (verified) return null;
  return (
    <span title="This car's data has not been verified against real sources" className="ml-2 rounded bg-amber-100 px-1.5 py-0.5 text-xs font-medium text-amber-800">
      ⚠ unverified data
    </span>
  );
}

export function Button(props: React.ButtonHTMLAttributes<HTMLButtonElement> & { variant?: "primary" | "secondary" }) {
  const { variant = "primary", className = "", ...rest } = props;
  const style =
    variant === "primary" ? "bg-indigo-600 text-white hover:bg-indigo-700 disabled:bg-indigo-300" : "border bg-white hover:bg-slate-50 disabled:text-slate-400";
  return <button {...rest} className={`rounded-lg px-4 py-2 text-sm font-medium ${style} ${className}`} />;
}

export const inputClass = "w-full rounded-lg border px-3 py-2 text-sm";
