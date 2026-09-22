"use client"

import { useEffect, useMemo, useState } from "react"
import { CopyCheck, Loader2, RefreshCw } from "lucide-react"
import { toast } from "sonner"
import { fetchDuplicates } from "@/lib/api"
import { formatFileSize, formatRelativeDate } from "@/lib/format"
import type { DuplicateGroup, Track } from "@/lib/types"
import { useLibraryStore } from "@/stores/library"
import { Button } from "@/components/ui/button"
import { EmptyState } from "@/components/music/EmptyState"
import { TrackRow } from "@/components/music/TrackRow"

/**
 * Tracks that look like the same song — same title and artist, or same
 * title and length — grouped, oldest first, with what tells them apart
 * (format, size, where they came from, plays) under each, so the extra
 * copies can be deleted from the row menu. Groups shrink as tracks go.
 */
export function DuplicatesView() {
  // Keyed by scan so "Scan again" shows the spinner without a reset in the
  // effect: a result from an older scan is simply not the current one.
  const [scanKey, setScanKey] = useState(0)
  const [result, setResult] = useState<{ key: number; groups?: DuplicateGroup[]; error?: string } | null>(null)
  const trackIds = useLibraryStore((s) => s.tracks)

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

  // A deleted track leaves its group; a group of one is no longer a group.
  const shown = useMemo(() => {
    if (!groups) return null
    const alive = new Set(trackIds.map((t) => t.id))
    return groups
      .map((g) => ({ ...g, tracks: g.tracks.filter((t) => alive.has(t.id)) }))
      .filter((g) => g.tracks.length > 1)
  }, [groups, trackIds])

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
                      <p className="-mt-1 px-3 pb-2 text-[11px] text-zinc-500 md:pl-[3.75rem]">{describe(t)}</p>
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

/** What tells two copies apart. */
function describe(t: Track): string {
  const parts: string[] = []
  parts.push(formatLabel(t.mimeType))
  if (t.fileSize) parts.push(formatFileSize(t.fileSize))
  if (t.source === "youtube") parts.push("from YouTube")
  else if (t.source === "ncs") parts.push("from NCS")
  else parts.push("uploaded")
  if (t.uploadedBy?.username) parts.push(`by ${t.uploadedBy.username}`)
  if (t.createdAt) parts.push(formatRelativeDate(t.createdAt))
  parts.push(`${t.plays} ${t.plays === 1 ? "play" : "plays"}`)
  return parts.join(" · ")
}

function formatLabel(mime: string): string {
  const m = (mime || "").toLowerCase()
  if (m.includes("mpeg")) return "MP3"
  if (m.includes("flac")) return "FLAC"
  if (m.includes("wav")) return "WAV"
  if (m.includes("ogg")) return "OGG"
  if (m.includes("mp4") || m.includes("m4a")) return "M4A"
  if (m.includes("aac")) return "AAC"
  return m || "audio"
}
