"use client"

import { useCallback, useEffect, useRef, useState } from "react"
import { toast } from "sonner"
import { Check, ChevronLeft, ChevronRight, ImageIcon, ImageOff, Link2, Loader2, RefreshCw, Sparkles } from "lucide-react"
import { cn } from "@/lib/utils"
import { addYouTubeCandidate, applyMetadataCandidate, candidateArtworkUrl, fetchMetadataCandidates, requestMetadataLookup } from "@/lib/api"
import { formatDuration } from "@/lib/format"
import type { MetadataCandidate, MetadataCandidatesResponse, Track } from "@/lib/types"
import { useLibraryStore } from "@/stores/library"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"

/** Colour-code the provider so the origin of a suggestion is obvious at a glance. */
const SOURCE_STYLES: Record<string, string> = {
  MusicBrainz: "bg-sky-500/15 text-sky-300 ring-sky-500/30",
  Deezer: "bg-violet-500/15 text-violet-300 ring-violet-500/30",
  iTunes: "bg-pink-500/15 text-pink-300 ring-pink-500/30",
  YouTube: "bg-red-500/15 text-red-300 ring-red-500/30",
}

/**
 * Thumbnail of a suggestion's cover art, so the right release can be told
 * apart at a glance. The image is fetched lazily through the server. Two
 * distinct fallbacks, because they mean different things to the person
 * choosing: a candidate that simply has no artwork shows a neutral image
 * icon, while one that *should* have artwork but couldn't be fetched (the
 * server answers 404 for an expired list or an unreachable provider) shows a
 * broken-image icon — the art may still arrive when the candidate is applied.
 */
function CandidateThumb({ trackId, candidate }: { trackId: string; candidate: MetadataCandidate }) {
  const [failed, setFailed] = useState(false)
  const frame = "flex size-12 shrink-0 items-center justify-center overflow-hidden rounded-md bg-zinc-800/80 ring-1 ring-white/5"
  if (!candidate.hasArtwork) {
    return (
      <div className={frame} title="No artwork available">
        <ImageIcon className="size-5 text-zinc-600" aria-hidden="true" />
      </div>
    )
  }
  if (failed) {
    return (
      <div className={cn(frame, "border border-dashed border-zinc-600 ring-0")} title="Artwork couldn't be loaded">
        <ImageOff className="size-5 text-zinc-500" aria-hidden="true" />
      </div>
    )
  }
  return (
    <div className={frame}>
      <img
        src={candidateArtworkUrl(trackId, candidate.id)}
        alt=""
        loading="lazy"
        onError={() => setFailed(true)}
        className="size-full object-cover"
      />
    </div>
  )
}

function confidenceLabel(c: number): { text: string; className: string } {
  if (c >= 0.8) return { text: "Strong match", className: "text-emerald-400" }
  if (c >= 0.5) return { text: "Likely match", className: "text-amber-400" }
  return { text: "Weak match", className: "text-zinc-500" }
}

interface Props {
  /** Tracks to review, in order. The dialog closes when the last one is resolved. */
  tracks: Track[]
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function MetadataReviewDialog({ tracks, open, onOpenChange }: Props) {
  const [index, setIndex] = useState(0)
  const [candidates, setCandidates] = useState<MetadataCandidate[]>([])
  const [loading, setLoading] = useState(false)
  const [applyingId, setApplyingId] = useState<string | null>(null)
  const [lookupDisabled, setLookupDisabled] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // Queue state of the current track's lookup, from the last response.
  const [queue, setQueue] = useState<Pick<MetadataCandidatesResponse, "pending" | "queuePosition" | "partial" | "attempts" | "retryAt"> | null>(null)
  // Provider tab, keyed by track so a new track starts back on "all".
  const [tab, setTab] = useState<{ id: string; tab: string } | null>(null)
  // "Paste a YouTube link" row.
  const [ytUrl, setYtUrl] = useState("")
  const [ytBusy, setYtBusy] = useState(false)
  // Tracks the user has already resolved, so revisiting shows the outcome.
  const [resolved, setResolved] = useState<Record<string, string>>({})

  const abortRef = useRef<AbortController | null>(null)
  const track = tracks[index]

  const load = useCallback(async (trackId: string, quiet = false) => {
    abortRef.current?.abort()
    const ac = new AbortController()
    abortRef.current = ac
    if (!quiet) {
      setLoading(true)
      setError(null)
      setCandidates([])
      setQueue(null)
    }
    try {
      const res = await fetchMetadataCandidates(trackId, ac.signal)
      if (ac.signal.aborted) return
      setCandidates(res.candidates ?? [])
      setLookupDisabled(res.lookupDisabled)
      setQueue({ pending: res.pending, queuePosition: res.queuePosition, partial: res.partial, attempts: res.attempts, retryAt: res.retryAt })
    } catch (e) {
      if ((e as Error)?.name === "AbortError") return
      setError((e as Error).message)
    } finally {
      if (!ac.signal.aborted) setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (!open || !track) return
    load(track.id)
    return () => abortRef.current?.abort()
  }, [open, track, load])

  // While the lookup is queued or running, ask again every few seconds; the
  // list grows as providers answer and the "position" counts down.
  useEffect(() => {
    if (!open || !track || !queue?.pending) return
    const t = setInterval(() => load(track.id, true), 2500)
    return () => clearInterval(t)
  }, [open, track, queue?.pending, load])

  const lookupAgain = async () => {
    if (!track) return
    try {
      await requestMetadataLookup(track.id)
      setResolved((r) => {
        const next = { ...r }
        delete next[track.id]
        return next
      })
      setCandidates([])
      setQueue({ pending: true, queuePosition: 1, partial: false, attempts: 0 })
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  const addFromYouTube = async () => {
    if (!track || !ytUrl.trim()) return
    setYtBusy(true)
    try {
      const c = await addYouTubeCandidate(track.id, ytUrl.trim())
      setCandidates((cs) => [c, ...cs.filter((x) => x.id !== c.id)])
      setYtUrl("")
      setSourceTab("all")
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setYtBusy(false)
    }
  }

  const sources = Array.from(new Set(candidates.map((c) => c.source)))
  const sourceTab = track && tab?.id === track.id ? tab.tab : "all"
  const setSourceTab = (t: string) => track && setTab({ id: track.id, tab: t })
  const shown = sourceTab === "all" ? candidates : candidates.filter((c) => c.source === sourceTab)

  // Reset position whenever a fresh batch is handed to the dialog.
  useEffect(() => {
    if (open) {
      setIndex(0)
      setResolved({})
    }
  }, [open, tracks])

  const goNext = () => {
    if (index < tracks.length - 1) setIndex((i) => i + 1)
    else onOpenChange(false)
  }

  const handleApply = async (candidate: MetadataCandidate) => {
    if (!track) return
    setApplyingId(candidate.id)
    try {
      await applyMetadataCandidate(track.id, candidate.id)
      setResolved((r) => ({ ...r, [track.id]: candidate.id }))
      toast.success(`Updated to “${candidate.title}” by ${candidate.artist || "Unknown Artist"}`)
      useLibraryStore.getState().fetchAll().catch(() => {})
      goNext()
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setApplyingId(null)
    }
  }

  if (!track) return null

  const appliedId = resolved[track.id]

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {/* min-w-0 on the grid children below is load-bearing: DialogContent is a
          CSS grid, whose items default to min-width:auto and so refuse to shrink
          below the intrinsic width of their content. Combined with `truncate`
          (white-space: nowrap) on the long title/album lines, that made the list
          overflow the dialog panel instead of ellipsising. */}
      <DialogContent
        aria-describedby={undefined}
        className="flex max-h-[92vh] max-w-3xl flex-col gap-0 overflow-hidden border-zinc-800 bg-zinc-900 p-0 sm:max-w-3xl"
      >
        <DialogHeader className="border-b border-zinc-800/80 px-5 py-4">
          <DialogTitle className="flex items-center gap-2 text-base">
            <Sparkles className="size-5 text-emerald-400" aria-hidden="true" />
            Confirm track details
          </DialogTitle>
          {tracks.length > 1 ? (
            <DialogDescription className="text-xs">
              Track {index + 1} of {tracks.length}
            </DialogDescription>
          ) : null}
        </DialogHeader>

        <div className="flex min-h-0 min-w-0 flex-1 flex-col px-5 py-4">
          <div className="min-w-0 rounded-lg border border-zinc-800/80 bg-zinc-950/50 px-3 py-2.5">
            <p className="text-[11px] uppercase tracking-wide text-zinc-500">Currently saved as</p>
            <p className="mt-0.5 truncate text-sm font-medium text-zinc-100">{track.title}</p>
            <p className="truncate text-xs text-zinc-400">
              {track.artist}
              {track.album ? ` · ${track.album}` : ""}
              {track.duration > 0 ? ` · ${formatDuration(track.duration)}` : ""}
            </p>
          </div>

          {loading && (
            <div className="flex items-center justify-center gap-2 py-10 text-sm text-zinc-400">
              <Loader2 className="size-4 animate-spin" aria-hidden="true" />
              Loading suggestions…
            </div>
          )}

          {!loading && !error && queue?.pending && (
            <div className="mt-3 flex items-center gap-2 rounded-lg border border-zinc-800/80 bg-zinc-950/50 px-3 py-2 text-xs text-zinc-400">
              <Loader2 className="size-3.5 shrink-0 animate-spin text-emerald-400" aria-hidden="true" />
              <span>
                {queue.retryAt
                  ? `A provider didn't answer — trying again ${formatIn(queue.retryAt)} (attempt ${queue.attempts + 1})`
                  : queue.queuePosition && queue.queuePosition > 1
                    ? `Waiting for the lookup queue · ${queue.queuePosition - 1} ahead`
                    : queue.attempts > 0
                      ? `A provider was rate-limited — retrying (attempt ${queue.attempts + 1})`
                      : "Searching MusicBrainz, Deezer, iTunes…"}
                {candidates.length > 0 ? ` · ${candidates.length} found so far` : ""}
              </span>
            </div>
          )}

          {!loading && !error && !queue?.pending && queue?.partial && (
            <p className="mt-3 text-xs text-amber-300/80">
              Not every provider answered (rate limits); “Look up again” tries them all once more.
            </p>
          )}

          {!loading && !error && sources.length > 1 && (
            <div role="tablist" aria-label="Source" className="mt-3 flex flex-wrap gap-1">
              {["all", ...sources].map((s) => {
                const n = s === "all" ? candidates.length : candidates.filter((c) => c.source === s).length
                return (
                  <button
                    key={s}
                    role="tab"
                    type="button"
                    aria-selected={sourceTab === s}
                    onClick={() => setSourceTab(s)}
                    className={cn(
                      "rounded-full px-2.5 py-1 text-[11px] font-medium ring-1 ring-inset transition-colors",
                      sourceTab === s
                        ? (SOURCE_STYLES[s] ?? "bg-zinc-700/40 text-zinc-100 ring-zinc-500/50")
                        : "bg-transparent text-zinc-400 ring-zinc-800 hover:text-zinc-200"
                    )}
                  >
                    {s === "all" ? "All" : s} · {n}
                  </button>
                )
              })}
            </div>
          )}

          {!loading && error && (
            <div className="py-8 text-center">
              <p className="text-sm text-red-400">{error}</p>
              <Button
                variant="outline"
                size="sm"
                className="mt-3 border-zinc-700 bg-zinc-800/60 text-zinc-200 hover:bg-zinc-800"
                onClick={() => load(track.id)}
              >
                Try again
              </Button>
            </div>
          )}

          {!loading && !error && lookupDisabled && (
            <p className="py-8 text-center text-sm text-zinc-500">
              Online metadata lookup is disabled on this server.
            </p>
          )}

          {!loading && !error && !lookupDisabled && !queue?.pending && candidates.length === 0 && (
            <p className="py-8 text-center text-sm text-zinc-500">
              No online matches found for this track.
            </p>
          )}

          {!loading && !error && shown.length > 0 && (
            <ul className="mt-3 flex min-h-0 min-w-0 flex-1 flex-col gap-2 overflow-y-auto pr-1">
              {shown.map((c) => {
                const conf = confidenceLabel(c.confidence)
                const isApplied = appliedId === c.id
                const wasAuto = c.applied && !appliedId
                return (
                  <li
                    key={c.id}
                    className={cn(
                      "min-w-0 rounded-lg border px-3 py-2.5 transition-colors",
                      isApplied
                        ? "border-emerald-500/60 bg-emerald-500/10"
                        : "border-zinc-800/80 bg-zinc-950/50 hover:border-zinc-700"
                    )}
                  >
                    <div className="flex items-start gap-3">
                      <CandidateThumb trackId={track.id} candidate={c} />
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-1.5">
                          <span
                            className={cn(
                              "rounded px-1.5 py-0.5 text-[10px] font-medium ring-1 ring-inset",
                              SOURCE_STYLES[c.source] ?? "bg-zinc-700/30 text-zinc-300 ring-zinc-600/40"
                            )}
                          >
                            {c.source}
                          </span>
                          {wasAuto && (
                            <span className="rounded bg-emerald-500/15 px-1.5 py-0.5 text-[10px] font-medium text-emerald-300 ring-1 ring-inset ring-emerald-500/30">
                              Applied automatically
                            </span>
                          )}
                        </div>
                        <p className="mt-1 truncate text-sm font-medium text-zinc-100">{c.title}</p>
                        <p className="truncate text-xs text-zinc-400">
                          {c.artist || "Unknown artist"}
                          {c.album ? ` · ${c.album}` : ""}
                          {c.year > 0 ? ` · ${c.year}` : ""}
                          {c.duration > 0 ? ` · ${formatDuration(c.duration)}` : ""}
                        </p>
                        <p className={cn("mt-1 text-[11px]", conf.className)}>
                          {conf.text} · {Math.round(c.confidence * 100)}%
                        </p>
                      </div>
                      <Button
                        size="sm"
                        variant={isApplied ? "secondary" : "outline"}
                        disabled={applyingId !== null || isApplied}
                        className={cn(
                          "h-8 shrink-0 px-3 text-xs",
                          isApplied
                            ? "bg-emerald-600/20 text-emerald-300"
                            : "border-zinc-700 bg-zinc-800/60 text-zinc-200 hover:bg-zinc-800 hover:text-emerald-300"
                        )}
                        onClick={() => handleApply(c)}
                      >
                        {applyingId === c.id ? (
                          <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />
                        ) : isApplied ? (
                          <>
                            <Check className="mr-1 size-3.5" aria-hidden="true" />
                            Applied
                          </>
                        ) : (
                          "Use this"
                        )}
                      </Button>
                    </div>
                  </li>
                )
              })}
            </ul>
          )}
        </div>

        {!lookupDisabled && (
          <form
            className="flex items-center gap-2 border-t border-zinc-800/80 px-5 py-2.5"
            onSubmit={(e) => {
              e.preventDefault()
              void addFromYouTube()
            }}
          >
            <Link2 className="size-4 shrink-0 text-zinc-500" aria-hidden="true" />
            <Input
              value={ytUrl}
              onChange={(e) => setYtUrl(e.target.value)}
              placeholder="Not listed? Paste a YouTube link to use that video's details"
              aria-label="YouTube link"
              disabled={ytBusy}
              className="h-8 border-zinc-800 bg-zinc-950/60 text-xs"
            />
            <Button
              type="submit"
              size="sm"
              variant="outline"
              disabled={ytBusy || !ytUrl.trim()}
              className="h-8 shrink-0 border-zinc-700 bg-zinc-800/60 px-3 text-xs text-zinc-200 hover:bg-zinc-800"
            >
              {ytBusy ? <Loader2 className="size-3.5 animate-spin" aria-hidden="true" /> : "Fetch"}
            </Button>
          </form>
        )}

        <DialogFooter className="flex-row items-center justify-between gap-2 border-t border-zinc-800/80 px-5 py-3">
          <Button
            variant="ghost"
            size="sm"
            disabled={index === 0}
            className="h-8 px-2 text-xs text-zinc-400 hover:text-zinc-100 disabled:opacity-40"
            onClick={() => setIndex((i) => Math.max(0, i - 1))}
          >
            <ChevronLeft className="mr-1 size-4" aria-hidden="true" />
            Previous
          </Button>
          <div className="flex items-center gap-2">
            {!lookupDisabled && (
              <Button
                variant="ghost"
                size="sm"
                disabled={loading || !!queue?.pending}
                className="h-8 px-2 text-xs text-zinc-400 hover:text-zinc-100"
                onClick={lookupAgain}
                title="Search every provider again"
              >
                <RefreshCw className="mr-1 size-3.5" aria-hidden="true" />
                Look up again
              </Button>
            )}
            <Button
              variant="ghost"
              size="sm"
              className="h-8 px-2 text-xs text-zinc-400 hover:text-zinc-100"
              onClick={goNext}
            >
              {index < tracks.length - 1 ? (
                <>
                  Keep current
                  <ChevronRight className="ml-1 size-4" aria-hidden="true" />
                </>
              ) : (
                "Done"
              )}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** "in 10 min", "in 12 h", "in a day" — when a parked retry runs. */
function formatIn(iso: string): string {
  const minutes = Math.round((new Date(iso).getTime() - Date.now()) / 60_000)
  if (minutes < 1) return "any moment"
  if (minutes < 60) return `in ${minutes} min`
  if (minutes < 60 * 23) return `in ${Math.round(minutes / 60)} h`
  return "in a day"
}
