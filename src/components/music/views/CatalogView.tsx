"use client"

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react"
import { Check, ChevronDown, ExternalLink, Library, Loader2, Play, Plus, Search, SearchX } from "lucide-react"
import { toast } from "sonner"
import { cn } from "@/lib/utils"
import { catalog, discover } from "@/lib/api"
import { formatDuration } from "@/lib/format"
import { routeToPath } from "@/lib/routes"
import type {
  CatalogArtist,
  CatalogArtistPage,
  CatalogArtistSongs,
  CatalogRelease,
  CatalogReleasePage,
  CatalogTrack,
  DiscoverResult,
  DiscoverSource,
  PlaylistImport,
} from "@/lib/types"
import { useLibraryStore } from "@/stores/library"
import { usePlayerStore } from "@/stores/player"
import { useUIStore } from "@/stores/ui"
import { ArtistImage } from "@/components/music/ArtistImage"
import { EmptyState } from "@/components/music/EmptyState"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"

type FindState =
  | { state: "finding" }
  | { state: "found"; result: DiscoverResult }
  | { state: "missing" }
  | { state: "error"; error: string }
  | { state: "added"; trackId: string }

const FIND_CONCURRENCY = 2
const RELEASE_CONCURRENCY = 3
const IMPORT_POLL_MS = 2000

const RELEASE_GROUPS: { type: string; label: string }[] = [
  { type: "album", label: "Albums" },
  { type: "ep", label: "EPs" },
  { type: "single", label: "Singles" },
  { type: "compile", label: "Compilations" },
]

function replaceRoute(arg: string) {
  useUIStore.getState().applyRoute("catalog", arg)
  window.history.replaceState({ view: "catalog", viewArg: arg }, "", routeToPath("catalog", arg))
}

async function runPool<T>(items: T[], limit: number, work: (item: T) => Promise<void>, signal?: AbortSignal) {
  let next = 0
  const workers = Array.from({ length: Math.min(limit, items.length) }, async () => {
    while (next < items.length && !signal?.aborted) {
      await work(items[next++])
    }
  })
  await Promise.all(workers)
}

function playLibraryTrack(trackId: string) {
  const track = useLibraryStore.getState().tracks.find((t) => t.id === trackId)
  if (track) usePlayerStore.getState().playNow(track, [track])
  else toast.error("Refresh the library to play this track")
}

function jobActive(j: PlaylistImport | null): boolean {
  return !!j && (j.state === "queued" || j.state === "listing" || j.state === "running")
}

interface FindTools {
  sources: DiscoverSource[]
  sourceKey: string
  setSourceKey: (k: string) => void
  states: Record<number, FindState>
  stateOf: (t: CatalogTrack) => FindState | undefined
  finding: boolean
  job: PlaylistImport | null
  find: (tracks: CatalogTrack[]) => Promise<void>
  addOne: (track: CatalogTrack, result: DiscoverResult) => Promise<void>
  addAll: (title: string, tracks: CatalogTrack[]) => Promise<void>
  addRelease: (key: string, title: string, tracks: () => Promise<CatalogTrack[]>) => Promise<void>
  releaseStatus: (key: string) => ReleaseStatus | null
}

type ReleaseStatus = { state: "finding"; done: number; total: number } | { state: "adding"; job: PlaylistImport }

const FindToolsContext = createContext<FindTools | null>(null)

function useFindTools(): FindTools {
  const tools = useContext(FindToolsContext)
  if (!tools) throw new Error("FindToolsProvider is missing")
  return tools
}

function FindToolsProvider({ onImported, children }: { onImported: () => void; children: React.ReactNode }) {
  const [sources, setSources] = useState<DiscoverSource[]>([])
  const [sourceKey, setSourceKeyRaw] = useState("")
  const [states, setStates] = useState<Record<number, FindState>>({})
  const [finding, setFinding] = useState(false)
  const [jobs, setJobs] = useState<Record<string, PlaylistImport>>({})
  const [progress, setProgress] = useState<Record<string, { done: number; total: number }>>({})
  const abortRef = useRef<AbortController | null>(null)

  useEffect(() => {
    discover
      .sources()
      .then((list) => {
        setSources(list)
        setSourceKeyRaw((k) => k || list.find((s) => s.key === "youtube")?.key || list[0]?.key || "")
      })
      .catch(() => setSources([]))
    return () => abortRef.current?.abort()
  }, [])

  const setSourceKey = useCallback((k: string) => {
    abortRef.current?.abort()
    setFinding(false)
    setStates((prev) => {
      const next: Record<number, FindState> = {}
      for (const [id, s] of Object.entries(prev)) if (s.state === "added") next[Number(id)] = s
      return next
    })
    setSourceKeyRaw(k)
  }, [])

  const setOne = (id: number, s: FindState) => setStates((prev) => ({ ...prev, [id]: s }))

  const stateOf = useCallback(
    (t: CatalogTrack): FindState | undefined => {
      const s = states[t.id]
      if (s) return s
      const hit = t.found?.find((f) => f.source === sourceKey)
      return hit ? { state: "found", result: hit } : undefined
    },
    [states, sourceKey]
  )

  const find = useCallback(
    async (tracks: CatalogTrack[]) => {
      if (!sourceKey || tracks.length === 0) return
      abortRef.current?.abort()
      const ctl = new AbortController()
      abortRef.current = ctl
      setFinding(true)
      try {
        await runPool(
          tracks,
          FIND_CONCURRENCY,
          async (t) => {
            setOne(t.id, { state: "finding" })
            try {
              const { result } = await catalog.find(sourceKey, t, ctl.signal)
              if (ctl.signal.aborted) return
              if (result?.trackId) setOne(t.id, { state: "added", trackId: result.trackId })
              else setOne(t.id, result ? { state: "found", result } : { state: "missing" })
            } catch (e) {
              if (!ctl.signal.aborted) setOne(t.id, { state: "error", error: (e as Error).message })
            }
          },
          ctl.signal
        )
      } finally {
        if (abortRef.current === ctl) setFinding(false)
      }
    },
    [sourceKey]
  )

  const addOne = useCallback(async (track: CatalogTrack, result: DiscoverResult) => {
    setOne(track.id, { state: "finding" })
    try {
      const added = await discover.add(result.source, result.id)
      setOne(track.id, { state: "added", trackId: added.id })
      toast.success(`Added “${added.title}”`)
      useLibraryStore.getState().fetchAll().catch(() => {})
    } catch (e) {
      setOne(track.id, { state: "found", result })
      toast.error((e as Error).message)
    }
  }, [])

  const addAll = useCallback(
    async (title: string, tracks: CatalogTrack[]) => {
      const seen = new Set<string>()
      const entries: DiscoverResult[] = []
      for (const t of tracks) {
        const s = stateOf(t)
        if (s?.state === "found" && !seen.has(s.result.id)) {
          seen.add(s.result.id)
          entries.push(s.result)
        }
      }
      if (entries.length === 0) return
      try {
        const started = await catalog.add(sourceKey, title, entries)
        setJobs((prev) => ({ ...prev, toolbar: started }))
      } catch (e) {
        toast.error((e as Error).message)
      }
    },
    [stateOf, sourceKey]
  )

  const addRelease = useCallback(
    async (key: string, title: string, load: () => Promise<CatalogTrack[]>) => {
      if (!sourceKey) return
      const sourceName = sources.find((x) => x.key === sourceKey)?.name ?? "the source"
      setProgress((prev) => ({ ...prev, [key]: { done: 0, total: 0 } }))
      const results = new Map<number, DiscoverResult>()
      try {
        const tracks = await load()
        const missing = tracks.filter((t) => !t.trackId && stateOf(t)?.state !== "added")
        if (missing.length === 0) {
          toast("You already have all of these")
          return
        }
        setProgress((prev) => ({ ...prev, [key]: { done: 0, total: missing.length } }))
        await runPool(missing, FIND_CONCURRENCY, async (t) => {
          const known = stateOf(t)
          if (known?.state === "found") {
            results.set(t.id, known.result)
          } else {
            setOne(t.id, { state: "finding" })
            try {
              const { result } = await catalog.find(sourceKey, t)
              if (result?.trackId) setOne(t.id, { state: "added", trackId: result.trackId })
              else if (result) {
                results.set(t.id, result)
                setOne(t.id, { state: "found", result })
              } else setOne(t.id, { state: "missing" })
            } catch (e) {
              setOne(t.id, { state: "error", error: (e as Error).message })
            }
          }
          setProgress((prev) => (prev[key] ? { ...prev, [key]: { ...prev[key], done: prev[key].done + 1 } } : prev))
        })
        const seen = new Set<string>()
        const entries: DiscoverResult[] = []
        for (const t of missing) {
          const r = results.get(t.id)
          if (r && !seen.has(r.id)) {
            seen.add(r.id)
            entries.push(r)
          }
        }
        if (entries.length === 0) {
          toast.error(`None of the missing songs are on ${sourceName}`)
          return
        }
        if (entries.length < missing.length) {
          toast(`${missing.length - entries.length} of ${missing.length} aren't on ${sourceName}; adding the rest`)
        }
        const started = await catalog.add(sourceKey, title, entries)
        setJobs((prev) => ({ ...prev, [key]: started }))
      } catch (e) {
        toast.error((e as Error).message)
      } finally {
        setProgress((prev) => {
          const next = { ...prev }
          delete next[key]
          return next
        })
      }
    },
    [sourceKey, sources, stateOf]
  )

  const releaseStatus = useCallback(
    (key: string): ReleaseStatus | null => {
      const p = progress[key]
      if (p) return { state: "finding", ...p }
      const j = jobs[key]
      return j && jobActive(j) ? { state: "adding", job: j } : null
    },
    [progress, jobs]
  )

  useEffect(() => {
    if (!Object.values(jobs).some(jobActive)) return
    const timer = setTimeout(async () => {
      try {
        const all = await discover.imports()
        const updates: Record<string, PlaylistImport> = {}
        const finished: PlaylistImport[] = []
        for (const [key, j] of Object.entries(jobs)) {
          const fresh = all.find((x) => x.id === j.id)
          if (!fresh) continue
          updates[key] = fresh
          if (jobActive(j) && !jobActive(fresh)) finished.push(fresh)
        }
        setJobs((prev) => ({ ...prev, ...updates }))
        if (finished.length === 0) return
        await useLibraryStore.getState().fetchAll()
        onImported()
        for (const fresh of finished) {
          toast.success(
            fresh.added > 0
              ? `Added ${fresh.added} ${fresh.added === 1 ? "song" : "songs"} from ${fresh.title}`
              : `Nothing new was added from ${fresh.title}`,
            fresh.failed > 0 ? { description: `${fresh.failed} couldn't be fetched` } : undefined
          )
        }
      } catch {}
    }, IMPORT_POLL_MS)
    return () => clearTimeout(timer)
  }, [jobs, onImported])

  const job = jobs.toolbar ?? null
  const tools = useMemo(
    () => ({
      sources, sourceKey, setSourceKey, states, stateOf, finding, job, find, addOne, addAll, addRelease, releaseStatus,
    }),
    [sources, sourceKey, setSourceKey, states, stateOf, finding, job, find, addOne, addAll, addRelease, releaseStatus]
  )
  return <FindToolsContext.Provider value={tools}>{children}</FindToolsContext.Provider>
}

export function CatalogView() {
  const viewArg = useUIStore((s) => s.viewArg)
  const [reload, setReload] = useState(0)
  const bump = useCallback(() => setReload((n) => n + 1), [])

  let body: React.ReactNode
  if (!viewArg || viewArg.startsWith("find:")) {
    const query = viewArg ? viewArg.slice(5) : ""
    body = <CatalogSearch key={viewArg ?? ""} initial={query} autoOpen={!!viewArg} />
  } else if (viewArg.startsWith("release:")) {
    const id = viewArg.slice(8)
    body = (
      <FindToolsProvider key={viewArg} onImported={bump}>
        <ReleasePage id={id} reload={reload} />
      </FindToolsProvider>
    )
  } else {
    body = (
      <FindToolsProvider key={viewArg} onImported={bump}>
        <ArtistPage id={viewArg} reload={reload} />
      </FindToolsProvider>
    )
  }
  return <div className="p-4 pb-16 md:p-6 md:pb-12">{body}</div>
}

function CatalogSearch({ initial, autoOpen }: { initial: string; autoOpen: boolean }) {
  const [query, setQuery] = useState(initial)
  const [submitted, setSubmitted] = useState(initial)
  const [results, setResults] = useState<{ q: string; artists: CatalogArtist[]; releases: CatalogRelease[] } | null>(null)
  const [error, setError] = useState<{ q: string; message: string } | null>(null)

  useEffect(() => {
    if (!submitted) return
    const ctl = new AbortController()
    catalog
      .search(submitted, ctl.signal)
      .then((r) => {
        if (ctl.signal.aborted) return
        if (autoOpen && submitted === initial) {
          const exact = r.artists.find((a) => a.name.toLowerCase() === initial.toLowerCase())
          if (exact) {
            replaceRoute(String(exact.id))
            return
          }
        }
        setResults({ q: submitted, ...r })
      })
      .catch((e: Error) => {
        if (!ctl.signal.aborted) setError({ q: submitted, message: e.message })
      })
    return () => ctl.abort()
  }, [submitted, autoOpen, initial])

  const shown = results?.q === submitted ? results : null
  const failed = error?.q === submitted ? error.message : null
  const loading = submitted !== "" && !shown && !failed

  return (
    <>
      <p className="text-[11px] font-semibold tracking-widest text-emerald-400/90 uppercase">Discographies</p>
      <h1 className="mt-1 text-2xl font-bold tracking-tight text-zinc-50">Find an artist or album</h1>
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
            placeholder="Artist or album"
            aria-label="Search artists and albums"
            autoFocus
            className="border-zinc-800 bg-zinc-900/60 pl-9"
          />
        </div>
        <Button type="submit" disabled={!query.trim()} className="bg-emerald-500 text-zinc-950 hover:bg-emerald-400">
          Search
        </Button>
      </form>

      <div className="mt-6">
        {loading ? (
          <Spinner />
        ) : failed ? (
          <EmptyState icon={SearchX} title="Search failed" subtitle={failed} compact />
        ) : shown && shown.artists.length === 0 && shown.releases.length === 0 ? (
          <EmptyState icon={SearchX} title={`Nothing found for “${submitted}”`} compact />
        ) : shown ? (
          <>
            {shown.artists.length > 0 ? (
              <section>
                <h2 className="mb-2 text-lg font-bold text-zinc-100">Artists</h2>
                <div className="-mx-2 flex gap-1 overflow-x-auto pb-2 scrollbar-none! [&::-webkit-scrollbar]:hidden!">
                  {shown.artists.map((a) => (
                    <button
                      key={a.id}
                      type="button"
                      onClick={() => useUIStore.getState().navigate("catalog", String(a.id))}
                      className="group w-32 shrink-0 rounded-xl p-2 text-left transition-colors hover:bg-zinc-800/70 md:w-40"
                    >
                      <ArtistImage name={a.name} picture={a.picture} className="shadow-lg shadow-black/40" initialsClassName="text-3xl" />
                      <p className="mt-2.5 truncate text-sm font-semibold text-zinc-100">{a.name}</p>
                      <p className="truncate text-xs text-zinc-500">
                        {a.trackCount > 0 ? `${a.trackCount} in your library` : `${a.releases} releases`}
                      </p>
                    </button>
                  ))}
                </div>
              </section>
            ) : null}
            {shown.releases.length > 0 ? (
              <section className="mt-6">
                <h2 className="mb-2 text-lg font-bold text-zinc-100">Albums</h2>
                <div className="-mx-2 grid grid-cols-2 gap-1 sm:grid-cols-3 lg:grid-cols-5">
                  {shown.releases.map((r) => (
                    <button
                      key={r.id}
                      type="button"
                      onClick={() => useUIStore.getState().navigate("catalog", `release:${r.id}`)}
                      className="rounded-xl p-2 text-left transition-colors hover:bg-zinc-800/70"
                    >
                      <ReleaseArt release={r} className="w-full" />
                      <p className="mt-2 truncate text-sm font-semibold text-zinc-100">{r.title}</p>
                      <p className="truncate text-xs text-zinc-500">
                        {r.artist}
                        {r.year ? ` · ${r.year}` : ""}
                      </p>
                    </button>
                  ))}
                </div>
              </section>
            ) : null}
          </>
        ) : null}
      </div>
    </>
  )
}

function Spinner() {
  return (
    <div className="flex items-center justify-center py-16 text-zinc-500">
      <Loader2 className="size-5 animate-spin" />
    </div>
  )
}

function ReleaseArt({ release, className }: { release: CatalogRelease; className?: string }) {
  return release.cover ? (
    <img
      src={catalog.imageUrl(release.cover)}
      alt=""
      loading="lazy"
      className={cn("aspect-square rounded-md bg-zinc-800 object-cover shadow-md", className)}
    />
  ) : (
    <div className={cn("aspect-square rounded-md bg-zinc-800", className)} />
  )
}

function useLoaded<T>(key: string, load: (signal: AbortSignal) => Promise<T>) {
  const [data, setData] = useState<{ value: T | null; error: string | null }>({ value: null, error: null })
  const loadRef = useRef(load)
  useEffect(() => {
    loadRef.current = load
  })
  useEffect(() => {
    const ctl = new AbortController()
    loadRef.current(ctl.signal)
      .then((value) => {
        if (!ctl.signal.aborted) setData({ value, error: null })
      })
      .catch((e: Error) => {
        if (!ctl.signal.aborted) setData((d) => ({ value: d.value, error: e.message }))
      })
    return () => ctl.abort()
  }, [key])
  return data
}

function titleKey(title: string): string {
  return title.toLowerCase().normalize("NFKD").replace(/[^\p{L}\p{N}]+/gu, "")
}

function sameLength(a: number, b: number): boolean {
  return !a || !b || Math.abs(a - b) <= 8
}

function haveCount(tracks: CatalogTrack[], tools: FindTools): number {
  return tracks.filter((t) => t.trackId || tools.stateOf(t)?.state === "added").length
}

function missingTracks(tracks: CatalogTrack[], tools: FindTools): CatalogTrack[] {
  return tracks.filter((t) => !t.trackId && tools.stateOf(t)?.state !== "added")
}

function foundCount(tracks: CatalogTrack[], tools: FindTools): number {
  return tracks.filter((t) => !t.trackId && tools.stateOf(t)?.state === "found").length
}

const SOURCE_BADGES: Record<string, { label: string; className: string }> = {
  deezer: { label: "Deezer", className: "bg-violet-500/15 text-violet-300 hover:bg-violet-500/25" },
  youtube: { label: "YouTube", className: "bg-red-500/15 text-red-300 hover:bg-red-500/25" },
  soundcloud: { label: "SoundCloud", className: "bg-orange-500/15 text-orange-300 hover:bg-orange-500/25" },
}

function SourceBadges({ found }: { found: DiscoverResult[] }) {
  return (
    <span className="mt-1 flex flex-wrap gap-1">
      {found.map((f) => {
        const style = SOURCE_BADGES[f.source] ?? { label: f.source, className: "bg-zinc-800 text-zinc-300" }
        return (
          <a
            key={`${f.source}:${f.id}`}
            href={f.pageUrl || undefined}
            target="_blank"
            rel="noreferrer"
            title={`On ${style.label}: ${f.artist} – ${f.title}${f.album ? ` (${f.album})` : ""}`}
            onClick={(e) => e.stopPropagation()}
            className={cn("rounded px-1.5 text-[10px] leading-4 font-medium transition-colors", style.className)}
          >
            {style.label}
          </a>
        )
      })}
    </span>
  )
}

function SourceSummary({ songs }: { songs: CatalogArtistSongs }) {
  return (
    <p className="mb-3 flex flex-wrap gap-x-3 gap-y-1 text-xs text-zinc-500">
      {songs.sources.map((src) => (
        <span key={src.key} title={src.error} className={cn(src.error && "text-red-400/80")}>
          {src.name} {src.error ? `· ${src.error}` : `· ${src.count}`}
        </span>
      ))}
    </p>
  )
}

function ArtistPage({ id, reload }: { id: string; reload: number }) {
  const page = useLoaded<CatalogArtistPage>(`${id}|${reload}`, (signal) => catalog.artist(id, signal))
  const songs = useLoaded<CatalogArtistSongs>(`${id}|${reload}|songs`, (signal) => catalog.artistSongs(id, signal))
  const merged = useMemo(() => {
    if (!songs.value) return null
    const used = new Set<number>()
    return songs.value.songs.map((s, i) => {
      const sid = s.id && !used.has(s.id) ? s.id : -(i + 1)
      used.add(sid)
      return { ...s, id: sid }
    })
  }, [songs.value])
  const known = useMemo(() => {
    const byId = new Map<number, CatalogTrack>()
    const byTitle = new Map<string, CatalogTrack[]>()
    for (const song of merged ?? []) {
      if (song.id > 0) byId.set(song.id, song)
      for (const f of song.found ?? []) if (f.source === "deezer") byId.set(Number(f.id), song)
      const key = titleKey(song.title)
      byTitle.set(key, [...(byTitle.get(key) ?? []), song])
    }
    return { byId, byTitle }
  }, [merged])
  const withFound = useCallback(
    (tracks: CatalogTrack[]) =>
      tracks.map((t) => {
        const song =
          known.byId.get(t.id) ?? known.byTitle.get(titleKey(t.title))?.find((x) => sameLength(x.duration, t.duration))
        return song ? { ...t, found: t.found ?? song.found, trackId: t.trackId || song.trackId } : t
      }),
    [known]
  )
  const [releaseTracks, setReleaseTracks] = useState<Record<number, CatalogTrack[]>>({})
  const [open, setOpen] = useState<Set<number>>(new Set())
  const [checking, setChecking] = useState<{ done: number; total: number } | null>(null)
  const tools = useFindTools()

  const loadRelease = useCallback(async (releaseId: number, signal?: AbortSignal) => {
    const r = await catalog.release(releaseId, signal)
    setReleaseTracks((prev) => ({ ...prev, [releaseId]: r.tracks }))
    return r.tracks
  }, [])

  const loadedIds = useRef<number[]>([])
  useEffect(() => {
    loadedIds.current = Object.keys(releaseTracks).map(Number)
  }, [releaseTracks])
  useEffect(() => {
    if (reload === 0) return
    loadedIds.current.forEach((rid) => loadRelease(rid).catch(() => {}))
  }, [reload, loadRelease])

  const data = page.value
  if (!data) {
    return page.error ? <EmptyState icon={SearchX} title="Couldn't load this artist" subtitle={page.error} compact /> : <Spinner />
  }

  const toggle = (rid: number) => {
    setOpen((prev) => {
      const next = new Set(prev)
      if (next.has(rid)) next.delete(rid)
      else next.add(rid)
      return next
    })
    if (!releaseTracks[rid]) loadRelease(rid).catch((e: Error) => toast.error(e.message))
  }

  const checkAll = async () => {
    const todo = data.releases.filter((r) => !releaseTracks[r.id])
    setChecking({ done: 0, total: todo.length })
    await runPool(todo, RELEASE_CONCURRENCY, async (r) => {
      await loadRelease(r.id).catch(() => {})
      setChecking((c) => (c ? { ...c, done: c.done + 1 } : c))
    })
    setChecking(null)
  }

  const loaded = Object.values(releaseTracks).flat()
  const everything = [...data.top, ...loaded]
  const unique = new Map<number, CatalogTrack>()
  for (const t of everything) unique.set(t.id, t)
  const all = merged ?? [...unique.values()]
  const allLoaded = !!merged || data.releases.every((r) => releaseTracks[r.id])
  const have = haveCount(all, tools)
  const missing = missingTracks(all, tools)
  const found = foundCount(all, tools)
  const sourceCount = songs.value?.sources.filter((src) => !src.error && src.count > 0).length ?? 0

  return (
    <>
      <header className="flex flex-col gap-5 md:flex-row md:items-end md:gap-6">
        <div className="w-36 shrink-0 md:w-44">
          <ArtistImage name={data.artist.name} picture={data.artist.picture} className="shadow-2xl" initialsClassName="text-5xl" />
        </div>
        <div className="min-w-0">
          <p className="text-[11px] font-semibold tracking-widest text-emerald-400/90 uppercase">Discography</p>
          <h1 className="text-3xl font-black tracking-tight wrap-break-word text-zinc-50 md:text-5xl">{data.artist.name}</h1>
          <p className="mt-2 text-sm text-zinc-400">
            {data.releases.length} releases and {data.artist.fans.toLocaleString()} fans on Deezer
            {data.artist.trackCount > 0 ? (
              <>
                {" · "}
                <button
                  type="button"
                  onClick={() => useUIStore.getState().navigate("artist", data.artist.name)}
                  className="text-emerald-300 hover:underline"
                >
                  {data.artist.trackCount} in your library
                </button>
              </>
            ) : null}
          </p>
        </div>
      </header>

      <Toolbar
        summary={
          merged
            ? `You have ${have} of ${all.length} songs found on ${sourceCount} ${sourceCount === 1 ? "source" : "sources"}`
            : allLoaded
              ? `You have ${have} of ${all.length} songs`
              : `${Object.keys(releaseTracks).length} of ${data.releases.length} releases checked`
        }
        extra={
          !allLoaded ? (
            <Button
              variant="outline"
              size="sm"
              disabled={!!checking}
              onClick={checkAll}
              className="border-zinc-700 bg-zinc-900/60 text-zinc-200 hover:bg-zinc-800"
            >
              {checking ? <Loader2 className="size-4 animate-spin" /> : <Library className="size-4" />}
              {checking ? `Checking ${checking.done} of ${checking.total}` : "Check all releases"}
            </Button>
          ) : null
        }
        missing={missing}
        found={found}
        title={data.artist.name}
        tracks={all}
      />

      {merged && songs.value ? (
        <section className="mt-8">
          <h2 className="mb-1 text-lg font-bold text-zinc-100">All songs</h2>
          <SourceSummary songs={songs.value} />
          <TrackRows tracks={merged} showAlbum />
        </section>
      ) : (
        <>
          <p className="mt-6 flex items-center gap-2 text-xs text-zinc-500">
            {songs.error ? (
              <>Couldn&apos;t look on other sources: {songs.error}</>
            ) : (
              <>
                <Loader2 className="size-3.5 animate-spin" /> Looking for more songs on YouTube Music and SoundCloud…
              </>
            )}
          </p>
          {data.top.length > 0 ? (
            <section className="mt-6">
              <h2 className="mb-2 text-lg font-bold text-zinc-100">Popular</h2>
              <TrackRows tracks={data.top} showAlbum />
            </section>
          ) : null}
        </>
      )}

      {RELEASE_GROUPS.map((g) => {
        const list = data.releases.filter((r) => r.type === g.type)
        if (list.length === 0) return null
        return (
          <section key={g.type} className="mt-8">
            <h2 className="mb-2 text-lg font-bold text-zinc-100">{g.label}</h2>
            <div className="flex flex-col gap-1">
              {list.map((r) => (
                <ReleaseRow
                  key={r.id}
                  release={r}
                  tracks={releaseTracks[r.id] ? withFound(releaseTracks[r.id]) : undefined}
                  open={open.has(r.id)}
                  onToggle={() => toggle(r.id)}
                  load={async () => withFound(releaseTracks[r.id] ?? (await loadRelease(r.id)))}
                />
              ))}
            </div>
          </section>
        )
      })}
    </>
  )
}

function ReleasePage({ id, reload }: { id: string; reload: number }) {
  const page = useLoaded<CatalogReleasePage>(`${id}|${reload}`, (signal) => catalog.release(id, signal))
  const tools = useFindTools()
  const data = page.value
  if (!data) {
    return page.error ? <EmptyState icon={SearchX} title="Couldn't load this album" subtitle={page.error} compact /> : <Spinner />
  }
  const have = haveCount(data.tracks, tools)
  return (
    <>
      <header className="flex flex-col gap-5 md:flex-row md:items-end md:gap-6">
        <ReleaseArt release={data.release} className="w-40 shadow-2xl md:w-48" />
        <div className="min-w-0">
          <p className="text-[11px] font-semibold tracking-widest text-emerald-400/90 uppercase">
            {data.release.type || "Album"}
            {data.release.year ? ` · ${data.release.year}` : ""}
          </p>
          <h1 className="text-3xl font-black tracking-tight wrap-break-word text-zinc-50 md:text-4xl">{data.release.title}</h1>
          <p className="mt-2 text-sm text-zinc-400">{data.release.artist}</p>
        </div>
      </header>
      <Toolbar
        summary={`You have ${have} of ${data.tracks.length} songs`}
        extra={
          have < data.tracks.length ? (
            <AddAllButton
              statusKey={`release:${data.release.id}`}
              title={`${data.release.artist} – ${data.release.title}`}
              load={async () => data.tracks}
            />
          ) : null
        }
        missing={missingTracks(data.tracks, tools)}
        found={foundCount(data.tracks, tools)}
        title={`${data.release.artist} – ${data.release.title}`}
        tracks={data.tracks}
      />
      <div className="mt-6">
        <TrackRows tracks={data.tracks} />
      </div>
    </>
  )
}

function Toolbar({
  summary,
  extra,
  missing,
  found,
  title,
  tracks,
}: {
  summary: string
  extra?: React.ReactNode
  missing: CatalogTrack[]
  found: number
  title: string
  tracks: CatalogTrack[]
}) {
  const tools = useFindTools()
  const unchecked = missing.filter((t) => {
    const s = tools.stateOf(t)?.state
    return !s || s === "error"
  })
  const running = jobActive(tools.job)
  return (
    <div className="sticky top-0 z-10 -mx-4 mt-6 flex flex-wrap items-center gap-2 border-b border-zinc-800/70 bg-zinc-950/90 px-4 py-3 backdrop-blur md:-mx-6 md:px-6">
      <span className="mr-auto text-sm text-zinc-300">{summary}</span>
      {extra}
      {tools.sources.length > 0 ? (
        <Select value={tools.sourceKey} onValueChange={tools.setSourceKey}>
          <SelectTrigger aria-label="Where to look for missing songs" className="h-9 w-36 border-zinc-700/80 bg-zinc-900/60 text-sm">
            <SelectValue placeholder="Source" />
          </SelectTrigger>
          <SelectContent>
            {tools.sources.map((s) => (
              <SelectItem key={s.key} value={s.key}>
                {s.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      ) : null}
      <Button
        variant="outline"
        size="sm"
        disabled={tools.finding || unchecked.length === 0 || !tools.sourceKey}
        onClick={() => tools.find(unchecked)}
        className="border-zinc-700 bg-zinc-900/60 text-zinc-200 hover:bg-zinc-800"
      >
        {tools.finding ? <Loader2 className="size-4 animate-spin" /> : <Search className="size-4" />}
        Find missing{unchecked.length > 0 ? ` (${unchecked.length})` : ""}
      </Button>
      <Button
        size="sm"
        disabled={found === 0 || running}
        onClick={() => tools.addAll(title, tracks)}
        className="bg-emerald-500 font-semibold text-zinc-950 hover:bg-emerald-400"
      >
        {running ? <Loader2 className="size-4 animate-spin" /> : <Plus className="size-4" />}
        {running && tools.job
          ? `Adding ${tools.job.done} of ${tools.job.total}`
          : found > 0
            ? `Add ${found} found`
            : "Add found"}
      </Button>
    </div>
  )
}

function AddAllButton({
  statusKey,
  title,
  load,
}: {
  statusKey: string
  title: string
  load: () => Promise<CatalogTrack[]>
}) {
  const tools = useFindTools()
  const status = tools.releaseStatus(statusKey)
  const sourceName = tools.sources.find((x) => x.key === tools.sourceKey)?.name ?? "the source"
  return (
    <Button
      variant="outline"
      size="sm"
      disabled={!!status || !tools.sourceKey}
      onClick={() => tools.addRelease(statusKey, title, load)}
      title={`Find the songs you're missing on ${sourceName} and add them all`}
      className="h-8 shrink-0 border-zinc-700 bg-zinc-900/60 px-3 text-xs text-zinc-200 hover:bg-zinc-800 hover:text-emerald-300"
    >
      {status ? <Loader2 className="size-3.5 animate-spin" /> : <Plus className="size-3.5" />}
      {status?.state === "finding"
        ? status.total > 0
          ? `Finding ${status.done} of ${status.total}`
          : "Loading…"
        : status?.state === "adding"
          ? `Adding ${status.job.done} of ${status.job.total}`
          : "Add all"}
    </Button>
  )
}

function ReleaseRow({
  release,
  tracks,
  open,
  onToggle,
  load,
}: {
  release: CatalogRelease
  tracks?: CatalogTrack[]
  open: boolean
  onToggle: () => void
  load: () => Promise<CatalogTrack[]>
}) {
  const tools = useFindTools()
  const have = tracks ? haveCount(tracks, tools) : null
  const complete = tracks && have === tracks.length
  const canAddAll = (release.type === "album" || release.type === "ep") && !complete
  const songCount = tracks?.length || release.trackCount
  return (
    <div className={cn("rounded-lg", open && "bg-zinc-900/50")}>
      <div className="flex items-center gap-2 pr-2">
        <button
          type="button"
          onClick={onToggle}
          aria-expanded={open}
          className="flex min-w-0 flex-1 items-center gap-3 rounded-lg p-2 text-left transition-colors hover:bg-zinc-800/60"
        >
          <ReleaseArt release={release} className="w-12 shrink-0" />
          <div className="min-w-0 flex-1">
            <p className="truncate text-sm font-semibold text-zinc-100">{release.title}</p>
            <p className="truncate text-xs text-zinc-500">
              {[release.year || "", songCount ? `${songCount} ${songCount === 1 ? "song" : "songs"}` : ""]
                .filter(Boolean)
                .join(" · ")}
            </p>
          </div>
          {have !== null && tracks ? (
            <span
              className={cn(
                "shrink-0 rounded-full px-2 py-0.5 text-xs font-medium",
                complete
                  ? "bg-emerald-500/15 text-emerald-300"
                  : have > 0
                    ? "bg-amber-500/10 text-amber-300"
                    : "bg-zinc-800 text-zinc-400"
              )}
            >
              {complete ? "Complete" : `${have}/${tracks.length}`}
            </span>
          ) : null}
          <ChevronDown className={cn("size-4 shrink-0 text-zinc-500 transition-transform", open && "rotate-180")} />
        </button>
        {canAddAll ? (
          <AddAllButton statusKey={`release:${release.id}`} title={`${release.artist} – ${release.title}`} load={load} />
        ) : null}
      </div>
      {open ? (
        <div className="px-2 pb-2">{tracks ? <TrackRows tracks={tracks} /> : <Spinner />}</div>
      ) : null}
    </div>
  )
}

function TrackRows({ tracks, showAlbum = false }: { tracks: CatalogTrack[]; showAlbum?: boolean }) {
  return (
    <ol className="flex flex-col">
      {tracks.map((t, i) => (
        <CatalogTrackRow key={`${t.id}-${i}`} track={t} index={showAlbum ? i + 1 : (t.position ?? i + 1)} showAlbum={showAlbum} />
      ))}
    </ol>
  )
}

function CatalogTrackRow({ track, index, showAlbum }: { track: CatalogTrack; index: number; showAlbum: boolean }) {
  const tools = useFindTools()
  const s = tools.stateOf(track)
  const libraryId = track.trackId || (s?.state === "added" ? s.trackId : "")
  const sourceName = tools.sources.find((x) => x.key === tools.sourceKey)?.name ?? "the source"

  let status: React.ReactNode
  if (libraryId) {
    status = (
      <span className="inline-flex items-center gap-1 rounded-full bg-emerald-500/15 px-2 py-0.5 text-xs text-emerald-300">
        <Check className="size-3" /> In library
      </span>
    )
  } else if (s?.state === "finding") {
    status = <Loader2 className="size-4 animate-spin text-zinc-500" aria-label="Looking" />
  } else if (s?.state === "found") {
    status = (
      <span className="flex items-center gap-2">
        {s.result.pageUrl ? (
          <a
            href={s.result.pageUrl}
            target="_blank"
            rel="noreferrer"
            title={`${s.result.artist} – ${s.result.title}`}
            className="hidden items-center gap-1 text-xs text-zinc-400 hover:text-zinc-100 sm:inline-flex"
          >
            {s.result.duration ? formatDuration(s.result.duration) : sourceName}
            <ExternalLink className="size-3" />
          </a>
        ) : null}
        <Button
          size="sm"
          onClick={() => tools.addOne(track, s.result)}
          className="h-7 bg-zinc-100 px-2.5 text-xs text-zinc-900 hover:bg-white"
        >
          <Plus className="size-3.5" /> Add
        </Button>
      </span>
    )
  } else if (s?.state === "missing") {
    status = <span className="text-xs text-zinc-500">Not on {sourceName}</span>
  } else if (s?.state === "error") {
    status = (
      <button type="button" title={s.error} onClick={() => tools.find([track])} className="text-xs text-red-400 hover:text-red-300">
        Failed · retry
      </button>
    )
  } else {
    status = (
      <button
        type="button"
        onClick={() => tools.find([track])}
        disabled={!tools.sourceKey}
        className="text-xs text-zinc-400 hover:text-emerald-300"
      >
        Find
      </button>
    )
  }

  return (
    <li
      onDoubleClick={() => libraryId && playLibraryTrack(libraryId)}
      className={cn("group flex items-center gap-3 rounded-md px-2 py-1.5", libraryId ? "hover:bg-zinc-800/60" : "opacity-90")}
    >
      <span className="w-6 shrink-0 text-right text-xs text-zinc-500 tabular-nums">
        {libraryId ? (
          <button
            type="button"
            aria-label={`Play ${track.title}`}
            onClick={() => playLibraryTrack(libraryId)}
            className="hidden text-zinc-200 group-hover:inline"
          >
            <Play className="ml-auto size-3.5 fill-current" />
          </button>
        ) : null}
        <span className={cn(libraryId && "group-hover:hidden")}>{index}</span>
      </span>
      {showAlbum && track.cover ? (
        <img src={catalog.imageUrl(track.cover)} alt="" loading="lazy" className="size-9 shrink-0 rounded bg-zinc-800 object-cover" />
      ) : null}
      <div className="min-w-0 flex-1">
        <p className={cn("truncate text-sm", libraryId ? "text-zinc-100" : "text-zinc-300")}>{track.title}</p>
        <p className="truncate text-xs text-zinc-500">
          {track.artist}
          {showAlbum && track.album ? ` · ${track.album}` : ""}
          {showAlbum && track.year ? ` · ${track.year}` : ""}
        </p>
        {track.found?.length ? <SourceBadges found={track.found} /> : null}
      </div>
      <span className="hidden w-12 shrink-0 text-right text-xs text-zinc-500 tabular-nums sm:block">
        {track.duration ? formatDuration(track.duration) : ""}
      </span>
      <div className="flex w-28 shrink-0 justify-end sm:w-36">{status}</div>
    </li>
  )
}
