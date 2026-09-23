"use client"

import { useEffect, useState } from "react"
import { avatarUrl } from "@/lib/api"
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
  const [failed, setFailed] = useState(false)

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
