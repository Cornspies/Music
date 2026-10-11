"use client"

import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react"
import { createPortal } from "react-dom"
import { Check, ListPlus, Loader2, Plus, Search } from "lucide-react"
import { cn } from "@/lib/utils"
import { formatDuration } from "@/lib/format"
import type { Track } from "@/lib/types"
import { addTracksWithToast, useLibraryStore } from "@/stores/library"
import { CoverArt } from "@/components/music/CoverArt"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"

const MAX_RESULTS = 60
const RECENT = 30

function matches(track: Track, words: string[]): boolean {
  const hay = `${track.title} ${track.artist} ${track.album}`.toLowerCase()
  return words.every((w) => hay.includes(w))
}

export function AddSongsButton({ playlist, trackIds }: { playlist: { id: string; name: string }; trackIds: string[] }) {
  const tracks = useLibraryStore((s) => s.tracks)
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState("")
  const [active, setActive] = useState(0)
  const [pending, setPending] = useState<Set<string>>(() => new Set())
  const [added, setAdded] = useState<Set<string>>(() => new Set())
  const [pos, setPos] = useState<{ left: number; top?: number; bottom?: number; width: number; maxHeight: number } | null>(null)
  const buttonRef = useRef<HTMLButtonElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  const inPlaylist = useMemo(() => new Set(trackIds), [trackIds])
  const has = (id: string) => inPlaylist.has(id) || added.has(id)

  const words = useMemo(() => query.toLowerCase().split(/\s+/).filter(Boolean), [query])
  const results = useMemo(() => {
    if (!open) return []
    if (words.length === 0) {
      return [...tracks].sort((a, b) => (a.createdAt < b.createdAt ? 1 : a.createdAt > b.createdAt ? -1 : 0)).slice(0, RECENT)
    }
    const out: Track[] = []
    for (const t of tracks) {
      if (matches(t, words)) out.push(t)
      if (out.length >= MAX_RESULTS) break
    }
    return out
  }, [open, tracks, words])
  const addable = results.filter((t) => !inPlaylist.has(t.id) && !added.has(t.id))

  useLayoutEffect(() => {
    if (!open) return
    const place = () => {
      const r = buttonRef.current?.getBoundingClientRect()
      if (!r) return
      const vw = window.innerWidth
      const vh = window.innerHeight
      const width = Math.min(440, vw - 24)
      const left = Math.max(12, Math.min(r.left, vw - width - 12))
      const below = vh - r.bottom - 16
      const above = r.top - 16
      if (below >= 320 || below >= above) setPos({ left, top: r.bottom + 6, width, maxHeight: Math.min(520, below) })
      else setPos({ left, bottom: vh - r.top + 6, width, maxHeight: Math.min(520, above) })
    }
    place()
    window.addEventListener("resize", place)
    const main = buttonRef.current?.closest("main")
    main?.addEventListener("scroll", place, { passive: true })
    return () => {
      window.removeEventListener("resize", place)
      main?.removeEventListener("scroll", place)
    }
  }, [open])

  useEffect(() => {
    if (!open) return
    const onDown = (e: PointerEvent) => {
      const t = e.target as Node
      if (panelRef.current?.contains(t) || buttonRef.current?.contains(t)) return
      setOpen(false)
    }
    document.addEventListener("pointerdown", onDown)
    return () => document.removeEventListener("pointerdown", onDown)
  }, [open])

  useEffect(() => {
    listRef.current?.querySelector(`[data-index="${active}"]`)?.scrollIntoView({ block: "nearest" })
  }, [active])

  const add = async (list: Track[]) => {
    const fresh = list.filter((t) => !has(t.id) && !pending.has(t.id))
    if (fresh.length === 0) return
    setPending((prev) => new Set([...prev, ...fresh.map((t) => t.id)]))
    try {
      if (await addTracksWithToast(playlist, fresh)) {
        setAdded((prev) => new Set([...prev, ...fresh.map((t) => t.id)]))
      }
    } finally {
      setPending((prev) => {
        const next = new Set(prev)
        for (const t of fresh) next.delete(t.id)
        return next
      })
    }
  }

  const close = () => {
    setOpen(false)
    buttonRef.current?.focus()
  }

  return (
    <>
      <Button
        ref={buttonRef}
        variant="outline"
        aria-expanded={open}
        aria-haspopup="dialog"
        onClick={() => {
          setOpen((o) => !o)
          setActive(0)
        }}
        className={cn(
          "h-10 rounded-full border-zinc-600/80 bg-zinc-900/60 px-5 text-zinc-200 hover:bg-zinc-800 hover:text-emerald-300",
          open && "border-emerald-500/60 text-emerald-300"
        )}
      >
        <ListPlus className="size-4" /> Add songs
      </Button>
      {open && pos
        ? createPortal(
            <div
              ref={panelRef}
              role="dialog"
              aria-label={`Add songs to ${playlist.name}`}
              style={{ left: pos.left, top: pos.top, bottom: pos.bottom, width: pos.width, maxHeight: pos.maxHeight }}
              className="fixed z-50 flex flex-col overflow-hidden rounded-xl border border-zinc-700/80 bg-zinc-900 shadow-2xl shadow-black/60"
              onKeyDown={(e) => {
                if (e.key === "Escape") {
                  e.stopPropagation()
                  close()
                }
              }}
            >
              <div className="relative border-b border-zinc-800 p-2">
                <Search
                  className="pointer-events-none absolute top-1/2 left-5 size-4 -translate-y-1/2 text-zinc-500"
                  aria-hidden="true"
                />
                <Input
                  autoFocus
                  type="search"
                  value={query}
                  placeholder="Search your library…"
                  aria-label="Search songs to add"
                  onChange={(e) => {
                    setQuery(e.target.value)
                    setActive(0)
                  }}
                  onKeyDown={(e) => {
                    if (e.key === "ArrowDown") {
                      e.preventDefault()
                      setActive((i) => Math.min(results.length - 1, i + 1))
                    } else if (e.key === "ArrowUp") {
                      e.preventDefault()
                      setActive((i) => Math.max(0, i - 1))
                    } else if (e.key === "Enter") {
                      e.preventDefault()
                      const t = results[active]
                      if (t) add([t])
                    }
                  }}
                  className="h-9 border-zinc-700/80 bg-zinc-950/60 pl-9"
                />
              </div>
              <div className="flex items-center justify-between gap-2 px-3 pt-2 pb-1 text-[11px] text-zinc-500">
                <span>
                  {words.length === 0
                    ? "Recently added"
                    : results.length === 0
                      ? "No matches"
                      : `${results.length >= MAX_RESULTS ? `First ${MAX_RESULTS}` : results.length} ${
                          results.length === 1 ? "match" : "matches"
                        }`}
                </span>
                {words.length > 0 && addable.length > 1 ? (
                  <button
                    type="button"
                    onClick={() => add(addable)}
                    className="rounded-full px-2 py-0.5 font-semibold text-emerald-300 hover:bg-emerald-500/10"
                  >
                    Add all {addable.length}
                  </button>
                ) : null}
              </div>
              <div ref={listRef} role="listbox" aria-label="Songs" className="min-h-0 flex-1 overflow-y-auto p-1.5 pt-0">
                {results.map((t, i) => {
                  const inside = has(t.id)
                  const busy = pending.has(t.id)
                  return (
                    <button
                      key={t.id}
                      type="button"
                      role="option"
                      aria-selected={i === active}
                      data-index={i}
                      disabled={inside}
                      onMouseEnter={() => setActive(i)}
                      onClick={() => add([t])}
                      className={cn(
                        "flex w-full items-center gap-3 rounded-lg px-2 py-1.5 text-left transition-colors",
                        i === active && !inside && "bg-zinc-800/80",
                        inside && "cursor-default opacity-60"
                      )}
                    >
                      <div className="w-9 shrink-0">
                        <CoverArt trackId={t.id} title={t.title} hasCover={t.hasCover} />
                      </div>
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-sm font-medium text-zinc-200">{t.title}</div>
                        <div className="truncate text-xs text-zinc-500">
                          {t.artist || "Unknown artist"}
                          {t.album ? ` · ${t.album}` : ""}
                        </div>
                      </div>
                      <span className="shrink-0 text-[11px] tabular-nums text-zinc-600">{formatDuration(t.duration)}</span>
                      <span
                        className={cn(
                          "flex size-7 shrink-0 items-center justify-center rounded-full",
                          inside ? "text-emerald-400" : "text-zinc-400",
                          i === active && !inside && "bg-emerald-500 text-zinc-950"
                        )}
                        aria-label={inside ? "Already in playlist" : "Add"}
                      >
                        {busy ? (
                          <Loader2 className="size-4 animate-spin" />
                        ) : inside ? (
                          <Check className="size-4" />
                        ) : (
                          <Plus className="size-4" />
                        )}
                      </span>
                    </button>
                  )
                })}
              </div>
            </div>,
            document.body
          )
        : null}
    </>
  )
}
