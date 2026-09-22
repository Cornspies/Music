"use client"

// UI/navigation store. View state is the source of truth for rendering and is
// kept in sync with the address bar via the History API (see lib/routes.ts),
// so deep links and the browser back/forward buttons both work.

import { create } from "zustand"
import type { Track } from "@/lib/types"
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
  | "share"
  | "admin"

export type PlaylistDialogState =
  | { mode: "create" }
  | { mode: "rename"; id: string; name: string }
  | null

interface UIState {
  view: View
  /** playlist id / album key (JSON) / artist name, depending on view */
  viewArg: string | null
  navigate: (view: View, arg?: string) => void
  /** Applies a route that came from the URL (initial load / back / forward). */
  applyRoute: (view: View, arg: string | null) => void
  uploadOpen: boolean
  setUploadOpen: (open: boolean) => void
  queueOpen: boolean
  setQueueOpen: (open: boolean) => void
  /** full-screen now playing overlay */
  nowPlayingOpen: boolean
  setNowPlayingOpen: (open: boolean) => void
  /** keyboard shortcuts help dialog */
  shortcutsOpen: boolean
  setShortcutsOpen: (open: boolean) => void
  /** the "Android app" download dialog */
  androidAppOpen: boolean
  setAndroidAppOpen: (open: boolean) => void
  /** change password dialog */
  passwordOpen: boolean
  setPasswordOpen: (open: boolean) => void
  /** account settings: name, password, deleting the account */
  accountOpen: boolean
  setAccountOpen: (open: boolean) => void
  /** playback settings (crossfade, normalisation) popover */
  playbackSettingsOpen: boolean
  setPlaybackSettingsOpen: (open: boolean) => void
  sidebarOpen: boolean
  setSidebarOpen: (open: boolean) => void
  /** lyrics panel docked to the right of the main area (desktop) */
  lyricsPanelOpen: boolean
  setLyricsPanelOpen: (open: boolean) => void
  editTrackId: string | null
  setEditTrackId: (id: string | null) => void
  /** tracks open in the metadata review dialog (from a track's menu) */
  metadataReviewTracks: Track[] | null
  setMetadataReviewTracks: (tracks: Track[] | null) => void
  /** track open in the share-link dialog */
  shareTrack: Track | null
  setShareTrack: (track: Track | null) => void
  /** track open in the trim editor */
  trimTrack: Track | null
  setTrimTrack: (track: Track | null) => void
  playlistDialog: PlaylistDialogState
  openPlaylistDialog: (dialog: NonNullable<PlaylistDialogState>) => void
  closePlaylistDialog: () => void
  /** track to auto-add to a freshly created playlist */
  pendingPlaylistTrack: Track | null
  setPendingPlaylistTrack: (track: Track | null) => void
}

const LYRICS_PANEL_PREF = "lyrics-panel"

/** Whether the lyrics panel was open last time; applied after mount so the
 * server-rendered markup (panel closed) matches the first client render. */
export function readLyricsPanelPref(): boolean {
  try {
    return typeof localStorage !== "undefined" && localStorage.getItem(LYRICS_PANEL_PREF) === "1"
  } catch {
    return false
  }
}

export const useUIStore = create<UIState>()((set) => ({
  // Rendered on the server and on the first client paint, so it must not read
  // window here — the URL is applied on mount by useRouteSync.
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
  androidAppOpen: false,
  setAndroidAppOpen: (open) => set({ androidAppOpen: open }),
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
    } catch {
      // per-viewer convenience only
    }
  },
  editTrackId: null,
  metadataReviewTracks: null,
  setMetadataReviewTracks: (tracks) => set({ metadataReviewTracks: tracks }),
  shareTrack: null,
  setShareTrack: (track) => set({ shareTrack: track }),
  trimTrack: null,
  setTrimTrack: (track) => set({ trimTrack: track }),
  setEditTrackId: (id) => set({ editTrackId: id }),
  playlistDialog: null,
  openPlaylistDialog: (dialog) => set({ playlistDialog: dialog }),
  closePlaylistDialog: () => set({ playlistDialog: null }),
  pendingPlaylistTrack: null,
  setPendingPlaylistTrack: (track) => set({ pendingPlaylistTrack: track }),
}))
