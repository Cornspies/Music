"use client"

// Offline store — which tracks are on this device, what's downloading, and
// whether we're currently cut off from the server. The bytes live in Cache
// Storage (see lib/offline.ts); this holds the bookkeeping the UI renders.

import { create } from "zustand"
import { toast } from "sonner"
import type { Track } from "@/lib/types"
import {
  clearOffline,
  downloadTrackOffline,
  listOfflineTracks,
  offlineBytesUsed,
  offlineSupported,
  removeTrackOffline,
  requestPersistentStorage,
} from "@/lib/offline"

interface DownloadJob {
  loaded: number
  total: number
  controller: AbortController
}

interface OfflineState {
  supported: boolean
  /** Downloaded tracks, newest first. */
  tracks: Track[]
  ids: Set<string>
  bytesUsed: number
  /** In-flight downloads keyed by track id. */
  jobs: Record<string, DownloadJob>
  /** True when the last server request failed with a network error. */
  offline: boolean
  initialized: boolean
  init: () => Promise<void>
  download: (track: Track) => Promise<void>
  cancel: (id: string) => void
  remove: (id: string) => Promise<void>
  removeAll: () => Promise<void>
  setOffline: (offline: boolean) => void
  /** Keeps stored metadata in step when a track is edited. */
  replaceTrack: (track: Track) => void
}

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

  download: async (track) => {
    const { jobs, ids } = get()
    if (jobs[track.id] || ids.has(track.id)) return
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
      toast.success(`“${track.title}” is available offline`)
    } catch (e) {
      if ((e as Error).name !== "AbortError") {
        toast.error((e as Error).message || "Download failed")
      }
      await removeTrackOffline(track.id).catch(() => {})
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
    set({ tracks: get().tracks.map((t) => (t.id === track.id ? { ...track, updatedAt: t.updatedAt } : t)) })
  },
}))

if (typeof window !== "undefined") {
  window.addEventListener("online", () => useOfflineStore.getState().setOffline(false))
  window.addEventListener("offline", () => useOfflineStore.getState().setOffline(true))
}
