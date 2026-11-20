"use client"

import { create } from "zustand"
import { toast } from "sonner"
import type { AvatarCrop } from "@/lib/api"
import {
  api,
  isUnreachable,
  fetchHome,
  fetchTrackChanges,
  pins as pinsApi,
  removePlaylistCover,
  uploadPlaylistCover,
} from "@/lib/api"
import type { HomeFeed, LibraryPin, Playlist, PlaylistAddResult, Stats, Track, TrackPatch } from "@/lib/types"
import { usePlayerStore } from "@/stores/player"
import { useMediaTokenStore } from "@/stores/media-token"
import { useOfflineStore } from "@/stores/offline"
import { loadUserPrefs } from "@/stores/ui"
import { noteCoverVersions } from "@/lib/covers"

export type TrackSort = "created" | "title" | "artist" | "plays" | "recent"

interface LibraryState {
  tracks: Track[]
  playlists: Playlist[]
  favorites: Track[]
  stats: Stats | null
  home: HomeFeed | null
  pins: LibraryPin[]
  loading: boolean
  initialized: boolean
  playlistVersion: number
  fetchAll: () => Promise<void>
  fetchTracks: (sort?: TrackSort) => Promise<void>
  syncTracks: () => Promise<void>
  fetchPlaylists: () => Promise<void>
  fetchFavorites: () => Promise<void>
  fetchStats: () => Promise<void>
  fetchHome: () => Promise<void>
  fetchPins: () => Promise<void>
  togglePin: (kind: LibraryPin["kind"], name: string, artist?: string) => Promise<void>
  toggleFavorite: (trackId: string) => Promise<void>
  createPlaylist: (name: string) => Promise<Playlist | null>
  deletePlaylist: (id: string) => Promise<boolean>
  renamePlaylist: (id: string, name: string) => Promise<boolean>
  publicPlaylists: Playlist[]
  fetchPublicPlaylists: () => Promise<void>
  setPlaylistPublic: (id: string, isPublic: boolean) => Promise<boolean>
  setPlaylistCover: (id: string, file: File | null, crop?: AvatarCrop) => Promise<boolean>
  setPlaylistPinned: (id: string, pinned: boolean) => Promise<boolean>
  addTrackToPlaylist: (playlistId: string, trackId: string) => Promise<boolean>
  addTracksToPlaylist: (playlistId: string, trackIds: string[]) => Promise<PlaylistAddResult | null>
  removePlaylistTrack: (playlistId: string, trackId: string) => Promise<void>
  deleteTrack: (id: string, into?: string) => Promise<boolean>
  updateTrack: (id: string, patch: TrackPatch) => Promise<boolean>
  addTrack: (track: Track) => void
  replaceTrack: (track: Track) => void
  replaceAudio: (track: Track) => void
  setUploaderAvatar: (userId: string, hasAvatar: boolean) => void
  followAnalysis: (trackId: string) => void
  reset: () => void
}

// The newest change the track list is known to include, by the server's clock.
// Empty while the list didn't come from the server.
let syncedAt = ""

// Changes are asked for from a little before that: a write that was under way
// may carry an earlier time, and a cover's colour is stored without a new one.
const SYNC_OVERLAP_MS = 60_000

function syncSince(): string {
  const at = new Date(syncedAt).getTime()
  return Number.isNaN(at) ? "" : new Date(at - SYNC_OVERLAP_MS).toISOString().replace(/\.\d+Z$/, "Z")
}

// Loads and syncs of the track list run one after another, so an older answer
// never lands on top of a newer one.
let listJobs: Promise<unknown> = Promise.resolve()

function inOrder<T>(job: () => Promise<T>): Promise<T> {
  const run = listJobs.then(job)
  listJobs = run.catch(() => {})
  return run
}

// The server goes on working on a track after it has answered (cover colour,
// loudness, waveform), so its answer is already a little behind what a reload
// would show. One sync shortly after the last change catches up.
const FOLLOW_UP_MS = 1500
let followUp: ReturnType<typeof setTimeout> | null = null

function syncSoon() {
  if (followUp) clearTimeout(followUp)
  followUp = setTimeout(() => {
    followUp = null
    useLibraryStore.getState().syncTracks().catch(() => {})
  }, FOLLOW_UP_MS)
}

// New tracks go where a reload lists them: newest first.
function withAdded(list: Track[], added: Track[]): Track[] {
  if (added.length === 0) return list
  const out = [...list]
  for (const track of added) {
    const at = out.findIndex((t) => t.createdAt < track.createdAt)
    out.splice(at === -1 ? out.length : at, 0, track)
  }
  return out
}

export const useLibraryStore = create<LibraryState>()((set, get) => ({
  tracks: [],
  playlists: [],
  publicPlaylists: [],
  favorites: [],
  stats: null,
  home: null,
  pins: [],
  loading: false,
  initialized: false,
  playlistVersion: 0,

  fetchAll: async () => {
    set({ loading: true })
    await Promise.allSettled([useMediaTokenStore.getState().ensure(), useOfflineStore.getState().init()])
    const results = await Promise.allSettled([
      get().fetchTracks(),
      get().fetchPlaylists(),
      get().fetchPublicPlaylists(),
      get().fetchFavorites(),
      get().fetchStats(),
      get().fetchPins(),
      usePlayerStore.getState().loadContextPrefs(),
      loadUserPrefs(),
    ])
    const tracksFailed = results[0].status === "rejected" ? results[0].reason : null
    if (tracksFailed && isUnreachable(tracksFailed)) {
      const offline = useOfflineStore.getState()
      offline.setOffline(true)
      if (get().tracks.length === 0) {
        set({ tracks: offline.tracks })
        syncedAt = ""
      }
    } else if (!tracksFailed) {
      useOfflineStore.getState().setOffline(false)
    }
    set({ loading: false, initialized: true })
  },

  fetchTracks: (sort: TrackSort = "created") =>
    inOrder(async () => {
      const tracks = await api<Track[]>(`/api/tracks?sort=${sort}`)
      set({ tracks, initialized: true })
      syncedAt = tracks.reduce((newest, t) => (t.updatedAt > newest ? t.updatedAt : newest), "")
      useOfflineStore.getState().refreshFrom(tracks)
    }),

  // Brings the track list to what a reload would give, asking only for what was
  // added, edited or deleted since it was last loaded or synced.
  syncTracks: async () => {
    const synced = await inOrder(async () => {
      const since = syncSince()
      if (!since) return false
      const changes = await fetchTrackChanges(since)
      // Signed out while the answer was on its way.
      if (!get().initialized) return true
      // The server's clock is behind the list it gave: it would not report what changes now.
      if (changes.now < since) return false
      syncedAt = changes.now

      const { tracks, favorites } = get()
      const have = new Map(tracks.map((t) => [t.id, t]))
      const changed = new Map<string, Track>()
      for (const t of changes.tracks) {
        if (JSON.stringify(have.get(t.id)) !== JSON.stringify(t)) changed.set(t.id, t)
      }
      const gone = new Set(changes.deleted.filter((id) => have.has(id)))
      if (changed.size === 0 && gone.size === 0) return true

      const merge = (list: Track[]) => list.filter((t) => !gone.has(t.id)).map((t) => changed.get(t.id) ?? t)
      const added = [...changed.values()].filter((t) => !have.has(t.id))
      set({ tracks: withAdded(merge(tracks), added), favorites: merge(favorites) })
      for (const t of changed.values()) {
        if (!have.has(t.id)) continue
        usePlayerStore.getState().replaceTrack(t)
        useOfflineStore.getState().replaceTrack(t)
      }
      // Favorites are listed in the order they were added, which only the server knows.
      const favorite = new Set(favorites.map((t) => t.id))
      if ([...changed.values()].some((t) => t.isFavorite !== favorite.has(t.id))) {
        await get().fetchFavorites().catch(() => {})
      }
      return true
    })
    // The list isn't the server's (it is the downloads, or was never loaded):
    // everything is loaded, unless that is already under way.
    if (!synced && get().initialized && !get().loading) await get().fetchAll()
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
      await api(`/api/playlists/${encodeURIComponent(id)}/pin`, { method: pinned ? "POST" : "DELETE" })
      await Promise.allSettled([get().fetchPlaylists(), get().fetchPublicPlaylists()])
      return true
    } catch (e) {
      toast.error((e as Error).message)
      return false
    }
  },

  setPlaylistPublic: async (id, isPublic) => {
    try {
      const updated = await api<Playlist>(`/api/playlists/${encodeURIComponent(id)}`, {
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

  setPlaylistCover: async (id, file, crop) => {
    try {
      const updated = file ? await uploadPlaylistCover(id, file, crop) : await removePlaylistCover(id)
      set({
        playlists: get().playlists.map((p) => (p.id === id ? updated : p)),
        playlistVersion: get().playlistVersion + 1,
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

  fetchHome: async () => {
    const home = await fetchHome()
    set({ home })
  },

  fetchPins: async () => {
    set({ pins: await pinsApi.list() })
  },

  togglePin: async (kind, name, artist = "") => {
    const key = (p: LibraryPin) => p.kind === kind && p.name === name && p.artist === (kind === "artist" ? "" : artist)
    const before = get().pins
    const pinned = before.some(key)
    set({
      pins: pinned
        ? before.filter((p) => !key(p))
        : [{ kind, name, artist: kind === "artist" ? "" : artist, pinnedAt: new Date().toISOString() }, ...before],
    })
    try {
      if (pinned) await pinsApi.remove(kind, name, artist)
      else await pinsApi.add(kind, name, artist)
    } catch (e) {
      set({ pins: before })
      toast.error((e as Error).message)
    }
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
      await api(`/api/playlists/${encodeURIComponent(id)}`, { method: "DELETE" })
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
      await api(`/api/playlists/${encodeURIComponent(id)}`, {
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
      await api(`/api/playlists/${encodeURIComponent(playlistId)}/tracks`, {
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

  addTracksToPlaylist: async (playlistId, trackIds) => {
    try {
      const r = await api<PlaylistAddResult>(`/api/playlists/${encodeURIComponent(playlistId)}/tracks`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ trackIds }),
      })
      set({
        playlists: get().playlists.map((p) =>
          p.id === playlistId ? { ...p, trackCount: p.trackCount + r.added } : p
        ),
        playlistVersion: get().playlistVersion + 1,
      })
      return r
    } catch (e) {
      toast.error((e as Error).message)
      return null
    }
  },

  removePlaylistTrack: async (playlistId, trackId) => {
    try {
      await api(`/api/playlists/${encodeURIComponent(playlistId)}/tracks/${encodeURIComponent(trackId)}`, { method: "DELETE" })
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

  deleteTrack: async (id, into) => {
    try {
      const query = into ? `?into=${encodeURIComponent(into)}` : ""
      await api(`/api/tracks/${id}${query}`, { method: "DELETE" })
    } catch (e) {
      toast.error((e as Error).message)
      return false
    }
    const { tracks, favorites } = get()
    const removed = tracks.find((t) => t.id === id)
    set({
      tracks: tracks.filter((t) => t.id !== id),
      favorites: favorites.filter((t) => t.id !== id),
    })
    usePlayerStore.getState().handleTrackDeleted(id)
    useOfflineStore.getState().remove(id).catch(() => {})
    if (into) {
      set({ playlistVersion: get().playlistVersion + 1 })
      await Promise.allSettled([get().fetchTracks(), get().fetchFavorites(), get().fetchStats(), get().fetchPlaylists()])
    } else if (removed) {
      await Promise.allSettled([get().fetchStats(), get().fetchPlaylists()])
    }
    return true
  },

  addTrack: (track) => {
    if (get().tracks.some((t) => t.id === track.id)) {
      get().replaceTrack(track)
      return
    }
    set({ tracks: withAdded(get().tracks, [track]) })
    syncSoon()
  },

  replaceTrack: (updated) => {
    set({
      tracks: get().tracks.map((t) => (t.id === updated.id ? updated : t)),
      favorites: get().favorites.map((t) => (t.id === updated.id ? updated : t)),
    })
    usePlayerStore.getState().replaceTrack(updated)
    useOfflineStore.getState().replaceTrack(updated)
    syncSoon()
  },

  // A track got another audio file. Its length may differ, and playlists and
  // the stats add the lengths up.
  replaceAudio: (track) => {
    get().replaceTrack(track)
    void Promise.allSettled([get().fetchStats(), get().fetchPlaylists(), get().fetchPublicPlaylists()])
  },

  // Tracks and playlists carry their owner's avatar flag; the server doesn't
  // count a new or removed avatar as a change to them.
  setUploaderAvatar: (userId, hasAvatar) => {
    const by = (t: Track) =>
      t.uploadedBy.id === userId && t.uploadedBy.hasAvatar !== hasAvatar
        ? { ...t, uploadedBy: { ...t.uploadedBy, hasAvatar } }
        : t
    const owned = (p: Playlist) =>
      p.owner.id === userId && p.owner.hasAvatar !== hasAvatar ? { ...p, owner: { ...p.owner, hasAvatar } } : p
    const { tracks, favorites, playlists, publicPlaylists, stats } = get()
    set({
      tracks: tracks.map(by),
      favorites: favorites.map(by),
      playlists: playlists.map(owned),
      publicPlaylists: publicPlaylists.map(owned),
      stats: stats && { ...stats, mostPlayed: stats.mostPlayed.map(by), recentlyPlayed: stats.recentlyPlayed.map(by) },
    })
    useOfflineStore.getState().refreshFrom(get().tracks)
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
    if (followUp) clearTimeout(followUp)
    followUp = null
    syncedAt = ""
    set({
      tracks: [],
      playlists: [],
      publicPlaylists: [],
      favorites: [],
      stats: null,
      home: null,
      pins: [],
      loading: false,
      initialized: false,
      playlistVersion: 0,
    })
  },
}))

useLibraryStore.subscribe((s, prev) => {
  if (s.tracks !== prev.tracks) noteCoverVersions(s.tracks)
})

export async function addTracksWithToast(playlist: { id: string; name: string }, tracks: Track[]): Promise<boolean> {
  const r = await useLibraryStore.getState().addTracksToPlaylist(
    playlist.id,
    tracks.map((t) => t.id)
  )
  if (!r) return false
  if (r.added === 0) {
    toast(tracks.length === 1 ? `Already in “${playlist.name}”` : `All of these are already in “${playlist.name}”`)
  } else {
    toast.success(
      r.added === 1 && tracks.length === 1
        ? `Added “${tracks[0].title}” to ${playlist.name}`
        : `Added ${r.added} ${r.added === 1 ? "song" : "songs"} to ${playlist.name}`,
      { description: r.skipped > 0 ? `${r.skipped} already there` : undefined }
    )
  }
  return true
}
