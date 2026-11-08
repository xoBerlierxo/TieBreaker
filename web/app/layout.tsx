import type { Metadata } from "next";
import Link from "next/link";
import "./globals.css";

export const metadata: Metadata = {
  title: "Tiebreaker: which car is best for you?",
  description: "Shortlist 2–4 cars; Tiebreaker simulates thousands of ownership futures and tells you the probability each one is your best choice.",
};

function Logo() {
  return (
    <svg width="34" height="20" viewBox="0 0 34 20" aria-hidden className="text-emerald-400">
      <circle cx="5" cy="14" r="4" fill="currentColor" />
      <circle cx="17" cy="6" r="4" fill="currentColor" opacity="0.8" />
      <circle cx="29" cy="14" r="4" fill="currentColor" opacity="0.6" />
      <path d="M5 14 L17 6 L29 14" stroke="currentColor" strokeWidth="2" fill="none" opacity="0.5" />
    </svg>
  );
}

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en-IN">
      <body className="min-h-screen">
        <header className="sticky top-0 z-20 border-b border-white/5 bg-[#050606]/80 backdrop-blur">
          <div className="mx-auto flex max-w-6xl items-center justify-between px-5 py-4">
            <div className="flex items-center gap-3">
              <Link href="/" className="flex items-center gap-2.5 text-lg font-bold text-white">
                <Logo />
                Tiebreaker
              </Link>
              <span className="hidden rounded-full border border-white/10 px-3 py-1 text-xs text-zinc-400 sm:inline">decision engine</span>
              <nav className="ml-6 hidden gap-6 text-sm text-zinc-400 md:flex">
                <Link href="/#how" className="hover:text-white">
                  How it works
                </Link>
                <a href="https://github.com/xoBerlierxo/TieBreaker" className="hover:text-white">
                  GitHub
                </a>
              </nav>
            </div>
            <Link
              href="/"
              className="rounded-xl bg-gradient-to-b from-emerald-400 to-teal-600 px-4 py-2 text-sm font-semibold text-white shadow-[0_4px_0_0_rgba(13,148,136,0.45)]"
            >
              Start comparing
            </Link>
          </div>
        </header>
        <main className="mx-auto max-w-6xl px-5 py-10">{children}</main>
        <footer className="border-t border-white/5 py-6 text-center font-mono text-xs text-zinc-600">
          Everything in rupees · numbers from simulation, never from AI
        </footer>
      </body>
    </html>
  );
}
