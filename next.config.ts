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

/**
 * Content Security Policy. The app has no third-party scripts, so this can be
 * strict: scripts and styles only from this origin (plus the inline styles
 * Next/Tailwind emit and the blob: workers Next uses), media and images from
 * this origin (the API is same-origin through the rewrite) and blob: for the
 * offline cache, fonts from Google's CDN which next/font pulls at build time.
 *
 * 'unsafe-inline' for scripts is needed by Next's hydration payload in
 * production; 'unsafe-eval' is only added in development for HMR.
 */
function contentSecurityPolicy(): string {
  const dev = process.env.NODE_ENV !== "production";
  const directives = [
    "default-src 'self'",
    `script-src 'self' 'unsafe-inline'${dev ? " 'unsafe-eval'" : ""} blob:`,
    "style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
    "font-src 'self' https://fonts.gstatic.com data:",
    "img-src 'self' blob: data:",
    "media-src 'self' blob:",
    "connect-src 'self' blob:",
    "worker-src 'self' blob:",
    "manifest-src 'self'",
    "object-src 'none'",
    "base-uri 'self'",
    "form-action 'self'",
    "frame-ancestors 'none'",
  ];
  return directives.join("; ");
}

const securityHeaders = [
  { key: "Content-Security-Policy", value: contentSecurityPolicy() },
  // The whole app is one origin, so never leak the URL (which may carry a
  // media token) to anyone else — not even the path to same-site pages.
  { key: "Referrer-Policy", value: "same-origin" },
  { key: "X-Frame-Options", value: "DENY" },
  { key: "X-Content-Type-Options", value: "nosniff" },
  { key: "Permissions-Policy", value: "camera=(), microphone=(), geolocation=(), payment=(), usb=()" },
  { key: "Cross-Origin-Opener-Policy", value: "same-origin" },
];

const nextConfig: NextConfig = {
  output: "standalone",
  reactStrictMode: false,
  async headers() {
    return [
      { source: "/:path*", headers: securityHeaders },
      {
        // Apple requires the association file to be served as JSON; it has
        // no extension, so Next would otherwise guess.
        source: "/.well-known/apple-app-site-association",
        headers: [{ key: "Content-Type", value: "application/json" }],
      },
    ];
  },
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
