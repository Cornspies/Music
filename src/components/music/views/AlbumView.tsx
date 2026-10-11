"use client"

import { useMemo } from "react"
import { Disc3, Music2 } from "lucide-react"
import { formatTotal, hashGradient } from "@/lib/format"
import { useLibraryStore } from "@/stores/library"
import { useUIStore } from "@/stores/ui"
import { CoverArt } from "@/components/music/CoverArt"
import { CollectionHeader, PinButton } from "@/components/music/CollectionHeader"
import { EmptyState } from "@/components/music/EmptyState"
import { TrackList } from "@/components/music/TrackList"

export function AlbumView() {
  const viewArg = useUIStore((s) => s.viewArg)
  const tracks = useLibraryStore((s) => s.tracks)

  const albumKey: { name: string; artist: string } = useMemo(() => {
    try {
      return JSON.parse(viewArg ?? "{}") as { name: string; artist: string }
    } catch {
      return { name: "", artist: "" }
    }
  }, [viewArg])

  const albumTracks = useMemo(
    () => tracks.filter((t) => t.album === albumKey.name && t.artist === albumKey.artist),
    [tracks, albumKey]
  )

  const withCover = albumTracks.find((t) => t.hasCover)
  const totalDuration = albumTracks.reduce((sum, t) => sum + t.duration, 0)
  const title = albumKey.name || "Unknown album"

  return (
    <div>
      <CollectionHeader
        icon={Disc3}
        iconGradient={hashGradient(title)}
        eyebrow="Album"
        title={title}
        meta={`${albumKey.artist || "Unknown artist"} · ${albumTracks.length} track${
          albumTracks.length === 1 ? "" : "s"
        } · ${formatTotal(totalDuration)}`}
        tracks={albumTracks}
        actions={albumKey.name ? <PinButton kind="album" name={albumKey.name} artist={albumKey.artist} /> : null}
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
        {albumTracks.length === 0 ? (
          <EmptyState
            icon={Music2}
            title="No tracks in this album"
            compact
          />
        ) : (
          <TrackList tracks={albumTracks} />
        )}
      </div>
    </div>
  )
}
