"use client"

import { lazy, Suspense, useEffect, useLayoutEffect, useRef, useState, type ComponentType, type ReactNode } from "react"
import { AudioLines, Home, Library, Menu, Search, Upload } from "lucide-react"
import { toast } from "sonner"
import { cn } from "@/lib/utils"
import { fetchSharedTrackForMember } from "@/lib/api"
import { readSidebarCollapsedPref, useUIStore, type View } from "@/stores/ui"
import { useAuthStore } from "@/stores/auth"
import { useLibraryStore } from "@/stores/library"
import { reattachBackground, restorePlayback, usePlayerStore } from "@/stores/player"
import { forgetListenInvite, pendingListenInvite, restoreListening, useListenStore } from "@/stores/listen"
import { ListenInviteDialog, ListenTogetherDialog } from "@/components/music/ListenTogetherDialog"
import { SidebarContent } from "@/components/music/Sidebar"
import { PlayerBar } from "@/components/music/PlayerBar"
import { QueuePanel } from "@/components/music/QueuePanel"
import { LyricsSidebar } from "@/components/music/LyricsSidebar"
import { NowPlayingView } from "@/components/music/NowPlayingView"
import { MetadataReviewDialog } from "@/components/music/MetadataReviewDialog"
import { PlaylistDialog } from "@/components/music/PlaylistDialog"
import { HomeView } from "@/components/music/views/HomeView"
import { SearchView } from "@/components/music/views/SearchView"
import { LibraryView } from "@/components/music/views/LibraryView"
import { AlbumView } from "@/components/music/views/AlbumView"
import { ArtistView } from "@/components/music/views/ArtistView"
import { CatalogView } from "@/components/music/views/CatalogView"
import { MixView } from "@/components/music/views/MixView"
import { PlaylistsView } from "@/components/music/views/PlaylistsView"
import { PlaylistView } from "@/components/music/views/PlaylistView"
import { FavoritesView } from "@/components/music/views/FavoritesView"
import { OfflineView } from "@/components/music/views/OfflineView"
import { HistoryView } from "@/components/music/views/HistoryView"
import { OfflineBanner } from "@/components/music/OfflineBanner"
import { Button } from "@/components/ui/button"
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet"
import { Skeleton } from "@/components/ui/skeleton"
import { useKeyboardShortcuts } from "@/hooks/use-keyboard-shortcuts"
import { useDesktopUpdates } from "@/hooks/use-desktop-updates"

function deferred<P extends object>(load: () => Promise<ComponentType<P>>) {
  return lazy(async () => ({ default: await load() }))
}

const DiscoverView = deferred(() => import("@/components/music/views/DiscoverView").then((m) => m.DiscoverView))
const DuplicatesView = deferred(() => import("@/components/music/views/DuplicatesView").then((m) => m.DuplicatesView))
const FileTagsView = deferred(() => import("@/components/music/views/FileTagsView").then((m) => m.FileTagsView))
const AdminView = deferred(() => import("@/components/music/views/AdminView").then((m) => m.AdminView))
const ShortcutsDialog = deferred(() => import("@/components/music/ShortcutsDialog").then((m) => m.ShortcutsDialog))
const AppsDialog = deferred(() => import("@/components/music/AppsDialog").then((m) => m.AppsDialog))
const AppSettingsDialog = deferred(() =>
  import("@/components/music/AppSettingsDialog").then((m) => m.AppSettingsDialog)
)
const AccountDialog = deferred(() => import("@/components/music/AccountDialog").then((m) => m.AccountDialog))
const PasswordDialog = deferred(() => import("@/components/music/PasswordDialog").then((m) => m.PasswordDialog))
const PlaybackSettingsDialog = deferred(() =>
  import("@/components/music/PlaybackSettingsDialog").then((m) => m.PlaybackSettingsDialog)
)
const UploadDialog = deferred(() => import("@/components/music/UploadDialog").then((m) => m.UploadDialog))
const EditTrackDialog = deferred(() => import("@/components/music/EditTrackDialog").then((m) => m.EditTrackDialog))
const TrimDialog = deferred(() => import("@/components/music/TrimDialog").then((m) => m.TrimDialog))
const RestoreTagsDialog = deferred(() => import("@/components/music/RestoreTagsDialog").then((m) => m.RestoreTagsDialog))
const ShareDialog = deferred(() => import("@/components/music/ShareDialog").then((m) => m.ShareDialog))

function WhenOpened({ open, children }: { open: boolean; children: ReactNode }) {
  const [opened, setOpened] = useState(open)
  if (open && !opened) setOpened(true)
  return opened ? <Suspense fallback={null}>{children}</Suspense> : null
}

function DeferredDialogs() {
  const shortcuts = useUIStore((s) => s.shortcutsOpen)
  const apps = useUIStore((s) => s.appsOpen)
  const appSettings = useUIStore((s) => s.appSettingsOpen)
  const account = useUIStore((s) => s.accountOpen)
  const password = useUIStore((s) => s.passwordOpen)
  const playbackSettings = useUIStore((s) => s.playbackSettingsOpen)
  const upload = useUIStore((s) => s.uploadOpen)
  const editTrack = useUIStore((s) => s.editTrackId !== null)
  const trim = useUIStore((s) => s.trimTrack !== null)
  const restoreTags = useUIStore((s) => s.restoreTagsTrack !== null)
  const share = useUIStore((s) => s.shareTrack !== null)

  return (
    <>
      <WhenOpened open={shortcuts}>
        <ShortcutsDialog />
      </WhenOpened>
      <WhenOpened open={apps}>
        <AppsDialog />
      </WhenOpened>
      <WhenOpened open={appSettings}>
        <AppSettingsDialog />
      </WhenOpened>
      <WhenOpened open={account}>
        <AccountDialog />
      </WhenOpened>
      <WhenOpened open={password}>
        <PasswordDialog />
      </WhenOpened>
      <WhenOpened open={playbackSettings}>
        <PlaybackSettingsDialog />
      </WhenOpened>
      <WhenOpened open={upload}>
        <UploadDialog />
      </WhenOpened>
      <WhenOpened open={editTrack}>
        <EditTrackDialog />
      </WhenOpened>
      <WhenOpened open={trim}>
        <TrimDialog />
      </WhenOpened>
      <WhenOpened open={restoreTags}>
        <RestoreTagsDialog />
      </WhenOpened>
      <WhenOpened open={share}>
        <ShareDialog />
      </WhenOpened>
    </>
  )
}

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
    case "share":
    case "listen":
      return <HomeView />
    case "search":
      return <SearchView />
    case "library":
      return <LibraryView />
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
    case "catalog":
      return <CatalogView />
    case "mix":
      return <MixView />
    case "duplicates":
      return <DuplicatesView />
    case "filetags":
      return <FileTagsView />
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
  const sidebarCollapsed = useUIStore((s) => s.sidebarCollapsed)

  const fetchAll = useLibraryStore((s) => s.fetchAll)
  const initialized = useLibraryStore((s) => s.initialized)

  const mainRef = useRef<HTMLElement>(null)

  useKeyboardShortcuts()
  useDesktopUpdates()
  useSharedTrackLink(view, viewArg, initialized)
  useListenInviteLink(view, viewArg)

  useEffect(() => {
    if (initialized) return
    const reattached = reattachBackground()
    fetchAll()
      .catch(() => {})
      .finally(async () => {
        if (await reattached) return
        if (!(await restoreListening())) restorePlayback()
      })
  }, [initialized, fetchAll])

  useEffect(() => {
    mainRef.current?.scrollTo({ top: 0 })
  }, [view, viewArg])

  useLayoutEffect(() => {
    if (readSidebarCollapsedPref()) useUIStore.setState({ sidebarCollapsed: true })
  }, [])

  return (
    <div className="flex h-dvh flex-col overflow-hidden bg-background text-foreground">
      <MobileHeader />
      <OfflineBanner />

      <div className="flex min-h-0 flex-1">
        <aside
          className={cn(
            "hidden shrink-0 flex-col border-r border-zinc-800/60 bg-zinc-950/70 transition-[width] duration-200 md:flex",
            sidebarCollapsed ? "w-[72px]" : "w-60"
          )}
        >
          <SidebarContent collapsed={sidebarCollapsed} collapsible />
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
            <div key={`${view}:${viewArg ?? ""}`} className="fade-up min-h-full">
              <Suspense fallback={null}>{renderView(view)}</Suspense>
            </div>
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
      <DeferredDialogs />
      <GlobalMetadataReview />
      <PlaylistDialog />
      <ListenTogetherDialog />
      <ListenInviteDialog />
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

function useListenInviteLink(view: View, code: string | null) {
  useEffect(() => {
    const invite = view === "listen" && code ? code : pendingListenInvite()
    if (!invite) return
    forgetListenInvite()
    if (view === "listen") {
      window.history.replaceState({ view: "home", viewArg: null }, "", "/")
      useUIStore.getState().applyRoute("home", null)
    }
    useListenStore.getState().setInvite(invite)
  }, [view, code])
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
