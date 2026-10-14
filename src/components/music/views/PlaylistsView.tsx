"use client"

import { useState } from "react"
import { Globe, ListMusic, Music4, Plus } from "lucide-react"
import { formatTotal, hashGradient } from "@/lib/format"
import type { Playlist } from "@/lib/types"
import { useLibraryStore } from "@/stores/library"
import { useUIStore } from "@/stores/ui"
import { EmptyState } from "@/components/music/EmptyState"
import { PlaylistCoverImage } from "@/components/music/PlaylistCover"
import { MenuAtPoint, useContextMenu } from "@/components/music/MenuAtPoint"
import { DeletePlaylistDialog, PlaylistMenuContent } from "@/components/music/PlaylistMenu"
import { UserAvatar } from "@/components/music/UserAvatar"

export function PlaylistsView() {
  const playlists = useLibraryStore((s) => s.playlists)
  const publicPlaylists = useLibraryStore((s) => s.publicPlaylists)
  const navigate = useUIStore((s) => s.navigate)
  const openPlaylistDialog = useUIStore((s) => s.openPlaylistDialog)

  return (
    <div className="p-4 md:p-6">
      <div className="flex items-center justify-between gap-4">
        <h1 className="text-2xl font-bold tracking-tight md:text-3xl">Playlists</h1>
        <button
          type="button"
          onClick={() => openPlaylistDialog({ mode: "create" })}
          className="inline-flex items-center gap-2 rounded-full bg-emerald-500 px-4 py-2 text-sm font-semibold text-zinc-950 transition-all hover:scale-105 hover:bg-emerald-400"
        >
          <Plus className="size-4" /> New playlist
        </button>
      </div>

      <div className="mt-5">
        {playlists.length === 0 ? (
          <EmptyState
            icon={ListMusic}
            title="No playlists yet"
            actionLabel="Create playlist"
            onAction={() => openPlaylistDialog({ mode: "create" })}
          />
        ) : (
          <PlaylistGrid playlists={playlists} onOpen={(id) => navigate("playlist", id)} />
        )}
      </div>

      {publicPlaylists.length > 0 && (
        <div className="mt-8">
          <h2 className="text-lg font-semibold tracking-tight text-zinc-200">Shared with you</h2>
          <div className="mt-3">
            <PlaylistGrid
              playlists={publicPlaylists}
              onOpen={(id) => navigate("playlist", id)}
              showOwner
            />
          </div>
        </div>
      )}
    </div>
  )
}

function PlaylistGrid({
  playlists,
  onOpen,
  showOwner = false,
}: {
  playlists: Playlist[]
  onOpen: (id: string) => void
  showOwner?: boolean
}) {
  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-5">
      {playlists.map((playlist) => (
        <PlaylistCard key={playlist.id} playlist={playlist} onOpen={onOpen} showOwner={showOwner} />
      ))}
    </div>
  )
}

function PlaylistCard({
  playlist,
  onOpen,
  showOwner,
}: {
  playlist: Playlist
  onOpen: (id: string) => void
  showOwner: boolean
}) {
  const menu = useContextMenu()
  const [deleteOpen, setDeleteOpen] = useState(false)

  return (
    <>
      <button
        type="button"
        onClick={() => onOpen(playlist.id)}
        onContextMenu={menu.onContextMenu}
        className="group rounded-xl p-2 text-left transition-colors hover:bg-zinc-800/60"
        aria-label={`Open playlist ${playlist.name}`}
      >
        <div
          aria-hidden="true"
          className={`relative flex aspect-square w-full items-center justify-center overflow-hidden rounded-lg bg-linear-to-br shadow-lg ${hashGradient(playlist.name)}`}
        >
          <Music4 className="size-8 text-zinc-400/70" />
          <PlaylistCoverImage playlist={playlist} />
          <span className="absolute bottom-2 left-2 rounded-full bg-zinc-950/60 px-2 py-0.5 text-[10px] font-medium text-zinc-300">
            {playlist.trackCount}
          </span>
          {playlist.isMine && playlist.isPublic && (
            <span
              title="Public — anyone signed in can see this"
              className="absolute top-2 right-2 rounded-full bg-zinc-950/60 p-1 text-emerald-400"
            >
              <Globe className="size-3" />
            </span>
          )}
        </div>
        <p className="mt-2 truncate text-sm font-medium text-zinc-200">{playlist.name}</p>
        <p className="flex items-center gap-1.5 truncate text-xs text-zinc-500">
          {showOwner && playlist.owner?.id ? (
            <>
              <UserAvatar
                userId={playlist.owner.id}
                username={playlist.owner.username}
                hasAvatar={playlist.owner.hasAvatar}
                size={14}
                label={`Shared by ${playlist.owner.username}`}
              />
              <span className="truncate">{playlist.owner.username}</span>
              <span aria-hidden="true">·</span>
            </>
          ) : null}
          <span className="truncate">
            {playlist.trackCount} track{playlist.trackCount === 1 ? "" : "s"} ·{" "}
            {formatTotal(playlist.totalDuration)}
          </span>
        </p>
      </button>
      {menu.open && menu.point ? (
        <MenuAtPoint point={menu.point} onOpenChange={menu.onOpenChange}>
          <PlaylistMenuContent playlist={playlist} onDelete={() => setDeleteOpen(true)} />
        </MenuAtPoint>
      ) : null}
      <DeletePlaylistDialog playlist={playlist} open={deleteOpen} onOpenChange={setDeleteOpen} />
    </>
  )
}
