"use client"

import { useEffect } from "react"
import { pathToRoute, routeToPath } from "@/lib/routes"
import { useUIStore } from "@/stores/ui"
import { useAuthStore } from "@/stores/auth"

/**
 * Keeps the address bar and the view state in step.
 *
 *  - On mount, adopts whatever path the user landed on (deep link or refresh).
 *  - On popstate, adopts the path the browser moved to (back / forward).
 *
 * Pushing new entries happens in `navigate` (stores/ui.ts); this hook only ever
 * reads the URL, so the two can't fight over the same history entry.
 */
export function useRouteSync() {
  useEffect(() => {
    const applyRoute = useUIStore.getState().applyRoute

    const adopt = () => {
      const { view, arg } = pathToRoute(window.location.pathname)
      applyRoute(view, arg)
    }

    // Initial load. Replace rather than push so the first entry in the
    // session's history is the canonical path for this view (an unknown path
    // resolves to home, and we don't want Back to return to the dead URL).
    const { view, arg } = pathToRoute(window.location.pathname)
    applyRoute(view, arg)
    const canonical = routeToPath(view, arg)
    // /login and /register (with its ?invite=) belong to the auth screen,
    // which reads them itself and tidies the URL once signed in.
    const authPath =
      /^\/(login|register)\/?$/.test(window.location.pathname) && !useAuthStore.getState().token
    if (canonical !== window.location.pathname && !authPath) {
      window.history.replaceState({ view, viewArg: arg }, "", canonical)
    }

    window.addEventListener("popstate", adopt)
    return () => window.removeEventListener("popstate", adopt)
  }, [])
}
