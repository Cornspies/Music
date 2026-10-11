"use client"

import { useMemo } from "react"
import { Mic2 } from "lucide-react"
import { cn } from "@/lib/utils"
import { hasArtist, hashGradient, initialsOf } from "@/lib/format"
import { catalog } from "@/lib/api"
import { useLibraryStore } from "@/stores/library"
import { CoverArt } from "@/components/music/CoverArt"

export function ArtistImage({
  name,
  size = 256,
  sizes,
  className,
  initialsClassName,
  picture,
}: {
  name: string
  picture?: string
  size?: 128 | 256 | 512
  sizes?: string
  className?: string
  initialsClassName?: string
}) {
  const tracks = useLibraryStore((s) => s.tracks)
  const coverId = useMemo(() => {
    let best: { id: string; plays: number } | null = null
    for (const t of tracks) {
      if (!t.hasCover || !hasArtist(t.artist, name)) continue
      if (!best || t.plays > best.plays) best = { id: t.id, plays: t.plays }
    }
    return best?.id ?? null
  }, [tracks, name])

  if (picture) {
    return (
      <img
        src={catalog.imageUrl(picture)}
        alt=""
        loading="lazy"
        className={cn("aspect-square w-full rounded-full bg-zinc-800 object-cover", className)}
      />
    )
  }
  if (coverId) {
    return (
      <CoverArt
        trackId={coverId}
        title={name}
        hasCover
        size={size}
        sizes={sizes}
        className={cn("rounded-full", className)}
      />
    )
  }
  return (
    <div
      aria-hidden="true"
      className={cn(
        "flex aspect-square w-full items-center justify-center rounded-full bg-linear-to-br font-black text-zinc-300/85",
        hashGradient(name),
        className,
        initialsClassName
      )}
    >
      {initialsOf(name) || <Mic2 className="size-1/3 text-zinc-400/70" />}
    </div>
  )
}
