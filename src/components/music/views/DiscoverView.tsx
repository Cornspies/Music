"use client"

import { useEffect, useRef, useState } from "react"
import { Check, Compass, ExternalLink, Loader2, Play, Plus, Search } from "lucide-react"
import { toast } from "sonner"
import { cn } from "@/lib/utils"
import { discover } from "@/lib/api"
import type { DiscoverResult, DiscoverSource, Track } from "@/lib/types"
import { useLibraryStore } from "@/stores/library"
import { usePlayerStore } from "@/stores/player"
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
}

/**
 * Search an online catalogue and add tracks to the library with one click.
 * The server downloads, tags and stores the file; the row then flips to
 * "in library" and can be played straight away.
 */
export function DiscoverView() {
  const [sources, setSources] = useState<DiscoverSource[] | null>(null)
  const [sourceKey, setSourceKey] = useState<string>("")
  const [query, setQuery] = useState("")
  const [submitted, setSubmitted] = useState("")
  const [state, setState] = useState<SearchState | null>(null)
  const abortRef = useRef<AbortController | null>(null)

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
          setState({ key, results: r.results, page: r.page, hasMore: r.hasMore, error: null, loadingMore: false })
      })
      .catch((e: Error) => {
        if (e.name === "AbortError" || ctl.signal.aborted) return
        setState({ key, results: [], page: 1, hasMore: false, error: e.message, loadingMore: false })
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
                placeholder={source ? `Search ${source.name}` : "Search"}
                aria-label="Search the catalogue"
                autoFocus
                className="border-zinc-800 bg-zinc-900/60 pl-9"
              />
            </div>
            <Button type="submit" disabled={!query.trim() || !sourceKey} className="bg-emerald-500 text-zinc-950 hover:bg-emerald-400">
              Search
            </Button>
          </form>

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
