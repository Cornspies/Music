"use client"

import { useMemo } from "react"
import { Mic2 } from "lucide-react"
import { cn } from "@/lib/utils"
import { hashGradient, initialsOf } from "@/lib/format"
import { useLibraryStore } from "@/stores/library"
import { CoverArt } from "@/components/music/CoverArt"

export function ArtistImage({
  name,
  size = 256,
  sizes,
  className,
  initialsClassName,
}: {
  name: string
  size?: 128 | 256 | 512
  sizes?: string
  className?: string
  initialsClassName?: string
}) {
  const tracks = useLibraryStore((s) => s.tracks)
  const coverId = useMemo(() => {
    let best: { id: string; plays: number } | null = null
    for (const t of tracks) {
      if (t.artist !== name || !t.hasCover) continue
      if (!best || t.plays > best.plays) best = { id: t.id, plays: t.plays }
    }
    return best?.id ?? null
  }, [tracks, name])

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
