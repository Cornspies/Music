"use client"

import { useEffect, useState } from "react"
import { AppWindow, X } from "lucide-react"
import { appLink, isDesktop } from "@/lib/desktop"
import { Button } from "@/components/ui/button"

const PREF = "music-open-links-in-app"

function landingLink(): string | null {
  if (isDesktop || typeof window === "undefined") return null
  const ua = navigator.userAgent
  if (!/Windows NT|Linux/.test(ua) || /Android|CrOS/.test(ua)) return null
  const { pathname, search, href } = window.location
  const shared = /^\/(listen|s|playlists)\/[^/]+\/?$/.test(pathname)
  const invite = /^\/register\/?$/.test(pathname) && new URLSearchParams(search).has("invite")
  return shared || invite ? appLink(href) : null
}

function launch(link: string) {
  if (!/Firefox\//.test(navigator.userAgent)) {
    window.location.href = link
    return
  }
  const frame = document.createElement("iframe")
  frame.hidden = true
  frame.src = link
  document.body.append(frame)
  window.setTimeout(() => frame.remove(), 3000)
}

function readPref(): string | null {
  try {
    return localStorage.getItem(PREF)
  } catch {
    return null
  }
}

function writePref(value: string | null) {
  try {
    if (value) localStorage.setItem(PREF, value)
    else localStorage.removeItem(PREF)
  } catch {}
}

export function OpenInAppBanner() {
  const [link] = useState(landingLink)
  const [auto] = useState(() => link !== null && readPref() === "always")
  const [state, setState] = useState<"ask" | "opened" | null>(() => {
    if (!link || readPref() === "never") return null
    return auto ? "opened" : "ask"
  })

  useEffect(() => {
    if (auto && link) launch(link)
  }, [auto, link])

  if (!state || !link) return null

  const open = () => {
    writePref("always")
    launch(link)
    setState("opened")
  }

  const keepInBrowser = () => {
    writePref(null)
    setState(null)
  }

  const dismiss = () => {
    if (state === "ask") writePref("never")
    setState(null)
  }

  return (
    <div className="pointer-events-none fixed inset-x-0 bottom-28 z-50 flex justify-center px-4">
      <div className="pointer-events-auto flex items-center gap-1 rounded-full border border-zinc-800 bg-zinc-900/95 p-1 text-sm text-zinc-200 shadow-xl shadow-black/40 backdrop-blur">
        {state === "ask" ? (
          <Button size="sm" variant="ghost" onClick={open} className="rounded-full text-zinc-100 hover:bg-zinc-800">
            <AppWindow className="size-4 text-emerald-400" />
            Open in the desktop app
          </Button>
        ) : (
          <>
            <span className="pl-3 pr-1 text-zinc-400">Opened in the desktop app</span>
            <Button size="sm" variant="ghost" onClick={keepInBrowser} className="rounded-full text-zinc-300 hover:bg-zinc-800">
              Keep links in the browser
            </Button>
          </>
        )}
        <Button
          size="icon"
          variant="ghost"
          aria-label="Dismiss"
          onClick={dismiss}
          className="size-8 rounded-full text-zinc-500 hover:bg-zinc-800 hover:text-zinc-200"
        >
          <X className="size-4" />
        </Button>
      </div>
    </div>
  )
}
