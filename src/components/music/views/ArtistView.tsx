"use client"

import { useMemo } from "react"
import { Mic2, Music2 } from "lucide-react"
import { formatTotal, hashGradient } from "@/lib/format"
import { useLibraryStore } from "@/stores/library"
import { useUIStore } from "@/stores/ui"
import { ArtistImage } from "@/components/music/ArtistImage"
import { CollectionHeader } from "@/components/music/CollectionHeader"
import { EmptyState } from "@/components/music/EmptyState"
import { TrackList } from "@/components/music/TrackList"

export function ArtistView() {
  const viewArg = useUIStore((s) => s.viewArg)
  const tracks = useLibraryStore((s) => s.tracks)

  const artistName = viewArg ?? ""
  const artistTracks = useMemo(
    () => tracks.filter((t) => t.artist === artistName),
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
