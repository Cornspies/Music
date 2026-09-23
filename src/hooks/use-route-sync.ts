"use client"

import { useEffect } from "react"
import { pathToRoute, routeToPath } from "@/lib/routes"
import { useUIStore } from "@/stores/ui"
import { useAuthStore } from "@/stores/auth"

export function useRouteSync() {
  useEffect(() => {
    const applyRoute = useUIStore.getState().applyRoute

    const adopt = () => {
      const { view, arg } = pathToRoute(window.location.pathname)
      applyRoute(view, arg)
    }

    const { view, arg } = pathToRoute(window.location.pathname)
    applyRoute(view, arg)
    const canonical = routeToPath(view, arg)
    const authPath =
      /^\/(login|register)\/?$/.test(window.location.pathname) && !useAuthStore.getState().token
    if (canonical !== window.location.pathname && !authPath) {
      window.history.replaceState({ view, viewArg: arg }, "", canonical)
    }

    window.addEventListener("popstate", adopt)
    return () => window.removeEventListener("popstate", adopt)
  }, [])
}
