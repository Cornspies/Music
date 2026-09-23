"use client"

import { useEffect, useState } from "react"
import { Download, Loader2, Smartphone } from "lucide-react"
import { appReleaseUrl, fetchAppReleases } from "@/lib/api"
import { formatFileSize, formatRelativeDate } from "@/lib/format"
import type { AppRelease } from "@/lib/types"
import { useUIStore } from "@/stores/ui"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"

export function AndroidAppDialog() {
  const open = useUIStore((s) => s.androidAppOpen)
  const setOpen = useUIStore((s) => s.setAndroidAppOpen)
  const [releases, setReleases] = useState<AppRelease[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open) return
    let alive = true
    fetchAppReleases()
      .then((r) => alive && setReleases(r))
      .catch((e: Error) => alive && setError(e.message))
    return () => {
      alive = false
    }
  }, [open])

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent aria-describedby={undefined} className="border-zinc-800 bg-zinc-950 text-zinc-100 sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-base">
            <Smartphone className="size-4 text-emerald-400" aria-hidden="true" />
            Android app
          </DialogTitle>
        </DialogHeader>

        {error ? (
          <p className="text-sm text-red-400">{error}</p>
        ) : releases === null ? (
          <div className="flex justify-center py-6">
            <Loader2 className="size-5 animate-spin text-zinc-500" />
          </div>
        ) : releases.length === 0 ? (
          <p className="text-sm text-zinc-400">No builds yet.</p>
        ) : (
          <ul className="flex flex-col gap-2">
            {releases.map((r, i) => (
              <li
                key={r.fileName}
                className="flex items-center justify-between gap-3 rounded-lg border border-zinc-800/80 bg-zinc-900/50 px-3 py-2"
              >
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium text-zinc-100">
                    Version {r.version}
                    {i === 0 ? <span className="ml-2 text-[11px] text-emerald-400">latest</span> : null}
                  </p>
                  <p className="text-xs text-zinc-500">
                    {formatFileSize(r.size)}
                    {r.modifiedAt ? ` · ${formatRelativeDate(r.modifiedAt)}` : ""}
                  </p>
                </div>
                <Button
                  asChild
                  size="sm"
                  className={
                    i === 0
                      ? "bg-emerald-500 text-zinc-950 hover:bg-emerald-400"
                      : "bg-zinc-800 text-zinc-100 hover:bg-zinc-700"
                  }
                >
                  <a href={appReleaseUrl(r.fileName)} download={r.fileName}>
                    <Download className="size-4" /> Download
                  </a>
                </Button>
              </li>
            ))}
          </ul>
        )}
      </DialogContent>
    </Dialog>
  )
}
