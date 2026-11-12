import type { NextConfig } from "next";
import { PHASE_DEVELOPMENT_SERVER } from "next/constants";

const API_HOST = process.env.MUSIC_API_HOST ?? "localhost";
const API_PORT = process.env.MUSIC_API_PORT ?? "3001";
const API_URL = process.env.MUSIC_API_URL ?? `http://${API_HOST}:${API_PORT}`;

const desktop = process.env.NEXT_PUBLIC_MUSIC_DESKTOP === "1";
const DEV_API = desktop ? (process.env.MUSIC_DESKTOP_DEV_SERVER ?? API_URL) : API_URL;

const shared: NextConfig = {
  reactStrictMode: false,
  ...(desktop ? { distDir: ".next-desktop" } : {}),
};

const devConfig: NextConfig = {
  ...shared,
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination: `${DEV_API}/api/:path*`,
      },
    ];
  },
};

const exportConfig: NextConfig = {
  ...shared,
  output: "export",
  experimental: { turbopackFileSystemCacheForBuild: false },
};

export default function config(phase: string): NextConfig {
  return phase === PHASE_DEVELOPMENT_SERVER ? devConfig : exportConfig;
}
