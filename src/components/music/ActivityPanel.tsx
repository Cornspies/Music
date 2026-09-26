"use client"

import { useEffect, useState } from "react"
import { toast } from "sonner"
import { Pause, Play } from "lucide-react"
import { cn } from "@/lib/utils"
import { admin } from "@/lib/api"
import { formatFileSize, formatRelativeDate } from "@/lib/format"
import type { ActivityStatus, SourceHealth } from "@/lib/types"
import { MetadataQueueLine, useMetadataQueue } from "@/components/music/MetadataQueueStatus"
import { Button } from "@/components/ui/button"
import { Progress } from "@/components/ui/progress"

function Row({ title, children, action }: { title: string; children: React.ReactNode; action?: React.ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-4 px-4 py-3">
      <div className="min-w-0 flex-1">
        <p className="text-xs font-medium tracking-wide text-zinc-500 uppercase">{title}</p>
        <div className="mt-1 text-sm text-zinc-300">{children}</div>
      </div>
      {action ? <div className="shrink-0">{action}</div> : null}
    </div>
  )
}

function useActivity() {
  const [status, setStatus] = useState<ActivityStatus | null>(null)
  useEffect(() => {
    let alive = true
    const tick = () =>
      admin
        .activity()
        .then((s) => {
          if (alive) setStatus(s)
        })
        .catch(() => {})
    tick()
    const t = setInterval(tick, 5000)
    return () => {
      alive = false
      clearInterval(t)
    }
  }, [])
  return status
}

function SourceLine({ source }: { source: SourceHealth }) {
  const failing = Boolean(source.failingSince)
  return (
    <li className="flex min-w-0 items-start gap-2">
      <span
        aria-hidden="true"
        className={cn(
          "mt-1.5 size-2 shrink-0 rounded-full",
          failing ? "bg-red-400" : source.lastOk ? "bg-emerald-400" : "bg-zinc-600"
        )}
      />
      <span className="min-w-0">
        <span className="text-zinc-200">{source.name}</span>
        <span className="text-zinc-500">
          {failing
            ? ` · failing since ${formatRelativeDate(source.failingSince!)}`
            : source.lastOk
              ? ` · worked ${formatRelativeDate(source.lastOk)}`
              : " · not used since the server started"}
        </span>
        {failing && source.lastError ? (
          <span className="block truncate text-xs text-red-300/80">{source.lastError}</span>
        ) : null}
      </span>
    </li>
  )
}

export function ActivityPanel() {
  const { status: queue, refresh } = useMetadataQueue()
  const activity = useActivity()
  const [busy, setBusy] = useState(false)

  const setPaused = async (paused: boolean) => {
    setBusy(true)
    try {
      await admin.updateSettings({ metadataLookupPaused: paused })
      await refresh()
      toast.success(paused ? "Metadata lookups paused" : "Metadata lookups resumed")
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const silence = activity?.silence
  const checked = silence ? silence.total - silence.pending : 0
  const trimFailures = activity?.errors.find((e) => e.context === "trim silence")?.count ?? 0
  const errorTotal = activity?.errors.reduce((n, e) => n + e.count, 0) ?? 0
  const activeImports = activity?.imports.filter((i) => ["queued", "listing", "running"].includes(i.state)) ?? []
  const disk = activity?.disk

  return (
    <section className="mt-6">
      <h2 className="mb-3 text-sm font-semibold text-zinc-300">Background activity</h2>
      <div className="divide-y divide-zinc-800/80 overflow-hidden rounded-xl border border-zinc-800/80 bg-zinc-900/50">
        <Row
          title="Metadata lookups"
          action={
            queue && !queue.disabled ? (
              <Button
                size="sm"
                variant="outline"
                disabled={busy}
                onClick={() => setPaused(!queue.paused)}
                className="h-8 border-zinc-700 bg-zinc-800/60 px-3 text-xs text-zinc-200 hover:bg-zinc-800"
              >
                {queue.paused ? (
                  <Play className="mr-1.5 size-3.5" aria-hidden="true" />
                ) : (
                  <Pause className="mr-1.5 size-3.5" aria-hidden="true" />
                )}
                {queue.paused ? "Resume" : "Pause"}
              </Button>
            ) : null
          }
        >
          <MetadataQueueLine status={queue} showIdle />
        </Row>

        {!activity ? (
          <p className="px-4 py-3 text-sm text-zinc-500">Loading…</p>
        ) : (
          <>
            <Row title={`Silence check · ${silence!.rule}`}>
              {!silence!.enabled ? (
                <span className="text-zinc-500">Off on this server</span>
              ) : silence!.pending === 0 ? (
                <span>All {silence!.total} tracks checked</span>
              ) : (
                <div className="flex flex-col gap-1.5">
                  <span>
                    {checked} of {silence!.total} checked · {silence!.pending} waiting
                  </span>
                  <Progress
                    value={silence!.total > 0 ? (checked / silence!.total) * 100 : 0}
                    className="h-1.5 bg-zinc-800 [&_[data-slot=progress-indicator]]:bg-emerald-500"
                  />
                </div>
              )}
              {silence!.recentCuts.length > 0 ? (
                <details className="mt-2 text-xs text-zinc-400">
                  <summary className="cursor-pointer text-zinc-500 hover:text-zinc-300">
                    Recent cuts ({silence!.recentCuts.length})
                  </summary>
                  <ul className="mt-1.5 flex flex-col gap-1">
                    {silence!.recentCuts.map((c) => (
                      <li key={c.at + c.trackId} className="truncate">
                        <span className="text-zinc-200">{c.title || c.trackId}</span> · {c.fromStart.toFixed(1)} s at the
                        start, {c.fromEnd.toFixed(1)} s at the end · {formatRelativeDate(c.at)}
                      </li>
                    ))}
                  </ul>
                </details>
              ) : null}
              {trimFailures > 0 ? (
                <p className="mt-2 text-xs text-red-300/80">
                  {trimFailures} failed since the server started — see the errors below
                </p>
              ) : null}
            </Row>

            <Row title="Analysis">
              {!activity.analysis.enabled ? (
                <span className="text-zinc-500">Off on this server</span>
              ) : activity.analysis.current ? (
                <span>
                  Analysing “{activity.analysis.current.title}”
                  {activity.analysis.waiting > 1 ? ` · ${activity.analysis.waiting - 1} more waiting` : ""}
                </span>
              ) : activity.analysis.waiting > 0 ? (
                <span>{activity.analysis.waiting} waiting</span>
              ) : (
                <span>Up to date</span>
              )}
            </Row>

            <Row title="Imports">
              {activeImports.length === 0 ? (
                <span className="text-zinc-500">None running</span>
              ) : (
                <ul className="flex flex-col gap-1">
                  {activeImports.map((i) => (
                    <li key={i.id} className="truncate">
                      “{i.title || i.listId}” · {i.state === "running" ? `${i.done} of ${i.total}` : i.state}
                      {i.current ? <span className="text-zinc-500"> · {i.current}</span> : null}
                    </li>
                  ))}
                </ul>
              )}
            </Row>

            <Row title="Discover sources">
              {activity.sources.length === 0 ? (
                <span className="text-zinc-500">None configured</span>
              ) : (
                <ul className="flex flex-col gap-1">
                  {activity.sources.map((s) => (
                    <SourceLine key={s.key} source={s} />
                  ))}
                </ul>
              )}
              {activity.ytdlp ? (
                <p className="mt-2 text-xs text-zinc-500">
                  yt-dlp {activity.ytdlp.version} · JS runtime: {activity.ytdlp.jsRuntime}
                </p>
              ) : null}
            </Row>

            <Row title="Disk">
              <span>
                Music {formatFileSize(disk!.libraryBytes)} · database {formatFileSize(disk!.databaseBytes)}
                {disk!.totalBytes > 0
                  ? ` · ${formatFileSize(disk!.freeBytes)} free of ${formatFileSize(disk!.totalBytes)}`
                  : ""}
              </span>
            </Row>

            <Row title="Recent errors">
              {activity.errors.length === 0 ? (
                <span className="text-zinc-500">None since the server started</span>
              ) : (
                <details className="text-xs text-zinc-400">
                  <summary className="cursor-pointer text-sm text-zinc-300 hover:text-zinc-100">
                    {errorTotal} error{errorTotal === 1 ? "" : "s"} · latest {formatRelativeDate(activity.errors[0].at)}
                  </summary>
                  <ul className="mt-2 flex flex-col gap-1.5">
                    {activity.errors.map((e, i) => (
                      <li key={e.at + i} className="break-words">
                        <span className="text-zinc-500">
                          {formatRelativeDate(e.at)} · {e.context}
                          {e.count > 1 ? ` ×${e.count}` : ""}:
                        </span>{" "}
                        <span className="text-zinc-300">{e.message}</span>
                      </li>
                    ))}
                  </ul>
                </details>
              )}
            </Row>
          </>
        )}
      </div>
    </section>
  )
}
