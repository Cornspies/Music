"use client"

import { useEffect, useMemo, useState } from "react"
import { Clock3, Disc3, Heart, History, ListMusic, Mic2, Music2, Play } from "lucide-react"
import { cn } from "@/lib/utils"
import { formatTotal } from "@/lib/format"
import type { Track } from "@/lib/types"
import { useLibraryStore } from "@/stores/library"
import { usePlayerStore } from "@/stores/player"
import { useUIStore } from "@/stores/ui"
import { CoverArt } from "@/components/music/CoverArt"
import { PlaylistCoverImage } from "@/components/music/PlaylistCover"
import { EmptyState } from "@/components/music/EmptyState"
import { TrackList } from "@/components/music/TrackList"
import { TrackMenu } from "@/components/music/TrackMenu"
import { ArtistLinks } from "@/components/music/ArtistLinks"
import { useContextMenu } from "@/components/music/MenuAtPoint"
import { ArtistCircle, IconArt, MixCard, MixCover, QuickTile, Shelf, useTracksById } from "@/components/music/HomeCards"
import { Button } from "@/components/ui/button"

const HOME_RETRY_MS = 5000
const HOME_RETRIES = 4

function StatChip({ icon: Icon, children }: { icon: typeof Music2; children: React.ReactNode }) {
  return (
    <span className="inline-flex shrink-0 items-center gap-1.5 rounded-full border border-zinc-800/90 bg-zinc-900/70 px-3 py-1 text-xs whitespace-nowrap text-zinc-400 transition-colors hover:border-zinc-700 hover:text-zinc-100">
      <Icon className="size-3.5 text-emerald-500/90" aria-hidden="true" />
      {children}
    </span>
  )
}

function greeting(): string {
  const h = new Date().getHours()
  if (h < 5) return "Good night"
  if (h < 12) return "Good morning"
  if (h < 18) return "Good afternoon"
  return "Good evening"
}

function TrackCard({ track, queue }: { track: Track; queue: Track[] }) {
  const menu = useContextMenu()
  const play = () => usePlayerStore.getState().playNow(track, queue)
  return (
    <div
      role="button"
      tabIndex={0}
      aria-label={`Play ${track.title}`}
      onClick={play}
      onContextMenu={menu.onContextMenu}
      onKeyDown={(e) => {
        if (e.key === "Enter" && e.target === e.currentTarget) play()
      }}
      className="group w-36 shrink-0 cursor-pointer snap-start rounded-xl p-2 transition-colors hover:bg-zinc-800/70 md:w-44"
    >
      <div className="relative">
        <div className="overflow-hidden rounded-lg">
          <div className="transition-transform duration-300 group-hover:scale-[1.03]">
            <CoverArt
              trackId={track.id}
              title={track.title}
              hasCover={track.hasCover}
              size={256}
              sizes="(min-width: 768px) 160px, 128px"
            />
          </div>
        </div>
        <button
          type="button"
          aria-label={`Play ${track.title}`}
          onClick={(e) => {
            e.stopPropagation()
            play()
          }}
          className="absolute right-2 bottom-2 flex size-10 translate-y-1 items-center justify-center rounded-full bg-emerald-500 text-zinc-950 opacity-0 shadow-xl shadow-black/50 transition-all group-hover:translate-y-0 group-hover:opacity-100 hover:scale-105 hover:bg-emerald-400 [@media(hover:none)]:hidden"
        >
          <Play className="size-4 translate-x-px fill-current" />
        </button>
      </div>
      <div className="mt-2 flex items-start gap-1">
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-semibold text-zinc-100">{track.title}</p>
          <p className="mt-0.5 truncate text-xs text-zinc-400">
            <ArtistLinks credit={track.artist} />
          </p>
        </div>
        <TrackMenu
          track={track}
          triggerClassName="-mr-1 size-7 opacity-0 transition-opacity focus-visible:opacity-100 group-hover:opacity-100 data-[state=open]:opacity-100 [@media(hover:none)]:opacity-100"
          open={menu.open}
          onOpenChange={menu.onOpenChange}
          point={menu.point}
        />
      </div>
    </div>
  )
}

function TrackShelf({
  title,
  subtitle,
  tracks,
  action,
}: {
  title: string
  subtitle?: React.ReactNode
  tracks: Track[]
  action?: React.ReactNode
}) {
  if (tracks.length === 0) return null
  return (
    <Shelf title={title} subtitle={subtitle} action={action}>
      {tracks.map((t) => (
        <TrackCard key={t.id} track={t} queue={tracks} />
      ))}
    </Shelf>
  )
}

function SegmentToggle<T extends string>({
  value,
  options,
  onChange,
  label,
}: {
  value: T
  options: { value: T; label: string; disabled?: boolean }[]
  onChange: (v: T) => void
  label: string
}) {
  return (
    <div role="radiogroup" aria-label={label} className="mr-1 flex rounded-full bg-zinc-900 p-0.5">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={value === o.value}
          disabled={o.disabled}
          onClick={() => onChange(o.value)}
          className={cn(
            "rounded-full px-3 py-1 text-xs font-medium transition-colors disabled:opacity-40",
            value === o.value ? "bg-zinc-100 text-zinc-900" : "text-zinc-400 hover:text-zinc-100"
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

function listing(names: string[]): string {
  if (names.length <= 1) return names[0] ?? ""
  return `${names.slice(0, -1).join(", ")} and ${names[names.length - 1]}`
}

export function HomeView() {
  const stats = useLibraryStore((s) => s.stats)
  const tracks = useLibraryStore((s) => s.tracks)
  const favorites = useLibraryStore((s) => s.favorites)
  const playlists = useLibraryStore((s) => s.playlists)
  const home = useLibraryStore((s) => s.home)
  const navigate = useUIStore((s) => s.navigate)
  const [similarSpan, setSimilarSpan] = useState<"week" | "month" | null>(null)

  useEffect(() => {
    let tries = 0
    let timer: ReturnType<typeof setTimeout> | null = null
    let alive = true
    const load = () => {
      useLibraryStore
        .getState()
        .fetchHome()
        .then(() => {
          if (alive && useLibraryStore.getState().home?.pending && tries++ < HOME_RETRIES) {
            timer = setTimeout(load, HOME_RETRY_MS)
          }
        })
        .catch(() => {})
    }
    load()
    return () => {
      alive = false
      if (timer) clearTimeout(timer)
    }
  }, [])

  useEffect(() => {
    const unsub = usePlayerStore.subscribe((state, prev) => {
      if (state.currentTrack?.id && state.currentTrack.id !== prev.currentTrack?.id) {
        useLibraryStore.getState().fetchStats().catch(() => {})
      }
    })
    return unsub
  }, [])

  const recent = useMemo(
    () => [...tracks].sort((a, b) => (b.createdAt < a.createdAt ? -1 : b.createdAt > a.createdAt ? 1 : 0)).slice(0, 12),
    [tracks]
  )

  const serverRecent = stats?.recentlyPlayed
  const recentlyPlayed = useMemo(
    () =>
      serverRecent && serverRecent.length > 0
        ? serverRecent.slice(0, 12)
        : tracks
            .filter((t) => t.lastPlayedAt)
            .sort((a, b) => (b.lastPlayedAt! < a.lastPlayedAt! ? -1 : b.lastPlayedAt! > a.lastPlayedAt! ? 1 : 0))
            .slice(0, 12),
    [serverRecent, tracks]
  )

  const weekIds = home?.similar.week.trackIds
  const monthIds = home?.similar.month.trackIds
  const span = similarSpan ?? ((weekIds?.length ?? 0) > 0 ? "week" : "month")
  const similar = useTracksById(span === "week" ? weekIds : monthIds)
  const basedOn = home?.similar[span].basedOn ?? []
  const mixes = home?.mixes ?? []

  const quick = useMemo(() => {
    const items: { key: string; title: string; art: React.ReactNode; open: () => void; play?: () => void }[] = []
    const byId = new Map(tracks.map((t) => [t.id, t]))
    if (favorites.length > 0) {
      items.push({
        key: "favorites",
        title: "Favorites",
        art: <IconArt icon={Heart} seed="favorites-liked" />,
        open: () => navigate("favorites"),
        play: () => usePlayerStore.getState().playAll(favorites),
      })
    }
    for (const mix of mixes.slice(0, 3)) {
      const list = mix.trackIds.map((id) => byId.get(id)).filter((t): t is Track => !!t)
      items.push({
        key: mix.id,
        title: mix.title,
        art: <MixCover mix={mix} compact />,
        open: () => navigate("mix", mix.id),
        play: list.length > 0 ? () => usePlayerStore.getState().playAll(list) : undefined,
      })
    }
    const lists = [...playlists].sort((a, b) => Number(b.isPinned) - Number(a.isPinned))
    for (const p of lists) {
      if (items.length >= 8) break
      items.push({
        key: p.id,
        title: p.name,
        art: p.coverVersion ? (
          <div className="relative size-full">
            <PlaylistCoverImage playlist={p} />
          </div>
        ) : (
          <IconArt icon={ListMusic} seed={p.id} />
        ),
        open: () => navigate("playlist", p.id),
      })
    }
    return items
  }, [favorites, mixes, playlists, tracks, navigate])

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

  const openSuggestion = (name: string, trackCount: number, deezerId?: number) => {
    if (trackCount > 0 || !deezerId) navigate("artist", name)
    else navigate("catalog", String(deezerId))
  }

  return (
    <div className="relative pb-16 md:pb-12">
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-x-0 top-0 h-72 bg-linear-to-b from-emerald-900/25 via-emerald-950/5 to-transparent"
      />
      <div className="relative px-4 pt-5 md:px-6 md:pt-7">
        <h1 className="text-2xl font-black tracking-tight text-zinc-50 md:text-3xl">{greeting()}</h1>

        {quick.length > 0 ? (
          <div className="mt-5 grid grid-cols-2 gap-2 md:gap-3 xl:grid-cols-4">
            {quick.map((q) => (
              <QuickTile key={q.key} title={q.title} art={q.art} onOpen={q.open} onPlay={q.play} />
            ))}
          </div>
        ) : null}

        <TrackShelf
          title="Recently played"
          tracks={recentlyPlayed}
          action={
            <Button
              variant="ghost"
              size="sm"
              onClick={() => navigate("history")}
              className="h-8 text-xs text-zinc-400 hover:text-zinc-100"
            >
              History
            </Button>
          }
        />

        {mixes.length > 0 ? (
          <Shelf title="Mixes for you" subtitle="Fresh every day from what you listen to">
            {mixes.map((m) => (
              <MixCard key={m.id} mix={m} />
            ))}
          </Shelf>
        ) : null}

        <TrackShelf
          title={span === "week" ? "Like what you played this week" : "Like what you played this month"}
          subtitle={basedOn.length > 0 ? `Based on ${listing(basedOn)}` : undefined}
          tracks={similar}
          action={
            (weekIds?.length ?? 0) > 0 && (monthIds?.length ?? 0) > 0 ? (
              <SegmentToggle
                label="Time span"
                value={span}
                onChange={setSimilarSpan}
                options={[
                  { value: "week", label: "Week" },
                  { value: "month", label: "Month" },
                ]}
              />
            ) : undefined
          }
        />

        {home && home.topArtists.length > 0 ? (
          <Shelf title="Your top artists">
            {home.topArtists.map((a) => (
              <ArtistCircle
                key={a.name}
                name={a.name}
                picture={a.picture}
                caption={a.listens > 0 ? `${a.listens} ${a.listens === 1 ? "play" : "plays"} lately` : `${a.trackCount} songs`}
                onOpen={() => navigate("artist", a.name)}
              />
            ))}
          </Shelf>
        ) : null}

        {home && home.suggestedArtists.length > 0 ? (
          <Shelf title="Artists you might like">
            {home.suggestedArtists.map((a) => (
              <ArtistCircle
                key={a.name}
                name={a.name}
                picture={a.picture}
                caption={
                  a.trackCount > 0
                    ? `${a.trackCount} ${a.trackCount === 1 ? "song" : "songs"} in your library`
                    : `Because you like ${a.because}`
                }
                onOpen={() => openSuggestion(a.name, a.trackCount, a.deezerId)}
              />
            ))}
          </Shelf>
        ) : null}

        <TrackShelf title="Recently added" tracks={recent} />

        {stats && stats.mostPlayed.length > 0 ? (
          <section className="mt-9">
            <div className="mb-3 flex items-end justify-between gap-4">
              <h2 className="text-xl font-bold tracking-tight text-zinc-50">Most played</h2>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => navigate("library")}
                className="h-8 text-xs text-zinc-400 hover:text-zinc-100"
              >
                Library
              </Button>
            </div>
            <TrackList tracks={stats.mostPlayed.slice(0, 5)} limit={5} />
          </section>
        ) : null}

        {stats ? (
          <div className="mt-10 flex flex-wrap gap-2">
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
        ) : null}
      </div>
    </div>
  )
}
