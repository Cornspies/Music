import type { NextConfig } from "next";
import { PHASE_DEVELOPMENT_SERVER } from "next/constants";

const API_HOST = process.env.MUSIC_API_HOST ?? "localhost";
const API_PORT = process.env.MUSIC_API_PORT ?? "3001";
const API_URL = process.env.MUSIC_API_URL ?? `http://${API_HOST}:${API_PORT}`;

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
    "frame-src 'self' com.cornspies.music:",
    "base-uri 'self'",
    "form-action 'self'",
    "frame-ancestors 'none'",
  ];
  return directives.join("; ");
}

const securityHeaders = [
  { key: "Content-Security-Policy", value: contentSecurityPolicy() },
  { key: "Referrer-Policy", value: "same-origin" },
  { key: "X-Frame-Options", value: "DENY" },
  { key: "X-Content-Type-Options", value: "nosniff" },
  { key: "Permissions-Policy", value: "camera=(), microphone=(), geolocation=(), payment=(), usb=()" },
  { key: "Cross-Origin-Opener-Policy", value: "same-origin" },
  { key: "Strict-Transport-Security", value: "max-age=31536000" },
];

const desktop = process.env.NEXT_PUBLIC_MUSIC_DESKTOP === "1";
const webStatic = process.env.MUSIC_WEB_STATIC === "1";
const DESKTOP_DEV_SERVER = process.env.MUSIC_DESKTOP_DEV_SERVER ?? "https://music.cornspies.com";

const desktopConfig: NextConfig = {
  output: "export",
  distDir: ".next-desktop",
  reactStrictMode: false,
  experimental: { turbopackFileSystemCacheForBuild: false },
};

const staticWebConfig: NextConfig = {
  output: "export",
  reactStrictMode: false,
  experimental: { turbopackFileSystemCacheForBuild: false },
};

const desktopDevConfig: NextConfig = {
  distDir: ".next-desktop",
  reactStrictMode: false,
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination: `${DESKTOP_DEV_SERVER}/api/:path*`,
      },
    ];
  },
};

const webConfig: NextConfig = {
  output: "standalone",
  reactStrictMode: false,
  async headers() {
    return [
      { source: "/:path*", headers: securityHeaders },
      {
        source: "/.well-known/apple-app-site-association",
        headers: [{ key: "Content-Type", value: "application/json" }],
      },
    ];
  },
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination: `${API_URL}/api/:path*`,
      },
    ];
  },
};

export default function config(phase: string): NextConfig {
  if (desktop) return phase === PHASE_DEVELOPMENT_SERVER ? desktopDevConfig : desktopConfig;
  return webStatic && phase !== PHASE_DEVELOPMENT_SERVER ? staticWebConfig : webConfig;
}
