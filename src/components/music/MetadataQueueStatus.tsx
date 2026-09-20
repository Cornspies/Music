"use client"

import { useEffect, useState } from "react"
import { Loader2, Sparkles } from "lucide-react"
import { cn } from "@/lib/utils"
import { fetchMetadataQueue } from "@/lib/api"
import type { MetadataQueueStatus as Status } from "@/lib/types"

/**
 * One line on the server's metadata lookup queue — how many tracks are
 * waiting, which one is being looked up, and whether it's paused on a
 * provider's rate limit. Polls while mounted; renders nothing when idle
 * unless `showIdle` asks for it.
 */
export function MetadataQueueStatus({ className, showIdle = false }: { className?: string; showIdle?: boolean }) {
  const [status, setStatus] = useState<Status | null>(null)

  useEffect(() => {
    let alive = true
    const tick = () =>
      fetchMetadataQueue()
        .then((s) => {
          if (alive) setStatus(s)
        })
        .catch(() => {})
    tick()
    const t = setInterval(tick, 3000)
    return () => {
      alive = false
      clearInterval(t)
    }
  }, [])

  if (!status || status.disabled) return null
  const busy = status.queued > 0 || status.running !== null
  if (!busy && !showIdle) return null

  let text: string
  if (!busy) {
    text = "Metadata lookups: nothing queued."
  } else if (status.nextRetryAt) {
    const at = new Date(status.nextRetryAt)
    text = `Waiting for a rate limit to clear — ${status.queued} track${status.queued === 1 ? "" : "s"} queued, retrying at ${at.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`
  } else {
    const parts: string[] = []
    if (status.running) parts.push(`looking up “${status.running.title}”`)
    if (status.queued > 0) parts.push(`${status.queued} more queued`)
    text = `Metadata lookup: ${parts.join(", ")}`
  }

  return (
    <p className={cn("flex items-center gap-2 text-xs text-zinc-400", className)}>
      {busy ? (
        <Loader2 className="size-3.5 shrink-0 animate-spin text-emerald-400" aria-hidden="true" />
      ) : (
        <Sparkles className="size-3.5 shrink-0 text-zinc-500" aria-hidden="true" />
      )}
      <span>{text}</span>
    </p>
  )
}
