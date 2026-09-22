"use client"

import { useEffect, useMemo, useRef, useState } from "react"
import { CopyCheck, Music2, Search, Upload } from "lucide-react"
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

// Rows are added in batches as the end of the list comes near, so a big
// library doesn't render every row up front.
const PAGE_ROWS = 50

export function LibraryView() {
  const tracks = useLibraryStore((s) => s.tracks)
  const fetchTracks = useLibraryStore((s) => s.fetchTracks)
  const setUploadOpen = useUIStore((s) => s.setUploadOpen)

  const [sort, setSort] = useState<TrackSort>("created")
  const [query, setQuery] = useState("")
  const [visible, setVisible] = useState(PAGE_ROWS)
  const sentinelRef = useRef<HTMLDivElement>(null)
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
  const complete = shown.length >= filtered.length

  // The next batch loads once the end of the list is within a screen or so
  // of view. The page scrolls inside <main>, not the window, so that is the
  // observer's root — otherwise the look-ahead margin would be clipped away.
  // The observer is recreated after every batch: a new one reports straight
  // away whether the end is still in reach (a tall screen, a short batch),
  // which an existing one wouldn't, since nothing about it changed.
  useEffect(() => {
    const el = sentinelRef.current
    if (!el || complete) return
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) setVisible((v) => v + PAGE_ROWS)
      },
      { root: el.closest("main"), rootMargin: "0px 0px 800px 0px" }
    )
    observer.observe(el)
    return () => observer.disconnect()
  }, [visible, complete])

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
              setVisible(PAGE_ROWS)
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
            compact
          />
        ) : (
          <>
            <TrackList tracks={shown} />
            {complete ? (
              <p className="mt-4 text-center text-[11px] text-zinc-600">
                {filtered.length} track{filtered.length === 1 ? "" : "s"} · sorted by{" "}
                {SORTS.find((s) => s.value === sort)?.label.toLowerCase()}
              </p>
            ) : (
              <div ref={sentinelRef} aria-hidden="true" className="h-px" />
            )}
          </>
        )}
      </div>
    </div>
  )
}
