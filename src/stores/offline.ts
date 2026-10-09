"use client"

import { create } from "zustand"
import { toast } from "sonner"
import type { Track } from "@/lib/types"
import {
  clearOffline,
  downloadTrackOffline,
  listOfflineTracks,
  offlineBytesUsed,
  offlineSupported,
  offlineAudioChanged,
  removeTrackOffline,
  requestPersistentStorage,
  updateOfflineTrack,
} from "@/lib/offline"

interface DownloadJob {
  loaded: number
  total: number
  controller: AbortController
}

interface OfflineState {
  supported: boolean
  tracks: Track[]
  ids: Set<string>
  bytesUsed: number
  jobs: Record<string, DownloadJob>
  offline: boolean
  initialized: boolean
  init: () => Promise<void>
  download: (track: Track, refresh?: boolean) => Promise<void>
  cancel: (id: string) => void
  remove: (id: string) => Promise<void>
  removeAll: () => Promise<void>
  setOffline: (offline: boolean) => void
  replaceTrack: (track: Track) => void
  refreshFrom: (tracks: Track[]) => void
}

const sameTrack = (a: Track, b: Track) =>
  (Object.keys({ ...a, ...b }) as (keyof Track)[]).every(
    (k) => k === "updatedAt" || JSON.stringify(a[k]) === JSON.stringify(b[k])
  )

export const useOfflineStore = create<OfflineState>()((set, get) => ({
  supported: offlineSupported(),
  tracks: [],
  ids: new Set(),
  bytesUsed: 0,
  jobs: {},
  offline: false,
  initialized: false,

  init: async () => {
    if (!get().supported) {
      set({ initialized: true })
      return
    }
    const [tracks, bytesUsed] = await Promise.all([listOfflineTracks(), offlineBytesUsed()])
    set({ tracks, ids: new Set(tracks.map((t) => t.id)), bytesUsed, initialized: true })
  },

  download: async (track, refresh = false) => {
    const { jobs, ids } = get()
    if (jobs[track.id] || (ids.has(track.id) && !refresh)) return
    const controller = new AbortController()
    set({ jobs: { ...get().jobs, [track.id]: { loaded: 0, total: 0, controller } } })
    if (Object.keys(get().jobs).length === 1) requestPersistentStorage().catch(() => {})
    try {
      await downloadTrackOffline(
        track,
        (loaded, total) => {
          const job = get().jobs[track.id]
          if (job) set({ jobs: { ...get().jobs, [track.id]: { ...job, loaded, total } } })
        },
        controller.signal
      )
      const [tracks, bytesUsed] = await Promise.all([listOfflineTracks(), offlineBytesUsed()])
      set({ tracks, ids: new Set(tracks.map((t) => t.id)), bytesUsed })
      if (!refresh) toast.success(`“${track.title}” is available offline`)
    } catch (e) {
      if ((e as Error).name !== "AbortError") {
        toast.error((e as Error).message || "Download failed")
      }
      await removeTrackOffline(track.id).catch(() => {})
      if (refresh) {
        const tracks = get().tracks.filter((t) => t.id !== track.id)
        set({ tracks, ids: new Set(tracks.map((t) => t.id)), bytesUsed: await offlineBytesUsed() })
      }
    } finally {
      const rest = { ...get().jobs }
      delete rest[track.id]
      set({ jobs: rest })
    }
  },

  cancel: (id) => {
    get().jobs[id]?.controller.abort()
  },

  remove: async (id) => {
    await removeTrackOffline(id)
    const tracks = get().tracks.filter((t) => t.id !== id)
    set({ tracks, ids: new Set(tracks.map((t) => t.id)), bytesUsed: await offlineBytesUsed() })
  },

  removeAll: async () => {
    for (const id of Object.keys(get().jobs)) get().cancel(id)
    await clearOffline()
    set({ tracks: [], ids: new Set(), bytesUsed: 0 })
  },

  setOffline: (offline) => {
    if (get().offline !== offline) set({ offline })
  },

  replaceTrack: (track) => {
    if (!get().ids.has(track.id)) return
    get().refreshFrom([track])
  },

  refreshFrom: (fresh) => {
    if (get().tracks.length === 0) return
    const byId = new Map(fresh.map((t) => [t.id, t]))
    const changed: Track[] = []
    const stale: Track[] = []
    const tracks = get().tracks.map((old) => {
      const track = byId.get(old.id)
      if (!track || sameTrack(old, track)) return old
      if (offlineAudioChanged(old, track)) stale.push(track)
      const next = { ...track, updatedAt: old.updatedAt }
      changed.push(next)
      return next
    })
    if (changed.length === 0) return
    set({ tracks })
    const staleIds = new Set(stale.map((t) => t.id))
    for (const track of changed) {
      if (!staleIds.has(track.id)) updateOfflineTrack(track).catch(() => {})
    }
    for (const track of stale) void get().download(track, true)
  },
}))

if (typeof window !== "undefined") {
  window.addEventListener("online", () => useOfflineStore.getState().setOffline(false))
  window.addEventListener("offline", () => useOfflineStore.getState().setOffline(true))
}
