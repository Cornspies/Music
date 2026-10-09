"use client"

import { useEffect, useMemo, useState } from "react"
import { Check, CopyCheck, Loader2, RefreshCw } from "lucide-react"
import { toast } from "sonner"
import { fetchDuplicates } from "@/lib/api"
import { describeCopy } from "@/lib/format"
import type { DuplicateGroup, Track } from "@/lib/types"
import { useAuthStore } from "@/stores/auth"
import { useLibraryStore } from "@/stores/library"
import { Button } from "@/components/ui/button"
import { EmptyState } from "@/components/music/EmptyState"
import { TrackRow } from "@/components/music/TrackRow"

export function DuplicatesView() {
  const [scanKey, setScanKey] = useState(0)
  const [result, setResult] = useState<{ key: number; groups?: DuplicateGroup[]; error?: string } | null>(null)
  const trackIds = useLibraryStore((s) => s.tracks)
  const user = useAuthStore((s) => s.user)
  const [merging, setMerging] = useState<string | null>(null)

  useEffect(() => {
    let alive = true
    fetchDuplicates()
      .then((groups) => {
        if (alive) setResult({ key: scanKey, groups })
      })
      .catch((e: Error) => {
        if (alive) setResult({ key: scanKey, error: e.message })
      })
    return () => {
      alive = false
    }
  }, [scanKey])

  const current = result?.key === scanKey ? result : null
  const groups = current?.groups ?? null
  const error = current?.error ?? null

  const shown = useMemo(() => {
    if (!groups) return null
    const alive = new Set(trackIds.map((t) => t.id))
    return groups
      .map((g) => ({ ...g, tracks: g.tracks.filter((t) => alive.has(t.id)) }))
      .filter((g) => g.tracks.length > 1)
  }, [groups, trackIds])

  const canDelete = (t: Track) => user?.id === t.uploadedBy.id || user?.role === "admin"

  const keepOnly = async (group: DuplicateGroup, keep: Track) => {
    const others = group.tracks.filter((t) => t.id !== keep.id)
    const removable = others.filter(canDelete)
    setMerging(keep.id)
    let merged = 0
    try {
      for (const t of removable) {
        if (await useLibraryStore.getState().deleteTrack(t.id, keep.id)) merged++
      }
    } finally {
      setMerging(null)
    }
    if (merged > 0) {
      const left = others.length - merged
      toast.success(`Kept one copy of “${keep.title}”`, {
        description: left > 0 ? `${left} uploaded by someone else stayed` : "It took the other copies' places in playlists and favorites",
      })
    } else if (removable.length === 0) {
      toast.error("The other copies were uploaded by someone else")
    }
  }

  const total = shown?.reduce((n, g) => n + g.tracks.length - 1, 0) ?? 0

  return (
    <div className="p-4 pb-16 md:p-6 md:pb-12">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-zinc-50">Duplicates</h1>
          {shown && shown.length > 0 ? (
            <p className="mt-1 text-sm text-zinc-400">
              {shown.length} {shown.length === 1 ? "song" : "songs"} with extra copies · {total} {total === 1 ? "copy" : "copies"} to look at
            </p>
          ) : null}
        </div>
        <Button
          variant="outline"
          onClick={() => setScanKey((k) => k + 1)}
          disabled={groups === null && !error}
          className="border-zinc-800 bg-transparent text-zinc-300 hover:bg-zinc-800"
        >
          <RefreshCw className="size-4" /> Scan again
        </Button>
      </header>

      <div className="mt-6">
        {error ? (
          <EmptyState icon={CopyCheck} title="Couldn't scan the library" subtitle={error} compact />
        ) : shown === null ? (
          <div className="flex items-center justify-center py-16 text-zinc-500">
            <Loader2 className="size-5 animate-spin" />
          </div>
        ) : shown.length === 0 ? (
          <EmptyState icon={CopyCheck} title="No duplicates found" compact />
        ) : (
          <div className="flex flex-col gap-4">
            {shown.map((g) => (
              <section key={g.tracks.map((t) => t.id).join("+")} className="rounded-xl border border-zinc-800 bg-zinc-900/40 p-2">
                <p className="px-2 pb-1 pt-1 text-xs text-zinc-500">
                  <span className="font-medium text-zinc-300">{g.tracks[0].title}</span>
                  {g.tracks[0].artist ? ` · ${g.tracks[0].artist}` : ""} · {g.reason}
                </p>
                <ul className="flex flex-col">
                  {g.tracks.map((t) => (
                    <li key={t.id}>
                      <TrackRow track={t} contextQueue={g.tracks} />
                      <div className="-mt-1 flex items-center gap-2 px-3 pb-2 md:pl-[3.75rem]">
                        <p className="min-w-0 flex-1 truncate text-[11px] text-zinc-500">{describeCopy(t)}</p>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => keepOnly(g, t)}
                          title="Delete the other copies and put this one in their place"
                          disabled={merging !== null || !g.tracks.some((o) => o.id !== t.id && canDelete(o))}
                          className="h-7 shrink-0 text-xs text-zinc-400 hover:bg-emerald-500/10 hover:text-emerald-300"
                        >
                          {merging === t.id ? <Loader2 className="size-3.5 animate-spin" /> : <Check className="size-3.5" />}
                          Keep this one
                        </Button>
                      </div>
                    </li>
                  ))}
                </ul>
              </section>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
