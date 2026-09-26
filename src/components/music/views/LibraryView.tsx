"use client"

import { useCallback, useEffect, useMemo, useState } from "react"
import { Check, CircleAlert, CopyCheck, Loader2, Music2, Search, Sparkles, Tags, Upload } from "lucide-react"
import { toast } from "sonner"
import { applyConfidentMatches, dismissAttention, fetchApplyStatus, fetchAttention, restoreAttention } from "@/lib/api"
import type { AttentionItem, AttentionReason, BulkApplyResult, BulkApplyStatus, Track } from "@/lib/types"
import { cn } from "@/lib/utils"
import { useLibraryStore, type TrackSort } from "@/stores/library"
import { usePlayerStore } from "@/stores/player"
import { useUIStore } from "@/stores/ui"
import { VirtualTrackList } from "@/components/music/VirtualTrackList"
import { MetadataReviewDialog } from "@/components/music/MetadataReviewDialog"
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

const REASONS: { key: AttentionReason; label: string }[] = [
  { key: "artist", label: "without an artist" },
  { key: "album", label: "without an album" },
  { key: "cover", label: "without a cover" },
  { key: "thumbnail", label: "with a video thumbnail as cover" },
]

function applyNotes(r: BulkApplyResult): string | undefined {
  const parts: string[] = []
  if (r.lookups > 0) parts.push(`${r.lookups} still being looked up`)
  if (r.uncertain > 0) parts.push(`${r.uncertain} need a manual review`)
  return parts.length ? parts.join(" · ") : undefined
}

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
  const [attention, setAttention] = useState<AttentionItem[] | null>(null)
  const [attentionKey, setAttentionKey] = useState(0)
  const [attentionOnly, setAttentionOnly] = useState(false)
  const [reviewing, setReviewing] = useState<Track[] | null>(null)
  const [showHidden, setShowHidden] = useState(false)
  const [hidden, setHidden] = useState<AttentionItem[] | null>(null)
  const [applying, setApplying] = useState<BulkApplyStatus | null>(null)

  useEffect(() => {
    let alive = true
    const t = window.setTimeout(() => {
      Promise.all([fetchAttention(), fetchAttention(true)])
        .then(([items, hiddenItems]) => {
          if (!alive) return
          setAttention(items)
          setHidden(hiddenItems)
        })
        .catch(() => {})
    }, 300)
    return () => {
      alive = false
      window.clearTimeout(t)
    }
  }, [tracks, attentionKey])

  useEffect(() => {
    if (!applying?.running) return
    const t = window.setInterval(() => {
      fetchApplyStatus()
        .then((st) => {
          setApplying(st)
          if (st.running) return
          const applied = st.done - st.failed
          toast.success(`Applied ${applied} match${applied === 1 ? "" : "es"}`, {
            description: st.failed ? `${st.failed} couldn't be applied` : undefined,
          })
          useLibraryStore.getState().fetchAll().catch(() => {})
        })
        .catch(() => {})
    }, 1500)
    return () => window.clearInterval(t)
  }, [applying?.running])

  const attentionIds = useMemo(() => new Set(attention?.map((a) => a.trackId)), [attention])
  const hiddenIds = useMemo(() => new Set(hidden?.map((a) => a.trackId)), [hidden])
  const summary = useMemo(() => {
    const counts = new Map<AttentionReason, number>()
    for (const a of attention ?? []) for (const r of a.reasons) counts.set(r, (counts.get(r) ?? 0) + 1)
    return REASONS.filter((r) => counts.get(r.key)).map((r) => `${counts.get(r.key)} ${r.label}`).join(" · ")
  }, [attention])

  const restore = useCallback((trackId: string) => {
    setHidden((prev) => prev?.filter((a) => a.trackId !== trackId) ?? prev)
    restoreAttention(trackId)
      .catch((e: Error) => toast.error(e.message))
      .finally(() => setAttentionKey((k) => k + 1))
  }, [])

  const dismiss = useCallback(
    (trackId: string) => {
      setAttention((prev) => prev?.filter((a) => a.trackId !== trackId) ?? prev)
      dismissAttention(trackId)
        .then(() => toast("Hidden from Needs attention", { action: { label: "Undo", onClick: () => restore(trackId) } }))
        .catch((e: Error) => toast.error(e.message))
        .finally(() => setAttentionKey((k) => k + 1))
    },
    [restore]
  )

  const applyMatches = async (ids: string[]) => {
    try {
      const r = await applyConfidentMatches(ids)
      if (r.applying === 0) {
        toast("No matches at 90% or more", { description: applyNotes(r) })
        return
      }
      toast(`Applying ${r.applying} match${r.applying === 1 ? "" : "es"}…`, { description: applyNotes(r) })
      setApplying({ running: true, total: r.applying, done: 0, failed: 0 })
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  const sorted = useMemo(() => sortTracks(tracks, sort), [tracks, sort])
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    const ids = showHidden ? hiddenIds : attentionIds
    const pool = attentionOnly ? sorted.filter((t) => ids.has(t.id)) : sorted
    if (!q) return pool
    return pool.filter((t) =>
      [t.title, t.artist, t.album].some((f) => f.toLowerCase().includes(q))
    )
  }, [sorted, query, attentionOnly, showHidden, attentionIds, hiddenIds])

  return (
    <div className="p-4 md:p-6">
      <header className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <h1 className="text-2xl font-bold tracking-tight md:text-3xl">Your library</h1>
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            onClick={() => useUIStore.getState().navigate("filetags")}
            className="h-9 border-zinc-800 bg-transparent text-zinc-300 hover:bg-zinc-800"
          >
            <Tags className="size-4" /> File tags
          </Button>
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
        {(attention?.length ?? 0) > 0 || attentionOnly ? (
          <Button
            variant="outline"
            aria-pressed={attentionOnly}
            onClick={() => {
              setAttentionOnly((v) => !v)
              setShowHidden(false)
            }}
            className={cn(
              "h-9 shrink-0 border-zinc-700/80 bg-zinc-900/60 text-zinc-300 hover:bg-zinc-800",
              attentionOnly && "border-amber-500/60 bg-amber-500/10 text-amber-200 hover:bg-amber-500/20"
            )}
          >
            <CircleAlert className="size-4" /> Needs attention · {attention?.length ?? 0}
          </Button>
        ) : null}
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
        ) : (
          <>
            {attentionOnly ? (
              <div className="mb-3 flex flex-wrap items-center justify-between gap-2 rounded-xl border border-amber-500/20 bg-amber-500/5 px-4 py-2.5 text-xs text-amber-100/80">
                <span>
                  {showHidden
                    ? `${hidden?.length ?? 0} marked “Looks fine”`
                    : summary || "Nothing needs attention"}
                </span>
                <div className="flex flex-wrap items-center gap-2">
                  <Button
                    variant="ghost"
                    size="sm"
                    aria-pressed={showHidden}
                    onClick={() => setShowHidden((v) => !v)}
                    className="h-8 px-2 text-xs text-amber-100/80 hover:bg-amber-500/10 hover:text-amber-50"
                  >
                    {showHidden ? "Back to Needs attention" : `Hidden · ${hidden?.length ?? 0}`}
                  </Button>
                  {!showHidden && filtered.length > 0 ? (
                    <>
                      <Button
                        variant="outline"
                        size="sm"
                        disabled={applying?.running}
                        onClick={() => applyMatches(filtered.map((t) => t.id))}
                        className="h-8 border-emerald-700/60 bg-emerald-600/10 text-xs text-emerald-200 hover:bg-emerald-600/20"
                      >
                        {applying?.running ? (
                          <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />
                        ) : (
                          <Sparkles className="size-3.5" aria-hidden="true" />
                        )}
                        {applying?.running ? `Applying ${applying.done} of ${applying.total}…` : "Apply confident matches"}
                      </Button>
                      <Button
                        size="sm"
                        onClick={() => setReviewing(filtered)}
                        className="h-8 bg-emerald-500 font-semibold text-zinc-950 hover:bg-emerald-400"
                      >
                        Review {filtered.length}
                      </Button>
                    </>
                  ) : null}
                </div>
              </div>
            ) : null}
            {filtered.length === 0 ? (
              <EmptyState
                icon={attentionOnly && !query.trim() ? Check : Search}
                title={
                  attentionOnly && !query.trim()
                    ? showHidden
                      ? "No tracks are hidden"
                      : "Nothing needs attention"
                    : "No matches"
                }
                compact
              />
            ) : (
              <VirtualTrackList
                tracks={filtered}
                followTrackId={attentionOnly ? null : currentId}
                onDismissAttention={attentionOnly && !showHidden ? dismiss : undefined}
                onRestoreAttention={attentionOnly && showHidden ? restore : undefined}
              />
            )}
            {filtered.length > 0 ? (
              <p className="mt-4 text-center text-[11px] text-zinc-600">
                {filtered.length} track{filtered.length === 1 ? "" : "s"} · sorted by{" "}
                {SORTS.find((s) => s.value === sort)?.label.toLowerCase()}
              </p>
            ) : null}
          </>
        )}
      </div>

      <MetadataReviewDialog
        tracks={reviewing ?? []}
        open={reviewing !== null}
        onOpenChange={(o) => {
          if (!o) {
            setReviewing(null)
            setAttentionKey((k) => k + 1)
          }
        }}
        onKeep={dismiss}
      />
    </div>
  )
}
