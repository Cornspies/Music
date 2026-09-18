"use client"

import { useState } from "react"
import { Check, ListPlus, Loader2, Music4 } from "lucide-react"
import { toast } from "sonner"
import { cn } from "@/lib/utils"
import type { Track } from "@/lib/types"
import { useLibraryStore } from "@/stores/library"
import { useUIStore } from "@/stores/ui"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

/**
 * "Add to playlist" for whatever is playing.
 *
 * The menu stays open while you tick playlists, so one track can go into
 * several in a row, and each one shows a tick once it's in. Only your own
 * playlists are listed — pinned ones belong to someone else and are read-only.
 */
export function AddToPlaylistButton({ track }: { track: Track }) {
  const playlists = useLibraryStore((s) => s.playlists)
  const ownPlaylists = playlists.filter((p) => p.isMine)

  const [pending, setPending] = useState<string | null>(null)
  const [added, setAdded] = useState<Record<string, boolean>>({})

  const addTo = async (playlistId: string, name: string) => {
    setPending(playlistId)
    try {
      const ok = await useLibraryStore.getState().addTrackToPlaylist(playlistId, track.id)
      if (ok) {
        setAdded((prev) => ({ ...prev, [playlistId]: true }))
        toast.success(`Added “${track.title}” to ${name}`)
      }
    } finally {
      setPending(null)
    }
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          aria-label={`Add ${track.title} to a playlist`}
          title="Add to playlist"
          className="flex size-8 shrink-0 items-center justify-center rounded-md text-zinc-400 transition-colors hover:bg-zinc-800/60 hover:text-zinc-100"
        >
          <ListPlus className="size-4" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" side="top" className="max-h-80 w-56 overflow-y-auto">
        <DropdownMenuLabel className="truncate text-xs font-normal text-zinc-500">
          Add “{track.title}” to…
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        {ownPlaylists.length === 0 ? (
          <DropdownMenuItem disabled className="text-zinc-500">
            No playlists yet
          </DropdownMenuItem>
        ) : (
          ownPlaylists.map((p) => (
            <DropdownMenuItem
              key={p.id}
              disabled={pending !== null}
              // Keep the menu open so several playlists can be ticked in one go.
              onSelect={(e) => {
                e.preventDefault()
                addTo(p.id, p.name)
              }}
              className={cn(added[p.id] && "text-emerald-300 focus:text-emerald-300")}
            >
              {pending === p.id ? (
                <Loader2 className="size-4 animate-spin" />
              ) : added[p.id] ? (
                <Check className="size-4" />
              ) : (
                <Music4 className="size-4" />
              )}
              <span className="truncate">{p.name}</span>
            </DropdownMenuItem>
          ))
        )}
        <DropdownMenuSeparator />
        <DropdownMenuItem
          onSelect={() => {
            useUIStore.getState().setPendingPlaylistTrack(track)
            useUIStore.getState().openPlaylistDialog({ mode: "create" })
          }}
        >
          <ListPlus className="size-4" /> New playlist…
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
