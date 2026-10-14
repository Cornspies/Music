"use client"

import { useState } from "react"
import { toast } from "sonner"
import {
  Globe,
  ImageMinus,
  ImagePlus,
  Link as LinkIcon,
  ListEnd,
  Lock,
  Pencil,
  Pin,
  PinOff,
  Play,
  Shuffle,
  Trash2,
} from "lucide-react"
import { api } from "@/lib/api"
import { copyText } from "@/lib/clipboard"
import { publicUrl } from "@/lib/desktop"
import type { Playlist, PlaylistDetail, Track } from "@/lib/types"
import { useLibraryStore } from "@/stores/library"
import { usePlayerStore } from "@/stores/player"
import { useUIStore } from "@/stores/ui"
import { inListenSession } from "@/stores/listen"
import { clearPlaylistCover, pickPlaylistCover } from "@/components/music/PlaylistCover"
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
import { DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator } from "@/components/ui/dropdown-menu"

type PlaylistInfo = Pick<Playlist, "id" | "name" | "isMine" | "isPublic" | "isPinned" | "coverVersion">

export async function copyPlaylistLink(playlistId: string): Promise<void> {
  const url = publicUrl(`/playlists/${encodeURIComponent(playlistId)}`)
  if (await copyText(url)) toast.success("Link copied")
  else toast.error(`Couldn't copy automatically. The link is: ${url}`)
}

async function playlistTracks(id: string): Promise<Track[] | null> {
  try {
    const detail = await api<PlaylistDetail>(`/api/playlists/${id}`)
    if (detail.tracks.length === 0) {
      toast("This playlist is empty")
      return null
    }
    return detail.tracks
  } catch (e) {
    toast.error((e as Error).message)
    return null
  }
}

export function PlaylistManageItems({
  playlist,
  onDelete,
  onChange,
}: {
  playlist: PlaylistInfo
  onDelete: () => void
  onChange?: (patch: Partial<Playlist>) => void
}) {
  const togglePublic = async () => {
    const next = !playlist.isPublic
    if (await useLibraryStore.getState().setPlaylistPublic(playlist.id, next)) {
      onChange?.({ isPublic: next })
      toast.success(next ? "Playlist is public — anyone signed in can find it" : "Playlist is private again")
    }
  }

  const togglePinned = async () => {
    const next = !playlist.isPinned
    if (await useLibraryStore.getState().setPlaylistPinned(playlist.id, next)) {
      onChange?.({ isPinned: next })
      toast.success(next ? "Pinned to your sidebar" : "Removed from your sidebar")
    }
  }

  const copyLink = playlist.isPublic ? (
    <DropdownMenuItem onClick={() => copyPlaylistLink(playlist.id)}>
      <LinkIcon className="size-4" /> Copy link
    </DropdownMenuItem>
  ) : null

  if (!playlist.isMine) {
    return (
      <>
        {copyLink}
        <DropdownMenuItem onClick={togglePinned}>
          {playlist.isPinned ? (
            <>
              <PinOff className="size-4" /> Unpin
            </>
          ) : (
            <>
              <Pin className="size-4" /> Pin to sidebar
            </>
          )}
        </DropdownMenuItem>
      </>
    )
  }

  return (
    <>
      <DropdownMenuItem
        onClick={() =>
          useUIStore.getState().openPlaylistDialog({ mode: "rename", id: playlist.id, name: playlist.name })
        }
      >
        <Pencil className="size-4" /> Rename
      </DropdownMenuItem>
      <DropdownMenuItem onSelect={() => pickPlaylistCover(playlist.id)}>
        <ImagePlus className="size-4" /> {playlist.coverVersion ? "Change cover" : "Upload cover"}
      </DropdownMenuItem>
      {playlist.coverVersion ? (
        <DropdownMenuItem onSelect={() => clearPlaylistCover(playlist.id)}>
          <ImageMinus className="size-4" /> Remove cover
        </DropdownMenuItem>
      ) : null}
      <DropdownMenuItem onClick={togglePublic}>
        {playlist.isPublic ? (
          <>
            <Lock className="size-4" /> Make private
          </>
        ) : (
          <>
            <Globe className="size-4" /> Make public
          </>
        )}
      </DropdownMenuItem>
      {copyLink}
      <DropdownMenuItem onClick={togglePinned}>
        {playlist.isPinned ? (
          <>
            <PinOff className="size-4" /> Unpin
          </>
        ) : (
          <>
            <Pin className="size-4" /> Pin to sidebar
          </>
        )}
      </DropdownMenuItem>
      <DropdownMenuItem onClick={onDelete} className="text-red-400">
        <Trash2 className="size-4" /> Delete playlist
      </DropdownMenuItem>
    </>
  )
}

export function PlaylistMenuContent({ playlist, onDelete }: { playlist: PlaylistInfo; onDelete: () => void }) {
  const play = async (shuffle: boolean) => {
    const tracks = await playlistTracks(playlist.id)
    if (tracks) usePlayerStore.getState().playAll(tracks, { shuffle })
  }

  const queue = async () => {
    const tracks = await playlistTracks(playlist.id)
    if (!tracks) return
    usePlayerStore.getState().addAllToQueue(tracks)
    if (!inListenSession()) {
      toast.success(`Added ${tracks.length} ${tracks.length === 1 ? "song" : "songs"} to the queue`)
    }
  }

  return (
    <DropdownMenuContent align="start" side="bottom" className="w-52">
      <DropdownMenuItem onClick={() => play(false)}>
        <Play className="size-4" /> Play
      </DropdownMenuItem>
      <DropdownMenuItem onClick={() => play(true)}>
        <Shuffle className="size-4" /> Shuffle
      </DropdownMenuItem>
      <DropdownMenuItem onClick={queue}>
        <ListEnd className="size-4" /> Add to queue
      </DropdownMenuItem>
      <DropdownMenuSeparator />
      <PlaylistManageItems playlist={playlist} onDelete={onDelete} />
    </DropdownMenuContent>
  )
}

export function DeletePlaylistDialog({
  playlist,
  open,
  onOpenChange,
  onDeleted,
}: {
  playlist: Pick<Playlist, "id" | "name">
  open: boolean
  onOpenChange: (open: boolean) => void
  onDeleted?: () => void
}) {
  const [deleting, setDeleting] = useState(false)

  const confirm = async () => {
    setDeleting(true)
    const ok = await useLibraryStore.getState().deletePlaylist(playlist.id)
    setDeleting(false)
    if (ok) {
      toast.success(`Deleted playlist “${playlist.name}”`)
      onDeleted?.()
    }
  }

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete this playlist?</AlertDialogTitle>
          <AlertDialogDescription>
            “{playlist.name}” will be permanently deleted. The tracks stay in your library.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction disabled={deleting} className="bg-red-600 text-white hover:bg-red-500" onClick={confirm}>
            Delete
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
