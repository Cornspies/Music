"use client"

import { useEffect } from "react"
import { invoke } from "@tauri-apps/api/core"
import { listen } from "@tauri-apps/api/event"
import { toast } from "sonner"
import { appPathname, isDesktop, linkedUrl } from "@/lib/desktop"
import { pathToRoute } from "@/lib/routes"
import { useAuthStore } from "@/stores/auth"
import { useUIStore } from "@/stores/ui"

function openLink(link: string) {
  const target = linkedUrl(link)
  if (!target) return
  const pathname = appPathname(target)
  if (pathname === null) {
    toast.error(`This link is for ${target.host}`, {
      action: { label: "Open in browser", onClick: () => window.open(target.href, "_blank") },
    })
    return
  }
  if (!useAuthStore.getState().token) {
    window.location.assign(pathname + target.search)
    return
  }
  const { view, arg } = pathToRoute(pathname)
  const ui = useUIStore.getState()
  ui.setNowPlayingOpen(false)
  ui.navigate(view, arg ?? undefined)
}

export function useDesktopLinks() {
  useEffect(() => {
    if (!isDesktop) return
    let alive = true
    let unlisten: (() => void) | undefined
    const take = () => {
      invoke<string[]>("take_links")
        .then((links) => links.forEach(openLink))
        .catch((err: unknown) => console.error("take_links failed", err))
    }
    listen("links", take)
      .then((stop) => {
        if (alive) unlisten = stop
        else stop()
      })
      .catch((err: unknown) => console.error("listening for links failed", err))
    take()
    return () => {
      alive = false
      unlisten?.()
    }
  }, [])
}
