"use client"

import { useEffect, useMemo, useState } from "react"
import { Check, ExternalLink, Loader2, SearchX, X } from "lucide-react"
import { cn } from "@/lib/utils"
import { catalog, fieldLookup } from "@/lib/api"
import { artistKey, formatDuration } from "@/lib/format"
import type { FieldOption, LookupSource } from "@/lib/types"

export type LookupField = "artist" | "album" | "albumArtist" | "genre" | "year"

export interface LookupPick {
  value: string
  albumArtist?: string
  year?: number
  cover?: string
  source: string
}

const SOURCE_PREF = "field-lookup-source"

const FIELD_LABELS: Record<LookupField, string> = {
  artist: "artist",
  album: "album",
  albumArtist: "album artist",
  genre: "genre",
  year: "year",
}

const TYPE_LABELS: Record<string, string> = {
  album: "Album",
  single: "Single",
  ep: "EP",
  compile: "Compilation",
  broadcast: "Broadcast",
}

function readSourcePref(): string {
  try {
    return localStorage.getItem(SOURCE_PREF) ?? ""
  } catch {
    return ""
  }
}

function writeSourcePref(key: string) {
  try {
    localStorage.setItem(SOURCE_PREF, key)
  } catch {}
}

function valueOf(o: FieldOption, field: LookupField): string {
  switch (field) {
    case "album":
      return o.album
    case "artist":
      return o.artist
    case "albumArtist":
      return o.albumArtist || o.artist
    case "genre":
      return o.genre
    case "year":
      return o.year ? String(o.year) : ""
  }
}

let sourcesCache: Promise<LookupSource[]> | null = null

function loadSources(): Promise<LookupSource[]> {
  sourcesCache ??= fieldLookup.sources().catch((e) => {
    sourcesCache = null
    throw e
  })
  return sourcesCache
}

export function FieldLookup({
  trackId,
  field,
  title,
  artist,
  current,
  onPick,
  onClose,
}: {
  trackId: string
  field: LookupField
  title: string
  artist: string
  current: string
  onPick: (pick: LookupPick) => void
  onClose: () => void
}) {
  const [sources, setSources] = useState<LookupSource[] | null>(null)
  const [sourceKey, setSourceKey] = useState("")
  const [result, setResult] = useState<{ key: string; options: FieldOption[]; error: string | null } | null>(null)
  const [withYear, setWithYear] = useState(true)
  const [withCover, setWithCover] = useState(true)

  useEffect(() => {
    let alive = true
    loadSources()
      .then((list) => {
        if (!alive) return
        setSources(list)
        const pref = readSourcePref()
        setSourceKey(list.some((s) => s.key === pref) ? pref : (list[0]?.key ?? ""))
      })
      .catch(() => {
        if (alive) setSources([])
      })
    return () => {
      alive = false
    }
  }, [])

  const requestKey = `${sourceKey}\n${title}\n${artist}`
  useEffect(() => {
    if (!sourceKey) return
    const ctl = new AbortController()
    fieldLookup
      .search(trackId, sourceKey, title, artist, ctl.signal)
      .then((r) => {
        if (!ctl.signal.aborted) setResult({ key: requestKey, options: r.options, error: null })
      })
      .catch((e: Error) => {
        if (!ctl.signal.aborted) setResult({ key: requestKey, options: [], error: e.message })
      })
    return () => ctl.abort()
  }, [trackId, sourceKey, title, artist, requestKey])

  const shown = result?.key === requestKey ? result : null

  const rows = useMemo(() => {
    if (!shown) return []
    const list = shown.options.filter((o) => valueOf(o, field).trim() !== "")
    if (field === "album") return list
    const seen = new Set<string>()
    return list.filter((o) => {
      const k = artistKey(valueOf(o, field))
      if (seen.has(k)) return false
      seen.add(k)
      return true
    })
  }, [shown, field])

  const currentKey = artistKey(current)
  const sourceName = sources?.find((s) => s.key === sourceKey)?.name ?? ""

  return (
    <section className="min-w-0 rounded-lg border border-zinc-800 bg-zinc-950/60" aria-label={`Look up the ${FIELD_LABELS[field]}`}>
      <div className="flex items-center gap-2 border-b border-zinc-800 px-3 py-2">
        <p className="text-sm font-medium text-zinc-200">
          Other {FIELD_LABELS[field] === "album" ? "albums" : `${FIELD_LABELS[field]}s`} for this song
        </p>
        <button
          type="button"
          onClick={onClose}
          aria-label="Close the lookup"
          className="ml-auto rounded p-1 text-zinc-500 hover:text-zinc-100"
        >
          <X className="size-4" />
        </button>
      </div>

      {sources && sources.length === 0 ? (
        <p className="px-3 py-4 text-sm text-zinc-500">Online lookups are turned off on this server.</p>
      ) : (
        <>
          <div className="flex flex-wrap gap-1 px-3 pt-2.5" role="tablist" aria-label="Source">
            {(sources ?? []).map((s) => (
              <button
                key={s.key}
                type="button"
                role="tab"
                aria-selected={sourceKey === s.key}
                onClick={() => {
                  setSourceKey(s.key)
                  writeSourcePref(s.key)
                }}
                className={cn(
                  "rounded-full px-2.5 py-1 text-xs font-medium transition-colors",
                  sourceKey === s.key ? "bg-zinc-100 text-zinc-900" : "bg-zinc-800/70 text-zinc-400 hover:text-zinc-100"
                )}
              >
                {s.name}
              </button>
            ))}
          </div>

          {field === "album" ? (
            <div className="flex flex-wrap gap-x-4 gap-y-1 px-3 pt-2 text-xs text-zinc-400">
              <label className="flex items-center gap-1.5">
                <input type="checkbox" checked={withYear} onChange={(e) => setWithYear(e.target.checked)} className="accent-emerald-500" />
                Also take its year
              </label>
              <label className="flex items-center gap-1.5">
                <input type="checkbox" checked={withCover} onChange={(e) => setWithCover(e.target.checked)} className="accent-emerald-500" />
                Also take its cover
              </label>
            </div>
          ) : null}

          <div className="mt-2 max-h-72 overflow-y-auto px-1.5 pb-1.5">
            {!shown ? (
              <div className="flex items-center justify-center py-8 text-zinc-500">
                <Loader2 className="size-5 animate-spin" />
              </div>
            ) : shown.error ? (
              <p className="px-2 py-4 text-sm text-red-400">{shown.error}</p>
            ) : rows.length === 0 ? (
              <p className="flex items-center gap-2 px-2 py-4 text-sm text-zinc-500">
                <SearchX className="size-4" /> {sourceName} has no other {FIELD_LABELS[field]} for “{title}”
                {artist ? ` by ${artist}` : ""}
              </p>
            ) : (
              <ul className="flex flex-col">
                {rows.map((o, i) => {
                  const value = valueOf(o, field)
                  const isCurrent = artistKey(value) === currentKey
                  const meta = [
                    field !== "year" && o.year ? String(o.year) : "",
                    field === "album" ? TYPE_LABELS[o.type] ?? "" : o.album,
                    field !== "artist" ? o.artist : "",
                    o.duration ? formatDuration(o.duration) : "",
                  ].filter(Boolean)
                  return (
                    <li key={`${value}-${o.year}-${i}`} className="group flex items-center gap-1">
                      <button
                        type="button"
                        onClick={() =>
                          onPick({
                            value,
                            albumArtist: field === "album" ? o.albumArtist || o.artist : undefined,
                            year: field === "album" && withYear && o.year ? o.year : undefined,
                            cover: field === "album" && withCover ? o.cover : undefined,
                            source: o.source,
                          })
                        }
                        className="flex min-w-0 flex-1 items-center gap-3 rounded-md px-2 py-1.5 text-left transition-colors hover:bg-zinc-800/70"
                      >
                        {field === "album" ? (
                          o.cover ? (
                            <img
                              src={catalog.imageUrl(o.cover)}
                              alt=""
                              loading="lazy"
                              className="size-10 shrink-0 rounded bg-zinc-800 object-cover"
                            />
                          ) : (
                            <span className="size-10 shrink-0 rounded bg-zinc-800" />
                          )
                        ) : null}
                        <span className="min-w-0 flex-1">
                          <span className="flex items-center gap-2 text-sm text-zinc-100">
                            <span className="truncate">{value}</span>
                            {isCurrent ? (
                              <span className="inline-flex shrink-0 items-center gap-0.5 rounded-full bg-emerald-500/15 px-1.5 text-[10px] font-medium text-emerald-300">
                                <Check className="size-3" /> current
                              </span>
                            ) : null}
                          </span>
                          <span className="block truncate text-xs text-zinc-500">{meta.join(" · ")}</span>
                        </span>
                      </button>
                      {o.url ? (
                        <a
                          href={o.url}
                          target="_blank"
                          rel="noreferrer"
                          aria-label={`Open on ${o.source}`}
                          className="rounded p-1.5 text-zinc-600 hover:text-zinc-200"
                        >
                          <ExternalLink className="size-3.5" />
                        </a>
                      ) : null}
                    </li>
                  )
                })}
              </ul>
            )}
          </div>
        </>
      )}
    </section>
  )
}
