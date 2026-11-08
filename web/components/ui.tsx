import type { ReactNode } from "react";

// Shared building blocks for the dark, emerald-accented design.

/** Eyebrow is the small green monospace label above sections ("● STEP 1 · SHORTLIST"). */
export function Eyebrow({ children, dot = false }: { children: ReactNode; dot?: boolean }) {
  return (
    <p className="font-mono text-xs font-semibold uppercase tracking-[0.2em] text-emerald-400">
      {dot && <span className="mr-2 inline-block h-1.5 w-1.5 -translate-y-0.5 rounded-full bg-emerald-400" />}
      {children}
    </p>
  );
}

/** Pill is a rounded outlined badge, e.g. the hero tag. */
export function Pill({ children }: { children: ReactNode }) {
  return (
    <span className="inline-flex items-center gap-2 rounded-full border border-emerald-500/40 bg-emerald-500/5 px-4 py-1.5 font-mono text-xs font-semibold uppercase tracking-[0.2em] text-emerald-400">
      <span className="h-1.5 w-1.5 rounded-full bg-emerald-400" />
      {children}
    </span>
  );
}

export function Card({ title, eyebrow, aside, children, className = "" }: { title?: string; eyebrow?: string; aside?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section className={`mb-5 rounded-2xl border border-white/10 bg-white/[0.02] p-5 sm:p-6 ${className}`}>
      {(eyebrow || title || aside) && (
        <div className="mb-4 flex flex-wrap items-start justify-between gap-2">
          <div>
            {eyebrow && <Eyebrow>{eyebrow}</Eyebrow>}
            {title && <h2 className="mt-1 text-lg font-semibold text-white">{title}</h2>}
          </div>
          {aside && <div className="font-mono text-xs text-zinc-500">{aside}</div>}
        </div>
      )}
      {children}
    </section>
  );
}

export function ErrorBanner({ message }: { message?: string }) {
  if (!message) return null;
  return <div className="mb-5 rounded-xl border border-red-500/40 bg-red-500/10 p-3 text-sm text-red-200">{message}</div>;
}

/** UnverifiedBadge marks catalogue data that has no verified sources (SPEC §7). */
export function UnverifiedBadge({ verified }: { verified: boolean }) {
  if (verified) return null;
  return (
    <span title="This car's data has not been verified against real sources" className="ml-2 rounded-md border border-amber-500/40 bg-amber-500/10 px-1.5 py-0.5 font-mono text-[10px] uppercase tracking-wider text-amber-300">
      unverified
    </span>
  );
}

type ButtonProps = React.ButtonHTMLAttributes<HTMLButtonElement> & { variant?: "primary" | "secondary" };

export function Button({ variant = "primary", className = "", ...rest }: ButtonProps) {
  const style =
    variant === "primary"
      ? "bg-gradient-to-b from-emerald-400 to-teal-600 text-white shadow-[0_4px_0_0_rgba(13,148,136,0.45)] hover:from-emerald-300 hover:to-teal-500 disabled:opacity-50"
      : "border border-white/10 bg-zinc-900 text-white shadow-[0_4px_0_0_rgba(0,0,0,0.6)] hover:bg-zinc-800 disabled:opacity-50";
  return <button {...rest} className={`rounded-xl px-5 py-2.5 text-sm font-semibold transition active:translate-y-0.5 ${style} ${className}`} />;
}

/** OptionCard is a tap-to-select tile; the selected one gets an emerald border and a check. */
export function OptionCard({
  selected,
  onClick,
  title,
  subtitle,
  disabled,
}: {
  selected: boolean;
  onClick: () => void;
  title: ReactNode;
  subtitle?: ReactNode;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={`relative rounded-xl border px-4 py-3 text-left transition disabled:opacity-50 ${
        selected ? "border-emerald-500 bg-emerald-500/10 text-emerald-300 shadow-[0_0_0_1px_rgba(16,185,129,0.4)]" : "border-white/10 bg-zinc-900/60 text-white hover:border-white/25"
      }`}
    >
      <span className="block text-sm font-semibold">
        {selected && <span className="mr-1.5">✓</span>}
        {title}
      </span>
      {subtitle && <span className={`mt-1 block font-mono text-xs ${selected ? "text-emerald-400/80" : "text-zinc-500"}`}>{subtitle}</span>}
    </button>
  );
}

const STEPS = ["Shortlist & Budget", "Usage & Trade-offs", "Simulation & Verdict"];

/** StepBar shows "Step N of 3" with three segments, like a checkout progress bar. */
export function StepBar({ step, percent }: { step: 1 | 2 | 3; percent?: number }) {
  const pct = percent ?? Math.round(((step - 1) / 3) * 100 + 17);
  return (
    <div className="mb-8">
      <div className="mb-3 flex items-center justify-between font-mono text-sm">
        <p>
          <span className="text-emerald-400">Step {step} of 3:</span> <span className="text-zinc-200">{STEPS[step - 1]}</span>
        </p>
        <p className="text-zinc-500">{Math.min(100, pct)}% completed</p>
      </div>
      <div className="grid grid-cols-3 gap-3">
        {STEPS.map((label, i) => (
          <div key={label}>
            <div className={`h-1.5 rounded-full ${i + 1 < step ? "bg-emerald-600" : i + 1 === step ? "bg-emerald-400" : "bg-zinc-800"}`} />
            <p className={`mt-2 font-mono text-xs ${i + 1 === step ? "text-emerald-400" : "text-zinc-500"}`}>
              <span className="mr-1.5">{i + 1}</span>
              {label}
            </p>
          </div>
        ))}
      </div>
    </div>
  );
}

/** PageTitle is the large bold heading with an optional grey continuation. */
export function PageTitle({ children, muted, sub }: { children: ReactNode; muted?: ReactNode; sub?: ReactNode }) {
  return (
    <div className="mb-6">
      <h1 className="text-3xl font-bold tracking-tight text-white sm:text-4xl">
        {children}
        {muted && <span className="font-normal text-zinc-500"> {muted}</span>}
      </h1>
      {sub && <p className="mt-2 max-w-3xl text-zinc-400">{sub}</p>}
    </div>
  );
}

export const inputClass =
  "w-full rounded-xl border border-white/10 bg-zinc-950 px-3 py-2.5 text-sm text-white placeholder:text-zinc-600 outline-none focus:border-emerald-500";
