"use client"

import { useEffect, useState, useSyncExternalStore } from "react"
import { AudioLines } from "lucide-react"
import { api } from "@/lib/api"
import type { User } from "@/lib/types"
import { useAuthStore } from "@/stores/auth"
import { usePlayerStore } from "@/stores/player"
import { useLibraryStore } from "@/stores/library"
import { useUIStore } from "@/stores/ui"
import { AuthView } from "@/components/music/AuthView"
import { ResetPasswordView, resetCodeFromUrl } from "@/components/music/ResetPasswordView"
import { AppShell } from "@/components/music/AppShell"
import { SharePage } from "@/components/music/SharePage"
import { OpenInAppBanner } from "@/components/music/OpenInAppBanner"
import { useDesktopLinks } from "@/hooks/use-desktop-links"
import { useRouteSync } from "@/hooks/use-route-sync"
import { isDesktop } from "@/lib/desktop"
import { rememberListenInvite } from "@/stores/listen"

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

export function Home() {
  const token = useAuthStore((s) => s.token)
  const view = useUIStore((s) => s.view)
  const viewArg = useUIStore((s) => s.viewArg)
  const [resetCode, setResetCode] = useState(resetCodeFromUrl)
  useRouteSync()
  useDesktopLinks()
  const mounted = useSyncExternalStore(
    emptySubscribe,
    () => true,
    () => false
  )

  useEffect(() => {
    if (view === "listen" && viewArg && !token) rememberListenInvite(viewArg)
  }, [view, viewArg, token])

  useEffect(() => {
    if (!mounted) return
    if (!token) {
      usePlayerStore.getState().stop()
      useLibraryStore.getState().reset()
      return
    }
    api<User>("/api/auth/me")
      .then((user) => {
        useAuthStore.setState({ user })
      })
      .catch(() => {})
  }, [mounted, token])

  if (!mounted) return <Splash />
  if (resetCode !== null) return <ResetPasswordView code={resetCode} onDone={() => setResetCode(null)} />
  const sharePage = view === "share" && viewArg && !token && !isDesktop
  const page = sharePage ? <SharePage token={viewArg} /> : token ? <AppShell /> : <AuthView />
  return (
    <>
      {page}
      <OpenInAppBanner />
    </>
  )
}
