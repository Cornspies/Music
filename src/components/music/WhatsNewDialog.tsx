"use client"

import { useEffect, useState } from "react"
import { invoke } from "@tauri-apps/api/core"
import { Sparkles } from "lucide-react"
import { isDesktop } from "@/lib/desktop"
import { WHATS_NEW } from "@/lib/whats-new"
import { useAuthStore } from "@/stores/auth"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"

const SEEN = "music-version-seen"

function remember(version: string) {
  try {
    localStorage.setItem(SEEN, version)
  } catch {}
}

// Mounted from the start, signed in or not, so that a first start is told
// apart from an update; shown once someone is signed in.
export function WhatsNewDialog() {
  const signedIn = useAuthStore((s) => s.token !== null)
  const [version, setVersion] = useState<string | null>(null)

  useEffect(() => {
    if (!isDesktop) return
    let cancelled = false
    invoke<{ version: string }>("app_about")
      .then((about) => {
        if (cancelled) return
        let seen: string | null = null
        try {
          seen = localStorage.getItem(SEEN)
        } catch {}
        // A first start has no earlier version. An install from before this
        // note existed has none either, but is signed in already.
        const updated = seen !== null ? seen !== about.version : useAuthStore.getState().token !== null
        if (updated && WHATS_NEW[about.version]?.length) setVersion(about.version)
        else remember(about.version)
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [])

  if (version === null || !signedIn) return null
  const close = () => {
    remember(version)
    setVersion(null)
  }

  return (
    <Dialog open onOpenChange={(open) => !open && close()}>
      <DialogContent
        aria-describedby={undefined}
        className="max-w-md gap-0 border-zinc-800 bg-zinc-950 p-0 text-zinc-100 sm:max-w-md"
      >
        <DialogHeader className="border-b border-zinc-800/70 px-5 py-4">
          <DialogTitle className="flex items-center gap-2 text-base">
            <Sparkles className="size-4 text-emerald-400" aria-hidden="true" />
            What&apos;s new in Music {version}
          </DialogTitle>
        </DialogHeader>
        <ul className="flex max-h-[60vh] list-disc flex-col gap-2 overflow-y-auto py-4 pr-5 pl-9 text-sm text-zinc-300 marker:text-emerald-400">
          {WHATS_NEW[version].map((point) => (
            <li key={point}>{point}</li>
          ))}
        </ul>
        <div className="flex justify-end border-t border-zinc-800/70 px-5 py-3">
          <Button size="sm" onClick={close}>
            OK
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
