"use client"

import { useState } from "react"
import { toast } from "sonner"
import {
  Check,
  CloudDownload,
  CloudOff,
  Download,
  ListEnd,
  ListPlus,
  ListStart,
  Loader2,
  MoreHorizontal,
  Music4,
  Pencil,
  Play,
  RefreshCw,
  Scissors,
  ListMinus,
  Share2,
  Trash2,
  Undo2,
  Wrench,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { downloadUrl, requestMetadataLookup } from "@/lib/api"
import { codecName, qualityDetail } from "@/lib/format"
import type { Track } from "@/lib/types"
import { usePlayerStore } from "@/stores/player"
import { addTracksWithToast, useLibraryStore } from "@/stores/library"
import { useOfflineStore } from "@/stores/offline"
import { useAuthStore } from "@/stores/auth"
import { useUIStore } from "@/stores/ui"
import { inListenSession } from "@/stores/listen"
import { selectedTracks, useSelectionStore } from "@/stores/selection"
import { MenuAtPoint, type MenuPoint } from "@/components/music/MenuAtPoint"
import { DeleteTrackDialog } from "@/components/music/DeleteTrackDialog"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

function extForMime(mime: string): string {
  if (mime.includes("mpeg")) return ".mp3"
  if (mime.includes("wav")) return ".wav"
  if (mime.includes("flac")) return ".flac"
  if (mime.includes("ogg")) return ".ogg"
  if (mime.includes("mp4") || mime.includes("m4a")) return ".m4a"
  if (mime.includes("aac")) return ".aac"
  return ""
}

interface TrackMenuProps {
  track: Track
  onRemoveFromPlaylist?: (trackId: string) => void
  onDismissAttention?: (trackId: string) => void
  onRestoreAttention?: (trackId: string) => void
  triggerClassName?: string
  side?: "top" | "bottom"
  showPlay?: boolean
  open?: boolean
  onOpenChange?: (open: boolean) => void
  point?: MenuPoint | null
}

export function TrackMenu({
  track,
  onRemoveFromPlaylist,
  onDismissAttention,
  onRestoreAttention,
  triggerClassName,
  side = "bottom",
  showPlay = true,
  open,
  onOpenChange,
  point,
}: TrackMenuProps) {
  const [deleteOpen, setDeleteOpen] = useState(false)

  const offlineSupported = useOfflineStore((s) => s.supported)
  const isDownloaded = useOfflineStore((s) => s.ids.has(track.id))
  const downloading = useOfflineStore((s) => Boolean(s.jobs[track.id]))
  const canManage = useAuthStore(
    (s) => s.user?.id === track.uploadedBy.id || s.user?.role === "admin"
  )

  const stop = (e: React.SyntheticEvent) => e.stopPropagation()

  const content = (align: "start" | "end", at: "top" | "bottom") => (
    <TrackMenuContent
      track={track}
      side={at}
      align={align}
      showPlay={showPlay}
      onRemoveFromPlaylist={onRemoveFromPlaylist}
      onDismissAttention={onDismissAttention}
      onRestoreAttention={onRestoreAttention}
      onDelete={() => setDeleteOpen(true)}
      canManage={canManage}
      offline={{
        supported: offlineSupported,
        downloaded: isDownloaded,
        downloading,
      }}
    />
  )

  return (
    <span className="contents" onClick={stop} onDoubleClick={stop} onContextMenu={stop}>
      <DropdownMenu open={Boolean(open) && !point} onOpenChange={onOpenChange}>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="icon"
            aria-label={`More options for ${track.title}`}
            className={cn("size-8 shrink-0 text-zinc-500 hover:text-zinc-200", triggerClassName)}
          >
            <MoreHorizontal className="size-4" />
          </Button>
        </DropdownMenuTrigger>
        {content("end", side)}
      </DropdownMenu>

      {open && point && onOpenChange ? (
        <MenuAtPoint point={point} onOpenChange={onOpenChange}>
          {content("start", "bottom")}
        </MenuAtPoint>
      ) : null}

      <DeleteTrackDialog track={track} open={deleteOpen} onOpenChange={setDeleteOpen} />
    </span>
  )
}

function TrackMenuContent({
  track,
  side,
  align,
  showPlay,
  onRemoveFromPlaylist,
  onDismissAttention,
  onRestoreAttention,
  onDelete,
  canManage,
  offline,
}: {
  track: Track
  side: "top" | "bottom"
  align: "start" | "end"
  showPlay: boolean
  onRemoveFromPlaylist?: (trackId: string) => void
  onDismissAttention?: (trackId: string) => void
  onRestoreAttention?: (trackId: string) => void
  onDelete: () => void
  canManage: boolean
  offline: { supported: boolean; downloaded: boolean; downloading: boolean }
}) {
  const group = useSelectionStore((s) => (s.ids.size > 1 && s.ids.has(track.id) ? s.ids.size : 0))
  const pick = () => (group ? selectedTracks() : [track])

  return (
    <DropdownMenuContent align={align} side={side} className="w-56">
      <DropdownMenuLabel className="text-[11px] leading-snug font-normal text-zinc-500">
        {qualityDetail(track)}
      </DropdownMenuLabel>
      <DropdownMenuSeparator />
      {showPlay ? (
        <DropdownMenuItem onClick={() => usePlayerStore.getState().playNow(track)}>
          <Play className="size-4" /> Play
        </DropdownMenuItem>
      ) : null}
      <DropdownMenuItem
        onClick={() => {
          usePlayerStore.getState().playNext(track)
          if (!inListenSession()) toast.success(`Playing “${track.title}” next`)
        }}
      >
        <ListStart className="size-4" /> Play next
      </DropdownMenuItem>
      <DropdownMenuItem
        onClick={() => {
          usePlayerStore.getState().addToQueue(track)
          if (!inListenSession()) toast.success(`Added “${track.title}” to queue`)
        }}
      >
        <ListEnd className="size-4" /> Add to queue
      </DropdownMenuItem>

      <DropdownMenuSub>
        <DropdownMenuSubTrigger>
          <ListPlus className="size-4" /> {group ? `Add ${group} songs to playlist` : "Add to playlist"}
        </DropdownMenuSubTrigger>
        <DropdownMenuSubContent className="max-h-72 overflow-y-auto w-48">
          <PlaylistItems pick={pick} clearAfter={group > 0} />
          <DropdownMenuSeparator />
          <DropdownMenuItem
            onClick={() => {
              const ui = useUIStore.getState()
              ui.setPendingPlaylistTracks(pick())
              ui.openPlaylistDialog({ mode: "create" })
              if (group) useSelectionStore.getState().clear()
            }}
          >
            <ListPlus className="size-4" /> New playlist…
          </DropdownMenuItem>
        </DropdownMenuSubContent>
      </DropdownMenuSub>
      <DropdownMenuItem onClick={() => useUIStore.getState().setShareTrack(track)}>
        <Share2 className="size-4" /> Share link…
      </DropdownMenuItem>
      {offline.supported ? (
        offline.downloading ? (
          <DropdownMenuItem onClick={() => useOfflineStore.getState().cancel(track.id)}>
            <Loader2 className="size-4 animate-spin" /> Cancel download
          </DropdownMenuItem>
        ) : offline.downloaded ? (
          <DropdownMenuItem onClick={() => useOfflineStore.getState().remove(track.id)}>
            <CloudOff className="size-4" /> Remove download
          </DropdownMenuItem>
        ) : (
          <DropdownMenuItem onClick={() => useOfflineStore.getState().download(track)}>
            <CloudDownload className="size-4" /> Download for offline
          </DropdownMenuItem>
        )
      ) : null}

      {onDismissAttention && canManage ? (
        <DropdownMenuItem onClick={() => onDismissAttention(track.id)}>
          <Check className="size-4" /> Looks fine
        </DropdownMenuItem>
      ) : null}
      {onRestoreAttention && canManage ? (
        <DropdownMenuItem onClick={() => onRestoreAttention(track.id)}>
          <Undo2 className="size-4" /> Show in Needs attention
        </DropdownMenuItem>
      ) : null}
      {onRemoveFromPlaylist ? (
        <DropdownMenuItem onClick={() => onRemoveFromPlaylist(track.id)}>
          <ListMinus className="size-4" /> Remove from this playlist
        </DropdownMenuItem>
      ) : null}

      <DropdownMenuSeparator />
      <DropdownMenuSub>
        <DropdownMenuSubTrigger>
          <Wrench className="size-4" /> Advanced
        </DropdownMenuSubTrigger>
        <DropdownMenuSubContent className="w-56">
          {canManage ? (
            <>
              <DropdownMenuItem onClick={() => useUIStore.getState().setEditTrackId(track.id)}>
                <Pencil className="size-4" /> Edit metadata…
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => {
                  requestMetadataLookup(track.id).catch((e: Error) => toast.error(e.message))
                  useUIStore.getState().setMetadataReviewTracks([track])
                }}
              >
                <RefreshCw className="size-4" /> Look up metadata again
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => useUIStore.getState().setRestoreTagsTrack(track)}>
                <Undo2 className="size-4" /> Restore tags from file…
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => useUIStore.getState().setTrimTrack(track)}>
                <Scissors className="size-4" /> Trim silence…
              </DropdownMenuItem>
              <DropdownMenuSeparator />
            </>
          ) : null}
          <DropdownMenuItem asChild>
            <a
              href={downloadUrl(track, "original")}
              download={`${track.title}${extForMime(track.mimeType)}`}
              rel="noopener"
            >
              <Download className="size-4" />
              {track.hasCompatible ? `Download original (${codecName(track)})` : "Download file"}
            </a>
          </DropdownMenuItem>
          {track.hasCompatible ? (
            <DropdownMenuItem asChild>
              <a
                href={downloadUrl(track, "compatible")}
                download={`${track.title}.m4a`}
                rel="noopener"
              >
                <Download className="size-4" /> Download AAC (plays anywhere)
              </a>
            </DropdownMenuItem>
          ) : null}
          {canManage ? (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={onDelete} className="text-red-400 focus:text-red-300">
                <Trash2 className="size-4" /> Delete track
              </DropdownMenuItem>
            </>
          ) : null}
        </DropdownMenuSubContent>
      </DropdownMenuSub>
    </DropdownMenuContent>
  )
}

function PlaylistItems({ pick, clearAfter }: { pick: () => Track[]; clearAfter: boolean }) {
  const playlists = useLibraryStore((s) => s.playlists)
  const own = playlists.filter((p) => p.isMine)

  const addTo = async (playlist: { id: string; name: string }) => {
    const ok = await addTracksWithToast(playlist, pick())
    if (ok && clearAfter) useSelectionStore.getState().clear()
  }

  if (own.length === 0) {
    return (
      <DropdownMenuItem disabled className="text-zinc-500">
        No playlists yet
      </DropdownMenuItem>
    )
  }
  return (
    <>
      {own.map((p) => (
        <DropdownMenuItem key={p.id} onClick={() => addTo(p)}>
          <Music4 className="size-4" /> {p.name}
        </DropdownMenuItem>
      ))}
    </>
  )
}
