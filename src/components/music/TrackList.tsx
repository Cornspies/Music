"use client"

import type { ReactNode } from "react"
import type { LucideIcon } from "lucide-react"
import type { Track } from "@/lib/types"
import { TrackRow } from "@/components/music/TrackRow"

interface TrackListProps {
  tracks: Track[]
  /** playlist mode: pass a remover to show "Remove from this playlist" */
  onRemoveFromPlaylist?: (trackId: string) => void
  emptyIcon?: LucideIcon
  emptyTitle?: string
  emptySubtitle?: string
  emptyAction?: ReactNode
  /** rows to render initially (low-resource cap), default all */
  limit?: number
}

export function TrackList({
  tracks,
  onRemoveFromPlaylist,
  emptyTitle,
  emptySubtitle,
  emptyAction,
  limit,
}: TrackListProps) {
  if (tracks.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center gap-2 rounded-xl border border-dashed border-zinc-800 px-6 py-12 text-center">
        {emptyTitle ? (
          <p className="text-sm font-medium text-zinc-300">{emptyTitle}</p>
        ) : null}
        {emptySubtitle ? <p className="text-xs text-zinc-500">{emptySubtitle}</p> : null}
        {emptyAction}
      </div>
    )
  }

  const visible = limit ? tracks.slice(0, limit) : tracks

  return (
    <div role="list" aria-label="Tracks" className="flex flex-col">
      {visible.map((track, i) => (
        <TrackRow
          key={`${track.id}-${i}`}
          track={track}
          index={i}
          contextQueue={tracks}
          onRemoveFromPlaylist={onRemoveFromPlaylist}
        />
      ))}
    </div>
  )
}
