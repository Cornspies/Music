"use client"

import { useEffect, useState } from "react"
import { CheckCheck, ListEnd, ListMinus, ListPlus, Loader2, Music4, Play, X } from "lucide-react"
import { toast } from "sonner"
import { usePlayerStore } from "@/stores/player"
import { addTracksWithToast, useLibraryStore } from "@/stores/library"
import { inListenSession } from "@/stores/listen"
import { selectedTracks, useSelectionStore } from "@/stores/selection"
import { useUIStore } from "@/stores/ui"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

const songs = (n: number) => `${n} ${n === 1 ? "song" : "songs"}`

function typingIn(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  return target.tagName === "INPUT" || target.tagName === "TEXTAREA" || target.tagName === "SELECT" || target.isContentEditable
}

export function SelectionBar() {
  const count = useSelectionStore((s) => s.ids.size)
  const total = useSelectionStore((s) => new Set(s.context.map((t) => t.id)).size)
  const canRemove = useSelectionStore((s) => s.remove !== null)
  const playlists = useLibraryStore((s) => s.playlists)
  const own = playlists.filter((p) => p.isMine)
  const [pending, setPending] = useState<string | null>(null)

  useEffect(() => {
    if (count === 0) return
    const onKeyDown = (e: KeyboardEvent) => {
      if (typingIn(e.target)) return
      if (document.querySelector('[role="dialog"], [role="menu"]')) return
      if (e.key === "Escape") {
        useSelectionStore.getState().clear()
      } else if ((e.ctrlKey || e.metaKey) && (e.key === "a" || e.key === "A")) {
        e.preventDefault()
        useSelectionStore.getState().selectAll()
      }
    }
    window.addEventListener("keydown", onKeyDown)
    return () => window.removeEventListener("keydown", onKeyDown)
  }, [count])

  if (count === 0) return null

  const clear = () => useSelectionStore.getState().clear()

  const play = () => {
    const tracks = selectedTracks()
    if (tracks.length) usePlayerStore.getState().playAll(tracks)
  }

  const queue = () => {
    const tracks = selectedTracks()
    usePlayerStore.getState().addAllToQueue(tracks)
    if (!inListenSession()) toast.success(`Added ${songs(tracks.length)} to the queue`)
  }

  const addTo = async (playlist: { id: string; name: string }) => {
    setPending(playlist.id)
    try {
      if (await addTracksWithToast(playlist, selectedTracks())) clear()
    } finally {
      setPending(null)
    }
  }

  const removeFromPlaylist = () => {
    const { remove } = useSelectionStore.getState()
    if (!remove) return
    const tracks = selectedTracks()
    for (const t of tracks) remove(t.id)
    toast.success(`Removed ${songs(tracks.length)} from this playlist`)
    clear()
  }

  const button =
    "inline-flex h-8 items-center gap-1.5 rounded-full px-3 text-xs font-semibold text-zinc-200 transition-colors hover:bg-zinc-700/70 disabled:opacity-50"

  return (
    <div className="pointer-events-none sticky bottom-0 z-30 flex justify-center px-3 pt-2 pb-3 md:pb-4">
      <div
        role="toolbar"
        aria-label="Selected songs"
        className="pointer-events-auto flex max-w-full flex-wrap items-center justify-center gap-1 rounded-2xl border border-zinc-700/70 bg-zinc-900/95 p-1.5 shadow-2xl shadow-black/50 backdrop-blur"
      >
        <span className="px-2.5 text-xs font-semibold text-emerald-300 tabular-nums">{count} selected</span>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button type="button" className={`${button} bg-emerald-500 text-zinc-950 hover:bg-emerald-400`}>
              {pending ? <Loader2 className="size-3.5 animate-spin" /> : <ListPlus className="size-3.5" />}
              Add to playlist
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="center" side="top" className="max-h-80 w-60 overflow-y-auto">
            <DropdownMenuLabel className="text-xs font-normal text-zinc-500">Add {songs(count)} to…</DropdownMenuLabel>
            <DropdownMenuSeparator />
            {own.length === 0 ? (
              <DropdownMenuItem disabled className="text-zinc-500">
                No playlists yet
              </DropdownMenuItem>
            ) : (
              own.map((p) => (
                <DropdownMenuItem key={p.id} disabled={pending !== null} onSelect={() => addTo(p)}>
                  <Music4 className="size-4" />
                  <span className="truncate">{p.name}</span>
                </DropdownMenuItem>
              ))
            )}
            <DropdownMenuSeparator />
            <DropdownMenuItem
              onSelect={() => {
                const ui = useUIStore.getState()
                ui.setPendingPlaylistTracks(selectedTracks())
                ui.openPlaylistDialog({ mode: "create" })
                clear()
              }}
            >
              <ListPlus className="size-4" /> New playlist…
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <button type="button" onClick={play} className={button}>
          <Play className="size-3.5 fill-current" /> Play
        </button>
        <button type="button" onClick={queue} className={button}>
          <ListEnd className="size-3.5" /> Queue
        </button>
        {canRemove ? (
          <button type="button" onClick={removeFromPlaylist} className={`${button} text-red-300 hover:text-red-200`}>
            <ListMinus className="size-3.5" /> Remove
          </button>
        ) : null}
        {count < total ? (
          <button
            type="button"
            onClick={() => useSelectionStore.getState().selectAll()}
            className={button}
            title="Select all (Ctrl+A)"
          >
            <CheckCheck className="size-3.5" /> All {total}
          </button>
        ) : null}
        <button
          type="button"
          onClick={clear}
          aria-label="Clear selection"
          title="Clear selection (Esc)"
          className="flex size-8 items-center justify-center rounded-full text-zinc-400 transition-colors hover:bg-zinc-700/70 hover:text-zinc-100"
        >
          <X className="size-4" />
        </button>
      </div>
    </div>
  )
}
