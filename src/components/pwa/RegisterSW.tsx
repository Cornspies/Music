"use client"

import { useEffect } from "react"

export function RegisterSW() {
  useEffect(() => {
    if (typeof window === "undefined") return
    if (!("serviceWorker" in navigator)) return
    if (
      window.location.protocol !== "http:" &&
      window.location.protocol !== "https:"
    )
      return

    const register = () => {
      navigator.serviceWorker.register("/sw.js").catch(() => {})
    }

    if (document.readyState === "complete") register()
    else window.addEventListener("load", register, { once: true })

    return () => window.removeEventListener("load", register)
  }, [])

  return null
}
