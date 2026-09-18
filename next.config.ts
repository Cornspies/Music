import type { NextConfig } from "next";

/**
 * Configuration comes from environment variables, which Next loads from
 * .env / .env.local before this file is evaluated — so a single .env at the
 * repository root configures both this app and the Go backend.
 *
 * MUSIC_API_URL      full backend origin, e.g. http://127.0.0.1:3001
 * MUSIC_API_HOST     host only (default: localhost)
 * MUSIC_API_PORT     port only (default: 3001)
 *
 * These are read at build/start time on the server. The browser-side port
 * lives in NEXT_PUBLIC_MUSIC_API_PORT (see src/lib/api.ts), because only
 * NEXT_PUBLIC_* variables are exposed to client code.
 */
const API_HOST = process.env.MUSIC_API_HOST ?? "localhost";
const API_PORT = process.env.MUSIC_API_PORT ?? "3001";
const API_URL = process.env.MUSIC_API_URL ?? `http://${API_HOST}:${API_PORT}`;

const nextConfig: NextConfig = {
  output: "standalone",
  typescript: { ignoreBuildErrors: true },
  reactStrictMode: false,
  async rewrites() {
    return [
      {
        // Proxy the API to the Go backend so the browser only ever talks to
        // this origin (no CORS, and it works without a separate gateway).
        source: "/api/:path*",
        destination: `${API_URL}/api/:path*`,
      },
    ];
  },
};

export default nextConfig;
