"use client"

import { ListEnd, Play, Shuffle, MoreHorizontal } from "lucide-react"
import type { LucideIcon } from "lucide-react"
import { toast } from "sonner"
import { cn } from "@/lib/utils"
import type { Track } from "@/lib/types"
import { usePlayerStore } from "@/stores/player"
import { inListenSession } from "@/stores/listen"
import { Button } from "@/components/ui/button"

interface CollectionHeaderProps {
  cover?: React.ReactNode
  icon: LucideIcon
  iconGradient?: string
  eyebrow?: string
  title: string
  meta: React.ReactNode
  tracks: Track[]
  actions?: React.ReactNode
}

export function CollectionHeader({
  cover,
  icon: Icon,
  iconGradient,
  eyebrow,
  title,
  meta,
  tracks,
  actions,
}: CollectionHeaderProps) {
  const playAll = () => usePlayerStore.getState().playAll(tracks)

  const playShuffled = () => usePlayerStore.getState().playAll(tracks, { shuffle: true })

  const addAll = () => {
    usePlayerStore.getState().addAllToQueue(tracks)
    // A listening session says itself how that went.
    if (!inListenSession()) {
      toast.success(`Added ${tracks.length} ${tracks.length === 1 ? "song" : "songs"} to the queue`)
    }
  }

  return (
    <header
      className={cn(
        "relative -mt-4 overflow-hidden rounded-b-2xl border-b border-zinc-800/60 md:-mt-6",
        "bg-linear-to-b from-zinc-800/80 via-zinc-900/60 to-transparent px-4 pt-10 pb-6 md:px-6 md:pt-14"
      )}
    >
      <div
        aria-hidden="true"
        className="pointer-events-none absolute -top-24 -right-10 size-64 rounded-full bg-emerald-500/10 blur-3xl"
      />

      <div className="relative flex flex-col gap-5 md:flex-row md:items-end md:gap-6">
        {cover ? (
          <div className="w-40 shrink-0 drop-shadow-2xl md:w-48 lg:w-56">{cover}</div>
        ) : (
          <div
            aria-hidden="true"
            className={cn(
              "flex size-36 shrink-0 items-center justify-center rounded-xl bg-linear-to-br shadow-2xl md:size-44 lg:size-48",
              iconGradient ?? "from-zinc-700 via-zinc-800 to-zinc-900"
            )}
          >
            <Icon className="size-16 text-zinc-400/80 md:size-20" />
          </div>
        )}

        <div className="flex min-w-0 flex-1 flex-col gap-2">
          {eyebrow ? (
            <p className="text-[11px] font-semibold uppercase tracking-widest text-emerald-400/90">
              {eyebrow}
            </p>
          ) : null}
          <h1 className="text-3xl font-black tracking-tight wrap-break-word md:text-4xl lg:text-5xl">
            {title}
          </h1>
          <p className="text-sm text-zinc-400">{meta}</p>

          <div className="mt-3 flex flex-wrap items-center gap-2.5">
            <Button
              onClick={playAll}
              disabled={tracks.length === 0}
              className="h-10 rounded-full bg-emerald-500 px-5 font-semibold text-zinc-950 hover:scale-105 hover:bg-emerald-400 transition-transform disabled:hover:scale-100"
            >
              <Play className="size-4 fill-current" /> Play all
            </Button>
            <Button
              variant="outline"
              onClick={playShuffled}
              disabled={tracks.length === 0}
              className="h-10 rounded-full border-zinc-600/80 bg-zinc-900/60 px-5 text-zinc-200 hover:bg-zinc-800 hover:text-emerald-300"
            >
              <Shuffle className="size-4" /> Shuffle
            </Button>
            <Button
              variant="outline"
              onClick={addAll}
              disabled={tracks.length === 0}
              className="h-10 rounded-full border-zinc-600/80 bg-zinc-900/60 px-5 text-zinc-200 hover:bg-zinc-800 hover:text-emerald-300"
            >
              <ListEnd className="size-4" /> Add to queue
            </Button>
            {actions}
          </div>
        </div>
      </div>
    </header>
  )
}

export function HeaderMenuTrigger(props: React.ComponentProps<typeof Button>) {
  return (
    <Button
      variant="outline"
      size="icon"
      aria-label="More options"
      className="size-10 rounded-full border-zinc-600/80 bg-zinc-900/60 text-zinc-300 hover:bg-zinc-800 hover:text-zinc-100"
      {...props}
    >
      <MoreHorizontal className="size-4" />
    </Button>
  )
}
