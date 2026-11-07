// Indian-style formatting, matching the backend's internal/money package.

/** inr formats rupees with lakh/crore grouping: 102340 → "₹1,02,340". */
export function inr(amount: number): string {
  const sign = amount < 0 ? "−" : "";
  return sign + "₹" + Math.round(Math.abs(amount)).toLocaleString("en-IN");
}

/** lakh formats large amounts compactly: 250000 → "₹2.5 L". */
export function lakh(amount: number): string {
  const abs = Math.abs(amount);
  const sign = amount < 0 ? "−" : "";
  if (abs >= 1e7) return `${sign}₹${(abs / 1e7).toFixed(2).replace(/\.?0+$/, "")} Cr`;
  if (abs >= 1e5) return `${sign}₹${(abs / 1e5).toFixed(2).replace(/\.?0+$/, "")} L`;
  return inr(amount);
}

/** pct formats a probability: 0.623 → "62%". */
export function pct(p: number): string {
  return `${Math.round(p * 100)}%`;
}
