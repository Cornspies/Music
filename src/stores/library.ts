"use client"

// Library store — tracks, playlists, favorites, stats. All data via api().

import { create } from "zustand"
import { toast } from "sonner"
import { api } from "@/lib/api"
import type { Playlist, Stats, Track, TrackPatch } from "@/lib/types"
import { usePlayerStore } from "@/stores/player"
import { useMediaTokenStore } from "@/stores/media-token"
import { useOfflineStore } from "@/stores/offline"

/** True for the error api() throws when the server can't be reached at all. */
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
  /** bumped on any playlist mutation so detail views can refetch */
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
  /** Playlists other users have shared publicly. */
  publicPlaylists: Playlist[]
  fetchPublicPlaylists: () => Promise<void>
  /** Share or unshare one of your own playlists. */
  setPlaylistPublic: (id: string, isPublic: boolean) => Promise<boolean>
  /** Pin or unpin someone else's public playlist to your sidebar. */
  setPlaylistPinned: (id: string, pinned: boolean) => Promise<boolean>
  addTrackToPlaylist: (playlistId: string, trackId: string) => Promise<boolean>
  removePlaylistTrack: (playlistId: string, trackId: string) => Promise<void>
  deleteTrack: (id: string) => Promise<void>
  updateTrack: (id: string, patch: TrackPatch) => Promise<boolean>
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
    // The media token goes into every cover/stream URL, so it has to be in
    // hand before anything that renders one. Downloads are read in parallel:
    // they're what the library falls back to when the server is unreachable.
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
      // Offline: show what's on this device rather than an empty library.
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
      // The sidebar list is "mine + pinned", so a pin adds an entry that
      // wasn't there at all — refetch rather than patching in place.
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
  },

  fetchStats: async () => {
    const stats = await api<Stats>("/api/stats")
    set({ stats })
  },

  toggleFavorite: async (trackId) => {
    const { tracks, favorites } = get()
    const track = tracks.find((t) => t.id === trackId)
    const wasFav = track ? track.isFavorite : favorites.some((t) => t.id === trackId)

    // optimistic update everywhere
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

    try {
      if (wasFav) {
        await api(`/api/favorites/${trackId}`, { method: "DELETE" })
      } else {
        await api(`/api/favorites/${trackId}`, { method: "POST" })
      }
    } catch (e) {
      // revert
      set({
        tracks: get().tracks.map((t) => (t.id === trackId ? { ...t, isFavorite: wasFav } : t)),
        favorites: get().favorites,
      })
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
      // refresh aggregates (stats, playlists) in the background
      await Promise.allSettled([get().fetchStats(), get().fetchPlaylists()])
    }
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
