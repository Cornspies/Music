"use client"

import { useEffect, useRef, useState } from "react"
import { MicVocal, Pencil } from "lucide-react"
import { toast } from "sonner"
import { cn } from "@/lib/utils"
import { fetchLyrics, saveLyrics } from "@/lib/api"
import type { Lyrics } from "@/lib/types"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"

interface LyricsPanelProps {
  trackId: string
  /** Current playback position in seconds; drives the highlighted line. */
  position: number
  onSeek: (sec: number) => void
  className?: string
}

/** A line is "current" from its own time until the next one starts. */
function activeIndex(lines: { t: number }[], position: number): number {
  let idx = -1
  for (let i = 0; i < lines.length; i++) {
    if (lines[i].t <= position + 0.15) idx = i
    else break
  }
  return idx
}

const SOURCE_LABEL: Record<Lyrics["source"], string> = {
  embedded: "From the file",
  lrclib: "From LRCLIB",
  manual: "Added by hand",
  none: "",
}

/**
 * Lyrics for the current track. Synced lyrics follow playback — the current
 * line is highlighted and kept in view, and clicking a line seeks to it;
 * plain lyrics are just text to read. The panel scrolls, the page doesn't.
 */
export function LyricsPanel({ trackId, position, onSeek, className }: LyricsPanelProps) {
  // Keyed on the track so a stale result never shows under a new title.
  const [state, setState] = useState<{ id: string; lyrics: Lyrics | null; error: string | null } | null>(null)
  const [editOpen, setEditOpen] = useState(false)
  const loaded = state?.id === trackId ? state : null

  useEffect(() => {
    let alive = true
    fetchLyrics(trackId)
      .then((l) => {
        if (alive) setState({ id: trackId, lyrics: l, error: null })
      })
      .catch((e: Error) => {
        if (alive) setState({ id: trackId, lyrics: null, error: e.message })
      })
    return () => {
      alive = false
    }
  }, [trackId])

  const lyrics = loaded?.lyrics ?? null
  const synced = lyrics?.synced ?? null
  const current = synced ? activeIndex(synced, position) : -1

  // Follow playback, but not while the reader is scrolling on their own:
  // a manual scroll pauses the auto-follow for a few seconds.
  const scroller = useRef<HTMLDivElement>(null)
  const lastUserScroll = useRef(0)
  const programmatic = useRef(false)
  useEffect(() => {
    if (current < 0 || !scroller.current) return
    if (Date.now() - lastUserScroll.current < 4000) return
    const el = scroller.current.querySelector<HTMLElement>(`[data-line="${current}"]`)
    if (!el) return
    programmatic.current = true
    el.scrollIntoView({ block: "center", behavior: "smooth" })
    // scrollIntoView fires scroll events for a while; ignore those.
    const t = setTimeout(() => {
      programmatic.current = false
    }, 600)
    return () => clearTimeout(t)
  }, [current])

  const onScroll = () => {
    if (!programmatic.current) lastUserScroll.current = Date.now()
  }

  let body: React.ReactNode
  if (!loaded) {
    body = <p className="py-10 text-center text-sm text-zinc-500">Loading lyrics…</p>
  } else if (loaded.error) {
    body = <p className="py-10 text-center text-sm text-zinc-500">{loaded.error}</p>
  } else if (!lyrics || lyrics.source === "none") {
    body = (
      <div className="flex flex-col items-center gap-3 py-10 text-center">
        <MicVocal className="size-8 text-zinc-600" aria-hidden="true" />
        <p className="text-sm text-zinc-400">No lyrics found</p>
      </div>
    )
  } else if (lyrics.instrumental && !lyrics.plain) {
    body = <p className="py-10 text-center text-sm text-zinc-400">Instrumental</p>
  } else if (synced && synced.length > 0) {
    body = (
      <ol className="flex flex-col gap-1 py-[35%]">
        {synced.map((l, i) => (
          <li key={i}>
            <button
              type="button"
              data-line={i}
              onClick={() => onSeek(l.t)}
              className={cn(
                "w-full rounded-lg px-3 py-1.5 text-left text-lg font-semibold leading-snug transition-colors duration-300 md:text-xl",
                i === current
                  ? "text-white"
                  : i < current
                    ? "text-zinc-500 hover:text-zinc-300"
                    : "text-zinc-400 hover:text-zinc-200"
              )}
            >
              {l.line || "♪"}
            </button>
          </li>
        ))}
      </ol>
    )
  } else {
    body = (
      <p className="py-6 text-base leading-relaxed whitespace-pre-wrap text-zinc-200 md:text-lg">{lyrics.plain}</p>
    )
  }

  return (
    <div className={cn("flex min-h-0 flex-col", className)}>
      <div
        ref={scroller}
        onScroll={onScroll}
        className="min-h-0 flex-1 overflow-y-auto px-2 [mask-image:linear-gradient(to_bottom,transparent,black_12%,black_88%,transparent)]"
      >
        {body}
      </div>
      {loaded && (
        <div className="flex items-center justify-between gap-2 px-3 pt-1 text-[11px] text-zinc-500">
          <span>{lyrics && lyrics.source !== "none" ? SOURCE_LABEL[lyrics.source] : ""}</span>
          {lyrics?.editable && (
            <button
              type="button"
              onClick={() => setEditOpen(true)}
              className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 hover:text-zinc-200"
            >
              <Pencil className="size-3" aria-hidden="true" />
              {lyrics.source === "none" ? "Add lyrics" : "Edit"}
            </button>
          )}
        </div>
      )}
      {editOpen && lyrics && (
        <LyricsEditDialog
          trackId={trackId}
          initial={lyrics.synced ? lyricsToLRC(lyrics) : lyrics.plain}
          onClose={() => setEditOpen(false)}
          onSaved={(l) => setState({ id: trackId, lyrics: l, error: null })}
        />
      )}
    </div>
  )
}

/** Synced lyrics back into LRC so they can be edited with their timing. */
function lyricsToLRC(l: Lyrics): string {
  if (!l.synced) return l.plain
  return l.synced
    .map((x) => {
      const m = Math.floor(x.t / 60)
      const s = (x.t - m * 60).toFixed(2).padStart(5, "0")
      return `[${String(m).padStart(2, "0")}:${s}]${x.line}`
    })
    .join("\n")
}

function LyricsEditDialog({
  trackId,
  initial,
  onClose,
  onSaved,
}: {
  trackId: string
  initial: string
  onClose: () => void
  onSaved: (l: Lyrics) => void
}) {
  const [text, setText] = useState(initial)
  const [busy, setBusy] = useState(false)

  const save = async () => {
    setBusy(true)
    try {
      const saved = await saveLyrics(trackId, text)
      if (saved) {
        onSaved(saved)
        toast.success("Lyrics saved")
      } else {
        // Cleared: the next open looks them up again.
        onSaved({ plain: "", synced: null, source: "none", instrumental: false, editable: true })
        toast.success("Lyrics removed")
      }
      onClose()
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open onOpenChange={(o) => !o && !busy && onClose()}>
      <DialogContent className="border-zinc-800 bg-zinc-950 text-zinc-100 sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="text-base">Edit lyrics</DialogTitle>
          <DialogDescription className="text-xs text-zinc-500">
            Lines starting with [mm:ss.xx] are synced to playback.
          </DialogDescription>
        </DialogHeader>
        <textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          spellCheck={false}
          className="h-72 w-full resize-y rounded-md border border-zinc-800 bg-zinc-900/60 p-3 font-mono text-sm text-zinc-100 outline-none focus:border-emerald-500/60"
        />
        <DialogFooter className="gap-2 sm:justify-between">
          <Button
            variant="ghost"
            disabled={busy || initial === ""}
            onClick={() => {
              setText("")
              void saveLyrics(trackId, "").then(() => {
                onSaved({ plain: "", synced: null, source: "none", instrumental: false, editable: true })
                onClose()
              })
            }}
            className="text-zinc-400 hover:text-red-300"
          >
            Remove
          </Button>
          <div className="flex gap-2">
            <Button variant="ghost" disabled={busy} onClick={onClose} className="text-zinc-300">
              Cancel
            </Button>
            <Button disabled={busy} onClick={save} className="bg-emerald-500 text-zinc-950 hover:bg-emerald-400">
              Save
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
