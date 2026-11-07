import type { NextConfig } from "next";

// The frontend is a thin client of the Go API (SPEC §3: "kept thin").
// NEXT_PUBLIC_API_BASE_URL points it at the API (default: local docker compose).
const nextConfig: NextConfig = {
  reactStrictMode: true,
  poweredByHeader: false,
};

export default nextConfig;
