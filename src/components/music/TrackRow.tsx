"use client"

import { useState } from "react"
import { toast } from "sonner"
import {
  CloudDownload,
  CloudOff,
  Download,
  Heart,
  ListEnd,
  ListPlus,
  ListStart,
  Loader2,
  MoreHorizontal,
  Music4,
  Pencil,
  RefreshCw,
  Scissors,
  Play,
  Trash2,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { downloadUrl, requestMetadataLookup } from "@/lib/api"
import { formatDuration } from "@/lib/format"
import type { Track } from "@/lib/types"
import { usePlayerStore } from "@/stores/player"
import { useLibraryStore } from "@/stores/library"
import { useOfflineStore } from "@/stores/offline"
import { useAuthStore } from "@/stores/auth"
import { useUIStore } from "@/stores/ui"
import { CoverArt } from "@/components/music/CoverArt"
import { UserAvatar } from "@/components/music/UserAvatar"
import { EqualizerIcon } from "@/components/music/EqualizerIcon"
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

interface TrackRowProps {
  track: Track
  index?: number
  /** full list this row belongs to (becomes the play queue) */
  contextQueue?: Track[]
  /** playlist mode: shows "Remove from this playlist" */
  onRemoveFromPlaylist?: (trackId: string) => void
}

export function TrackRow({ track, index, contextQueue, onRemoveFromPlaylist }: TrackRowProps) {
  // Booleans, not the track object: with a long list only the row that
  // starts or stops being current re-renders on a track change, not all of
  // them.
  const isCurrent = usePlayerStore((s) => s.currentTrack?.id === track.id)
  const isPlaying = usePlayerStore((s) => s.isPlaying && s.currentTrack?.id === track.id)

  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleting, setDeleting] = useState(false)

  const offlineSupported = useOfflineStore((s) => s.supported)
  const isDownloaded = useOfflineStore((s) => s.ids.has(track.id))
  const downloadJob = useOfflineStore((s) => s.jobs[track.id])
  // Uploader or admin: what the server allows for edit/delete.
  const canManage = useAuthStore(
    (s) => s.user?.id === track.uploadedBy.id || s.user?.role === "admin"
  )

  const play = () => {
    usePlayerStore.getState().playNow(track, contextQueue ?? [track])
  }

  const playNext = () => {
    usePlayerStore.getState().playNext(track)
    toast.success(`Playing “${track.title}” next`)
  }

  const addToQueue = () => {
    usePlayerStore.getState().addToQueue(track)
    toast.success(`Added “${track.title}” to queue`)
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
      onKeyDown={(e) => {
        if (e.key === "Enter" && e.target === e.currentTarget) play()
      }}
      className={cn(
        "group flex cursor-default items-center gap-3 rounded-lg px-2 py-1.5 select-none md:px-3",
        "hover:bg-zinc-800/60 focus-visible:bg-zinc-800/60 focus-visible:outline-none",
        isCurrent && "bg-zinc-800/40"
      )}
    >
      {/* index / play button / equalizer */}
      <div className="hidden w-6 shrink-0 items-center justify-center sm:flex">
        {isCurrent ? (
          <EqualizerIcon playing={isPlaying} />
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

      {/* cover */}
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

      {/* title + artist */}
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
              // The online lookup hasn't been through this track yet; its
              // tags may still be filled in.
            />
          )}
          <span className="truncate">
            {track.artist || "Unknown artist"}
            {track.year ? ` · ${track.year}` : ""}
          </span>
        </div>
      </div>

      {/* album */}
      <div className="hidden w-40 shrink-0 truncate text-xs text-zinc-500 lg:block xl:w-52">
        {track.album || "—"}
      </div>

      {/* plays */}
      <div className="hidden w-12 shrink-0 text-right text-xs tabular-nums text-zinc-600 md:block">
        {track.plays > 0 ? track.plays : "—"}
      </div>

      {/* offline state: a quiet tick once stored, a spinner while it downloads */}
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

      {/* duration */}
      <div className="w-11 shrink-0 text-right text-xs tabular-nums text-zinc-500">
        {formatDuration(track.duration)}
      </div>

      {/* uploader — quiet but legible: dimmed at rest so it reads as provenance
          rather than another control, full strength when the row is hovered */}
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

      {/* favorite */}
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

      {/* overflow menu */}
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="icon"
            aria-label={`More options for ${track.title}`}
            className="size-8 shrink-0 text-zinc-500 opacity-0 transition-opacity focus-visible:opacity-100 group-hover:opacity-100 data-[state=open]:opacity-100 hover:text-zinc-200"
            onClick={(e) => e.stopPropagation()}
          >
            <MoreHorizontal className="size-4" />
          </Button>
        </DropdownMenuTrigger>
        <TrackRowMenu
          track={track}
          onRemoveFromPlaylist={onRemoveFromPlaylist}
          onDelete={() => setDeleteOpen(true)}
          canManage={canManage}
          offline={{
            supported: offlineSupported,
            downloaded: isDownloaded,
            downloading: Boolean(downloadJob),
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
    </div>
  )
}

function TrackRowMenu({
  track,
  onRemoveFromPlaylist,
  onDelete,
  canManage,
  offline,
}: {
  track: Track
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
    <DropdownMenuContent align="end" className="w-52" onClick={(e) => e.stopPropagation()}>
      <DropdownMenuItem onClick={() => usePlayerStore.getState().playNow(track)}>
        <Play className="size-4" /> Play
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
