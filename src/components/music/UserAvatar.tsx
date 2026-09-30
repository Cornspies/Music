"use client"

import { useState } from "react"
import { avatarUrl } from "@/lib/api"
import { useMediaToken } from "@/stores/media-token"
import { hashGradient, initialsOf } from "@/lib/format"
import { cn } from "@/lib/utils"

interface Props {
  userId: string
  username: string
  hasAvatar: boolean
  size?: number
  className?: string
  label?: string
}

export function UserAvatar({ userId, username, hasAvatar, size = 20, className, label }: Props) {
  const mediaToken = useMediaToken()
  // What failed to load; another user, or a new media token, gets a new try.
  const attempt = `${userId}:${mediaToken}`
  const [failedAttempt, setFailedAttempt] = useState<string | null>(null)

  const showImage = hasAvatar && Boolean(mediaToken) && failedAttempt !== attempt
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
          onError={() => setFailedAttempt(attempt)}
          draggable={false}
        />
      ) : (
        <span
          aria-label={label ?? username}
          role="img"
          className={cn(
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
