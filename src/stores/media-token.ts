"use client"

// Media token — the short-lived, read-only token that goes into <audio>,
// <img> and download URLs instead of the session token. A URL someone copies
// out of the page (or that lands in a proxy log) is then worth a few hours of
// listening rather than the whole account.
//
// Kept in memory only: it is cheap to fetch and there is no reason to write
// it anywhere. Refreshed on a timer well before it expires, and once more
// whenever the session token changes.

import { create } from "zustand"
import type { MediaToken } from "@/lib/types"
import { gw } from "@/lib/gateway"
import { useAuthStore } from "@/stores/auth"

/** Refresh this long before the server's expiry. */
const REFRESH_MARGIN_MS = 10 * 60_000
/** Never poll faster than this even if the server hands out short tokens. */
const MIN_REFRESH_MS = 60_000

interface MediaTokenState {
  token: string | null
  expiresAt: number
  /** Fetches a fresh token; resolves whether or not it succeeded. */
  refresh: () => Promise<void>
  clear: () => void
}

let refreshTimer: ReturnType<typeof setTimeout> | null = null
let inflight: Promise<void> | null = null

function scheduleRefresh(expiresAt: number) {
  if (refreshTimer) clearTimeout(refreshTimer)
  const delay = Math.max(MIN_REFRESH_MS, expiresAt - Date.now() - REFRESH_MARGIN_MS)
  refreshTimer = setTimeout(() => {
    useMediaTokenStore.getState().refresh().catch(() => {})
  }, delay)
}

export const useMediaTokenStore = create<MediaTokenState>()((set) => ({
  token: null,
  expiresAt: 0,

  refresh: () => {
    if (inflight) return inflight
    inflight = (async () => {
      const session = useAuthStore.getState().token
      if (!session) return
      try {
        // Plain fetch rather than api(): a 401 here is handled by the normal
        // /api/auth/me check, and api() imports this module.
        const res = await fetch(gw("/api/auth/media-token"), {
          headers: { Authorization: `Bearer ${session}` },
        })
        if (!res.ok) return
        const data = (await res.json()) as MediaToken
        const expiresAt = new Date(data.expiresAt).getTime()
        set({ token: data.token, expiresAt })
        scheduleRefresh(expiresAt)
      } catch {
        // Offline or the server is down: keep what we have and try again in
        // a minute; downloaded tracks don't need it anyway.
        scheduleRefresh(Date.now() + MIN_REFRESH_MS + REFRESH_MARGIN_MS)
      } finally {
        inflight = null
      }
    })()
    return inflight
  },

  clear: () => {
    if (refreshTimer) clearTimeout(refreshTimer)
    refreshTimer = null
    set({ token: null, expiresAt: 0 })
  },
}))

/**
 * The token to put in a media URL right now. Falls back to the session token
 * only when no media token has been fetched yet, so a URL built during the
 * first render still works.
 */
export function currentMediaToken(): string {
  const { token, expiresAt } = useMediaTokenStore.getState()
  if (token && expiresAt > Date.now()) return token
  return useAuthStore.getState().token ?? ""
}

if (typeof window !== "undefined") {
  // Session changes (sign in, refresh, sign out) — the media token follows.
  useAuthStore.subscribe((s, prev) => {
    if (s.token === prev.token) return
    if (!s.token) useMediaTokenStore.getState().clear()
    else useMediaTokenStore.getState().refresh().catch(() => {})
  })
}
