"use client"

import { useState } from "react"
import { toast } from "sonner"
import { Loader2, Pencil } from "lucide-react"
import { useUIStore } from "@/stores/ui"
import { useLibraryStore } from "@/stores/library"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import type { Track } from "@/lib/types"

function EditTrackForm({ track, onClose }: { track: Track; onClose: () => void }) {
  const [title, setTitle] = useState(track.title)
  const [artist, setArtist] = useState(track.artist)
  const [album, setAlbum] = useState(track.album)
  const [genre, setGenre] = useState(track.genre)
  const [year, setYear] = useState(track.year ? String(track.year) : "")
  const [saving, setSaving] = useState(false)

  const save = async () => {
    const trimmedTitle = title.trim()
    if (!trimmedTitle) {
      toast.error("Title is required")
      return
    }
    const yearNum = year.trim() ? Number(year.trim()) : 0
    if (yearNum && (!Number.isInteger(yearNum) || yearNum < 1 || yearNum > 3000)) {
      toast.error("Year must be a number between 1 and 3000")
      return
    }
    setSaving(true)
    const ok = await useLibraryStore.getState().updateTrack(track.id, {
      title: trimmedTitle,
      artist: artist.trim(),
      album: album.trim(),
      genre: genre.trim(),
      year: yearNum,
    })
    setSaving(false)
    if (ok) {
      toast.success("Track updated")
      onClose()
    }
  }

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault()
        save()
      }}
    >
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div className="flex flex-col gap-2 sm:col-span-2">
          <Label htmlFor="edit-title">Title</Label>
          <Input
            id="edit-title"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="Track title"
            className="border-zinc-700 bg-zinc-950/60"
            required
          />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="edit-artist">Artist</Label>
          <Input
            id="edit-artist"
            value={artist}
            onChange={(e) => setArtist(e.target.value)}
            placeholder="Artist name"
            className="border-zinc-700 bg-zinc-950/60"
          />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="edit-album">Album</Label>
          <Input
            id="edit-album"
            value={album}
            onChange={(e) => setAlbum(e.target.value)}
            placeholder="Album name"
            className="border-zinc-700 bg-zinc-950/60"
          />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="edit-genre">Genre</Label>
          <Input
            id="edit-genre"
            value={genre}
            onChange={(e) => setGenre(e.target.value)}
            placeholder="Genre"
            className="border-zinc-700 bg-zinc-950/60"
          />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="edit-year">Year</Label>
          <Input
            id="edit-year"
            inputMode="numeric"
            value={year}
            onChange={(e) => setYear(e.target.value.replace(/[^\d]/g, ""))}
            placeholder="e.g. 2024"
            className="border-zinc-700 bg-zinc-950/60"
          />
        </div>
      </div>

      <DialogFooter>
        <Button
          type="button"
          variant="ghost"
          onClick={onClose}
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
          Save changes
        </Button>
      </DialogFooter>
    </form>
  )
}

export function EditTrackDialog() {
  const editTrackId = useUIStore((s) => s.editTrackId)
  const setEditTrackId = useUIStore((s) => s.setEditTrackId)
  const track = useLibraryStore((s) =>
    s.tracks.find((t) => t.id === editTrackId) ?? s.favorites.find((t) => t.id === editTrackId)
  )

  return (
    <Dialog
      open={Boolean(editTrackId && track)}
      onOpenChange={(o) => {
        if (!o) setEditTrackId(null)
      }}
    >
      <DialogContent aria-describedby={undefined} className="border-zinc-800 bg-zinc-900">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-base">
            <Pencil className="size-4 text-emerald-400" aria-hidden="true" /> Edit track
          </DialogTitle>
        </DialogHeader>
        {track ? <EditTrackForm key={track.id} track={track} onClose={() => setEditTrackId(null)} /> : null}
      </DialogContent>
    </Dialog>
  )
}
