"use client"

import { useEffect, useState } from "react"
import { avatarUrl } from "@/lib/api"
import { hashGradient, initialsOf } from "@/lib/format"
import { cn } from "@/lib/utils"

interface Props {
  userId: string
  username: string
  hasAvatar: boolean
  /** Rendered edge in px. */
  size?: number
  className?: string
  /** Extra context for screen readers, e.g. "Uploaded by alice". */
  label?: string
}

/**
 * A user's avatar, falling back to initials on a deterministic gradient when
 * they haven't set one (or when the image fails to load).
 */
export function UserAvatar({ userId, username, hasAvatar, size = 20, className, label }: Props) {
  const [failed, setFailed] = useState(false)

  // A fresh upload reuses the same URL, so reset the error state when the
  // flag flips — otherwise a previously-404ing avatar would stay hidden.
  useEffect(() => {
    setFailed(false)
  }, [userId, hasAvatar])

  const showImage = hasAvatar && !failed
  const dimension = { width: size, height: size }

  return (
    <span
      className={cn(
        "relative inline-flex shrink-0 items-center justify-center overflow-hidden rounded-full",
        className
      )}
      style={dimension}
      title={label ?? username}
    >
      {showImage ? (
        <img
          src={avatarUrl(userId)}
          alt={label ?? username}
          width={size}
          height={size}
          className="size-full object-cover"
          onError={() => setFailed(true)}
          draggable={false}
        />
      ) : (
        <span
          aria-label={label ?? username}
          role="img"
          className={cn(
            // hashGradient returns Tailwind gradient classes (as used by
            // CoverArt), so it belongs in className — not in a style value.
            "flex size-full items-center justify-center bg-linear-to-br font-semibold tracking-tight text-zinc-100/90 select-none",
            hashGradient(username || userId)
          )}
          style={{ fontSize: Math.max(8, Math.round(size * 0.4)) }}
        >
          {initialsOf(username || "?")}
        </span>
      )}
    </span>
  )
}
