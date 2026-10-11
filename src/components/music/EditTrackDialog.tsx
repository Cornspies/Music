"use client"

import { useState } from "react"
import { toast } from "sonner"
import { Loader2, Pencil, Search, X } from "lucide-react"
import { cn } from "@/lib/utils"
import { catalog, fieldLookup } from "@/lib/api"
import { FieldLookup, type LookupField, type LookupPick } from "@/components/music/FieldLookup"
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

function LookupLabel({
  htmlFor,
  label,
  active,
  onLookup,
}: {
  htmlFor: string
  label: string
  active: boolean
  onLookup: () => void
}) {
  return (
    <div className="flex items-center justify-between gap-2">
      <Label htmlFor={htmlFor}>{label}</Label>
      <button
        type="button"
        onClick={onLookup}
        aria-pressed={active}
        className={cn(
          "inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] transition-colors",
          active ? "bg-emerald-500/15 text-emerald-300" : "text-zinc-500 hover:text-emerald-300"
        )}
      >
        <Search className="size-3" /> Look up
      </button>
    </div>
  )
}

function EditTrackForm({
  track,
  onClose,
  onLookupChange,
}: {
  track: Track
  onClose: () => void
  onLookupChange: (open: boolean) => void
}) {
  const [title, setTitle] = useState(track.title)
  const [artist, setArtist] = useState(track.artist)
  const [album, setAlbum] = useState(track.album)
  const [albumArtist, setAlbumArtist] = useState(track.albumArtist ?? "")
  const [genre, setGenre] = useState(track.genre)
  const [year, setYear] = useState(track.year ? String(track.year) : "")
  const [saving, setSaving] = useState(false)
  const [lookup, setLookupRaw] = useState<LookupField | null>(null)
  const [pendingCover, setPendingCover] = useState<{ url: string; source: string } | null>(null)

  const setLookup = (field: LookupField | null) => {
    setLookupRaw(field)
    onLookupChange(field !== null)
  }

  const pick = (field: LookupField, p: LookupPick) => {
    if (field === "album") setAlbum(p.value)
    if (field === "albumArtist") setAlbumArtist(p.value)
    if (field === "album" && p.albumArtist !== undefined) setAlbumArtist(p.albumArtist)
    if (field === "artist") setArtist(p.value)
    if (field === "genre") setGenre(p.value)
    if (field === "year" || p.year) setYear(String(p.year ?? p.value))
    if (p.cover) setPendingCover({ url: p.cover, source: p.source })
    setLookup(null)
  }

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
      albumArtist: albumArtist.trim().toLowerCase() === artist.trim().toLowerCase() ? "" : albumArtist.trim(),
      genre: genre.trim(),
      year: yearNum,
    })
    if (ok && pendingCover) {
      try {
        useLibraryStore.getState().replaceTrack(await fieldLookup.setCover(track.id, pendingCover.url))
      } catch (e) {
        toast.error(`The details were saved, but the cover wasn't: ${(e as Error).message}`)
      }
    }
    setSaving(false)
    if (ok) {
      toast.success("Track updated")
      onClose()
    }
  }

  return (
    <form
      className="flex min-w-0 flex-col gap-4"
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
          <LookupLabel
            htmlFor="edit-artist"
            label="Artist"
            active={lookup === "artist"}
            onLookup={() => setLookup(lookup === "artist" ? null : "artist")}
          />
          <Input
            id="edit-artist"
            value={artist}
            onChange={(e) => setArtist(e.target.value)}
            placeholder="Artist name"
            className="border-zinc-700 bg-zinc-950/60"
          />
        </div>
        <div className="flex flex-col gap-2">
          <LookupLabel
            htmlFor="edit-album"
            label="Album"
            active={lookup === "album"}
            onLookup={() => setLookup(lookup === "album" ? null : "album")}
          />
          <Input
            id="edit-album"
            value={album}
            onChange={(e) => setAlbum(e.target.value)}
            placeholder="Album name"
            className="border-zinc-700 bg-zinc-950/60"
          />
        </div>
        <div className="flex flex-col gap-2">
          <LookupLabel
            htmlFor="edit-album-artist"
            label="Album artist"
            active={lookup === "albumArtist"}
            onLookup={() => setLookup(lookup === "albumArtist" ? null : "albumArtist")}
          />
          <Input
            id="edit-album-artist"
            value={albumArtist}
            onChange={(e) => setAlbumArtist(e.target.value)}
            placeholder={artist.trim() ? `Same as artist (${artist.trim()})` : "Same as artist"}
            className="border-zinc-700 bg-zinc-950/60"
          />
        </div>
        <div className="flex flex-col gap-2">
          <LookupLabel
            htmlFor="edit-genre"
            label="Genre"
            active={lookup === "genre"}
            onLookup={() => setLookup(lookup === "genre" ? null : "genre")}
          />
          <Input
            id="edit-genre"
            value={genre}
            onChange={(e) => setGenre(e.target.value)}
            placeholder="Genre"
            className="border-zinc-700 bg-zinc-950/60"
          />
        </div>
        <div className="flex flex-col gap-2">
          <LookupLabel
            htmlFor="edit-year"
            label="Year"
            active={lookup === "year"}
            onLookup={() => setLookup(lookup === "year" ? null : "year")}
          />
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

      {pendingCover ? (
        <div className="flex items-center gap-3 rounded-lg border border-emerald-500/25 bg-emerald-500/5 p-2">
          <img src={catalog.imageUrl(pendingCover.url)} alt="" className="size-10 rounded bg-zinc-800 object-cover" />
          <p className="min-w-0 flex-1 text-xs text-zinc-300">The cover from {pendingCover.source} will be used when you save.</p>
          <button
            type="button"
            onClick={() => setPendingCover(null)}
            aria-label="Keep the current cover"
            className="rounded p-1 text-zinc-500 hover:text-zinc-100"
          >
            <X className="size-4" />
          </button>
        </div>
      ) : null}

      {lookup ? (
        <FieldLookup
          trackId={track.id}
          field={lookup}
          title={title.trim() || track.title}
          artist={artist.trim()}
          current={{ artist, album, albumArtist, genre, year }[lookup]}
          onPick={(p) => pick(lookup, p)}
          onClose={() => setLookup(null)}
        />
      ) : null}

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
  const [wide, setWide] = useState(false)
  const editTrackId = useUIStore((s) => s.editTrackId)
  const setEditTrackId = useUIStore((s) => s.setEditTrackId)
  const track = useLibraryStore((s) =>
    s.tracks.find((t) => t.id === editTrackId) ?? s.favorites.find((t) => t.id === editTrackId)
  )

  return (
    <Dialog
      open={Boolean(editTrackId && track)}
      onOpenChange={(o) => {
        if (!o) {
          setEditTrackId(null)
          setWide(false)
        }
      }}
    >
      <DialogContent
        aria-describedby={undefined}
        className={cn("max-h-[92dvh] overflow-y-auto border-zinc-800 bg-zinc-900", wide && "sm:max-w-2xl")}
      >
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-base">
            <Pencil className="size-4 text-emerald-400" aria-hidden="true" /> Edit track
          </DialogTitle>
        </DialogHeader>
        {track ? (
          <EditTrackForm
            key={track.id}
            track={track}
            onClose={() => {
              setEditTrackId(null)
              setWide(false)
            }}
            onLookupChange={setWide}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  )
}
