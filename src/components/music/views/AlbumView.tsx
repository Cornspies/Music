"use client"

import { useMemo, useState } from "react"
import { toast } from "sonner"
import { Combine, Disc3, Loader2, Music2, Split, UserPen } from "lucide-react"
import { albums as albumsApi } from "@/lib/api"
import { albumArtistOf, formatTotal, hashGradient, splitArtists } from "@/lib/format"
import { routeToPath } from "@/lib/routes"
import type { Track } from "@/lib/types"
import { cn } from "@/lib/utils"
import { useLibraryStore } from "@/stores/library"
import { useUIStore } from "@/stores/ui"
import { CoverArt } from "@/components/music/CoverArt"
import { CollectionHeader, HeaderMenuTrigger, PinButton } from "@/components/music/CollectionHeader"
import { EmptyState } from "@/components/music/EmptyState"
import { TrackList } from "@/components/music/TrackList"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"

const VARIOUS = "Various Artists"

const sameName = (a: string, b: string) => a.trim().toLocaleLowerCase() === b.trim().toLocaleLowerCase()

function byTrackNo(a: Track, b: Track) {
  const an = a.trackNo > 0 ? a.trackNo : Number.MAX_SAFE_INTEGER
  const bn = b.trackNo > 0 ? b.trackNo : Number.MAX_SAFE_INTEGER
  return an - bn || a.title.localeCompare(b.title)
}

function openAlbum(name: string, artist: string) {
  const arg = JSON.stringify({ name, artist })
  useUIStore.getState().applyRoute("album", arg)
  window.history.replaceState({ view: "album", viewArg: arg }, "", routeToPath("album", arg))
}

function listing(names: string[]): string {
  if (names.length <= 3) return names.join(", ")
  return `${names.slice(0, 3).join(", ")} and ${names.length - 3} more`
}

function AlbumArtistDialog({
  open,
  onOpenChange,
  album,
  current,
  initial,
  others,
  trackArtists,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  album: string
  current: string
  initial: string
  others: { artist: string; count: number }[]
  trackArtists: string[]
}) {
  const [name, setName] = useState(initial)
  const [include, setInclude] = useState<Set<string>>(() => new Set(others.map((o) => o.artist)))
  const [saving, setSaving] = useState(false)

  const suggestions = useMemo(() => {
    const seen = new Set<string>()
    const out: string[] = []
    for (const n of [VARIOUS, current, ...others.map((o) => o.artist), ...trackArtists]) {
      const key = n.toLocaleLowerCase()
      if (n && !seen.has(key)) {
        seen.add(key)
        out.push(n)
      }
    }
    return out.slice(0, 8)
  }, [current, others, trackArtists])

  const save = async () => {
    const target = name.trim()
    if (!target) {
      toast.error("Give the album an artist")
      return
    }
    setSaving(true)
    try {
      const r = await albumsApi.setArtist(album, [current, ...others.filter((o) => include.has(o.artist)).map((o) => o.artist)], target)
      await useLibraryStore.getState().fetchTracks(useUIStore.getState().librarySort)
      useLibraryStore.getState().fetchPins().catch(() => {})
      toast.success(`“${album}” is now by ${target}`, {
        description: r.skipped > 0 ? `${r.skipped} ${r.skipped === 1 ? "song belongs" : "songs belong"} to someone else and stayed as they were` : undefined,
      })
      onOpenChange(false)
      openAlbum(album, target)
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="border-zinc-800 bg-zinc-900">
        <DialogHeader>
          <DialogTitle className="text-base">{others.length > 0 ? "Combine into one album" : "Change album artist"}</DialogTitle>
          <DialogDescription>Each song keeps its own artist.</DialogDescription>
        </DialogHeader>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault()
            save()
          }}
        >
          <div className="flex flex-col gap-2">
            <label htmlFor="album-artist" className="text-sm font-medium text-zinc-200">
              Album artist
            </label>
            <Input
              id="album-artist"
              value={name}
              autoFocus
              onChange={(e) => setName(e.target.value)}
              className="border-zinc-700 bg-zinc-950/60"
            />
            <div className="flex flex-wrap gap-1.5">
              {suggestions.map((s) => (
                <button
                  key={s}
                  type="button"
                  onClick={() => setName(s)}
                  className={cn(
                    "rounded-full border px-2.5 py-0.5 text-xs transition-colors",
                    sameName(s, name)
                      ? "border-emerald-500/60 bg-emerald-500/10 text-emerald-300"
                      : "border-zinc-700 text-zinc-300 hover:border-zinc-500"
                  )}
                >
                  {s}
                </button>
              ))}
            </div>
          </div>
          {others.length > 0 ? (
            <fieldset className="flex flex-col gap-1.5">
              <legend className="mb-1.5 text-sm font-medium text-zinc-200">Include the albums by</legend>
              {others.map((o) => (
                <label key={o.artist} className="flex items-center gap-2 text-sm text-zinc-300">
                  <input
                    type="checkbox"
                    checked={include.has(o.artist)}
                    onChange={(e) =>
                      setInclude((prev) => {
                        const next = new Set(prev)
                        if (e.target.checked) next.add(o.artist)
                        else next.delete(o.artist)
                        return next
                      })
                    }
                    className="size-4 accent-emerald-500"
                  />
                  <span className="min-w-0 flex-1 truncate">{o.artist}</span>
                  <span className="text-xs text-zinc-500">
                    {o.count} {o.count === 1 ? "song" : "songs"}
                  </span>
                </label>
              ))}
            </fieldset>
          ) : null}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)} className="text-zinc-400 hover:text-zinc-100">
              Cancel
            </Button>
            <Button type="submit" disabled={saving} className="bg-emerald-500 text-zinc-950 hover:bg-emerald-400">
              {saving ? <Loader2 className="size-4 animate-spin" /> : null}
              Save
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

export function AlbumView() {
  const viewArg = useUIStore((s) => s.viewArg)
  const tracks = useLibraryStore((s) => s.tracks)
  const [dialog, setDialog] = useState<"combine" | "artist" | null>(null)
  const [splitting, setSplitting] = useState(false)

  const albumKey: { name: string; artist: string } = useMemo(() => {
    try {
      return JSON.parse(viewArg ?? "{}") as { name: string; artist: string }
    } catch {
      return { name: "", artist: "" }
    }
  }, [viewArg])

  const albumTracks = useMemo(
    () => tracks.filter((t) => t.album === albumKey.name && albumArtistOf(t) === albumKey.artist).sort(byTrackNo),
    [tracks, albumKey]
  )

  const others = useMemo(() => {
    const counts = new Map<string, number>()
    for (const t of tracks) {
      if (!albumKey.name || t.album !== albumKey.name) continue
      const artist = albumArtistOf(t)
      if (artist !== albumKey.artist) counts.set(artist, (counts.get(artist) ?? 0) + 1)
    }
    return [...counts].map(([artist, count]) => ({ artist, count })).sort((a, b) => a.artist.localeCompare(b.artist))
  }, [tracks, albumKey])

  const trackArtists = useMemo(() => {
    const seen = new Set<string>()
    const out: string[] = []
    for (const t of albumTracks) {
      for (const a of splitArtists(t.artist)) {
        if (!seen.has(a.toLocaleLowerCase())) {
          seen.add(a.toLocaleLowerCase())
          out.push(a)
        }
      }
    }
    return out
  }, [albumTracks])

  const combined = albumTracks.some((t) => t.albumArtist?.trim())

  const split = async () => {
    setSplitting(true)
    try {
      const r = await albumsApi.setArtist(albumKey.name, [albumKey.artist], "")
      await useLibraryStore.getState().fetchTracks(useUIStore.getState().librarySort)
      toast.success(`Split “${albumKey.name}” by song artist`, {
        description: r.skipped > 0 ? `${r.skipped} ${r.skipped === 1 ? "song belongs" : "songs belong"} to someone else and stayed as they were` : undefined,
      })
      const first = albumTracks[0]
      if (first) openAlbum(albumKey.name, first.artist)
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setSplitting(false)
    }
  }

  const withCover = albumTracks.find((t) => t.hasCover)
  const totalDuration = albumTracks.reduce((sum, t) => sum + t.duration, 0)
  const title = albumKey.name || "Unknown album"
  const distinctArtists = new Set(albumTracks.map((t) => t.artist)).size

  return (
    <div>
      <CollectionHeader
        icon={Disc3}
        iconGradient={hashGradient(title)}
        eyebrow="Album"
        title={title}
        meta={`${albumKey.artist || "Unknown artist"}${distinctArtists > 1 ? ` · ${distinctArtists} artists` : ""} · ${
          albumTracks.length
        } track${albumTracks.length === 1 ? "" : "s"} · ${formatTotal(totalDuration)}`}
        tracks={albumTracks}
        actions={
          albumKey.name ? (
            <>
              <PinButton kind="album" name={albumKey.name} artist={albumKey.artist} />
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <HeaderMenuTrigger />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start" className="w-60">
                  <DropdownMenuItem onSelect={() => setDialog("artist")}>
                    <UserPen className="size-4" /> Change album artist…
                  </DropdownMenuItem>
                  {others.length > 0 ? (
                    <DropdownMenuItem onSelect={() => setDialog("combine")}>
                      <Combine className="size-4" /> Combine with other “{albumKey.name}” albums…
                    </DropdownMenuItem>
                  ) : null}
                  {combined ? (
                    <DropdownMenuItem disabled={splitting} onSelect={split}>
                      <Split className="size-4" /> Split by song artist
                    </DropdownMenuItem>
                  ) : null}
                </DropdownMenuContent>
              </DropdownMenu>
            </>
          ) : null
        }
        cover={
          withCover ? (
            <CoverArt
              trackId={withCover.id}
              title={title}
              hasCover
              size={256}
              sizes="(min-width: 1024px) 224px, (min-width: 768px) 192px, 160px"
            />
          ) : (
            <CoverArt trackId={`${albumKey.name}|${albumKey.artist}`} title={title} hasCover={false} />
          )
        }
      />

      <div className="px-4 pt-5 pb-6 md:px-6">
        {others.length > 0 ? (
          <div className="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-xl border border-zinc-800 bg-zinc-900/50 px-4 py-3">
            <p className="min-w-0 text-sm text-zinc-300">
              Also in your library as separate albums by {listing(others.map((o) => o.artist))}
            </p>
            <Button
              size="sm"
              variant="outline"
              onClick={() => setDialog("combine")}
              className="border-zinc-700 bg-zinc-900/60 text-zinc-200 hover:bg-zinc-800 hover:text-emerald-300"
            >
              <Combine className="size-4" /> Combine…
            </Button>
          </div>
        ) : null}
        {albumTracks.length === 0 ? (
          <EmptyState icon={Music2} title="No tracks in this album" compact />
        ) : (
          <TrackList tracks={albumTracks} />
        )}
      </div>

      {dialog ? (
        <AlbumArtistDialog
          open
          onOpenChange={(o) => {
            if (!o) setDialog(null)
          }}
          album={albumKey.name}
          current={albumKey.artist}
          initial={dialog === "combine" ? VARIOUS : albumKey.artist}
          others={dialog === "combine" ? others : []}
          trackArtists={trackArtists}
        />
      ) : null}
    </div>
  )
}
