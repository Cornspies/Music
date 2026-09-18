"use client"

import { useEffect, useSyncExternalStore } from "react"
import { AudioLines } from "lucide-react"
import { api } from "@/lib/api"
import type { User } from "@/lib/types"
import { useAuthStore } from "@/stores/auth"
import { usePlayerStore } from "@/stores/player"
import { useLibraryStore } from "@/stores/library"
import { AuthView } from "@/components/music/AuthView"
import { AppShell } from "@/components/music/AppShell"
import { useRouteSync } from "@/hooks/use-route-sync"

const emptySubscribe = () => () => {}

function Splash() {
  return (
    <div className="flex h-dvh items-center justify-center bg-zinc-950">
      <div className="flex flex-col items-center gap-4">
        <div className="flex size-16 items-center justify-center rounded-2xl bg-linear-to-br from-emerald-400 to-teal-600 shadow-xl shadow-emerald-900/40">
          <AudioLines className="size-8 animate-pulse text-zinc-950" aria-hidden="true" />
        </div>
        <p className="text-sm font-medium tracking-wide text-zinc-500">Music</p>
      </div>
    </div>
  )
}

export default function Home() {
  const token = useAuthStore((s) => s.token)
  // Adopt the current URL and follow browser back/forward. Runs before the
  // token check so a deep link survives the login round-trip.
  useRouteSync()
  // hydration-safe "mounted" flag (true on client after hydration, false during SSR)
  const mounted = useSyncExternalStore(
    emptySubscribe,
    () => true,
    () => false
  )

  // Validate the persisted token on mount; reset app state when logged out.
  useEffect(() => {
    if (!token) {
      usePlayerStore.getState().stop()
      useLibraryStore.getState().reset()
      return
    }
    api<User>("/api/auth/me")
      .then((user) => {
        useAuthStore.setState({ user })
      })
      .catch(() => {
        // 401 already logs out via api(); other errors keep the session
      })
  }, [token])

  if (!mounted) return <Splash />
  return token ? <AppShell /> : <AuthView />
}
