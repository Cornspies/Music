"use client"

import { WifiOff } from "lucide-react"
import { useOfflineStore } from "@/stores/offline"
import { useUIStore } from "@/stores/ui"

/** Thin strip shown while the server can't be reached. */
export function OfflineBanner() {
  const offline = useOfflineStore((s) => s.offline)
  const count = useOfflineStore((s) => s.tracks.length)
  const navigate = useUIStore((s) => s.navigate)
  if (!offline) return null
  return (
    <div
      role="status"
      className="flex shrink-0 items-center justify-center gap-2 border-b border-amber-900/50 bg-amber-950/40 px-4 py-1.5 text-xs text-amber-200"
    >
      <WifiOff className="size-3.5" aria-hidden="true" />
      You&apos;re offline.
      {count > 0 ? (
        <button
          type="button"
          onClick={() => navigate("offline")}
          className="font-medium underline-offset-2 hover:underline"
        >
          {count} downloaded track{count === 1 ? "" : "s"} still play.
        </button>
      ) : (
        <span>Downloads would still play.</span>
      )}
    </div>
  )
}
