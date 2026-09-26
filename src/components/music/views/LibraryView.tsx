"use client"

import { useMemo, useState } from "react"
import { CopyCheck, Music2, Search, Upload } from "lucide-react"
import type { Track } from "@/lib/types"
import { useLibraryStore, type TrackSort } from "@/stores/library"
import { usePlayerStore } from "@/stores/player"
import { useUIStore } from "@/stores/ui"
import { VirtualTrackList } from "@/components/music/VirtualTrackList"
import { EmptyState } from "@/components/music/EmptyState"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"

const SORTS: { value: TrackSort; label: string }[] = [
  { value: "plays", label: "Most played" },
  { value: "created", label: "Recently added" },
  { value: "recent", label: "Recently played" },
  { value: "title", label: "Title" },
  { value: "artist", label: "Artist" },
]

const collator = new Intl.Collator(undefined, { sensitivity: "base", numeric: true })

const newestFirst = (a: Track, b: Track) => (a.createdAt < b.createdAt ? 1 : a.createdAt > b.createdAt ? -1 : 0)

function sortTracks(tracks: Track[], sort: TrackSort): Track[] {
  const list = [...tracks]
  switch (sort) {
    case "plays":
      return list.sort((a, b) => b.plays - a.plays || newestFirst(a, b))
    case "recent":
      return list.sort((a, b) =>
        a.lastPlayedAt && b.lastPlayedAt
          ? (a.lastPlayedAt < b.lastPlayedAt ? 1 : a.lastPlayedAt > b.lastPlayedAt ? -1 : newestFirst(a, b))
          : a.lastPlayedAt ? -1 : b.lastPlayedAt ? 1 : newestFirst(a, b)
      )
    case "title":
      return list.sort((a, b) => collator.compare(a.title, b.title))
    case "artist":
      return list.sort(
        (a, b) => collator.compare(a.artist, b.artist) || collator.compare(a.album, b.album) || a.trackNo - b.trackNo
      )
    default:
      return list.sort(newestFirst)
  }
}

export function LibraryView() {
  const tracks = useLibraryStore((s) => s.tracks)
  const initialized = useLibraryStore((s) => s.initialized)
  const setUploadOpen = useUIStore((s) => s.setUploadOpen)
  const sort = useUIStore((s) => s.librarySort)
  const currentId = usePlayerStore((s) => s.currentTrack?.id ?? null)
  const [query, setQuery] = useState("")

  const sorted = useMemo(() => sortTracks(tracks, sort), [tracks, sort])
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return sorted
    return sorted.filter((t) =>
      [t.title, t.artist, t.album].some((f) => f.toLowerCase().includes(q))
    )
  }, [sorted, query])

  return (
    <div className="p-4 md:p-6">
      <header className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <h1 className="text-2xl font-bold tracking-tight md:text-3xl">Your library</h1>
        <div className="flex gap-2">
          <Button
            variant="outline"
            onClick={() => useUIStore.getState().navigate("duplicates")}
            className="h-9 border-zinc-800 bg-transparent text-zinc-300 hover:bg-zinc-800"
          >
            <CopyCheck className="size-4" /> Find duplicates
          </Button>
          <Button
            onClick={() => setUploadOpen(true)}
            className="h-9 bg-emerald-500 text-zinc-950 font-semibold hover:bg-emerald-400"
          >
            <Upload className="size-4" /> Upload
          </Button>
        </div>
      </header>

      <div className="mt-4 flex flex-col gap-3 sm:flex-row sm:items-center">
        <Select value={sort} onValueChange={(v) => useUIStore.getState().setLibrarySort(v as TrackSort)}>
          <SelectTrigger aria-label="Sort tracks" className="h-9 w-full border-zinc-700/80 bg-zinc-900/60 text-sm sm:w-44">
            <SelectValue placeholder="Sort" />
          </SelectTrigger>
          <SelectContent>
            {SORTS.map((s) => (
              <SelectItem key={s.value} value={s.value}>
                {s.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <div className="relative flex-1">
          <Search
            className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-zinc-500"
            aria-hidden="true"
          />
          <Input
            type="search"
            role="searchbox"
            aria-label="Filter tracks in library"
            placeholder="Filter by title, artist or album…"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            className="h-9 border-zinc-700/80 bg-zinc-900/60 pl-9"
          />
        </div>
      </div>

      <div className="mt-4">
        {!initialized && tracks.length === 0 ? (
          <div className="flex flex-col gap-2">
            {Array.from({ length: 6 }).map((_, i) => (
              <Skeleton key={i} className="h-14 w-full rounded-lg" />
            ))}
          </div>
        ) : tracks.length === 0 ? (
          <EmptyState
            icon={Music2}
            title="No tracks yet"
            actionLabel="Upload music"
            onAction={() => setUploadOpen(true)}
          />
        ) : filtered.length === 0 ? (
          <EmptyState
            icon={Search}
            title="No matches"
            compact
          />
        ) : (
          <>
            <VirtualTrackList tracks={filtered} followTrackId={currentId} />
            <p className="mt-4 text-center text-[11px] text-zinc-600">
              {filtered.length} track{filtered.length === 1 ? "" : "s"} · sorted by{" "}
              {SORTS.find((s) => s.value === sort)?.label.toLowerCase()}
            </p>
          </>
        )}
      </div>
    </div>
  )
}
