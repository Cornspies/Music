"use client"

import { useMemo, useState } from "react"
import { Music2, Search, Upload } from "lucide-react"
import { useLibraryStore, type TrackSort } from "@/stores/library"
import { useUIStore } from "@/stores/ui"
import { TrackList } from "@/components/music/TrackList"
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
  { value: "created", label: "Recently added" },
  { value: "recent", label: "Recently played" },
  { value: "title", label: "Title" },
  { value: "artist", label: "Artist" },
  { value: "plays", label: "Most played" },
]

const INITIAL_ROWS = 50

export function LibraryView() {
  const tracks = useLibraryStore((s) => s.tracks)
  const fetchTracks = useLibraryStore((s) => s.fetchTracks)
  const setUploadOpen = useUIStore((s) => s.setUploadOpen)

  const [sort, setSort] = useState<TrackSort>("created")
  const [query, setQuery] = useState("")
  const [visible, setVisible] = useState(INITIAL_ROWS)
  const [loading, setLoading] = useState(false)

  const onSortChange = (next: string) => {
    const value = next as TrackSort
    setSort(value)
    setLoading(true)
    fetchTracks(value)
      .catch(() => {})
      .finally(() => setLoading(false))
  }

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return tracks
    return tracks.filter((t) =>
      [t.title, t.artist, t.album].some((f) => f.toLowerCase().includes(q))
    )
  }, [tracks, query])

  const shown = filtered.slice(0, visible)

  return (
    <div className="p-4 md:p-6">
      <header className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <h1 className="text-2xl font-bold tracking-tight md:text-3xl">Your library</h1>
        <Button
          onClick={() => setUploadOpen(true)}
          className="h-9 bg-emerald-500 text-zinc-950 font-semibold hover:bg-emerald-400"
        >
          <Upload className="size-4" /> Upload
        </Button>
      </header>

      {/* toolbar */}
      <div className="mt-4 flex flex-col gap-3 sm:flex-row sm:items-center">
        <Select value={sort} onValueChange={onSortChange}>
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
            onChange={(e) => {
              setQuery(e.target.value)
              setVisible(INITIAL_ROWS)
            }}
            className="h-9 border-zinc-700/80 bg-zinc-900/60 pl-9"
          />
        </div>
      </div>

      {/* list */}
      <div className="mt-4">
        {loading && tracks.length === 0 ? (
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
            subtitle={`Nothing matches “${query}” in your library.`}
            compact
          />
        ) : (
          <>
            <TrackList tracks={shown} />
            {filtered.length > shown.length && (
              <div className="mt-4 flex justify-center">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setVisible((v) => v + INITIAL_ROWS)}
                  className="border-zinc-700 bg-zinc-900/60 text-zinc-300 hover:bg-zinc-800 hover:text-zinc-100"
                >
                  Show more ({filtered.length - shown.length} remaining)
                </Button>
              </div>
            )}
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
