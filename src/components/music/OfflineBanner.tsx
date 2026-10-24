"use client"

import { useEffect } from "react"
import { Wrench, WifiOff } from "lucide-react"
import { apiUrl } from "@/lib/desktop"
import { useLibraryStore } from "@/stores/library"
import { useOfflineStore } from "@/stores/offline"
import { serverDownFrom, serverDownMessage, useServerStatusStore } from "@/stores/server-status"
import { useUIStore } from "@/stores/ui"

const CHECK_EVERY_MS = 30_000

// Asks the server whether it's back, and reloads the library once it is.
async function checkBack(): Promise<void> {
  let res: Response
  try {
    res = await fetch(apiUrl("/api/auth/config"), { cache: "no-store" })
  } catch {
    return
  }
  if (res.ok) {
    useServerStatusStore.getState().setDown(null)
    useLibraryStore.getState().fetchAll().catch(() => {})
    return
  }
  const down = serverDownFrom(res.status, await res.json().catch(() => null))
  if (down) useServerStatusStore.getState().setDown(down)
}

export function OfflineBanner() {
  const offline = useOfflineStore((s) => s.offline)
  const count = useOfflineStore((s) => s.tracks.length)
  const down = useServerStatusStore((s) => s.down)
  const navigate = useUIStore((s) => s.navigate)
  const away = offline || down !== null

  useEffect(() => {
    if (!away) return
    const timer = setInterval(() => {
      if (document.visibilityState === "visible") checkBack()
    }, CHECK_EVERY_MS)
    const onVisible = () => {
      if (document.visibilityState === "visible") checkBack()
    }
    document.addEventListener("visibilitychange", onVisible)
    return () => {
      clearInterval(timer)
      document.removeEventListener("visibilitychange", onVisible)
    }
  }, [away])

  if (!away) return null
  const Icon = down?.maintenance ? Wrench : WifiOff
  return (
    <div
      role="status"
      className="flex shrink-0 flex-wrap items-center justify-center gap-x-2 gap-y-0.5 border-b border-amber-900/50 bg-amber-950/40 px-4 py-1.5 text-center text-xs text-amber-200"
    >
      <Icon className="size-3.5 shrink-0" aria-hidden="true" />
      {down ? serverDownMessage(down) : "You're offline."}
      {down?.message ? <span className="text-amber-200/75">{down.message}</span> : null}
      {count > 0 ? (
        <button
          type="button"
          onClick={() => navigate("offline")}
          className="font-medium underline-offset-2 hover:underline"
        >
          {count} downloaded track{count === 1 ? "" : "s"} still play.
        </button>
      ) : null}
    </div>
  )
}
