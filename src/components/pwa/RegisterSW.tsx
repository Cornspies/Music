"use client"

import { useEffect } from "react"

/**
 * Registers the Music service worker (/public/sw.js) for the installable
 * PWA + offline app shell. Best-effort: registration failures (unsupported
 * browser, insecure context, blocked SW) are silently ignored so they can
 * never break the app or Next.js dev HMR.
 */
export function RegisterSW() {
  useEffect(() => {
    if (typeof window === "undefined") return
    if (!("serviceWorker" in navigator)) return
    // Only register on http/https origins (localhost http counts as secure).
    if (
      window.location.protocol !== "http:" &&
      window.location.protocol !== "https:"
    )
      return

    const register = () => {
      navigator.serviceWorker.register("/sw.js").catch(() => {
        // SW registration is best-effort (unsupported/blocked contexts)
      })
    }

    if (document.readyState === "complete") register()
    else window.addEventListener("load", register, { once: true })

    return () => window.removeEventListener("load", register)
  }, [])

  return null
}
