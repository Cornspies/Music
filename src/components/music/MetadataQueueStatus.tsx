"use client"

import { useCallback, useEffect, useRef, useState } from "react"
import { Loader2, Pause, Sparkles } from "lucide-react"
import { cn } from "@/lib/utils"
import { fetchMetadataQueue } from "@/lib/api"
import type { MetadataQueueStatus as Status } from "@/lib/types"

/** The server's lookup queue, polled every few seconds while mounted. */
export function useMetadataQueue() {
  const [status, setStatus] = useState<Status | null>(null)
  const alive = useRef(false)

  const refresh = useCallback(
    () =>
      fetchMetadataQueue()
        .then((s) => {
          if (alive.current) setStatus(s)
        })
        .catch(() => {}),
    []
  )

  useEffect(() => {
    alive.current = true
    refresh()
    const t = setInterval(refresh, 3000)
    return () => {
      alive.current = false
      clearInterval(t)
    }
  }, [refresh])

  return { status, refresh }
}

const trackCount = (n: number) => `${n} track${n === 1 ? "" : "s"}`

/**
 * One line on the server's metadata lookup queue — how many tracks are
 * waiting, which one is being looked up, and whether it's held up by a
 * provider's rate limit or an admin's pause. Polls while mounted; renders
 * nothing when idle unless `showIdle` asks for it.
 */
export function MetadataQueueStatus({ className, showIdle = false }: { className?: string; showIdle?: boolean }) {
  const { status } = useMetadataQueue()
  return <MetadataQueueLine status={status} className={className} showIdle={showIdle} />
}

/** The line itself, for a caller that polls the queue on its own. */
export function MetadataQueueLine({
  status,
  className,
  showIdle = false,
}: {
  status: Status | null
  className?: string
  showIdle?: boolean
}) {
  if (!status) return null
  if (status.disabled) {
    if (!showIdle) return null
    return (
      <p className={cn("text-xs text-zinc-500", className)}>
        Turned off by <code className="text-zinc-400">MUSIC_DISABLE_METADATA_LOOKUP</code>.
      </p>
    )
  }
  const busy = status.queued > 0 || status.running !== null
  if (!busy && !showIdle) return null

  let text: string
  if (status.paused) {
    const waiting = status.queued > 0 ? ` — ${trackCount(status.queued)} waiting` : ""
    text = status.running
      ? `Pausing after “${status.running.title}”${waiting}`
      : `Metadata lookup paused${waiting}`
  } else if (!busy) {
    text = "Metadata lookups: nothing queued."
  } else if (status.nextRetryAt) {
    const at = new Date(status.nextRetryAt)
    text = `Waiting for a rate limit to clear — ${trackCount(status.queued)} queued, retrying at ${at.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`
  } else {
    const parts: string[] = []
    if (status.running) parts.push(`looking up “${status.running.title}”`)
    if (status.queued > 0) parts.push(`${status.queued} more queued`)
    text = `Metadata lookup: ${parts.join(", ")}`
  }

  return (
    <p className={cn("flex items-center gap-2 text-xs text-zinc-400", className)}>
      {status.paused && !status.running ? (
        <Pause className="size-3.5 shrink-0 text-amber-300/80" aria-hidden="true" />
      ) : busy ? (
        <Loader2 className="size-3.5 shrink-0 animate-spin text-emerald-400" aria-hidden="true" />
      ) : (
        <Sparkles className="size-3.5 shrink-0 text-zinc-500" aria-hidden="true" />
      )}
      <span>{text}</span>
    </p>
  )
}
