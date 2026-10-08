"use client"

import { create } from "zustand"
import type { Track } from "@/lib/types"
import type { TrackSort } from "@/stores/library"
import { routeToPath } from "@/lib/routes"

export type View =
  | "home"
  | "search"
  | "library"
  | "albums"
  | "artists"
  | "favorites"
  | "playlists"
  | "playlist"
  | "album"
  | "artist"
  | "offline"
  | "history"
  | "discover"
  | "duplicates"
  | "filetags"
  | "share"
  | "listen"
  | "admin"

export type PlaylistDialogState =
  | { mode: "create" }
  | { mode: "rename"; id: string; name: string }
  | null

interface UIState {
  view: View
  viewArg: string | null
  navigate: (view: View, arg?: string) => void
  applyRoute: (view: View, arg: string | null) => void
  uploadOpen: boolean
  setUploadOpen: (open: boolean) => void
  queueOpen: boolean
  setQueueOpen: (open: boolean) => void
  nowPlayingOpen: boolean
  setNowPlayingOpen: (open: boolean) => void
  shortcutsOpen: boolean
  setShortcutsOpen: (open: boolean) => void
  appsOpen: boolean
  setAppsOpen: (open: boolean) => void
  appSettingsOpen: boolean
  setAppSettingsOpen: (open: boolean) => void
  passwordOpen: boolean
  setPasswordOpen: (open: boolean) => void
  accountOpen: boolean
  setAccountOpen: (open: boolean) => void
  playbackSettingsOpen: boolean
  setPlaybackSettingsOpen: (open: boolean) => void
  sidebarOpen: boolean
  setSidebarOpen: (open: boolean) => void
  lyricsPanelOpen: boolean
  setLyricsPanelOpen: (open: boolean) => void
  editTrackId: string | null
  setEditTrackId: (id: string | null) => void
  metadataReviewTracks: Track[] | null
  setMetadataReviewTracks: (tracks: Track[] | null) => void
  shareTrack: Track | null
  setShareTrack: (track: Track | null) => void
  trimTrack: Track | null
  setTrimTrack: (track: Track | null) => void
  restoreTagsTrack: Track | null
  setRestoreTagsTrack: (track: Track | null) => void
  playlistDialog: PlaylistDialogState
  openPlaylistDialog: (dialog: NonNullable<PlaylistDialogState>) => void
  closePlaylistDialog: () => void
  pendingPlaylistTrack: Track | null
  setPendingPlaylistTrack: (track: Track | null) => void
  librarySort: TrackSort
  setLibrarySort: (sort: TrackSort) => void
}

const LYRICS_PANEL_PREF = "lyrics-panel"

export function readLyricsPanelPref(): boolean {
  try {
    return typeof localStorage !== "undefined" && localStorage.getItem(LYRICS_PANEL_PREF) === "1"
  } catch {
    return false
  }
}

export const useUIStore = create<UIState>()((set) => ({
  view: "home",
  viewArg: null,
  navigate: (view, arg) => {
    const next = arg ?? null
    set({ view, viewArg: next, sidebarOpen: false })
    if (typeof window !== "undefined") {
      const path = routeToPath(view, next)
      if (path !== window.location.pathname) {
        window.history.pushState({ view, viewArg: next }, "", path)
      }
    }
  },
  applyRoute: (view, viewArg) => set({ view, viewArg, sidebarOpen: false }),
  uploadOpen: false,
  setUploadOpen: (open) => set({ uploadOpen: open }),
  queueOpen: false,
  setQueueOpen: (open) => set({ queueOpen: open }),
  nowPlayingOpen: false,
  setNowPlayingOpen: (open) => set({ nowPlayingOpen: open }),
  shortcutsOpen: false,
  setShortcutsOpen: (open) => set({ shortcutsOpen: open }),
  appsOpen: false,
  setAppsOpen: (open) => set({ appsOpen: open }),
  appSettingsOpen: false,
  setAppSettingsOpen: (open) => set({ appSettingsOpen: open }),
  passwordOpen: false,
  setPasswordOpen: (open) => set({ passwordOpen: open }),
  accountOpen: false,
  setAccountOpen: (open) => set({ accountOpen: open }),
  playbackSettingsOpen: false,
  setPlaybackSettingsOpen: (open) => set({ playbackSettingsOpen: open }),
  sidebarOpen: false,
  setSidebarOpen: (open) => set({ sidebarOpen: open }),
  lyricsPanelOpen: false,
  setLyricsPanelOpen: (open) => {
    set({ lyricsPanelOpen: open })
    try {
      localStorage.setItem(LYRICS_PANEL_PREF, open ? "1" : "0")
    } catch {}
  },
  editTrackId: null,
  metadataReviewTracks: null,
  setMetadataReviewTracks: (tracks) => set({ metadataReviewTracks: tracks }),
  shareTrack: null,
  setShareTrack: (track) => set({ shareTrack: track }),
  trimTrack: null,
  setTrimTrack: (track) => set({ trimTrack: track }),
  restoreTagsTrack: null,
  setRestoreTagsTrack: (track) => set({ restoreTagsTrack: track }),
  setEditTrackId: (id) => set({ editTrackId: id }),
  playlistDialog: null,
  openPlaylistDialog: (dialog) => set({ playlistDialog: dialog }),
  closePlaylistDialog: () => set({ playlistDialog: null }),
  pendingPlaylistTrack: null,
  setPendingPlaylistTrack: (track) => set({ pendingPlaylistTrack: track }),
  librarySort: "plays",
  setLibrarySort: (librarySort) => set({ librarySort }),
}))
