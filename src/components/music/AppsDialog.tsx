"use client"

import { useEffect, useState } from "react"
import { Download, Loader2, MonitorSmartphone } from "lucide-react"
import { appReleaseUrl, fetchAppReleases, type ReleasePlatform } from "@/lib/api"
import { isDesktop } from "@/lib/desktop"
import { formatFileSize, formatRelativeDate } from "@/lib/format"
import type { AppRelease } from "@/lib/types"
import { cn } from "@/lib/utils"
import { useUIStore } from "@/stores/ui"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"

type Tab = "android" | "windows" | "linux"
type LinuxFormat = "appimage" | "deb" | "rpm"

const TABS: { key: Tab; label: string }[] = [
  { key: "android", label: "Android" },
  { key: "windows", label: "Windows" },
  { key: "linux", label: "Linux" },
]

const LINUX_FORMATS: { key: LinuxFormat; label: string }[] = [
  { key: "appimage", label: "AppImage" },
  { key: "deb", label: ".deb" },
  { key: "rpm", label: ".rpm" },
]

interface Result {
  releases: AppRelease[] | null
  error: string | null
}

function detectTab(): Tab {
  const ua = navigator.userAgent
  if (/Android/i.test(ua)) return "android"
  if (/Windows/i.test(ua)) return "windows"
  if (/Linux|X11/i.test(ua)) return "linux"
  return "android"
}

export function AppsDialog() {
  const open = useUIStore((s) => s.appsOpen)
  const setOpen = useUIStore((s) => s.setAppsOpen)
  const [tab, setTab] = useState<Tab>(detectTab)
  const [linuxFormat, setLinuxFormat] = useState<LinuxFormat>("appimage")
  const [results, setResults] = useState<Partial<Record<ReleasePlatform, Result>>>({})
  const platform: ReleasePlatform = tab === "linux" ? linuxFormat : tab

  useEffect(() => {
    if (!open) return
    let alive = true
    fetchAppReleases(platform)
      .then((releases) => alive && setResults((r) => ({ ...r, [platform]: { releases, error: null } })))
      .catch((e: Error) => alive && setResults((r) => ({ ...r, [platform]: { releases: null, error: e.message } })))
    return () => {
      alive = false
    }
  }, [open, platform])

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent aria-describedby={undefined} className="border-zinc-800 bg-zinc-950 text-zinc-100 sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-base">
            <MonitorSmartphone className="size-4 text-emerald-400" aria-hidden="true" />
            Other platforms
          </DialogTitle>
        </DialogHeader>

        <div role="tablist" aria-label="Platform" className="flex gap-1 rounded-lg bg-zinc-900/70 p-1">
          {TABS.map((t) => (
            <button
              key={t.key}
              id={`apps-tab-${t.key}`}
              role="tab"
              type="button"
              aria-selected={tab === t.key}
              aria-controls="apps-panel"
              onClick={() => setTab(t.key)}
              className={cn(
                "flex-1 rounded-md px-2.5 py-1.5 text-xs font-medium transition-colors",
                tab === t.key ? "bg-zinc-800 text-emerald-300" : "text-zinc-400 hover:text-zinc-100"
              )}
            >
              {t.label}
            </button>
          ))}
        </div>

        <div id="apps-panel" role="tabpanel" aria-labelledby={`apps-tab-${tab}`} className="flex min-h-28 flex-col gap-3">
          {tab === "linux" ? (
            <div aria-label="Package format" className="flex gap-1.5">
              {LINUX_FORMATS.map((f) => (
                <button
                  key={f.key}
                  type="button"
                  aria-pressed={linuxFormat === f.key}
                  onClick={() => setLinuxFormat(f.key)}
                  className={cn(
                    "rounded-full border px-2.5 py-0.5 text-[11px] font-medium transition-colors",
                    linuxFormat === f.key
                      ? "border-emerald-500/50 bg-emerald-500/10 text-emerald-300"
                      : "border-zinc-800 text-zinc-400 hover:border-zinc-700 hover:text-zinc-100"
                  )}
                >
                  {f.label}
                </button>
              ))}
            </div>
          ) : null}
          <ReleaseList result={results[platform]} />
        </div>
      </DialogContent>
    </Dialog>
  )
}

function ReleaseList({ result }: { result: Result | undefined }) {
  if (!result) {
    return (
      <div className="flex justify-center py-6">
        <Loader2 className="size-5 animate-spin text-zinc-500" />
      </div>
    )
  }
  if (result.error) return <p className="text-sm text-red-400">{result.error}</p>
  if (!result.releases || result.releases.length === 0) {
    return <p className="py-6 text-center text-sm text-zinc-400">No builds yet.</p>
  }
  return (
    <ul className="flex flex-col gap-2">
      {result.releases.map((r, i) => (
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
            <a
              href={appReleaseUrl(r.fileName)}
              download={r.fileName}
              target={isDesktop ? "_blank" : undefined}
              rel={isDesktop ? "noreferrer" : undefined}
            >
              <Download className="size-4" /> Download
            </a>
          </Button>
        </li>
      ))}
    </ul>
  )
}
