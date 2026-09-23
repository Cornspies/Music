"use client"

import { useEffect, useState } from "react"
import { toast } from "sonner"
import { ListMusic, Loader2 } from "lucide-react"
import { useUIStore } from "@/stores/ui"
import { useLibraryStore } from "@/stores/library"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

export function PlaylistDialog() {
  const dialog = useUIStore((s) => s.playlistDialog)
  const openPlaylistDialog = useUIStore((s) => s.openPlaylistDialog)
  const closePlaylistDialog = useUIStore((s) => s.closePlaylistDialog)
  const pendingTrack = useUIStore((s) => s.pendingPlaylistTrack)

  const [name, setName] = useState("")
  const [saving, setSaving] = useState(false)

  const open = Boolean(dialog)

  useEffect(() => {
    setName(dialog?.mode === "rename" ? dialog.name : "")
  }, [dialog])

  const submit = async () => {
    const trimmed = name.trim()
    if (!trimmed) {
      toast.error("Playlist name is required")
      return
    }
    if (!dialog) return
    setSaving(true)
    try {
      if (dialog.mode === "create") {
        const playlist = await useLibraryStore.getState().createPlaylist(trimmed)
        if (playlist) {
          if (pendingTrack) {
            const ok = await useLibraryStore
              .getState()
              .addTrackToPlaylist(playlist.id, pendingTrack.id)
            if (ok) toast.success(`Playlist “${playlist.name}” created with “${pendingTrack.title}”`)
          } else {
            toast.success(`Playlist “${playlist.name}” created`)
          }
          useUIStore.getState().setPendingPlaylistTrack(null)
          closePlaylistDialog()
        }
      } else {
        const ok = await useLibraryStore.getState().renamePlaylist(dialog.id, trimmed)
        if (ok) {
          toast.success("Playlist renamed")
          closePlaylistDialog()
        }
      }
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (o) openPlaylistDialog(dialog ?? { mode: "create" })
        else {
          useUIStore.getState().setPendingPlaylistTrack(null)
          closePlaylistDialog()
        }
      }}
    >
      <DialogContent className="border-zinc-800 bg-zinc-900">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-base">
            <ListMusic className="size-4 text-emerald-400" aria-hidden="true" />
            {dialog?.mode === "rename" ? "Rename playlist" : "New playlist"}
          </DialogTitle>
        </DialogHeader>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault()
            submit()
          }}
        >
          <div className="flex flex-col gap-2">
            <Label htmlFor="playlist-name">Name</Label>
            <Input
              id="playlist-name"
              value={name}
              autoFocus
              maxLength={80}
              onChange={(e) => setName(e.target.value)}
              placeholder={dialog?.mode === "rename" ? "Playlist name" : "e.g. Late night walk"}
              className="border-zinc-700 bg-zinc-950/60"
            />
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              onClick={closePlaylistDialog}
              className="text-zinc-400 hover:text-zinc-100"
            >
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={saving}
              className="bg-emerald-500 text-zinc-950 hover:bg-emerald-400"
            >
              {saving ? <Loader2 className="size-4 animate-spin" /> : null}
              {dialog?.mode === "rename" ? "Save" : "Create"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
