"use client"

import { memo } from "react"
import { CloudDownload, Heart, Loader2, Play } from "lucide-react"
import { cn } from "@/lib/utils"
import { formatDuration } from "@/lib/format"
import type { Track } from "@/lib/types"
import { usePlayerStore } from "@/stores/player"
import { useLibraryStore } from "@/stores/library"
import { useOfflineStore } from "@/stores/offline"
import { useUIStore } from "@/stores/ui"
import { CoverArt } from "@/components/music/CoverArt"
import { UserAvatar } from "@/components/music/UserAvatar"
import { EqualizerButton } from "@/components/music/EqualizerIcon"
import { TrackMenu } from "@/components/music/TrackMenu"
import { useContextMenu } from "@/components/music/MenuAtPoint"
import { ArtistLinks, JumpLink } from "@/components/music/ArtistLinks"

interface TrackRowProps {
  track: Track
  index?: number
  contextQueue?: Track[]
  onRemoveFromPlaylist?: (trackId: string) => void
  onDismissAttention?: (trackId: string) => void
  onRestoreAttention?: (trackId: string) => void
}

export const TrackRow = memo(function TrackRow({ track, index, contextQueue, onRemoveFromPlaylist, onDismissAttention, onRestoreAttention }: TrackRowProps) {
  const isCurrent = usePlayerStore((s) => s.currentTrack?.id === track.id)
  const isPlaying = usePlayerStore((s) => s.isPlaying && s.currentTrack?.id === track.id)

  const isDownloaded = useOfflineStore((s) => s.ids.has(track.id))
  const downloadJob = useOfflineStore((s) => s.jobs[track.id])

  const menu = useContextMenu()

  const play = () => {
    usePlayerStore.getState().playNow(track, contextQueue ?? [track])
  }

  const isTouch = () =>
    typeof window !== "undefined" && window.matchMedia("(hover: none)").matches

  return (
    <div
      role="button"
      tabIndex={0}
      aria-label={`Play ${track.title} by ${track.artist || "unknown artist"}`}
      onDoubleClick={play}
      onClick={() => {
        if (isTouch()) play()
      }}
      onContextMenu={menu.onContextMenu}
      onKeyDown={(e) => {
        if (e.key === "Enter" && e.target === e.currentTarget) play()
      }}
      className={cn(
        "group flex cursor-default items-center gap-3 rounded-lg px-2 py-1.5 select-none md:px-3",
        "hover:bg-zinc-800/60 focus-visible:bg-zinc-800/60 focus-visible:outline-none",
        isCurrent && "bg-zinc-800/40"
      )}
    >
      <div className="hidden w-6 shrink-0 items-center justify-center sm:flex">
        {isCurrent ? (
          <EqualizerButton playing={isPlaying} />
        ) : (
          <>
            <span className="text-xs tabular-nums text-zinc-500 group-hover:hidden">
              {index !== undefined ? index + 1 : ""}
            </span>
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation()
                play()
              }}
              aria-label={`Play ${track.title}`}
              className="hidden text-zinc-200 transition-colors hover:text-emerald-400 group-hover:block"
            >
              <Play className="size-3.5 fill-current" />
            </button>
          </>
        )}
      </div>

      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation()
          play()
        }}
        aria-hidden="true"
        tabIndex={-1}
        className="w-10 shrink-0 md:w-11"
      >
        <CoverArt trackId={track.id} title={track.title} hasCover={track.hasCover} />
      </button>

      <div className="min-w-0 flex-1">
        <div
          className={cn(
            "truncate text-sm font-medium",
            isCurrent ? "text-emerald-400" : "text-zinc-200"
          )}
        >
          {track.title}
        </div>
        <div className="flex items-center gap-1.5 truncate text-xs text-zinc-500">
          {(track.metadataStatus === "queued" || track.metadataStatus === "running") && (
            <Loader2
              className="size-3 shrink-0 animate-spin text-emerald-500/80"
              aria-label="Looking up metadata"
            />
          )}
          <span className="truncate">
            <ArtistLinks credit={track.artist} />
            {track.year ? ` · ${track.year}` : ""}
          </span>
        </div>
      </div>

      <div className="hidden w-40 shrink-0 truncate text-xs text-zinc-500 lg:block xl:w-52">
        {track.album ? (
          <JumpLink
            label={track.album}
            onOpen={() =>
              useUIStore.getState().navigate("album", JSON.stringify({ name: track.album, artist: track.artist }))
            }
          />
        ) : (
          "—"
        )}
      </div>

      <div className="hidden w-12 shrink-0 text-right text-xs tabular-nums text-zinc-600 md:block">
        {track.plays > 0 ? track.plays : "—"}
      </div>

      {downloadJob ? (
        <span
          className="hidden shrink-0 items-center gap-1 text-[10px] tabular-nums text-sky-300 sm:flex"
          title="Downloading for offline"
        >
          <Loader2 className="size-3 animate-spin" aria-hidden="true" />
          {downloadJob.total > 0 ? `${Math.round((downloadJob.loaded / downloadJob.total) * 100)}%` : ""}
        </span>
      ) : isDownloaded ? (
        <CloudDownload
          className="hidden size-3.5 shrink-0 text-sky-400/70 sm:block"
          aria-label="Available offline"
        />
      ) : null}

      <div className="w-11 shrink-0 text-right text-xs tabular-nums text-zinc-500">
        {formatDuration(track.duration)}
      </div>

      <div className="hidden w-7 shrink-0 justify-center sm:flex">
        {track.uploadedBy?.id ? (
          <UserAvatar
            userId={track.uploadedBy.id}
            username={track.uploadedBy.username}
            hasAvatar={track.uploadedBy.hasAvatar}
            size={20}
            label={`Uploaded by ${track.uploadedBy.username || "someone"}`}
            className="opacity-70 ring-1 ring-zinc-700/60 transition-opacity group-hover:opacity-100"
          />
        ) : null}
      </div>

      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation()
          useLibraryStore.getState().toggleFavorite(track.id)
        }}
        aria-label={track.isFavorite ? "Remove from favorites" : "Add to favorites"}
        aria-pressed={track.isFavorite}
        className={cn(
          "flex size-8 shrink-0 items-center justify-center rounded-md text-zinc-500 transition-all hover:text-zinc-200",
          track.isFavorite
            ? "opacity-100 text-emerald-400 hover:text-emerald-400"
            : "opacity-0 focus-visible:opacity-100 group-hover:opacity-100"
        )}
      >
        <Heart className={cn("size-4", track.isFavorite && "fill-current")} />
      </button>

      <TrackMenu
        track={track}
        onRemoveFromPlaylist={onRemoveFromPlaylist}
        onDismissAttention={onDismissAttention}
        onRestoreAttention={onRestoreAttention}
        triggerClassName="opacity-0 transition-opacity focus-visible:opacity-100 group-hover:opacity-100 data-[state=open]:opacity-100"
        open={menu.open}
        onOpenChange={menu.onOpenChange}
        point={menu.point}
      />
    </div>
  )
})
