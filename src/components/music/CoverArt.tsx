"use client"

import { useState } from "react"
import { Music2 } from "lucide-react"
import { cn } from "@/lib/utils"
import { coverUrl } from "@/lib/api"
import { hashGradient, initialsOf } from "@/lib/format"

interface CoverArtProps {
  trackId: string
  title?: string
  hasCover: boolean
  className?: string
  iconClassName?: string
  initialsClassName?: string
}

/**
 * Cover art with graceful fallback: server cover when hasCover (onError →
 * gradient), otherwise a deterministic warm dark gradient with initials/icon
 * and a subtle top-light sheen. Size is controlled by the parent (className).
 */
export function CoverArt({
  trackId,
  title,
  hasCover,
  className,
  iconClassName,
  initialsClassName,
}: CoverArtProps) {
  const [failed, setFailed] = useState(false)

  if (hasCover && !failed) {
    return (
      <div className={cn("relative aspect-square w-full overflow-hidden rounded-md", className)}>
        <img
          src={coverUrl({ id: trackId })}
          alt={title ? `Album cover of ${title}` : "Album cover"}
          onError={() => setFailed(true)}
          loading="lazy"
          className="aspect-square w-full rounded-md bg-zinc-800/80 object-cover shadow-md"
        />
        {/* top-light sheen for depth */}
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
      {/* subtle sheen + inner vignette */}
      <div className="pointer-events-none absolute inset-0 bg-linear-to-br from-white/[0.07] via-transparent to-black/30" />
      <div className="pointer-events-none absolute -right-3 -bottom-4 size-1/2 rounded-full bg-black/20 blur-md" />
      {initials ? (
        <span
          className={cn(
            "relative truncate px-1 text-center font-bold tracking-wide text-zinc-200/90 select-none drop-shadow-sm",
            "text-sm md:text-base",
            initialsClassName
          )}
        >
          {initials}
        </span>
      ) : (
        <Music2 className={cn("relative size-1/3 text-zinc-400/70 drop-shadow-sm", iconClassName)} />
      )}
    </div>
  )
}
