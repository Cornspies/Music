"use client"

import { useEffect, useRef, useState } from "react"
import { Check, Compass, ExternalLink, ListPlus, Loader2, Play, Plus, Search, X } from "lucide-react"
import { toast } from "sonner"
import { cn } from "@/lib/utils"
import { discover } from "@/lib/api"
import { formatDuration } from "@/lib/format"
import type { DiscoverPlaylist, DiscoverResult, DiscoverSource, PlaylistImport, Track } from "@/lib/types"
import { useLibraryStore } from "@/stores/library"
import { usePlayerStore } from "@/stores/player"
import { useUIStore } from "@/stores/ui"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { EmptyState } from "@/components/music/EmptyState"

interface SearchState {
  key: string // `${source}\n${query}` — results only show for the query on screen
  results: DiscoverResult[]
  page: number
  hasMore: boolean
  error: string | null
  loadingMore: boolean
  /** The playlist behind a pasted link; the results are its tracks. */
  playlist: DiscoverPlaylist | null
}

const IMPORT_POLL_MS = 2000

function importActive(j: PlaylistImport): boolean {
  return j.state === "queued" || j.state === "listing" || j.state === "running"
}

/**
 * Search an online catalogue and add tracks to the library with one click.
 * The server downloads, tags and stores the file; the row then flips to
 * "in library" and can be played straight away.
 */
export function DiscoverView() {
  const [sources, setSources] = useState<DiscoverSource[] | null>(null)
  const [sourceKey, setSourceKey] = useState<string>("")
  const [pollKey, setPollKey] = useState(0)
  const [starting, setStarting] = useState(false)
  const [query, setQuery] = useState("")
  const [submitted, setSubmitted] = useState("")
  const [state, setState] = useState<SearchState | null>(null)
  const abortRef = useRef<AbortController | null>(null)
  // Playlist imports running on the server (and recently finished ones),
  // polled while any is active; a finished one can be dismissed here.
  const [imports, setImports] = useState<PlaylistImport[]>([])
  const [dismissed, setDismissed] = useState<Set<string>>(() => new Set())
  const finishedSeen = useRef<Set<string>>(new Set())

  useEffect(() => {
    discover
      .sources()
      .then((s) => {
        setSources(s)
        if (s.length > 0) setSourceKey((k) => k || s[0].key)
      })
      .catch((e: Error) => {
        setSources([])
        toast.error(e.message)
      })
  }, [])

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | null = null
    let alive = true
    const poll = async () => {
      try {
        const list = await discover.imports()
        if (!alive) return
        setImports(list)
        // A playlist that just got created or filled: show it in the sidebar.
        for (const j of list) {
          if (!importActive(j) && !finishedSeen.current.has(j.id)) {
            finishedSeen.current.add(j.id)
            useLibraryStore.getState().fetchAll().catch(() => {})
          }
        }
        if (list.some(importActive)) timer = setTimeout(poll, IMPORT_POLL_MS)
      } catch {
        if (alive) timer = setTimeout(poll, IMPORT_POLL_MS * 5)
      }
    }
    poll()
    return () => {
      alive = false
      if (timer) clearTimeout(timer)
    }
    // `pollKey` bumps when an import is started here, so polling resumes.
  }, [pollKey])

  const key = `${sourceKey}\n${submitted}`
  const shown = state?.key === key ? state : null
  const searching = submitted !== "" && sourceKey !== "" && !shown

  useEffect(() => {
    if (!submitted || !sourceKey) return
    abortRef.current?.abort()
    const ctl = new AbortController()
    abortRef.current = ctl
    discover
      .search(sourceKey, submitted, 1, ctl.signal)
      .then((r) => {
        if (!ctl.signal.aborted)
          setState({
            key,
            results: r.results,
            page: r.page,
            hasMore: r.hasMore,
            error: null,
            loadingMore: false,
            playlist: r.playlist ?? null,
          })
      })
      .catch((e: Error) => {
        if (e.name === "AbortError" || ctl.signal.aborted) return
        setState({ key, results: [], page: 1, hasMore: false, error: e.message, loadingMore: false, playlist: null })
      })
    return () => ctl.abort()
  }, [key, submitted, sourceKey])

  const loadMore = () => {
    if (!shown || shown.loadingMore || !shown.hasMore) return
    setState({ ...shown, loadingMore: true })
    discover
      .search(sourceKey, submitted, shown.page + 1)
      .then((r) =>
        setState((s) =>
          s && s.key === key
            ? { ...s, results: [...s.results, ...r.results], page: r.page, hasMore: r.hasMore, loadingMore: false }
            : s
        )
      )
      .catch((e: Error) => {
        toast.error(e.message)
        setState((s) => (s && s.key === key ? { ...s, loadingMore: false } : s))
      })
  }

  const markAdded = (id: string, track: Track) =>
    setState((s) =>
      s && s.key === key ? { ...s, results: s.results.map((r) => (r.id === id ? { ...r, trackId: track.id } : r)) } : s
    )

  const source = sources?.find((s) => s.key === sourceKey) ?? null

  const startImport = async () => {
    setStarting(true)
    try {
      const job = await discover.startImport(sourceKey, submitted)
      setImports((list) => (list.some((j) => j.id === job.id) ? list : [job, ...list]))
      setDismissed((d) => {
        if (!d.has(job.id)) return d
        const next = new Set(d)
        next.delete(job.id)
        return next
      })
      setPollKey((k) => k + 1)
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setStarting(false)
    }
  }

  const cancelImport = async (id: string) => {
    try {
      await discover.cancelImport(id)
      setPollKey((k) => k + 1)
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  const shownImports = imports.filter((j) => !dismissed.has(j.id))
  const importingShown = shown?.playlist
    ? imports.find((j) => importActive(j) && j.source === sourceKey && j.listId === shown.playlist?.id)
    : undefined

  return (
    <div className="p-4 pb-16 md:p-6 md:pb-12">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <p className="text-[11px] font-semibold uppercase tracking-widest text-zinc-500">Add music</p>
          <h1 className="text-2xl font-bold tracking-tight text-zinc-50">Discover</h1>
        </div>
        {sources && sources.length > 1 ? (
          <div role="tablist" aria-label="Catalogue" className="flex gap-1 rounded-lg bg-zinc-900/70 p-1">
            {sources.map((s) => (
              <button
                key={s.key}
                role="tab"
                type="button"
                aria-selected={sourceKey === s.key}
                onClick={() => setSourceKey(s.key)}
                className={cn(
                  "rounded-md px-2.5 py-1 text-xs font-medium transition-colors",
                  sourceKey === s.key ? "bg-zinc-800 text-emerald-300" : "text-zinc-400 hover:text-zinc-100"
                )}
              >
                {s.name}
              </button>
            ))}
          </div>
        ) : null}
      </div>

      {sources && sources.length === 0 ? (
        <div className="mt-10">
          <EmptyState icon={Compass} title="Discover is turned off on this server" compact />
        </div>
      ) : (
        <>
          <form
            className="mt-5 flex gap-2"
            onSubmit={(e) => {
              e.preventDefault()
              setSubmitted(query.trim())
            }}
          >
            <div className="relative flex-1">
              <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-zinc-500" aria-hidden="true" />
              <Input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder={source ? (source.playlists ? `Search ${source.name}, or paste a link` : `Search ${source.name}`) : "Search"}
                aria-label="Search the catalogue"
                autoFocus
                className="border-zinc-800 bg-zinc-900/60 pl-9"
              />
            </div>
            <Button type="submit" disabled={!query.trim() || !sourceKey} className="bg-emerald-500 text-zinc-950 hover:bg-emerald-400">
              Search
            </Button>
          </form>

          {shownImports.length > 0 ? (
            <div className="mt-5 flex flex-col gap-2">
              {shownImports.map((j) => (
                <ImportCard
                  key={j.id}
                  job={j}
                  onCancel={() => cancelImport(j.id)}
                  onDismiss={() => setDismissed((d) => new Set(d).add(j.id))}
                />
              ))}
            </div>
          ) : null}

          {shown?.playlist ? (
            <div className="mt-5 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-emerald-500/30 bg-emerald-500/10 px-4 py-3">
              <div className="min-w-0">
                <p className="truncate text-sm font-medium text-zinc-100">{shown.playlist.title}</p>
                <p className="text-xs text-zinc-400">
                  {shown.playlist.count} tracks
                  {shown.playlist.total ? ` · the first ${shown.playlist.count} of ${shown.playlist.total}` : ""}
                </p>
              </div>
              <Button
                size="sm"
                disabled={starting || !!importingShown}
                onClick={startImport}
                className="bg-emerald-500 text-zinc-950 hover:bg-emerald-400"
              >
                {starting ? <Loader2 className="size-4 animate-spin" /> : <ListPlus className="size-4" />}
                {importingShown ? "Importing…" : "Import all"}
              </Button>
            </div>
          ) : null}

          <div className="mt-6">
            {!submitted ? null : searching ? (
              <div className="flex items-center justify-center py-16 text-zinc-500">
                <Loader2 className="size-5 animate-spin" />
              </div>
            ) : shown?.error ? (
              <EmptyState icon={Compass} title="Search failed" subtitle={shown.error} compact />
            ) : shown && shown.results.length === 0 ? (
              <EmptyState icon={Compass} title={`No results for “${submitted}”`} compact />
            ) : shown ? (
              <>
                <ul className="flex flex-col">
                  {shown.results.map((r) => (
                    <ResultRow key={r.id} result={r} onAdded={(t) => markAdded(r.id, t)} />
                  ))}
                </ul>
                {shown.hasMore ? (
                  <div className="mt-4 flex justify-center">
                    <Button
                      variant="outline"
                      disabled={shown.loadingMore}
                      onClick={loadMore}
                      className="border-zinc-800 bg-transparent text-zinc-300 hover:bg-zinc-800"
                    >
                      {shown.loadingMore ? <Loader2 className="size-4 animate-spin" /> : null}
                      Load more
                    </Button>
                  </div>
                ) : null}
              </>
            ) : null}
          </div>
        </>
      )}
    </div>
  )
}

/** One playlist import: progress while it runs, the tally when it's done. */
function ImportCard({ job, onCancel, onDismiss }: { job: PlaylistImport; onCancel: () => void; onDismiss: () => void }) {
  const [showFailures, setShowFailures] = useState(false)
  const [showExisting, setShowExisting] = useState(false)
  const [showDuplicates, setShowDuplicates] = useState(false)
  const active = importActive(job)
  const pct = job.total > 0 ? Math.round((job.done / job.total) * 100) : 0
  const title = job.title || "Reading the playlist…"
  const parts: string[] = []
  if (job.added) parts.push(`${job.added} added`)
  if (job.possibleDuplicates.length) parts.push(`${job.possibleDuplicates.length} possible duplicates`)
  if (job.existing) parts.push(`${job.existing} already in the library`)
  if (job.failed) parts.push(`${job.failed} failed`)

  let status: string
  switch (job.state) {
    case "queued":
      status = "Waiting for another import to finish"
      break
    case "listing":
      status = "Reading the playlist…"
      break
    case "running":
      status = job.current ? `Fetching “${job.current}”` : "Fetching…"
      break
    case "done":
      status = parts.length ? parts.join(" · ") : "Nothing to add"
      break
    case "cancelled":
      status = `Stopped${parts.length ? " · " + parts.join(" · ") : ""}`
      break
    default:
      status = job.error || "Failed"
  }

  return (
    <div className="rounded-lg border border-zinc-800 bg-zinc-900/60 px-4 py-3">
      <div className="flex items-center gap-3">
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-medium text-zinc-100">{title}</p>
          <p className={cn("truncate text-xs", job.state === "failed" ? "text-red-400" : "text-zinc-400")}>
            {job.total > 0 ? `${job.done} of ${job.total} · ` : ""}
            {status}
          </p>
        </div>
        {job.playlistId ? (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => useUIStore.getState().navigate("playlist", job.playlistId)}
            className="text-emerald-300 hover:text-emerald-200"
          >
            Open playlist
          </Button>
        ) : null}
        {active ? (
          <Button variant="ghost" size="sm" onClick={onCancel} className="text-zinc-400 hover:text-zinc-100">
            Cancel
          </Button>
        ) : (
          <button
            type="button"
            onClick={onDismiss}
            aria-label="Dismiss"
            className="rounded p-1 text-zinc-500 hover:text-zinc-200"
          >
            <X className="size-4" />
          </button>
        )}
      </div>
      {active && job.total > 0 ? (
        <div className="mt-2 h-1.5 w-full overflow-hidden rounded-full bg-zinc-800">
          <div className="h-full bg-emerald-500 transition-[width]" style={{ width: `${pct}%` }} />
        </div>
      ) : null}
      {job.possibleDuplicates.length > 0 ? (
        <div className="mt-2">
          <button
            type="button"
            onClick={() => setShowDuplicates((v) => !v)}
            className="text-xs text-amber-300 hover:text-amber-200"
          >
            {showDuplicates ? "Hide" : "Show"} the {job.possibleDuplicates.length} possible duplicates
          </button>
          <button
            type="button"
            onClick={() => useUIStore.getState().navigate("duplicates")}
            className="ml-3 text-xs text-zinc-400 hover:text-zinc-200"
          >
            Review in Duplicates
          </button>
          {showDuplicates ? (
            <ul className="mt-1 flex flex-col gap-0.5 text-xs text-zinc-500">
              {job.possibleDuplicates.map((d) => (
                <li key={d.trackId} className="truncate">
                  <span className="text-zinc-300">{d.title}</span> — looks like “{d.existingTitle}”
                  {d.existingArtist ? ` by ${d.existingArtist}` : ""}
                </li>
              ))}
            </ul>
          ) : null}
        </div>
      ) : null}
      {job.existingTracks.length > 0 ? (
        <div className="mt-2">
          <button
            type="button"
            onClick={() => setShowExisting((v) => !v)}
            className="text-xs text-zinc-400 hover:text-zinc-200"
          >
            {showExisting ? "Hide" : "Show"} the {job.existingTracks.length} already in the library
          </button>
          {showExisting ? (
            <ul className="mt-1 flex flex-col gap-0.5 text-xs text-zinc-500">
              {job.existingTracks.map((t) => (
                <li key={t.trackId} className="truncate">
                  <button
                    type="button"
                    onClick={() => {
                      const track = useLibraryStore.getState().tracks.find((x) => x.id === t.trackId)
                      if (track) usePlayerStore.getState().playNow(track, [track])
                    }}
                    className="truncate text-left text-zinc-300 hover:text-emerald-300"
                    title="Play"
                  >
                    {t.title}
                  </button>
                  {t.reason ? <span> — {t.reason}</span> : null}
                </li>
              ))}
            </ul>
          ) : null}
        </div>
      ) : null}
      {job.failures.length > 0 ? (
        <div className="mt-2">
          <button
            type="button"
            onClick={() => setShowFailures((v) => !v)}
            className="text-xs text-zinc-400 hover:text-zinc-200"
          >
            {showFailures ? "Hide" : "Show"} what couldn't be added ({job.failures.length})
          </button>
          {showFailures ? (
            <ul className="mt-1 flex flex-col gap-0.5 text-xs text-zinc-500">
              {job.failures.map((f, i) => (
                <li key={i} className="truncate">
                  <span className="text-zinc-300">{f.title}</span> — {f.reason}
                </li>
              ))}
            </ul>
          ) : null}
        </div>
      ) : null}
    </div>
  )
}

function ResultRow({ result, onAdded }: { result: DiscoverResult; onAdded: (t: Track) => void }) {
  const [adding, setAdding] = useState(false)
  const inLibrary = !!result.trackId

  const add = async () => {
    setAdding(true)
    try {
      const track = await discover.add(result.source, result.id)
      onAdded(track)
      toast.success(`Added “${track.title}” to the library`)
      useLibraryStore.getState().fetchAll().catch(() => {})
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setAdding(false)
    }
  }

  const play = () => {
    const track = useLibraryStore.getState().tracks.find((t) => t.id === result.trackId)
    if (track) usePlayerStore.getState().playNow(track, [track])
    else toast.error("Refresh the library to play this track")
  }

  return (
    <li className="group flex items-center gap-3 rounded-lg px-2 py-2 hover:bg-zinc-900/60">
      <div className="relative size-12 shrink-0 overflow-hidden rounded-md bg-zinc-800">
        {result.coverUrl ? (
          <img src={discover.artworkUrl(result.source, result.coverUrl)} alt="" loading="lazy" className="size-full object-cover" />
        ) : null}
        {inLibrary ? (
          <button
            type="button"
            onClick={play}
            aria-label={`Play ${result.title}`}
            className="absolute inset-0 flex items-center justify-center bg-black/50 text-white opacity-0 transition-opacity group-hover:opacity-100"
          >
            <Play className="size-5 fill-current" />
          </button>
        ) : null}
      </div>
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium text-zinc-100">{result.title}</p>
        <p className="truncate text-xs text-zinc-500">
          {result.artist}
          {result.genre ? ` · ${result.genre}` : ""}
          {result.year ? ` · ${result.year}` : ""}
          {result.duration ? ` · ${formatDuration(result.duration)}` : ""}
        </p>
      </div>
      {result.pageUrl ? (
        <a
          href={result.pageUrl}
          target="_blank"
          rel="noreferrer"
          aria-label="Open on the catalogue's site"
          className="hidden rounded p-1.5 text-zinc-500 hover:text-zinc-200 sm:block"
        >
          <ExternalLink className="size-4" />
        </a>
      ) : null}
      {inLibrary ? (
        <span className="inline-flex items-center gap-1 rounded-full border border-emerald-500/30 bg-emerald-500/10 px-2.5 py-1 text-xs text-emerald-300">
          <Check className="size-3.5" /> In library
        </span>
      ) : (
        <Button
          size="sm"
          disabled={adding}
          onClick={add}
          className="bg-zinc-100 text-zinc-900 hover:bg-white"
        >
          {adding ? <Loader2 className="size-4 animate-spin" /> : <Plus className="size-4" />}
          Add
        </Button>
      )}
    </li>
  )
}
