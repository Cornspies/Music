"use client"

import { useEffect, useRef } from "react"
import { AnimatePresence, motion } from "framer-motion"
import { AudioLines, Home, Library, Menu, Search, Upload } from "lucide-react"
import { toast } from "sonner"
import { cn } from "@/lib/utils"
import { fetchSharedTrackForMember } from "@/lib/api"
import { useUIStore, type View } from "@/stores/ui"
import { useAuthStore } from "@/stores/auth"
import { useLibraryStore } from "@/stores/library"
import { restorePlayback, usePlayerStore } from "@/stores/player"
import { SidebarContent } from "@/components/music/Sidebar"
import { PlayerBar } from "@/components/music/PlayerBar"
import { QueuePanel } from "@/components/music/QueuePanel"
import { LyricsSidebar } from "@/components/music/LyricsSidebar"
import { NowPlayingView } from "@/components/music/NowPlayingView"
import { AccountDialog } from "@/components/music/AccountDialog"
import { AndroidAppDialog } from "@/components/music/AndroidAppDialog"
import { ShortcutsDialog } from "@/components/music/ShortcutsDialog"
import { PasswordDialog } from "@/components/music/PasswordDialog"
import { UploadDialog } from "@/components/music/UploadDialog"
import { EditTrackDialog } from "@/components/music/EditTrackDialog"
import { MetadataReviewDialog } from "@/components/music/MetadataReviewDialog"
import { ShareDialog } from "@/components/music/ShareDialog"
import { TrimDialog } from "@/components/music/TrimDialog"
import { PlaylistDialog } from "@/components/music/PlaylistDialog"
import { HomeView } from "@/components/music/views/HomeView"
import { SearchView } from "@/components/music/views/SearchView"
import { LibraryView } from "@/components/music/views/LibraryView"
import { AlbumsView } from "@/components/music/views/AlbumsView"
import { ArtistsView } from "@/components/music/views/ArtistsView"
import { AlbumView } from "@/components/music/views/AlbumView"
import { ArtistView } from "@/components/music/views/ArtistView"
import { PlaylistsView } from "@/components/music/views/PlaylistsView"
import { PlaylistView } from "@/components/music/views/PlaylistView"
import { FavoritesView } from "@/components/music/views/FavoritesView"
import { OfflineView } from "@/components/music/views/OfflineView"
import { HistoryView } from "@/components/music/views/HistoryView"
import { DiscoverView } from "@/components/music/views/DiscoverView"
import { DuplicatesView } from "@/components/music/views/DuplicatesView"
import { AdminView } from "@/components/music/views/AdminView"
import { OfflineBanner } from "@/components/music/OfflineBanner"
import { PlaybackSettingsDialog } from "@/components/music/PlaybackSettingsDialog"
import { Button } from "@/components/ui/button"
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet"
import { Skeleton } from "@/components/ui/skeleton"
import { useKeyboardShortcuts } from "@/hooks/use-keyboard-shortcuts"

function MobileHeader() {
  const setSidebarOpen = useUIStore((s) => s.setSidebarOpen)
  const setUploadOpen = useUIStore((s) => s.setUploadOpen)

  return (
    <header className="flex h-14 shrink-0 items-center justify-between border-b border-zinc-800/60 bg-zinc-950 px-4 md:hidden">
      <Button
        variant="ghost"
        size="icon"
        aria-label="Open navigation"
        onClick={() => setSidebarOpen(true)}
        className="size-10 text-zinc-300"
      >
        <Menu className="size-5" />
      </Button>
      <div className="flex items-center gap-2">
        <div className="flex size-8 items-center justify-center rounded-lg bg-linear-to-br from-emerald-400 to-teal-600">
          <AudioLines className="size-4.5 text-zinc-950" aria-hidden="true" />
        </div>
        <span className="text-lg font-bold tracking-tight">Music</span>
      </div>
      <Button
        variant="ghost"
        size="icon"
        aria-label="Upload music"
        onClick={() => setUploadOpen(true)}
        className="size-10 text-zinc-300"
      >
        <Upload className="size-5" />
      </Button>
    </header>
  )
}

const TABS: { view: View; label: string; icon: typeof Home }[] = [
  { view: "home", label: "Home", icon: Home },
  { view: "search", label: "Search", icon: Search },
  { view: "library", label: "Library", icon: Library },
]

function MobileTabBar() {
  const view = useUIStore((s) => s.view)
  const navigate = useUIStore((s) => s.navigate)

  return (
    <nav
      aria-label="Primary"
      className="flex h-12 shrink-0 items-stretch border-t border-zinc-800/70 bg-zinc-950 md:hidden"
    >
      {TABS.map((tab) => {
        const active = view === tab.view
        return (
          <button
            key={tab.view}
            type="button"
            onClick={() => navigate(tab.view)}
            aria-current={active ? "page" : undefined}
            className={cn(
              "flex flex-1 flex-col items-center justify-center gap-0.5 text-[10px] font-medium transition-colors",
              active ? "text-emerald-400" : "text-zinc-500"
            )}
          >
            <tab.icon className="size-5" aria-hidden="true" />
            {tab.label}
          </button>
        )
      })}
    </nav>
  )
}

function renderView(view: View) {
  switch (view) {
    case "home":
    case "share": // a share link: home, while the track starts in the player
      return <HomeView />
    case "search":
      return <SearchView />
    case "library":
      return <LibraryView />
    case "albums":
      return <AlbumsView />
    case "artists":
      return <ArtistsView />
    case "favorites":
      return <FavoritesView />
    case "playlists":
      return <PlaylistsView />
    case "playlist":
      return <PlaylistView />
    case "album":
      return <AlbumView />
    case "artist":
      return <ArtistView />
    case "offline":
      return <OfflineView />
    case "history":
      return <HistoryView />
    case "discover":
      return <DiscoverView />
    case "duplicates":
      return <DuplicatesView />
    case "admin":
      return <AdminView />
  }
}

function LibrarySkeleton() {
  return (
    <div className="flex flex-col gap-5 p-4 md:p-6">
      <Skeleton className="h-8 w-48 rounded-lg" />
      <div className="flex gap-4 overflow-hidden">
        {Array.from({ length: 5 }).map((_, i) => (
          <Skeleton key={i} className="size-36 shrink-0 rounded-xl md:size-44" />
        ))}
      </div>
      <Skeleton className="h-6 w-36 rounded-md" />
      {Array.from({ length: 6 }).map((_, i) => (
        <Skeleton key={i} className="h-14 w-full rounded-lg" />
      ))}
    </div>
  )
}

export function AppShell() {
  const view = useUIStore((s) => s.view)
  const viewArg = useUIStore((s) => s.viewArg)
  const sidebarOpen = useUIStore((s) => s.sidebarOpen)
  const setSidebarOpen = useUIStore((s) => s.setSidebarOpen)

  const fetchAll = useLibraryStore((s) => s.fetchAll)
  const initialized = useLibraryStore((s) => s.initialized)

  const mainRef = useRef<HTMLElement>(null)

  useKeyboardShortcuts()
  useSharedTrackLink(view, viewArg, initialized)

  useEffect(() => {
    if (initialized) return
    fetchAll()
      .catch(() => {})
      .finally(() => {
        restorePlayback()
      })
  }, [initialized, fetchAll])

  useEffect(() => {
    mainRef.current?.scrollTo({ top: 0 })
  }, [view, viewArg])

  return (
    <div className="flex h-dvh flex-col overflow-hidden bg-background text-foreground">
      <MobileHeader />
      <OfflineBanner />

      <div className="flex min-h-0 flex-1">
        <aside className="hidden w-60 shrink-0 flex-col border-r border-zinc-800/60 bg-zinc-950/70 md:flex">
          <SidebarContent />
        </aside>

        <Sheet open={sidebarOpen} onOpenChange={setSidebarOpen}>
          <SheetContent
            side="left"
            className="w-72 gap-0 border-zinc-800/70 bg-zinc-950 p-0 [&>button:last-child]:text-zinc-400"
          >
            <SheetTitle className="sr-only">Navigation</SheetTitle>
            <SidebarContent />
          </SheetContent>
        </Sheet>

        <main ref={mainRef} className="relative min-w-0 flex-1 overflow-y-auto">
          {!initialized ? (
            <LibrarySkeleton />
          ) : (
            <AnimatePresence mode="wait" initial={false}>
              <motion.div
                key={`${view}:${viewArg ?? ""}`}
                initial={{ opacity: 0, y: 6 }}
                animate={{ opacity: 1, y: 0 }}
                exit={{ opacity: 0, y: -6 }}
                transition={{ duration: 0.15, ease: "easeOut" }}
                className="min-h-full"
              >
                {renderView(view)}
              </motion.div>
            </AnimatePresence>
          )}
        </main>

        <LyricsSidebar />
      </div>

      <div className="shrink-0">
        <MobileTabBar />
        <PlayerBar />
      </div>

      <QueuePanel />
      <NowPlayingView />
      <ShortcutsDialog />
      <AndroidAppDialog />
      <AccountDialog />
      <PasswordDialog />
      <PlaybackSettingsDialog />
      <UploadDialog />
      <EditTrackDialog />
      <GlobalMetadataReview />
      <TrimDialog />
      <ShareDialog />
      <PlaylistDialog />
    </div>
  )
}

function GlobalMetadataReview() {
  const tracks = useUIStore((s) => s.metadataReviewTracks)
  return (
    <MetadataReviewDialog
      tracks={tracks ?? []}
      open={tracks !== null}
      onOpenChange={(o) => {
        if (!o) useUIStore.getState().setMetadataReviewTracks(null)
      }}
    />
  )
}

function useSharedTrackLink(view: View, token: string | null, ready: boolean) {
  useEffect(() => {
    if (view !== "share" || !token || !ready) return
    let alive = true
    const leaveLink = () => {
      window.history.replaceState({ view: "home", viewArg: null }, "", "/")
      useUIStore.getState().applyRoute("home", null)
    }
    fetchSharedTrackForMember(token)
      .then((track) => {
        if (!alive) return
        leaveLink()
        usePlayerStore.getState().playNow(track, [track])
        setTimeout(() => {
          const player = usePlayerStore.getState()
          if (player.currentTrack?.id === track.id && !player.isPlaying) {
            toast(`“${track.title}” is ready`, {
              action: { label: "Play", onClick: () => usePlayerStore.getState().togglePlay() },
            })
          }
        }, 1200)
      })
      .catch((e: Error) => {
        if (!alive || !useAuthStore.getState().token) return
        leaveLink()
        toast.error(e.message)
      })
    return () => {
      alive = false
    }
  }, [view, token, ready])
}
