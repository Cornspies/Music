"use client"

import { useEffect } from "react"
import { invoke } from "@tauri-apps/api/core"
import { listen } from "@tauri-apps/api/event"
import { toast } from "sonner"
import { appReleaseUrl, fetchLatestAppRelease } from "@/lib/api"
import { isDesktop, serverUrl } from "@/lib/desktop"
import { useAuthStore } from "@/stores/auth"

const DISMISSED = "music-update-dismissed"
const CHECK_EVERY_MS = 12 * 60 * 60_000
const TOAST = "desktop-update"

interface About {
  version: string
  bundle: string | null
  selfUpdates?: boolean
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

// Downloads, checks and installs the build; the app then restarts into it, so
// this only comes back when something went wrong.
async function install(version: string) {
  const label = `Updating to Music ${version}`
  const progress = { id: TOAST, duration: Infinity, action: null, onDismiss: undefined }
  toast.loading(`${label}…`, progress)
  const stop = await listen<{ downloaded: number; total: number | null }>("update-progress", ({ payload }) => {
    if (!payload.total) return
    const percent = Math.min(100, Math.round((payload.downloaded / payload.total) * 100))
    toast.loading(`${label}… ${percent}%`, progress)
  })
  try {
    await invoke("update_install")
  } catch (err) {
    toast.error(typeof err === "string" ? err : "The update couldn't be installed", { ...progress, duration: 10_000 })
  } finally {
    stop()
  }
}

async function checkSelf() {
  const token = useAuthStore.getState().token
  if (!token) return
  const offer = await invoke<{ version: string } | null>("update_check", { server: serverUrl(), token })
  if (!offer || dismissed() === offer.version) return
  toast(`Music ${offer.version} is available`, {
    id: TOAST,
    duration: Infinity,
    action: {
      label: "Update",
      onClick: (event) => {
        // Keeps this toast, which goes on to show the progress. Closed, it
        // would also count the update as dismissed, and one that then failed
        // would not be offered again.
        event.preventDefault()
        void install(offer.version)
      },
    },
    onDismiss: () => dismiss(offer.version),
  })
}

async function check() {
  const about = await invoke<About>("app_about")
  if (!about.bundle || about.bundle === "other") return
  if (about.selfUpdates) return checkSelf()
  const { latest, updateAvailable } = await fetchLatestAppRelease(about.bundle, about.version)
  if (!updateAvailable || !latest || dismissed() === latest.version) return
  toast(`Music ${latest.version} is available`, {
    id: TOAST,
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
