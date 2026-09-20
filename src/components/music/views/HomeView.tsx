"use client"

import { useEffect, useMemo } from "react"
import {
  Clock3,
  Disc3,
  Heart,
  History,
  ListMusic,
  Mic2,
  Music2,
  Play,
  Upload,
} from "lucide-react"
import { formatTotal } from "@/lib/format"
import type { Track } from "@/lib/types"
import { useLibraryStore } from "@/stores/library"
import { usePlayerStore } from "@/stores/player"
import { useUIStore } from "@/stores/ui"
import { CoverArt } from "@/components/music/CoverArt"
import { EmptyState } from "@/components/music/EmptyState"
import { TrackList } from "@/components/music/TrackList"
import { Button } from "@/components/ui/button"

function StatChip({
  icon: Icon,
  children,
}: {
  icon: typeof Music2
  children: React.ReactNode
}) {
  return (
    <span className="inline-flex shrink-0 items-center gap-1.5 rounded-full border border-zinc-800/90 bg-zinc-900/70 px-3 py-1 text-xs whitespace-nowrap text-zinc-300 shadow-sm transition-colors hover:border-zinc-700 hover:text-zinc-100">
      <Icon className="size-3.5 text-emerald-500/90" aria-hidden="true" />
      {children}
    </span>
  )
}

function SectionHeader({ title, action }: { title: string; action?: React.ReactNode }) {
  return (
    <div className="mb-3.5 mt-9 flex items-center justify-between gap-4">
      <h2 className="text-xl font-bold tracking-tight text-zinc-50">{title}</h2>
      {action}
    </div>
  )
}

function RecentCard({ track, queue }: { track: Track; queue: Track[] }) {
  return (
    <div
      role="button"
      tabIndex={0}
      aria-label={`Play ${track.title}`}
      onClick={() => usePlayerStore.getState().playNow(track, queue)}
      onKeyDown={(e) => {
        if (e.key === "Enter" && e.target === e.currentTarget) {
          usePlayerStore.getState().playNow(track, queue)
        }
      }}
      className="group w-36 shrink-0 cursor-pointer rounded-xl p-2 transition-all hover:bg-zinc-800/70 hover:shadow-lg hover:shadow-black/30 md:w-44"
    >
      <div className="relative">
        <div className="overflow-hidden rounded-lg">
          <div className="transition-transform duration-300 group-hover:scale-[1.03]">
            <CoverArt trackId={track.id} title={track.title} hasCover={track.hasCover} />
          </div>
        </div>
        <button
          type="button"
          aria-label={`Play ${track.title}`}
          onClick={(e) => {
            e.stopPropagation()
            usePlayerStore.getState().playNow(track, queue)
          }}
          className="absolute right-2 bottom-2 flex size-9 translate-y-1 items-center justify-center rounded-full bg-emerald-500 text-zinc-950 opacity-0 shadow-xl shadow-emerald-950/60 transition-all group-hover:translate-y-0 group-hover:opacity-100 hover:scale-105 hover:bg-emerald-400"
        >
          <Play className="size-4 translate-x-px fill-current" />
        </button>
      </div>
      <p className="mt-2.5 truncate text-sm font-semibold text-zinc-100">{track.title}</p>
      <p className="mt-0.5 truncate text-xs text-zinc-400">
        {track.artist || "Unknown artist"}
      </p>
    </div>
  )
}

function ShortcutCard({
  icon: Icon,
  label,
  sub,
  onClick,
}: {
  icon: typeof Heart
  label: string
  sub: string
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="group flex items-center gap-3 rounded-xl border border-zinc-800/80 bg-linear-to-br from-zinc-900/90 to-zinc-950/40 p-3 text-left shadow-sm transition-all hover:border-zinc-700 hover:from-zinc-800/90 hover:shadow-md hover:shadow-black/30 active:scale-[0.98]"
    >
      <div className="flex size-11 shrink-0 items-center justify-center rounded-lg bg-zinc-800 text-emerald-400 transition-all group-hover:bg-emerald-500 group-hover:text-zinc-950 group-hover:shadow-lg group-hover:shadow-emerald-950/60">
        <Icon className="size-5" aria-hidden="true" />
      </div>
      <div className="min-w-0">
        <p className="truncate text-sm font-semibold text-zinc-100">{label}</p>
        <p className="truncate text-xs text-zinc-500">{sub}</p>
      </div>
    </button>
  )
}

export function HomeView() {
  const stats = useLibraryStore((s) => s.stats)
  const tracks = useLibraryStore((s) => s.tracks)
  const playlists = useLibraryStore((s) => s.playlists)
  const navigate = useUIStore((s) => s.navigate)

  // Timestamps are RFC 3339 in UTC, so they order correctly as strings — no
  // Date parsing inside the comparator, and only when the list changes.
  const recent = useMemo(
    () => [...tracks].sort((a, b) => (b.createdAt < a.createdAt ? -1 : b.createdAt > a.createdAt ? 1 : 0)).slice(0, 8),
    [tracks]
  )

  // Recently played comes from the server's listening history, so it covers
  // every device; the local lastPlayedAt sort is the fallback for an older
  // server that doesn't send it.
  const serverRecent = stats?.recentlyPlayed
  const recentlyPlayed = useMemo(
    () =>
      serverRecent && serverRecent.length > 0
        ? serverRecent.slice(0, 8)
        : tracks
            .filter((t) => t.lastPlayedAt)
            .sort((a, b) => (b.lastPlayedAt! < a.lastPlayedAt! ? -1 : b.lastPlayedAt! > a.lastPlayedAt! ? 1 : 0))
            .slice(0, 6),
    [serverRecent, tracks]
  )

  // keep the history fresh: play events happen while Home is mounted
  useEffect(() => {
    const unsub = usePlayerStore.subscribe((state, prev) => {
      if (state.currentTrack?.id && state.currentTrack.id !== prev.currentTrack?.id) {
        useLibraryStore.getState().fetchStats().catch(() => {})
      }
    })
    return unsub
  }, [])

  if (stats && stats.trackCount === 0) {
    return (
      <div className="p-4 md:p-6">
        <div className="pt-10 md:pt-16">
          <EmptyState
            icon={Music2}
            title="Your library is empty"
            actionLabel="Upload music"
            onAction={() => useUIStore.getState().setUploadOpen(true)}
          />
        </div>
      </div>
    )
  }

  return (
    <div className="p-4 pb-16 md:p-6 md:pb-12">

      {stats && (
        <div className="mt-4 -mx-1 flex gap-2 overflow-x-auto px-1 pb-1 md:mx-0 md:flex-wrap md:overflow-visible md:px-0 md:pb-0">
          <StatChip icon={Music2}>
            {stats.trackCount} track{stats.trackCount === 1 ? "" : "s"}
          </StatChip>
          <StatChip icon={Clock3}>{formatTotal(stats.totalDuration)}</StatChip>
          <StatChip icon={Mic2}>
            {stats.artistCount} artist{stats.artistCount === 1 ? "" : "s"}
          </StatChip>
          <StatChip icon={Disc3}>
            {stats.albumCount} album{stats.albumCount === 1 ? "" : "s"}
          </StatChip>
          <StatChip icon={Heart}>
            {stats.favoriteCount} favorite{stats.favoriteCount === 1 ? "" : "s"}
          </StatChip>
          {stats.listenedSeconds > 0 ? (
            <button type="button" onClick={() => navigate("history")} className="contents">
              <StatChip icon={History}>{formatTotal(stats.listenedSeconds)} listened</StatChip>
            </button>
          ) : null}
        </div>
      )}

      {/* quick shortcuts */}
      <SectionHeader title="Jump back in" />
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <ShortcutCard
          icon={Heart}
          label="Favorites"
          sub="Your loved tracks"
          onClick={() => navigate("favorites")}
        />
        <ShortcutCard
          icon={ListMusic}
          label="Playlists"
          sub={`${playlists.length} playlist${playlists.length === 1 ? "" : "s"}`}
          onClick={() => navigate("playlists")}
        />
        <ShortcutCard
          icon={Upload}
          label="Upload"
          sub="Add new music"
          onClick={() => useUIStore.getState().setUploadOpen(true)}
        />
      </div>

      {recentlyPlayed.length > 0 && (
        <>
          <SectionHeader
            title="Recently played"
            action={
              <Button
                variant="ghost"
                size="sm"
                onClick={() => navigate("history")}
                className="text-xs text-zinc-400 hover:text-zinc-100"
              >
                Full history
              </Button>
            }
          />
          <div className="-mx-2 flex gap-2 overflow-x-auto px-1 pb-2">
            {recentlyPlayed.map((t) => (
              <RecentCard key={`rp-${t.id}`} track={t} queue={recentlyPlayed} />
            ))}
          </div>
        </>
      )}

      {recent.length > 0 && (
        <>
          <SectionHeader title="Recently added" />
          <div className="-mx-2 flex gap-2 overflow-x-auto px-1 pb-2">
            {recent.map((t) => (
              <RecentCard key={t.id} track={t} queue={recent} />
            ))}
          </div>
        </>
      )}

      {stats && stats.mostPlayed.length > 0 && (
        <>
          <SectionHeader
            title="Most played"
            action={
              <Button
                variant="ghost"
                size="sm"
                onClick={() => navigate("library")}
                className="text-xs text-zinc-400 hover:text-zinc-100"
              >
                See all
              </Button>
            }
          />
          <TrackList tracks={stats.mostPlayed.slice(0, 5)} limit={5} />
        </>
      )}

      <footer className="mt-12 flex items-center justify-center gap-2 border-t border-zinc-800/60 pt-5 text-[11px] text-zinc-600">
        <History className="size-3" aria-hidden="true" />
        Music · self-hosted · press
        <kbd className="rounded border border-zinc-800 bg-zinc-900 px-1 py-0.5 font-mono text-[10px] text-zinc-400">
          ?
        </kbd>
        for shortcuts
      </footer>
    </div>
  )
}
