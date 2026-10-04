"use client"

import { useEffect, useRef, useState } from "react"
import { toast } from "sonner"
import { Disc3, ListMusic, Loader2, Mic2, Music2, Search, SearchX, X } from "lucide-react"
import { api } from "@/lib/api"
import { formatTotal } from "@/lib/format"
import type { Album, Artist, Playlist, SearchResults } from "@/lib/types"
import { useUIStore } from "@/stores/ui"
import { CoverArt } from "@/components/music/CoverArt"
import { TrackList } from "@/components/music/TrackList"
import { VirtualTrackList } from "@/components/music/VirtualTrackList"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"

function useDebounced<T>(value: T, delay: number): T {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const t = window.setTimeout(() => setDebounced(value), delay)
    return () => window.clearTimeout(t)
  }, [value, delay])
  return debounced
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="mt-8">
      <h2 className="mb-3 text-lg font-bold tracking-tight">{title}</h2>
      {children}
    </section>
  )
}

export function SearchView() {
  const [query, setQuery] = useState("")
  const [results, setResults] = useState<SearchResults | null>(null)
  const debounced = useDebounced(query, 350)
  const inputRef = useRef<HTMLInputElement>(null)
  const navigate = useUIStore((s) => s.navigate)
  const [expanded, setExpanded] = useState<string | null>(null)
  const [loadingAll, setLoadingAll] = useState(false)

  useEffect(() => {
    const q = debounced.trim()
    if (!q) return
    const controller = new AbortController()
    const load = async () => {
      try {
        const r = await api<SearchResults>(`/api/search?q=${encodeURIComponent(q)}`, {
          signal: controller.signal,
        })
        setResults(r)
      } catch (e) {
        const err = e as Error
        if (err.name === "AbortError") return
        toast.error(err.message)
      }
    }
    load()
    return () => controller.abort()
  }, [debounced])

  useEffect(() => {
    const onFocusRequest = () => inputRef.current?.focus()
    window.addEventListener("music:focus-search", onFocusRequest)
    return () => window.removeEventListener("music:focus-search", onFocusRequest)
  }, [])

  const q = query.trim()
  const trackTotal = results ? (results.tracksTotal ?? results.tracks.length) : 0

  const showAllTracks = async () => {
    if (!results) return
    const forQuery = q
    if (results.tracks.length < trackTotal) {
      setLoadingAll(true)
      try {
        const all = await api<SearchResults>(`/api/search?q=${encodeURIComponent(forQuery)}&tracks=all`)
        if (inputRef.current?.value.trim() !== forQuery) return
        setResults(all)
      } catch (e) {
        toast.error((e as Error).message)
        return
      } finally {
        setLoadingAll(false)
      }
    }
    setExpanded(forQuery)
  }

  const hasResults =
    results &&
    (results.tracks.length > 0 ||
      results.albums.length > 0 ||
      results.artists.length > 0 ||
      results.playlists.length > 0)

  return (
    <div className="p-4 md:p-6">
      <h1 className="text-2xl font-bold tracking-tight md:text-3xl">Search</h1>

      <div className="relative mt-4 max-w-xl">
        <Search
          className="pointer-events-none absolute top-1/2 left-4 size-5 -translate-y-1/2 text-zinc-500"
          aria-hidden="true"
        />
        <Input
          ref={inputRef}
          type="search"
          autoFocus
          aria-label="Search your music"
          placeholder="Songs, artists, albums, playlists…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          className="h-12 rounded-full border-zinc-700/80 bg-zinc-900/70 pl-12 pr-11 text-base shadow-lg shadow-black/20"
        />
        {query && (
          <button
            type="button"
            aria-label="Clear search"
            onClick={() => {
              setQuery("")
              inputRef.current?.focus()
            }}
            className="absolute top-1/2 right-3 flex size-7 -translate-y-1/2 items-center justify-center rounded-full text-zinc-500 hover:bg-zinc-800 hover:text-zinc-200"
          >
            <X className="size-4" />
          </button>
        )}
      </div>

      <div className="mt-6">
        {!q ? (
          <div className="flex flex-col items-center gap-3 rounded-xl border border-dashed border-zinc-800 bg-zinc-900/40 px-6 py-12 text-center">
            <Search className="size-8 text-zinc-600" aria-hidden="true" />
            <p className="text-sm font-medium text-zinc-300">Search your library</p>
          </div>
        ) : !results ? (
          <div className="flex flex-col gap-2">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-14 w-full rounded-lg" />
            ))}
          </div>
        ) : !hasResults ? (
          <div className="flex flex-col items-center gap-3 rounded-xl border border-dashed border-zinc-800 px-6 py-12 text-center">
            <SearchX className="size-8 text-zinc-600" aria-hidden="true" />
            <p className="text-sm font-medium text-zinc-300">No results for “{q}”</p>
          </div>
        ) : (
          <>
            {results!.tracks.length > 0 && (
              <Section title="Tracks">
                {expanded === q ? (
                  <VirtualTrackList tracks={results!.tracks} />
                ) : (
                  <>
                    <TrackList tracks={results!.tracks} limit={10} />
                    {trackTotal > 10 ? (
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={loadingAll}
                        onClick={showAllTracks}
                        className="mt-2 h-8 px-3 text-xs text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100"
                      >
                        {loadingAll ? <Loader2 className="size-3.5 animate-spin" aria-hidden="true" /> : null}
                        Show all {trackTotal} tracks
                      </Button>
                    ) : null}
                  </>
                )}
              </Section>
            )}

            {results!.albums.length > 0 && (
              <Section title="Albums">
                <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-5">
                  {results!.albums.map((album: Album) => (
                    <AlbumResult key={`${album.name}|${album.artist}`} album={album} />
                  ))}
                </div>
              </Section>
            )}

            {results!.artists.length > 0 && (
              <Section title="Artists">
                <div className="flex flex-wrap gap-2">
                  {results!.artists.map((artist: Artist) => (
                    <button
                      key={artist.name}
                      type="button"
                      onClick={() => navigate("artist", artist.name)}
                      className="flex items-center gap-2 rounded-full border border-zinc-800 bg-zinc-900/70 py-1.5 pr-4 pl-1.5 text-sm text-zinc-300 transition-colors hover:border-zinc-600 hover:bg-zinc-800/70 hover:text-zinc-100"
                    >
                      <span className="flex size-8 items-center justify-center rounded-full bg-linear-to-br from-zinc-700 to-zinc-800 text-xs font-semibold text-emerald-400">
                        {artist.name.slice(0, 1).toUpperCase()}
                      </span>
                      {artist.name}
                      <span className="text-[11px] text-zinc-500">
                        {artist.trackCount} track{artist.trackCount === 1 ? "" : "s"}
                      </span>
                    </button>
                  ))}
                </div>
              </Section>
            )}

            {results!.playlists.length > 0 && (
              <Section title="Playlists">
                <div className="flex flex-col gap-1">
                  {results!.playlists.map((playlist: Playlist) => (
                    <button
                      key={playlist.id}
                      type="button"
                      onClick={() => navigate("playlist", playlist.id)}
                      className="flex items-center gap-3 rounded-lg px-2 py-2 text-left transition-colors hover:bg-zinc-800/60"
                    >
                      <span className="flex size-10 items-center justify-center rounded-md bg-linear-to-br from-zinc-700 to-zinc-800 text-zinc-400">
                        <ListMusic className="size-4" aria-hidden="true" />
                      </span>
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-sm font-medium text-zinc-200">
                          {playlist.name}
                        </span>
                        <span className="block text-xs text-zinc-500">
                          {playlist.trackCount} track{playlist.trackCount === 1 ? "" : "s"} ·{" "}
                          {formatTotal(playlist.totalDuration)}
                        </span>
                      </span>
                    </button>
                  ))}
                </div>
              </Section>
            )}
          </>
        )}
      </div>
    </div>
  )
}

function AlbumResult({ album }: { album: Album }) {
  const navigate = useUIStore((s) => s.navigate)
  const key = JSON.stringify({ name: album.name, artist: album.artist })
  return (
    <button
      type="button"
      onClick={() => navigate("album", key)}
      className="group rounded-xl p-2 text-left transition-colors hover:bg-zinc-800/60"
    >
      <div className="relative">
        <CoverArt
          trackId={album.hasCover ? album.coverTrackId : `${album.name}|${album.artist}`}
          title={album.name}
          hasCover={album.hasCover}
          size={256}
          sizes="(min-width: 1280px) calc((100vw - 340px) / 5), (min-width: 768px) calc((100vw - 340px) / 4), (min-width: 640px) calc((100vw - 60px) / 3), calc((100vw - 48px) / 2)"
        />
      </div>
      <p className="mt-2 truncate text-sm font-medium text-zinc-200">{album.name || "Unknown album"}</p>
      <p className="truncate text-xs text-zinc-500">
        {album.artist || "Unknown artist"} · {album.trackCount} track
        {album.trackCount === 1 ? "" : "s"}
      </p>
    </button>
  )
}
