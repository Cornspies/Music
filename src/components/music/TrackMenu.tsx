"use client"

import { useState } from "react"
import { toast } from "sonner"
import {
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
  Share2,
  Trash2,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { downloadUrl, requestMetadataLookup } from "@/lib/api"
import type { Track } from "@/lib/types"
import { usePlayerStore } from "@/stores/player"
import { useLibraryStore } from "@/stores/library"
import { useOfflineStore } from "@/stores/offline"
import { useAuthStore } from "@/stores/auth"
import { useUIStore } from "@/stores/ui"
import { Button } from "@/components/ui/button"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
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
  /** playlist mode: adds "Remove from this playlist" */
  onRemoveFromPlaylist?: (trackId: string) => void
  /** Extra classes for the ⋯ button (size, visibility on hover…). */
  triggerClassName?: string
  /** Which side of the button the menu opens on; "top" in the player bar. */
  side?: "top" | "bottom"
  /** False in the player bar, where "Play" would restart the current track alone. */
  showPlay?: boolean
}

/**
 * A track's ⋯ menu — play, queue, playlists, share, download, and for the
 * uploader or an admin editing and deleting — as one self-contained piece,
 * so the same menu sits on library rows, home cards and the player bar.
 *
 * Everything is wrapped in a `display: contents` span that stops clicks:
 * the places it sits are themselves clickable (a card that plays, a row
 * that plays on double-click), and React bubbles events out of the menu's
 * and the dialog's portals through the component tree, so without it
 * picking "Delete" would also start the track.
 */
export function TrackMenu({
  track,
  onRemoveFromPlaylist,
  triggerClassName,
  side = "bottom",
  showPlay = true,
}: TrackMenuProps) {
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleting, setDeleting] = useState(false)

  const offlineSupported = useOfflineStore((s) => s.supported)
  const isDownloaded = useOfflineStore((s) => s.ids.has(track.id))
  const downloading = useOfflineStore((s) => Boolean(s.jobs[track.id]))
  // Uploader or admin: what the server allows for edit/delete.
  const canManage = useAuthStore(
    (s) => s.user?.id === track.uploadedBy.id || s.user?.role === "admin"
  )

  const stop = (e: React.SyntheticEvent) => e.stopPropagation()

  return (
    <span className="contents" onClick={stop} onDoubleClick={stop}>
      <DropdownMenu>
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
        <TrackMenuContent
          track={track}
          side={side}
          showPlay={showPlay}
          onRemoveFromPlaylist={onRemoveFromPlaylist}
          onDelete={() => setDeleteOpen(true)}
          canManage={canManage}
          offline={{
            supported: offlineSupported,
            downloaded: isDownloaded,
            downloading,
          }}
        />
      </DropdownMenu>

      {/* delete confirmation */}
      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete this track?</AlertDialogTitle>
            <AlertDialogDescription>
              “{track.title}” will be permanently removed from your library, playlists and
              favorites. This cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              disabled={deleting}
              className="bg-red-600 text-white hover:bg-red-500"
              onClick={async () => {
                setDeleting(true)
                try {
                  await useLibraryStore.getState().deleteTrack(track.id)
                  toast.success(`Deleted “${track.title}”`)
                } finally {
                  setDeleting(false)
                }
              }}
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </span>
  )
}

function TrackMenuContent({
  track,
  side,
  showPlay,
  onRemoveFromPlaylist,
  onDelete,
  canManage,
  offline,
}: {
  track: Track
  side: "top" | "bottom"
  showPlay: boolean
  onRemoveFromPlaylist?: (trackId: string) => void
  onDelete: () => void
  canManage: boolean
  offline: { supported: boolean; downloaded: boolean; downloading: boolean }
}) {
  const playlists = useLibraryStore((s) => s.playlists)
  // The sidebar list also carries playlists pinned from other people; tracks
  // can only be added to your own.
  const ownPlaylists = playlists.filter((p) => p.isMine)
  const ui = useUIStore()

  const addTo = async (playlistId: string, name: string) => {
    const ok = await useLibraryStore.getState().addTrackToPlaylist(playlistId, track.id)
    if (ok) toast.success(`Added to “${name}”`)
  }

  return (
    <DropdownMenuContent align="end" side={side} className="w-52">
      {showPlay ? (
        <DropdownMenuItem onClick={() => usePlayerStore.getState().playNow(track)}>
          <Play className="size-4" /> Play
        </DropdownMenuItem>
      ) : null}
      <DropdownMenuItem onClick={() => useUIStore.getState().setShareTrack(track)}>
        <Share2 className="size-4" /> Share link…
      </DropdownMenuItem>
      <DropdownMenuItem
        onClick={() => {
          usePlayerStore.getState().playNext(track)
          toast.success(`Playing “${track.title}” next`)
        }}
      >
        <ListStart className="size-4" /> Play next
      </DropdownMenuItem>
      <DropdownMenuItem
        onClick={() => {
          usePlayerStore.getState().addToQueue(track)
          toast.success(`Added “${track.title}” to queue`)
        }}
      >
        <ListEnd className="size-4" /> Add to queue
      </DropdownMenuItem>

      <DropdownMenuSub>
        <DropdownMenuSubTrigger>
          <ListPlus className="size-4" /> Add to playlist
        </DropdownMenuSubTrigger>
        <DropdownMenuSubContent className="max-h-72 overflow-y-auto w-48">
          {ownPlaylists.length === 0 ? (
            <DropdownMenuItem disabled className="text-zinc-500">
              No playlists yet
            </DropdownMenuItem>
          ) : (
            ownPlaylists.map((p) => (
              <DropdownMenuItem key={p.id} onClick={() => addTo(p.id, p.name)}>
                <Music4 className="size-4" /> {p.name}
              </DropdownMenuItem>
            ))
          )}
          <DropdownMenuSeparator />
          <DropdownMenuItem
            onClick={() => {
              ui.setPendingPlaylistTrack(track)
              ui.openPlaylistDialog({ mode: "create" })
            }}
          >
            <ListPlus className="size-4" /> New playlist…
          </DropdownMenuItem>
        </DropdownMenuSubContent>
      </DropdownMenuSub>

      <DropdownMenuSeparator />
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
      {canManage ? (
        <>
          <DropdownMenuItem onClick={() => useUIStore.getState().setEditTrackId(track.id)}>
            <Pencil className="size-4" /> Edit metadata…
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={() => {
              // Fresh search across every provider, then the review dialog.
              requestMetadataLookup(track.id).catch((e: Error) => toast.error(e.message))
              useUIStore.getState().setMetadataReviewTracks([track])
            }}
          >
            <RefreshCw className="size-4" /> Look up metadata again
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => useUIStore.getState().setTrimTrack(track)}>
            <Scissors className="size-4" /> Trim silence…
          </DropdownMenuItem>
        </>
      ) : null}
      <DropdownMenuItem asChild>
        <a
          href={downloadUrl(track, "original")}
          download={`${track.title}${extForMime(track.mimeType)}`}
          rel="noopener"
        >
          <Download className="size-4" />
          {track.hasCompatible ? "Download original" : "Download"}
        </a>
      </DropdownMenuItem>
      {/* Only offered when a converted copy exists, so the menu stays a single
          item for the ordinary case. Playback picks a version on its own; this
          is the one place the choice is worth surfacing, because someone saving
          a file to keep may care which one they get. */}
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

      {onRemoveFromPlaylist ? (
        <>
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={() => onRemoveFromPlaylist(track.id)}>
            <ListEnd className="size-4" /> Remove from this playlist
          </DropdownMenuItem>
        </>
      ) : null}

      {canManage ? (
        <>
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={onDelete} className="text-red-400 focus:text-red-300">
            <Trash2 className="size-4" /> Delete track
          </DropdownMenuItem>
        </>
      ) : null}
    </DropdownMenuContent>
  )
}
