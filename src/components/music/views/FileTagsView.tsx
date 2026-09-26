"use client"

import { useEffect, useState } from "react"
import { Loader2, RefreshCw, Tags, Undo2 } from "lucide-react"
import { toast } from "sonner"
import { fetchLibraryFileTags, restoreLibraryTags } from "@/lib/api"
import { TAG_FIELD_LABELS } from "@/lib/format"
import type { FileTagDiff } from "@/lib/types"
import { useLibraryStore } from "@/stores/library"
import { Button } from "@/components/ui/button"
import { CoverArt } from "@/components/music/CoverArt"
import { EmptyState } from "@/components/music/EmptyState"

export function FileTagsView() {
  const [scanKey, setScanKey] = useState(0)
  const [result, setResult] = useState<{ key: number; diffs?: FileTagDiff[]; error?: string } | null>(null)
  const [excluded, setExcluded] = useState<{ key: number; ids: string[] }>({ key: -1, ids: [] })
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    let alive = true
    fetchLibraryFileTags()
      .then((diffs) => {
        if (alive) setResult({ key: scanKey, diffs })
      })
      .catch((e: Error) => {
        if (alive) setResult({ key: scanKey, error: e.message })
      })
    return () => {
      alive = false
    }
  }, [scanKey])

  const current = result?.key === scanKey ? result : null
  const diffs = current?.diffs ?? null
  const skip = excluded.key === scanKey ? excluded.ids : []
  const selected = diffs?.filter((d) => !skip.includes(d.track.id)) ?? []

  const toggle = (id: string) =>
    setExcluded({ key: scanKey, ids: skip.includes(id) ? skip.filter((x) => x !== id) : [...skip, id] })

  const selectAll = (all: boolean) =>
    setExcluded({ key: scanKey, ids: all ? [] : (diffs ?? []).map((d) => d.track.id) })

  const restore = async () => {
    if (selected.length === 0) return
    setBusy(true)
    try {
      const r = await restoreLibraryTags(selected.map((d) => d.track.id))
      toast.success(`Restored ${r.restored} track${r.restored === 1 ? "" : "s"} from their files`)
      useLibraryStore.getState().fetchAll().catch(() => {})
      setScanKey((k) => k + 1)
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="p-4 pb-16 md:p-6 md:pb-12">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-zinc-50">File tags</h1>
          {diffs && diffs.length > 0 ? (
            <p className="mt-1 text-sm text-zinc-400">
              {diffs.length} track{diffs.length === 1 ? "" : "s"} differ from their files&apos; own tags
            </p>
          ) : null}
        </div>
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            onClick={() => setScanKey((k) => k + 1)}
            disabled={diffs === null && !current?.error}
            className="border-zinc-800 bg-transparent text-zinc-300 hover:bg-zinc-800"
          >
            <RefreshCw className="size-4" /> Scan again
          </Button>
          {diffs && diffs.length > 0 ? (
            <Button
              onClick={restore}
              disabled={busy || selected.length === 0}
              className="bg-emerald-500 font-semibold text-zinc-950 hover:bg-emerald-400"
            >
              {busy ? <Loader2 className="size-4 animate-spin" /> : <Undo2 className="size-4" />}
              Restore {selected.length} from files
            </Button>
          ) : null}
        </div>
      </header>

      <div className="mt-6">
        {current?.error ? (
          <EmptyState icon={Tags} title="Couldn't read the tags" subtitle={current.error} compact />
        ) : diffs === null ? (
          <div className="flex items-center justify-center py-16 text-zinc-500">
            <Loader2 className="size-5 animate-spin" />
          </div>
        ) : diffs.length === 0 ? (
          <EmptyState icon={Tags} title="Every track matches its file's tags" compact />
        ) : (
          <>
            <div className="mb-2 flex gap-3 text-xs text-zinc-500">
              <button type="button" className="hover:text-zinc-200" onClick={() => selectAll(true)}>
                Select all
              </button>
              <button type="button" className="hover:text-zinc-200" onClick={() => selectAll(false)}>
                Select none
              </button>
            </div>
            <ul className="flex flex-col gap-2">
              {diffs.map((d) => (
                <li key={d.track.id}>
                  <label className="flex cursor-pointer items-start gap-3 rounded-xl border border-zinc-800 bg-zinc-900/40 p-3">
                    <input
                      type="checkbox"
                      checked={!skip.includes(d.track.id)}
                      onChange={() => toggle(d.track.id)}
                      className="mt-3 size-4 shrink-0 accent-emerald-500"
                    />
                    <div className="w-10 shrink-0">
                      <CoverArt trackId={d.track.id} title={d.track.title} hasCover={d.track.hasCover} />
                    </div>
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-sm font-medium text-zinc-100">{d.track.title}</p>
                      <p className="truncate text-xs text-zinc-500">{d.track.artist}</p>
                      <dl className="mt-1.5 grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-xs">
                        {d.changes.map((c) => (
                          <div key={c.field} className="contents">
                            <dt className="text-zinc-500">{TAG_FIELD_LABELS[c.field]}</dt>
                            <dd className="min-w-0 truncate">
                              <span className="text-zinc-500 line-through">{c.current || "—"}</span>
                              <span className="px-1.5 text-zinc-600">→</span>
                              <span className="text-emerald-300">{c.file}</span>
                            </dd>
                          </div>
                        ))}
                      </dl>
                    </div>
                  </label>
                </li>
              ))}
            </ul>
          </>
        )}
      </div>
    </div>
  )
}
