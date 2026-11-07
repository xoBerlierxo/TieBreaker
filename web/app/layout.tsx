import type { Metadata } from "next";
import Link from "next/link";
import "./globals.css";

export const metadata: Metadata = {
  title: "Tiebreaker: which car is best for you?",
  description: "Shortlist 2–4 cars; Tiebreaker simulates thousands of ownership futures and tells you the probability each one is your best choice.",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en-IN">
      <body>
        <header className="border-b bg-white">
          <div className="mx-auto flex max-w-4xl items-center justify-between px-4 py-3">
            <Link href="/" className="text-lg font-bold">
              🚗 Tiebreaker
            </Link>
            <span className="text-xs text-slate-500">Everything in rupees · a demo UI</span>
          </div>
        </header>
        <main className="mx-auto max-w-4xl px-4 py-6">{children}</main>
      </body>
    </html>
  );
}
