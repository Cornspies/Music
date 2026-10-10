"use client"

import { useCallback, useEffect, useState } from "react"
import { toast } from "sonner"
import { ArrowRight, ExternalLink, Loader2, Sparkles, X } from "lucide-react"
import { admin } from "@/lib/api"
import { formatFileSize, formatRelativeDate, formatSizeChange, storedBytes } from "@/lib/format"
import type { AudioQuality, UpgradeCandidate, UpgradeStatus } from "@/lib/types"
import { useLibraryStore } from "@/stores/library"
import { CoverArt } from "@/components/music/CoverArt"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"

const CODECS: Record<string, string> = {
  mp3: "MP3",
  aac: "AAC",
  opus: "Opus",
  vorbis: "Vorbis",
  flac: "FLAC",
  wav: "WAV",
}

function sizeChange(c: UpgradeCandidate): number {
  return c.estimatedBytes - storedBytes(c.track)
}

function quality(q: AudioQuality): string {
  const codec = CODECS[q.codec] ?? q.codec.toUpperCase()
  return q.kbps > 0 ? `${codec} ${q.kbps} kbps` : codec
}

export function UpgradesSection() {
  const [status, setStatus] = useState<UpgradeStatus | null>(null)
  const [source, setSource] = useState("")
  const [busy, setBusy] = useState<Set<string>>(new Set())
  const [starting, setStarting] = useState(false)

  const refresh = useCallback(
    () =>
      admin
        .upgrades()
        .then((st) => {
          setStatus(st)
          setSource((cur) => cur || st.source || st.sources[0]?.key || "")
        })
        .catch((e: Error) => toast.error(e.message)),
    []
  )

  useEffect(() => {
    refresh()
  }, [refresh])

  useEffect(() => {
    if (!status?.running) return
    const t = window.setInterval(refresh, 2000)
    return () => window.clearInterval(t)
  }, [status?.running, refresh])

  if (!status || status.sources.length === 0) return null

  const scan = async () => {
    setStarting(true)
    try {
      setStatus(await admin.scanUpgrades(source))
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setStarting(false)
    }
  }

  const setBusyFor = (id: string, on: boolean) =>
    setBusy((prev) => {
      const next = new Set(prev)
      if (on) next.add(id)
      else next.delete(id)
      return next
    })

  const drop = (id: string) =>
    setStatus((st) => (st ? { ...st, candidates: st.candidates.filter((c) => c.track.id !== id) } : st))

  const replace = async (c: UpgradeCandidate) => {
    setBusyFor(c.track.id, true)
    try {
      await admin.applyUpgrade(c.track.id)
      drop(c.track.id)
      toast.success(`Replaced “${c.track.title}”`, { description: `${quality(c.current)} → ${quality(c.quality)}` })
      useLibraryStore.getState().fetchAll().catch(() => {})
    } catch (e) {
      toast.error(`“${c.track.title}”: ${(e as Error).message}`)
    } finally {
      setBusyFor(c.track.id, false)
    }
  }

  const replaceAll = async () => {
    for (const c of status.candidates) {
      if (!busy.has(c.track.id)) await replace(c)
    }
  }

  const dismiss = (c: UpgradeCandidate) => {
    drop(c.track.id)
    admin.dismissUpgrade(c.track.id).catch(() => {})
  }

  const sourceName = (key: string) => status.sources.find((s) => s.key === key)?.name ?? key
  const found = status.candidates.length
  const totalChange = status.candidates.reduce((sum, c) => sum + sizeChange(c), 0)

  return (
    <section className="mt-8">
      <h2 className="mb-3 flex items-center gap-2 text-sm font-semibold text-zinc-300">
        <Sparkles className="size-4" aria-hidden="true" />
        Better quality {found > 0 ? <span className="text-zinc-600">· {found}</span> : null}
      </h2>

      <div className="flex flex-wrap items-center gap-2">
        <Select value={source} onValueChange={setSource} disabled={status.running}>
          <SelectTrigger aria-label="Source to search" className="h-9 w-40 border-zinc-700/80 bg-zinc-900/60 text-sm">
            <SelectValue placeholder="Source" />
          </SelectTrigger>
          <SelectContent>
            {status.sources.map((s) => (
              <SelectItem key={s.key} value={s.key}>
                {s.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {status.running ? (
          <Button
            variant="ghost"
            onClick={() => admin.cancelUpgrades().then(refresh).catch((e: Error) => toast.error(e.message))}
            className="h-9 text-zinc-300 hover:bg-zinc-800"
          >
            <X className="size-4" /> Stop
          </Button>
        ) : (
          <Button
            onClick={scan}
            disabled={!source || starting}
            className="h-9 bg-emerald-500 font-semibold text-zinc-950 hover:bg-emerald-400"
          >
            {starting ? <Loader2 className="size-4 animate-spin" /> : null} Scan library
          </Button>
        )}
        <span className="text-xs text-zinc-500">
          {status.running ? (
            <span className="inline-flex items-center gap-1.5">
              <Loader2 className="size-3 animate-spin" aria-hidden="true" />
              {sourceName(status.source)}: {status.done} of {status.total} checked
            </span>
          ) : status.finishedAt ? (
            `${sourceName(status.source)} · ${formatRelativeDate(status.finishedAt)} · ${status.done} checked, ${status.skipped} already as good`
          ) : null}
        </span>
      </div>

      {status.error ? <p className="mt-2 text-xs text-red-400">{status.error}</p> : null}

      {found > 0 ? (
        <>
          {found > 1 ? (
            <div className="mt-3 flex justify-end">
              <Button
                variant="ghost"
                size="sm"
                onClick={replaceAll}
                disabled={busy.size > 0}
                className="h-8 text-xs text-emerald-400 hover:bg-emerald-500/10 hover:text-emerald-300"
              >
                Replace all {found} · ≈ {formatSizeChange(totalChange)}
              </Button>
            </div>
          ) : null}
          <div role="list" className="mt-2 overflow-hidden rounded-xl border border-zinc-800/80">
            {status.candidates.map((c) => (
              <div
                key={c.track.id}
                role="listitem"
                className="flex items-center gap-3 border-b border-zinc-800/60 bg-zinc-900/30 px-3 py-2.5 last:border-b-0"
              >
                <span className="w-9 shrink-0">
                  <CoverArt trackId={c.track.id} title={c.track.title} hasCover={c.track.hasCover} />
                </span>
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium text-zinc-100">
                    {c.track.title}
                    <span className="font-normal text-zinc-500"> · {c.track.artist || "Unknown artist"}</span>
                  </p>
                  <p className="flex min-w-0 items-center gap-1.5 text-xs text-zinc-500">
                    <span className="shrink-0">{quality(c.current)}</span>
                    <ArrowRight className="size-3 shrink-0" aria-hidden="true" />
                    <span className="shrink-0 text-emerald-400">{quality(c.quality)}</span>
                    <span
                      className="shrink-0"
                      title={`Storage now ${formatFileSize(storedBytes(c.track))}, about ${formatFileSize(c.estimatedBytes)} after, iOS copy included`}
                    >
                      · ≈ {formatSizeChange(sizeChange(c))}
                    </span>
                    {c.found.pageUrl ? (
                      <a
                        href={c.found.pageUrl}
                        target="_blank"
                        rel="noreferrer"
                        className="inline-flex min-w-0 items-center gap-1 truncate hover:text-zinc-200"
                        title={c.found.title}
                      >
                        <span className="truncate">· {c.found.title}</span>
                        <ExternalLink className="size-3 shrink-0" aria-hidden="true" />
                      </a>
                    ) : null}
                  </p>
                </div>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => dismiss(c)}
                  disabled={busy.has(c.track.id)}
                  className="h-8 shrink-0 text-xs text-zinc-500 hover:text-zinc-200"
                >
                  Dismiss
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => replace(c)}
                  disabled={busy.has(c.track.id)}
                  className="h-8 shrink-0 border-emerald-700/60 bg-emerald-600/10 text-xs text-emerald-300 hover:bg-emerald-600/20 hover:text-emerald-200"
                >
                  {busy.has(c.track.id) ? <Loader2 className="size-3.5 animate-spin" /> : null} Replace
                </Button>
              </div>
            ))}
          </div>
        </>
      ) : status.finishedAt && !status.running && !status.error ? (
        <p className="mt-3 text-xs text-zinc-600">Nothing better found.</p>
      ) : null}
    </section>
  )
}
