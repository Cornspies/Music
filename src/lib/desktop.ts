import { convertFileSrc } from "@tauri-apps/api/core"

export const isDesktop = process.env.NEXT_PUBLIC_MUSIC_DESKTOP === "1"

export const usesDevProxy = isDesktop && process.env.NODE_ENV === "development"

export const DEFAULT_SERVER = "https://music.cornspies.com"

const SERVER_KEY = "music-server"

export function serverUrl(): string {
  if (!isDesktop) return ""
  if (usesDevProxy) return typeof window !== "undefined" ? window.location.origin : ""
  try {
    return localStorage.getItem(SERVER_KEY) || DEFAULT_SERVER
  } catch {
    return DEFAULT_SERVER
  }
}

export function setServerUrl(url: string): void {
  try {
    localStorage.setItem(SERVER_KEY, url)
  } catch {}
}

export const LINK_SCHEME = "com.cornspies.music"

export function appLink(url: string): string {
  return `${LINK_SCHEME}://open?url=${encodeURIComponent(url)}`
}

export function linkedUrl(link: string): URL | null {
  try {
    const parsed = new URL(link)
    if (parsed.protocol !== `${LINK_SCHEME}:`) return null
    const target = new URL(parsed.searchParams.get("url") ?? "")
    return target.protocol === "https:" || target.protocol === "http:" ? target : null
  } catch {
    return null
  }
}

export function appPathname(target: URL): string | null {
  if (usesDevProxy) return target.pathname
  try {
    const server = new URL(serverUrl())
    if (server.origin !== target.origin) return null
    const base = server.pathname.replace(/\/+$/, "")
    return base && target.pathname.startsWith(`${base}/`) ? target.pathname.slice(base.length) : target.pathname
  } catch {
    return null
  }
}

const localCovers = new Set<string>()

export function setLocalCover(id: string, present: boolean) {
  if (present) localCovers.add(id)
  else localCovers.delete(id)
}

export function resetLocalCovers(ids: string[]) {
  localCovers.clear()
  for (const id of ids) localCovers.add(id)
}

export function localCoverUrl(id: string): string | null {
  return isDesktop && localCovers.has(id) ? convertFileSrc(id, "music-cover") : null
}

export const SERVER_REFUSES_APP = "This server doesn't accept the desktop app yet. Update it to the latest version."

export async function serverAnswers(base: string): Promise<boolean> {
  try {
    await fetch(`${base}/api/auth/config`, { mode: "no-cors", cache: "no-store" })
    return true
  } catch {
    return false
  }
}

export function apiUrl(path: string): string {
  return isDesktop && path.startsWith("/") ? serverUrl() + path : path
}

export function publicUrl(path: string): string {
  if (isDesktop) return serverUrl() + path
  return typeof window !== "undefined" ? window.location.origin + path : path
}

export function normalizeServer(input: string): string | null {
  let s = input.trim()
  if (!s) return null
  if (!/^[a-z][a-z0-9+.-]*:\/\//i.test(s)) s = `https://${s}`
  try {
    const u = new URL(s)
    if ((u.protocol !== "http:" && u.protocol !== "https:") || !u.hostname) return null
    return `${u.protocol}//${u.host}${u.pathname.replace(/\/+$/, "")}`
  } catch {
    return null
  }
}

function isPrivateHost(host: string): boolean {
  const h = host.trim().replace(/^\[|\]$/g, "").toLowerCase()
  if (h === "localhost" || h.endsWith(".local") || h === "::1") return true
  if (h.startsWith("fc") || h.startsWith("fd") || h.startsWith("fe80:")) return true
  const parts = h.split(".")
  if (parts.length !== 4 || parts.some((p) => !/^\d+$/.test(p))) return false
  const [a, b] = parts.map(Number)
  return a === 10 || a === 127 || (a === 192 && b === 168) || (a === 172 && b >= 16 && b <= 31) || (a === 169 && b === 254)
}

export function insecureServerWarning(url: string): string | null {
  try {
    const u = new URL(url)
    if (u.protocol !== "http:" || isPrivateHost(u.hostname)) return null
  } catch {
    return null
  }
  return "Plain http:// outside your local network sends your password unencrypted. Use https://."
}
