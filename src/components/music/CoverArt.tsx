"use client"

import { useState } from "react"
import { Music2 } from "lucide-react"
import { cn } from "@/lib/utils"
import { coverUrl, type CoverSize } from "@/lib/api"
import { localCoverUrl } from "@/lib/desktop"
import { hashGradient, initialsOf } from "@/lib/format"
import { useMediaToken } from "@/stores/media-token"

interface CoverArtProps {
  trackId: string
  title?: string
  hasCover: boolean
  className?: string
  iconClassName?: string
  initialsClassName?: string
  size?: CoverSize | "full"
  sizes?: string
}

const SRCSET_SIZES = [128, 256, 512] as const

function coverSrcSet(trackId: string, full: boolean): string {
  const set = SRCSET_SIZES.map((w) => `${coverUrl({ id: trackId }, w)} ${w}w`)
  if (full) set.push(`${coverUrl({ id: trackId })} 1000w`)
  return set.join(", ")
}

export function CoverArt({
  trackId,
  title,
  hasCover,
  className,
  iconClassName,
  initialsClassName,
  size = 128,
  sizes,
}: CoverArtProps) {
  const mediaToken = useMediaToken()
  const [failedWith, setFailedWith] = useState<string | null>(null)
  const local = hasCover ? localCoverUrl(trackId) : null
  const source = local ?? mediaToken

  if (hasCover && source && failedWith !== source) {
    return (
      <div className={cn("relative aspect-square w-full overflow-hidden rounded-md", className)}>
        <img
          src={local ?? coverUrl({ id: trackId }, size === "full" ? undefined : size)}
          srcSet={!local && sizes ? coverSrcSet(trackId, size === "full") : undefined}
          sizes={!local && sizes ? sizes : undefined}
          alt={title ? `Album cover of ${title}` : "Album cover"}
          onError={() => setFailedWith(source)}
          loading="lazy"
          className="aspect-square w-full rounded-md bg-zinc-800/80 object-cover shadow-md"
        />
        <div
          aria-hidden="true"
          className="pointer-events-none absolute inset-0 rounded-md bg-linear-to-br from-white/10 via-transparent to-black/20 mix-blend-overlay"
        />
      </div>
    )
  }

  const initials = initialsOf(title ?? "")
  return (
    <div
      aria-hidden="true"
      className={cn(
        "relative flex aspect-square w-full items-center justify-center overflow-hidden rounded-md bg-linear-to-br shadow-md ring-1 ring-white/5",
        hashGradient(trackId),
        className
      )}
    >
      <div className="pointer-events-none absolute inset-0 bg-linear-to-br from-white/[0.07] via-transparent to-black/30" />
      <div className="pointer-events-none absolute -right-5 -bottom-6 size-3/4 rounded-full bg-[radial-gradient(closest-side,rgba(0,0,0,0.2),transparent)]" />
      {initials ? (
        <span
          className={cn(
            "relative truncate px-1 text-center font-bold tracking-wide text-zinc-200/90 select-none text-shadow-sm",
            "text-sm md:text-base",
            initialsClassName
          )}
        >
          {initials}
        </span>
      ) : (
        <Music2 className={cn("relative size-1/3 text-zinc-400/70", iconClassName)} />
      )}
    </div>
  )
}
