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

export function currentMediaToken(): string {
  const { token, expiresAt } = useMediaTokenStore.getState()
  if (token && expiresAt > Date.now()) return token
  return useAuthStore.getState().token ?? ""
}

if (typeof window !== "undefined") {
  useAuthStore.subscribe((s, prev) => {
    if (s.token === prev.token) return
    if (!s.token) useMediaTokenStore.getState().clear()
    else useMediaTokenStore.getState().refresh().catch(() => {})
  })
}
