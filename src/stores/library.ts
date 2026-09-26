"use client"

import { create } from "zustand"
import { toast } from "sonner"
import { api, fetchTrackChanges } from "@/lib/api"
import type { Playlist, Stats, Track, TrackPatch } from "@/lib/types"
import { usePlayerStore } from "@/stores/player"
import { useMediaTokenStore } from "@/stores/media-token"
import { useOfflineStore } from "@/stores/offline"

function isNetworkError(e: unknown): boolean {
  return e instanceof Error && e.message.startsWith("Network error")
}

export type TrackSort = "created" | "title" | "artist" | "plays" | "recent"

interface LibraryState {
  tracks: Track[]
  playlists: Playlist[]
  favorites: Track[]
  stats: Stats | null
  loading: boolean
  initialized: boolean
  playlistVersion: number
  fetchAll: () => Promise<void>
  fetchTracks: (sort?: TrackSort) => Promise<void>
  fetchPlaylists: () => Promise<void>
  fetchFavorites: () => Promise<void>
  fetchStats: () => Promise<void>
  toggleFavorite: (trackId: string) => Promise<void>
  createPlaylist: (name: string) => Promise<Playlist | null>
  deletePlaylist: (id: string) => Promise<boolean>
  renamePlaylist: (id: string, name: string) => Promise<boolean>
  publicPlaylists: Playlist[]
  fetchPublicPlaylists: () => Promise<void>
  setPlaylistPublic: (id: string, isPublic: boolean) => Promise<boolean>
  setPlaylistPinned: (id: string, pinned: boolean) => Promise<boolean>
  addTrackToPlaylist: (playlistId: string, trackId: string) => Promise<boolean>
  removePlaylistTrack: (playlistId: string, trackId: string) => Promise<void>
  deleteTrack: (id: string) => Promise<void>
  updateTrack: (id: string, patch: TrackPatch) => Promise<boolean>
  replaceTrack: (track: Track) => void
  followAnalysis: (trackId: string) => void
  reset: () => void
}

export const useLibraryStore = create<LibraryState>()((set, get) => ({
  tracks: [],
  playlists: [],
  publicPlaylists: [],
  favorites: [],
  stats: null,
  loading: false,
  initialized: false,
  playlistVersion: 0,

  fetchAll: async () => {
    set({ loading: true })
    await Promise.allSettled([useMediaTokenStore.getState().refresh(), useOfflineStore.getState().init()])
    const results = await Promise.allSettled([
      get().fetchTracks(),
      get().fetchPlaylists(),
      get().fetchPublicPlaylists(),
      get().fetchFavorites(),
      get().fetchStats(),
    ])
    const tracksFailed = results[0].status === "rejected" ? results[0].reason : null
    if (tracksFailed && isNetworkError(tracksFailed)) {
      const offline = useOfflineStore.getState()
      offline.setOffline(true)
      if (get().tracks.length === 0) set({ tracks: offline.tracks })
    } else if (!tracksFailed) {
      useOfflineStore.getState().setOffline(false)
    }
    set({ loading: false, initialized: true })
  },

  fetchTracks: async (sort: TrackSort = "created") => {
    const tracks = await api<Track[]>(`/api/tracks?sort=${sort}`)
    set({ tracks, initialized: true })
    useOfflineStore.getState().refreshFrom(tracks)
  },

  fetchPlaylists: async () => {
    const playlists = await api<Playlist[]>("/api/playlists")
    set({ playlists })
  },

  fetchPublicPlaylists: async () => {
    const publicPlaylists = await api<Playlist[]>("/api/playlists/public")
    set({ publicPlaylists })
  },

  setPlaylistPinned: async (id, pinned) => {
    try {
      await api(`/api/playlists/${id}/pin`, { method: pinned ? "POST" : "DELETE" })
      await Promise.allSettled([get().fetchPlaylists(), get().fetchPublicPlaylists()])
      return true
    } catch (e) {
      toast.error((e as Error).message)
      return false
    }
  },

  setPlaylistPublic: async (id, isPublic) => {
    try {
      const updated = await api<Playlist>(`/api/playlists/${id}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ isPublic }),
      })
      set({
        playlists: get().playlists.map((p) => (p.id === id ? updated : p)),
      })
      return true
    } catch (e) {
      toast.error((e as Error).message)
      return false
    }
  },

  fetchFavorites: async () => {
    const favorites = await api<Track[]>("/api/favorites")
    set({ favorites })
    useOfflineStore.getState().refreshFrom(favorites)
  },

  fetchStats: async () => {
    const stats = await api<Stats>("/api/stats")
    set({ stats })
  },

  toggleFavorite: async (trackId) => {
    const { tracks, favorites } = get()
    const track = tracks.find((t) => t.id === trackId)
    const wasFav = track ? track.isFavorite : favorites.some((t) => t.id === trackId)

    set({
      tracks: tracks.map((t) => (t.id === trackId ? { ...t, isFavorite: !wasFav } : t)),
      favorites: wasFav
        ? favorites.filter((t) => t.id !== trackId)
        : track
          ? [{ ...track, isFavorite: true }, ...favorites]
          : favorites,
    })
    const player = usePlayerStore.getState()
    const current = player.currentTrack
    if (current?.id === trackId) {
      player.replaceTrack({ ...current, isFavorite: !wasFav })
    }
    const offline = useOfflineStore.getState()
    const stored = offline.tracks.find((t) => t.id === trackId)
    if (stored) offline.replaceTrack({ ...stored, isFavorite: !wasFav })

    try {
      if (wasFav) {
        await api(`/api/favorites/${trackId}`, { method: "DELETE" })
      } else {
        await api(`/api/favorites/${trackId}`, { method: "POST" })
      }
    } catch (e) {
      set({
        tracks: get().tracks.map((t) => (t.id === trackId ? { ...t, isFavorite: wasFav } : t)),
        favorites: get().favorites,
      })
      if (stored) useOfflineStore.getState().replaceTrack({ ...stored, isFavorite: wasFav })
      toast.error((e as Error).message)
    }
  },

  createPlaylist: async (name) => {
    try {
      const playlist = await api<Playlist>("/api/playlists", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name }),
      })
      set({
        playlists: [playlist, ...get().playlists],
        playlistVersion: get().playlistVersion + 1,
      })
      return playlist
    } catch (e) {
      toast.error((e as Error).message)
      return null
    }
  },

  deletePlaylist: async (id) => {
    try {
      await api(`/api/playlists/${id}`, { method: "DELETE" })
      set({
        playlists: get().playlists.filter((p) => p.id !== id),
        playlistVersion: get().playlistVersion + 1,
      })
      return true
    } catch (e) {
      toast.error((e as Error).message)
      return false
    }
  },

  renamePlaylist: async (id, name) => {
    try {
      await api(`/api/playlists/${id}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name }),
      })
      set({
        playlists: get().playlists.map((p) => (p.id === id ? { ...p, name } : p)),
        playlistVersion: get().playlistVersion + 1,
      })
      return true
    } catch (e) {
      toast.error((e as Error).message)
      return false
    }
  },

  addTrackToPlaylist: async (playlistId, trackId) => {
    try {
      await api(`/api/playlists/${playlistId}/tracks`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ trackId }),
      })
      set({
        playlists: get().playlists.map((p) =>
          p.id === playlistId ? { ...p, trackCount: p.trackCount + 1 } : p
        ),
        playlistVersion: get().playlistVersion + 1,
      })
      return true
    } catch (e) {
      toast.error((e as Error).message)
      return false
    }
  },

  removePlaylistTrack: async (playlistId, trackId) => {
    try {
      await api(`/api/playlists/${playlistId}/tracks/${trackId}`, { method: "DELETE" })
      set({
        playlists: get().playlists.map((p) =>
          p.id === playlistId ? { ...p, trackCount: Math.max(0, p.trackCount - 1) } : p
        ),
        playlistVersion: get().playlistVersion + 1,
      })
    } catch (e) {
      toast.error((e as Error).message)
    }
  },

  deleteTrack: async (id) => {
    try {
      await api(`/api/tracks/${id}`, { method: "DELETE" })
    } catch (e) {
      toast.error((e as Error).message)
      return
    }
    const { tracks, favorites } = get()
    const removed = tracks.find((t) => t.id === id)
    set({
      tracks: tracks.filter((t) => t.id !== id),
      favorites: favorites.filter((t) => t.id !== id),
    })
    usePlayerStore.getState().handleTrackDeleted(id)
    useOfflineStore.getState().remove(id).catch(() => {})
    if (removed) {
      await Promise.allSettled([get().fetchStats(), get().fetchPlaylists()])
    }
  },

  replaceTrack: (updated) => {
    set({
      tracks: get().tracks.map((t) => (t.id === updated.id ? updated : t)),
      favorites: get().favorites.map((t) => (t.id === updated.id ? updated : t)),
    })
    usePlayerStore.getState().replaceTrack(updated)
    useOfflineStore.getState().replaceTrack(updated)
  },

  updateTrack: async (id, patch) => {
    try {
      const updated = await api<Track>(`/api/tracks/${id}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(patch),
      })
      set({
        tracks: get().tracks.map((t) => (t.id === id ? updated : t)),
        favorites: get().favorites.map((t) => (t.id === id ? updated : t)),
      })
      usePlayerStore.getState().replaceTrack(updated)
      useOfflineStore.getState().replaceTrack(updated)
      return true
    } catch (e) {
      toast.error((e as Error).message)
      return false
    }
  },

  followAnalysis: (trackId) => {
    let since = new Date(Date.now() - 60_000).toISOString().replace(/\.\d+Z$/, "Z")
    const delays = [1500, 3000, 5000, 10_000, 20_000]
    let step = 0
    const tick = async () => {
      try {
        const changes = await fetchTrackChanges(since)
        since = changes.now
        if (changes.tracks.length > 0) {
          const byID = new Map(changes.tracks.map((t) => [t.id, t]))
          const merge = (list: Track[]) => list.map((t) => byID.get(t.id) ?? t)
          set({ tracks: merge(get().tracks), favorites: merge(get().favorites) })
          for (const t of changes.tracks) {
            usePlayerStore.getState().replaceTrack(t)
            useOfflineStore.getState().replaceTrack(t)
          }
        }
        if (get().tracks.find((t) => t.id === trackId)?.hasWaveform) return
      } catch {}
      if (step < delays.length) setTimeout(tick, delays[step++])
    }
    setTimeout(tick, delays[step++])
  },

  reset: () => {
    set({
      tracks: [],
      playlists: [],
      publicPlaylists: [],
      favorites: [],
      stats: null,
      loading: false,
      initialized: false,
      playlistVersion: 0,
    })
  },
}))
