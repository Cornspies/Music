"use client"

import { create } from "zustand"
import type { MediaToken } from "@/lib/types"
import { useAuthStore } from "@/stores/auth"

const REFRESH_MARGIN_MS = 10 * 60_000
const MIN_REFRESH_MS = 60_000

interface MediaTokenState {
  token: string | null
  expiresAt: number
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
        const res = await fetch("/api/auth/media-token", {
          headers: { Authorization: `Bearer ${session}` },
        })
        if (!res.ok) return
        const data = (await res.json()) as MediaToken
        const expiresAt = new Date(data.expiresAt).getTime()
        set({ token: data.token, expiresAt })
        scheduleRefresh(expiresAt)
      } catch {
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
 * The token for media URLs. Never the session token: a URL ends up in logs,
 * and the server refuses session tokens in one. Empty until the first media
 * token arrives; components that show media re-render then (useMediaToken).
 */
export function currentMediaToken(): string {
  const { token, expiresAt } = useMediaTokenStore.getState()
  return token && expiresAt > Date.now() ? token : ""
}

/** Re-renders with each new media token, so image URLs pick it up. */
export function useMediaToken(): string | null {
  return useMediaTokenStore((s) => s.token)
}

if (typeof window !== "undefined") {
  useAuthStore.subscribe((s, prev) => {
    if (s.token === prev.token) return
    if (!s.token) useMediaTokenStore.getState().clear()
    else useMediaTokenStore.getState().refresh().catch(() => {})
  })
}
