"use client"

import { create } from "zustand"

// Set when the reverse proxy answers instead of the server: a maintenance
// notice (deploy/nginx/music.conf), or a 502/504 while the server is down.
export interface ServerDown {
  maintenance: boolean
  until?: string
  message?: string
}

interface ServerStatusState {
  down: ServerDown | null
  setDown: (down: ServerDown | null) => void
}

export const useServerStatusStore = create<ServerStatusState>()((set, get) => ({
  down: null,
  setDown: (down) => {
    if (JSON.stringify(get().down) !== JSON.stringify(down)) set({ down })
  },
}))

// The proxy's answer, or null when the server itself answered: the server
// also sends 502 and 503 (an upstream source failing), always as {"error"}.
export function serverDownFrom(status: number, data: unknown): ServerDown | null {
  const body = (data ?? {}) as { maintenance?: unknown; until?: unknown; message?: unknown; error?: unknown }
  if (status === 503 && body.maintenance === true) {
    return {
      maintenance: true,
      until: typeof body.until === "string" && body.until ? body.until : undefined,
      message: typeof body.message === "string" && body.message ? body.message : undefined,
    }
  }
  if ((status === 502 || status === 504) && typeof body.error !== "string") return { maintenance: false }
  return null
}

// "21:30", or "Saturday, Oct 24, 21:30" when it isn't today.
export function backAround(until: string | undefined): string | null {
  if (!until) return null
  const at = new Date(until)
  if (Number.isNaN(at.getTime())) return null
  const time = at.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" })
  if (at.toDateString() === new Date().toDateString()) return time
  return `${at.toLocaleDateString([], { weekday: "long", month: "short", day: "numeric" })}, ${time}`
}

export function serverDownMessage(down: ServerDown): string {
  if (!down.maintenance) return "Can't reach the server right now."
  const at = backAround(down.until)
  if (!at) return "Music is down for maintenance."
  return new Date(down.until!).getTime() > Date.now()
    ? `Music is down for maintenance, back around ${at}.`
    : "Music is down for maintenance, back any moment."
}
