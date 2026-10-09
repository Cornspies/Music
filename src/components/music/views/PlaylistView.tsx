"use client"

import { useCallback, useEffect, useState } from "react"
import { toast } from "sonner"
import { Link as LinkIcon, ListMusic, Music2, Music4, Pin, PinOff } from "lucide-react"
import { api } from "@/lib/api"
import { formatTotal, hashGradient } from "@/lib/format"
import { cn } from "@/lib/utils"
import type { PlaylistDetail } from "@/lib/types"
import { useLibraryStore } from "@/stores/library"
import { useUIStore } from "@/stores/ui"
import { CollectionHeader, HeaderMenuTrigger } from "@/components/music/CollectionHeader"
import { EmptyState } from "@/components/music/EmptyState"
import { VirtualTrackList } from "@/components/music/VirtualTrackList"
import { Skeleton } from "@/components/ui/skeleton"
import { copyPlaylistLink, DeletePlaylistDialog, PlaylistManageItems } from "@/components/music/PlaylistMenu"
import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"

export function PlaylistView() {
  const viewArg = useUIStore((s) => s.viewArg)
  const playlistVersion = useLibraryStore((s) => s.playlistVersion)
  const navigate = useUIStore((s) => s.navigate)

  const [detail, setDetail] = useState<PlaylistDetail | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [deleteOpen, setDeleteOpen] = useState(false)

  useEffect(() => {
    if (!viewArg) return
    let cancelled = false
    const load = async () => {
      try {
        const d = await api<PlaylistDetail>(`/api/playlists/${viewArg}`)
        if (!cancelled) setDetail(d)
      } catch (e) {
        if (!cancelled) setError((e as Error).message)
      }
    }
    load()
    return () => {
      cancelled = true
    }
  }, [viewArg, playlistVersion])

  const removeTrack = useCallback(
    (trackId: string) => {
      if (!viewArg) return
      useLibraryStore.getState().removePlaylistTrack(viewArg, trackId)
      setDetail((prev) =>
        prev
          ? { ...prev, tracks: prev.tracks.filter((t) => t.id !== trackId), trackCount: Math.max(0, prev.trackCount - 1) }
          : prev
      )
    },
    [viewArg]
  )

  if (error) {
    return (
      <div className="p-4 md:p-6">
        <EmptyState
          icon={Music2}
          title="Couldn't load this playlist"
          subtitle={error}
          compact
        />
      </div>
    )
  }

  if (!detail) {
    return (
      <div className="flex flex-col gap-4 p-4 md:p-6">
        <div className="flex flex-col gap-5 md:flex-row md:items-end">
          <Skeleton className="size-40 rounded-xl md:size-48" />
          <div className="flex flex-1 flex-col gap-3">
            <Skeleton className="h-4 w-16 rounded-md" />
            <Skeleton className="h-10 w-64 rounded-lg" />
            <Skeleton className="h-4 w-40 rounded-md" />
          </div>
        </div>
        <Skeleton className="mt-6 h-14 w-full rounded-lg" />
        <Skeleton className="h-14 w-full rounded-lg" />
        <Skeleton className="h-14 w-full rounded-lg" />
      </div>
    )
  }

  const totalDuration = detail.tracks.reduce((sum, t) => sum + t.duration, 0)

  return (
    <div>
      <CollectionHeader
        icon={ListMusic}
        iconGradient={hashGradient(detail.name)}
        eyebrow={
          detail.isMine
            ? detail.isPublic
              ? "Public playlist"
              : "Playlist"
            : `Shared by ${detail.owner?.username ?? "another user"}`
        }
        title={detail.name}
        meta={`${detail.tracks.length} track${detail.tracks.length === 1 ? "" : "s"} · ${formatTotal(totalDuration)}`}
        tracks={detail.tracks}
        cover={
          <div
            aria-hidden="true"
            className={`flex aspect-square w-full items-center justify-center rounded-lg bg-linear-to-br shadow-2xl ${hashGradient(detail.name)}`}
          >
            <Music4 className="size-16 text-zinc-400/70 md:size-20" />
          </div>
        }
        actions={
          detail.isMine ? (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <HeaderMenuTrigger />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start" className="w-56">
                <PlaylistManageItems
                  playlist={detail}
                  onDelete={() => setDeleteOpen(true)}
                  onChange={(patch) => setDetail((prev) => (prev ? { ...prev, ...patch } : prev))}
                />
              </DropdownMenuContent>
            </DropdownMenu>
          ) : detail.isPublic ? (
            <>
              <button
                type="button"
                onClick={() => copyPlaylistLink(detail.id)}
                aria-label="Copy link to playlist"
                title="Copy link"
                className="inline-flex items-center gap-2 rounded-full border border-zinc-700 bg-zinc-800/60 px-4 py-2 text-sm font-semibold text-zinc-200 transition-colors hover:border-zinc-600"
              >
                <LinkIcon className="size-4" /> Copy link
              </button>
            <button
              type="button"
              onClick={async () => {
                const next = !detail.isPinned
                const ok = await useLibraryStore.getState().setPlaylistPinned(detail.id, next)
                if (ok) {
                  setDetail((prev) => (prev ? { ...prev, isPinned: next } : prev))
                  toast.success(next ? "Pinned to your sidebar" : "Removed from your sidebar")
                }
              }}
              className={cn(
                "inline-flex items-center gap-2 rounded-full border px-4 py-2 text-sm font-semibold transition-colors",
                detail.isPinned
                  ? "border-emerald-500/50 bg-emerald-500/10 text-emerald-300 hover:bg-emerald-500/20"
                  : "border-zinc-700 bg-zinc-800/60 text-zinc-200 hover:border-zinc-600"
              )}
            >
              {detail.isPinned ? (
                <>
                  <PinOff className="size-4" /> Unpin
                </>
              ) : (
                <>
                  <Pin className="size-4" /> Pin to sidebar
                </>
              )}
            </button>
            </>
          ) : null
        }
      />

      <div className="px-4 pt-5 pb-6 md:px-6">
        {detail.tracks.length === 0 ? (
          <EmptyState
            icon={ListMusic}
            title="This playlist is empty"
            compact
          />
        ) : (
          <VirtualTrackList tracks={detail.tracks} onRemoveFromPlaylist={removeTrack} />
        )}
      </div>

      <DeletePlaylistDialog
        playlist={detail}
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        onDeleted={() => navigate("playlists")}
      />
    </div>
  )
}
