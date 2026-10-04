"use client"

import { useEffect } from "react"
import { invoke } from "@tauri-apps/api/core"
import { toast } from "sonner"
import { appReleaseUrl, fetchLatestAppRelease } from "@/lib/api"
import { isDesktop } from "@/lib/desktop"

const DISMISSED = "music-update-dismissed"
const CHECK_EVERY_MS = 12 * 60 * 60_000

interface About {
  version: string
  bundle: string | null
}

function dismissed(): string | null {
  try {
    return localStorage.getItem(DISMISSED)
  } catch {
    return null
  }
}

function dismiss(version: string) {
  try {
    localStorage.setItem(DISMISSED, version)
  } catch {}
}

async function check() {
  const about = await invoke<About>("app_about")
  if (!about.bundle || about.bundle === "other") return
  const { latest, updateAvailable } = await fetchLatestAppRelease(about.bundle, about.version)
  if (!updateAvailable || !latest || dismissed() === latest.version) return
  toast(`Music ${latest.version} is available`, {
    id: "desktop-update",
    duration: Infinity,
    action: {
      label: "Download",
      onClick: () => {
        dismiss(latest.version)
        window.open(appReleaseUrl(latest.fileName), "_blank")
      },
    },
    onDismiss: () => dismiss(latest.version),
  })
}

export function useDesktopUpdates() {
  useEffect(() => {
    if (!isDesktop) return
    const run = () => {
      check().catch(() => {})
    }
    run()
    const timer = window.setInterval(run, CHECK_EVERY_MS)
    return () => window.clearInterval(timer)
  }, [])
}
