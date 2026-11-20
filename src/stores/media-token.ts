"use client"

import { create } from "zustand"
import type { MediaToken } from "@/lib/types"
import { useAuthStore } from "@/stores/auth"
import { apiUrl } from "@/lib/desktop"
import { dropCoverCache } from "@/lib/covers"

const REFRESH_MARGIN_MS = 10 * 60_000
const MIN_REFRESH_MS = 60_000

interface MediaTokenState {
  token: string | null
  expiresAt: number
  refresh: () => Promise<void>
  ensure: () => Promise<void>
  adopt: (token: string, expiresAt: number) => void
  clear: () => void
}

let refreshTimer: ReturnType<typeof setTimeout> | null = null
let inflight: Promise<void> | null = null

function retrySoon() {
  scheduleRefresh(Date.now() + MIN_REFRESH_MS + REFRESH_MARGIN_MS)
}

function scheduleRefresh(expiresAt: number) {
  if (refreshTimer) clearTimeout(refreshTimer)
  const delay = Math.max(MIN_REFRESH_MS, expiresAt - Date.now() - REFRESH_MARGIN_MS)
  refreshTimer = setTimeout(() => {
    useMediaTokenStore.getState().refresh().catch(() => {})
  }, delay)
}

export const useMediaTokenStore = create<MediaTokenState>()((set, get) => ({
  token: null,
  expiresAt: 0,

  refresh: () => {
    if (inflight) return inflight
    inflight = (async () => {
      const session = useAuthStore.getState().token
      if (!session) return
      try {
        const res = await fetch(apiUrl("/api/auth/media-token"), {
          headers: { Authorization: `Bearer ${session}` },
        })
        if (!res.ok) {
          if (res.status !== 401) retrySoon()
          return
        }
        const data = (await res.json()) as MediaToken
        const expiresAt = new Date(data.expiresAt).getTime()
        set({ token: data.token, expiresAt })
        scheduleRefresh(expiresAt)
      } catch {
        retrySoon()
      } finally {
        inflight = null
      }
    })()
    return inflight
  },

  // A new token changes the address of every cover and avatar, so they would
  // all load again: only ask when there is none or it is about to run out.
  ensure: () => {
    if (inflight) return inflight
    const { token, expiresAt } = get()
    return !token || expiresAt - Date.now() < REFRESH_MARGIN_MS ? get().refresh() : Promise.resolve()
  },

  adopt: (token, expiresAt) => {
    set({ token, expiresAt })
    scheduleRefresh(expiresAt)
  },

  clear: () => {
    if (refreshTimer) clearTimeout(refreshTimer)
    refreshTimer = null
    set({ token: null, expiresAt: 0 })
  },
}))

export function currentMediaToken(): string {
  const { token, expiresAt } = useMediaTokenStore.getState()
  return token && expiresAt > Date.now() ? token : ""
}

export function useMediaToken(): string | null {
  return useMediaTokenStore((s) => s.token)
}

function refreshIfDue() {
  if (useAuthStore.getState().token) useMediaTokenStore.getState().ensure().catch(() => {})
}

// The account and token version a token was issued for. The server refuses a
// media token from before a password change, when the version went up.
function issuedFor(jwt: string | null): string | null {
  try {
    const payload = (jwt ?? "").split(".")[1].replace(/-/g, "+").replace(/_/g, "/")
    const claims = JSON.parse(atob(payload)) as { sub?: string; ver?: number }
    return `${claims.sub}:${claims.ver}`
  } catch {
    return null
  }
}

if (typeof window !== "undefined") {
  useAuthStore.subscribe((s, prev) => {
    if (s.token === prev.token) return
    if (!s.token) {
      useMediaTokenStore.getState().clear()
      dropCoverCache()
      return
    }
    // A session the server merely renewed keeps its media token.
    const session = issuedFor(s.token)
    if (!session || session !== issuedFor(useMediaTokenStore.getState().token)) {
      useMediaTokenStore.getState().refresh().catch(() => {})
    }
  })
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible") refreshIfDue()
  })
  window.addEventListener("online", refreshIfDue)
}
