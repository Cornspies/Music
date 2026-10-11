"use client"

import { useEffect, useMemo, useRef, useState } from "react"
import { ChevronLeft, ChevronRight, Disc3, Play } from "lucide-react"
import type { LucideIcon } from "lucide-react"
import { cn } from "@/lib/utils"
import { hashGradient } from "@/lib/format"
import type { HomeMix, Track } from "@/lib/types"
import { useLibraryStore } from "@/stores/library"
import { usePlayerStore } from "@/stores/player"
import { useUIStore } from "@/stores/ui"
import { ArtistImage } from "@/components/music/ArtistImage"
import { CoverArt } from "@/components/music/CoverArt"

export function useTracksById(ids: string[] | undefined): Track[] {
  const tracks = useLibraryStore((s) => s.tracks)
  return useMemo(() => {
    if (!ids || ids.length === 0) return []
    const byId = new Map(tracks.map((t) => [t.id, t]))
    return ids.map((id) => byId.get(id)).filter((t): t is Track => !!t)
  }, [tracks, ids])
}

export function Shelf({
  title,
  subtitle,
  action,
  children,
}: {
  title: string
  subtitle?: React.ReactNode
  action?: React.ReactNode
  children: React.ReactNode
}) {
  const rowRef = useRef<HTMLDivElement>(null)
  const [edges, setEdges] = useState({ start: true, end: true })
  const scroll = (dir: 1 | -1) => {
    const el = rowRef.current
    if (el) el.scrollBy({ left: dir * el.clientWidth * 0.8, behavior: "smooth" })
  }

  useEffect(() => {
    const el = rowRef.current
    if (!el) return
    const update = () => {
      const start = el.scrollLeft <= 1
      const end = el.scrollLeft + el.clientWidth >= el.scrollWidth - 1
      setEdges((prev) => (prev.start === start && prev.end === end ? prev : { start, end }))
    }
    update()
    el.addEventListener("scroll", update, { passive: true })
    const resize = new ResizeObserver(update)
    resize.observe(el)
    return () => {
      el.removeEventListener("scroll", update)
      resize.disconnect()
    }
  }, [children])
  return (
    <section className="mt-9 first:mt-6">
      <div className="mb-3 flex items-end justify-between gap-4">
        <div className="min-w-0">
          <h2 className="truncate text-xl font-bold tracking-tight text-zinc-50">{title}</h2>
          {subtitle ? <p className="mt-0.5 truncate text-xs text-zinc-500">{subtitle}</p> : null}
        </div>
        <div className="flex shrink-0 items-center gap-1">
          {action}
          <button
            type="button"
            aria-label={`Scroll ${title} back`}
            onClick={() => scroll(-1)}
            disabled={edges.start}
            className="hidden size-8 items-center justify-center rounded-full bg-zinc-900 text-zinc-400 transition-colors enabled:hover:bg-zinc-800 enabled:hover:text-zinc-100 disabled:cursor-default disabled:opacity-35 md:flex"
          >
            <ChevronLeft className="size-4" />
          </button>
          <button
            type="button"
            aria-label={`Scroll ${title} forward`}
            onClick={() => scroll(1)}
            disabled={edges.end}
            className="hidden size-8 items-center justify-center rounded-full bg-zinc-900 text-zinc-400 transition-colors enabled:hover:bg-zinc-800 enabled:hover:text-zinc-100 disabled:cursor-default disabled:opacity-35 md:flex"
          >
            <ChevronRight className="size-4" />
          </button>
        </div>
      </div>
      <div
        ref={rowRef}
        className="-mx-2 flex snap-x scroll-px-2 gap-1 overflow-x-auto pb-2 scrollbar-none! [&::-webkit-scrollbar]:hidden!"
      >
        {children}
      </div>
    </section>
  )
}

function PlayFab({ label, onPlay, className }: { label: string; onPlay: () => void; className?: string }) {
  return (
    <button
      type="button"
      aria-label={label}
      onClick={(e) => {
        e.stopPropagation()
        onPlay()
      }}
      className={cn(
        "flex size-10 translate-y-1 items-center justify-center rounded-full bg-emerald-500 text-zinc-950 opacity-0 shadow-xl shadow-black/50 transition-all group-hover:translate-y-0 group-hover:opacity-100 group-focus-within:opacity-100 hover:scale-105 hover:bg-emerald-400 [@media(hover:none)]:hidden",
        className
      )}
    >
      <Play className="size-4 translate-x-px fill-current" />
    </button>
  )
}

export function MixCover({ mix, compact = false }: { mix: HomeMix; compact?: boolean }) {
  const ids = mix.coverTrackIds
  const label = mix.kind === "artist" ? mix.seed ?? mix.title : mix.title
  return (
    <div className="relative aspect-square w-full overflow-hidden rounded-lg bg-zinc-800">
      {ids.length >= 4 && !compact ? (
        <div className="grid size-full grid-cols-2 grid-rows-2">
          {ids.slice(0, 4).map((id) => (
            <CoverArt key={id} trackId={id} hasCover size={128} sizes="96px" className="rounded-none" />
          ))}
        </div>
      ) : ids.length > 0 ? (
        <CoverArt trackId={ids[0]} hasCover size={256} sizes="176px" className="rounded-none" />
      ) : (
        <div className={cn("flex size-full items-center justify-center bg-linear-to-br", hashGradient(label))}>
          <Disc3 className="size-1/3 text-white/40" />
        </div>
      )}
      <div aria-hidden="true" className={cn("absolute inset-0 bg-linear-to-br opacity-40 mix-blend-multiply", hashGradient(label))} />
      {compact ? null : (
        <div className="absolute inset-x-0 bottom-0 bg-linear-to-t from-black/85 via-black/45 to-transparent px-3 pt-10 pb-3">
          <p className="text-[10px] font-semibold uppercase tracking-[0.18em] text-white/70">
            {mix.kind === "artist" ? "Mix" : "For you"}
          </p>
          <p className="line-clamp-2 text-lg leading-tight font-black text-white drop-shadow">{label}</p>
        </div>
      )}
    </div>
  )
}

export function MixCard({ mix }: { mix: HomeMix }) {
  const tracks = useTracksById(mix.trackIds)
  const open = () => useUIStore.getState().navigate("mix", mix.id)
  return (
    <div
      role="link"
      tabIndex={0}
      aria-label={`Open ${mix.title}`}
      onClick={open}
      onKeyDown={(e) => {
        if (e.key === "Enter" && e.target === e.currentTarget) open()
      }}
      className="group w-40 shrink-0 cursor-pointer snap-start rounded-xl p-2 transition-colors hover:bg-zinc-800/70 md:w-48"
    >
      <div className="relative">
        <div className="transition-transform duration-300 group-hover:scale-[1.02]">
          <MixCover mix={mix} />
        </div>
        {tracks.length > 0 ? (
          <PlayFab
            label={`Play ${mix.title}`}
            onPlay={() => usePlayerStore.getState().playAll(tracks)}
            className="absolute right-2 bottom-2"
          />
        ) : null}
      </div>
      <p className="mt-2 truncate text-sm font-semibold text-zinc-100">{mix.title}</p>
      <p className="mt-0.5 line-clamp-2 text-xs leading-snug text-zinc-500">{mix.subtitle}</p>
    </div>
  )
}

export function ArtistCircle({
  name,
  picture,
  caption,
  onOpen,
}: {
  name: string
  picture?: string
  caption: string
  onOpen: () => void
}) {
  return (
    <button
      type="button"
      onClick={onOpen}
      className="group w-32 shrink-0 snap-start rounded-xl p-2 text-left transition-colors hover:bg-zinc-800/70 md:w-40"
    >
      <div className="transition-transform duration-300 group-hover:scale-[1.03]">
        <ArtistImage
          name={name}
          picture={picture}
          sizes="(min-width: 768px) 144px, 112px"
          className="shadow-lg shadow-black/40"
          initialsClassName="text-3xl"
        />
      </div>
      <p className="mt-2.5 truncate text-sm font-semibold text-zinc-100">{name}</p>
      <p className="mt-0.5 truncate text-xs text-zinc-500">{caption}</p>
    </button>
  )
}

export function QuickTile({
  title,
  art,
  onOpen,
  onPlay,
}: {
  title: string
  art: React.ReactNode
  onOpen: () => void
  onPlay?: () => void
}) {
  return (
    <div
      role="link"
      tabIndex={0}
      onClick={onOpen}
      onKeyDown={(e) => {
        if (e.key === "Enter" && e.target === e.currentTarget) onOpen()
      }}
      className="group flex h-14 cursor-pointer items-center gap-3 overflow-hidden rounded-md bg-zinc-800/45 pr-2 transition-colors hover:bg-zinc-700/55 md:h-16"
    >
      <div className="size-14 shrink-0 overflow-hidden shadow-lg shadow-black/40 md:size-16">{art}</div>
      <p className="min-w-0 flex-1 truncate text-sm font-semibold text-zinc-100">{title}</p>
      {onPlay ? <PlayFab label={`Play ${title}`} onPlay={onPlay} className="size-8 shrink-0" /> : null}
    </div>
  )
}

export function IconArt({ icon: Icon, seed }: { icon: LucideIcon; seed: string }) {
  return (
    <div className={cn("flex size-full items-center justify-center bg-linear-to-br", hashGradient(seed))}>
      <Icon className="size-6 text-white/80" />
    </div>
  )
}
