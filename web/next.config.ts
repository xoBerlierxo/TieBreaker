import type { NextConfig } from "next";

// The frontend is a thin client of the Go API (SPEC §3: "kept thin").
// NEXT_PUBLIC_API_BASE_URL points it at the API (default: local docker compose).
// Browser hardening headers for every page. Session pages have the session ID
// in their URL, so no-referrer keeps it from leaking to other sites (e.g. the
// GitHub link). The CSP only restricts framing, plugins and <base>: Next.js
// needs inline scripts, so a script-src policy would require nonces.
const securityHeaders = [
  { key: "X-Content-Type-Options", value: "nosniff" },
  { key: "X-Frame-Options", value: "DENY" },
  { key: "Referrer-Policy", value: "no-referrer" },
  { key: "Permissions-Policy", value: "camera=(), microphone=(), geolocation=()" },
  { key: "Content-Security-Policy", value: "frame-ancestors 'none'; object-src 'none'; base-uri 'self'" },
];

const nextConfig: NextConfig = {
  reactStrictMode: true,
  poweredByHeader: false,
  async headers() {
    return [{ source: "/:path*", headers: securityHeaders }];
  },
};

export default nextConfig;
