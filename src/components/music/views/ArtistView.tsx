"use client"

import { useMemo } from "react"
import { Library, Mic2, Music2 } from "lucide-react"
import { formatTotal, hasArtist, hashGradient } from "@/lib/format"
import { useLibraryStore } from "@/stores/library"
import { useUIStore } from "@/stores/ui"
import { ArtistImage } from "@/components/music/ArtistImage"
import { CollectionHeader, PinButton } from "@/components/music/CollectionHeader"
import { EmptyState } from "@/components/music/EmptyState"
import { TrackList } from "@/components/music/TrackList"
import { Button } from "@/components/ui/button"

export function ArtistView() {
  const viewArg = useUIStore((s) => s.viewArg)
  const tracks = useLibraryStore((s) => s.tracks)

  const artistName = viewArg ?? ""
  const artistTracks = useMemo(
    () => tracks.filter((t) => hasArtist(t.artist, artistName)),
    [tracks, artistName]
  )

  const totalDuration = artistTracks.reduce((sum, t) => sum + t.duration, 0)
  const albumCount = new Set(artistTracks.map((t) => t.album)).size
  const name = artistName || "Unknown artist"

  return (
    <div>
      <CollectionHeader
        icon={Mic2}
        iconGradient={hashGradient(name)}
        eyebrow="Artist"
        title={name}
        meta={`${artistTracks.length} track${artistTracks.length === 1 ? "" : "s"} · ${
          albumCount > 0 ? `${albumCount} album${albumCount === 1 ? "" : "s"} · ` : ""
        }${formatTotal(totalDuration)}`}
        tracks={artistTracks}
        actions={
          artistName ? (
            <>
              <PinButton kind="artist" name={artistName} />
              <Button
                variant="outline"
                onClick={() => useUIStore.getState().navigate("catalog", `find:${artistName}`)}
                className="h-10 rounded-full border-zinc-600/80 bg-zinc-900/60 px-5 text-zinc-200 hover:bg-zinc-800 hover:text-emerald-300"
              >
                <Library className="size-4" /> Discography
              </Button>
            </>
          ) : null
        }
        cover={
          <ArtistImage
            name={artistName}
            sizes="(min-width: 1024px) 224px, (min-width: 768px) 192px, 160px"
            className="shadow-2xl"
            initialsClassName="text-6xl md:text-7xl"
          />
        }
      />

      <div className="px-4 pt-5 pb-6 md:px-6">
        {artistTracks.length === 0 ? (
          <EmptyState
            icon={Music2}
            title="No tracks by this artist"
            compact
          />
        ) : (
          <TrackList tracks={artistTracks} />
        )}
      </div>
    </div>
  )
}
