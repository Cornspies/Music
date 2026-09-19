"use client"

import { useEffect, useState } from "react"
import { Clock3, History, Loader2, Music2, Play } from "lucide-react"
import { cn } from "@/lib/utils"
import { fetchHistory } from "@/lib/api"
import { formatDuration, formatRelativeDate, formatTotal } from "@/lib/format"
import type { HistoryResponse, PlayEvent } from "@/lib/types"
import { usePlayerStore } from "@/stores/player"
import { useUIStore } from "@/stores/ui"
import { CoverArt } from "@/components/music/CoverArt"
import { EmptyState } from "@/components/music/EmptyState"
import { EqualizerIcon } from "@/components/music/EqualizerIcon"

type Range = "day" | "week" | "month" | "all"

const RANGES: { key: Range; label: string; days: number | null }[] = [
  { key: "day", label: "Today", days: 1 },
  { key: "week", label: "This week", days: 7 },
  { key: "month", label: "This month", days: 30 },
  { key: "all", label: "All time", days: null },
]

function sinceFor(range: Range): string | undefined {
  const days = RANGES.find((r) => r.key === range)?.days
  if (!days) return undefined
  return new Date(Date.now() - days * 86_400_000).toISOString()
}

function StatTile({ icon: Icon, value, label }: { icon: typeof Clock3; value: string; label: string }) {
  return (
    <div className="flex items-center gap-3 rounded-xl border border-zinc-800/80 bg-zinc-900/50 px-4 py-3">
      <div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-zinc-800 text-emerald-400">
        <Icon className="size-4" aria-hidden="true" />
      </div>
      <div className="min-w-0">
        <p className="truncate text-lg font-semibold tabular-nums text-zinc-100">{value}</p>
        <p className="truncate text-xs text-zinc-500">{label}</p>
      </div>
    </div>
  )
}

function EventRow({ event, queue }: { event: PlayEvent; queue: PlayEvent[] }) {
  const currentTrack = usePlayerStore((s) => s.currentTrack)
  const isPlaying = usePlayerStore((s) => s.isPlaying)
  const isCurrent = currentTrack?.id === event.track.id
  const play = () =>
    usePlayerStore.getState().playNow(
      event.track,
      queue.map((e) => e.track)
    )
  return (
    <div
      role="button"
      tabIndex={0}
      onDoubleClick={play}
      onKeyDown={(e) => {
        if (e.key === "Enter") play()
      }}
      className={cn(
        "group flex items-center gap-3 rounded-lg px-2 py-1.5 hover:bg-zinc-800/60 md:px-3",
        isCurrent && "bg-zinc-800/40"
      )}
    >
      <div className="relative size-10 shrink-0">
        <CoverArt trackId={event.track.id} title={event.track.title} hasCover={event.track.hasCover} />
        <button
          type="button"
          aria-label={`Play ${event.track.title}`}
          onClick={play}
          className="absolute inset-0 flex items-center justify-center rounded-md bg-black/50 text-white opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
        >
          {isCurrent ? <EqualizerIcon playing={isPlaying} /> : <Play className="size-4 fill-current" />}
        </button>
      </div>
      <div className="min-w-0 flex-1">
        <p className={cn("truncate text-sm font-medium", isCurrent ? "text-emerald-400" : "text-zinc-100")}>
          {event.track.title}
        </p>
        <p className="truncate text-xs text-zinc-500">
          {event.track.artist || "Unknown artist"}
          {event.track.album ? ` · ${event.track.album}` : ""}
        </p>
      </div>
      <span className="hidden shrink-0 text-xs text-zinc-500 sm:block">{formatRelativeDate(event.playedAt)}</span>
      <span className="w-10 shrink-0 text-right text-xs tabular-nums text-zinc-500">
        {formatDuration(event.track.duration)}
      </span>
    </div>
  )
}

/** Listening history across every device, from the server. */
export function HistoryView() {
  const [range, setRange] = useState<Range>("week")
  // The result carries the range it answers, so "loading" is simply "the
  // data on screen isn't for the range that's selected".
  const [result, setResult] = useState<{ range: Range; data: HistoryResponse | null; error: string | null } | null>(null)
  const navigate = useUIStore((s) => s.navigate)

  useEffect(() => {
    let alive = true
    fetchHistory({ limit: 200, since: sinceFor(range) })
      .then((d) => {
        if (alive) setResult({ range, data: d, error: null })
      })
      .catch((e: Error) => {
        if (alive) setResult({ range, data: null, error: e.message })
      })
    return () => {
      alive = false
    }
  }, [range])

  const loading = result?.range !== range
  const data = result?.data ?? null
  const error = loading ? null : (result?.error ?? null)

  return (
    <div className="p-4 pb-16 md:p-6 md:pb-12">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <p className="text-[11px] font-semibold uppercase tracking-widest text-zinc-500">Listening</p>
          <h1 className="text-2xl font-bold tracking-tight text-zinc-50">History</h1>
        </div>
        <div role="tablist" aria-label="Time range" className="flex gap-1 rounded-lg bg-zinc-900/70 p-1">
          {RANGES.map((r) => (
            <button
              key={r.key}
              role="tab"
              type="button"
              aria-selected={range === r.key}
              onClick={() => setRange(r.key)}
              className={cn(
                "rounded-md px-2.5 py-1 text-xs font-medium transition-colors",
                range === r.key ? "bg-zinc-800 text-emerald-300" : "text-zinc-400 hover:text-zinc-100"
              )}
            >
              {r.label}
            </button>
          ))}
        </div>
      </div>

      {data ? (
        <div className="mt-5 grid grid-cols-1 gap-3 sm:grid-cols-3">
          <StatTile icon={Clock3} value={formatTotal(data.listenedSeconds)} label="listened" />
          <StatTile icon={Play} value={data.playCount.toLocaleString()} label={`play${data.playCount === 1 ? "" : "s"}`} />
          <StatTile icon={Music2} value={data.trackCount.toLocaleString()} label="different tracks" />
        </div>
      ) : null}

      <div className="mt-6">
        {loading && !data ? (
          <div className="flex items-center justify-center py-16 text-zinc-500">
            <Loader2 className="size-5 animate-spin" />
          </div>
        ) : error ? (
          <EmptyState icon={History} title="Couldn't load your history" subtitle={error} compact />
        ) : data && data.events.length === 0 ? (
          <EmptyState
            icon={History}
            title="Nothing played yet"
            subtitle="Every track you play, on any device, shows up here."
            compact
            actionLabel="Browse library"
            onAction={() => navigate("library")}
          />
        ) : data ? (
          <div role="list" className={cn("flex flex-col", loading && "opacity-60")}>
            {data.events.map((ev, i) => (
              <EventRow key={`${ev.playedAt}-${ev.track.id}-${i}`} event={ev} queue={data.events} />
            ))}
          </div>
        ) : null}
      </div>
    </div>
  )
}
