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
  position: number
  onSeek: (sec: number) => void
  className?: string
  size?: "compact" | "full" | "side"
}

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

export function LyricsPanel({ trackId, position, onSeek, className, size = "compact" }: LyricsPanelProps) {
  const full = size === "full"
  const tinted = size !== "compact"
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
    body = <p className={cn("py-10 text-center text-sm", tinted ? "text-white/50" : "text-zinc-500")}>Loading lyrics…</p>
  } else if (loaded.error) {
    body = <p className={cn("py-10 text-center text-sm", tinted ? "text-white/50" : "text-zinc-500")}>{loaded.error}</p>
  } else if (!lyrics || lyrics.source === "none") {
    body = (
      <div className="flex flex-col items-center gap-3 py-10 text-center">
        <MicVocal className={cn("size-8", tinted ? "text-white/40" : "text-zinc-600")} aria-hidden="true" />
        <p className={cn("text-sm", tinted ? "text-white/60" : "text-zinc-400")}>No lyrics found</p>
      </div>
    )
  } else if (lyrics.instrumental && !lyrics.plain) {
    body = <p className={cn("py-10 text-center text-sm", tinted ? "text-white/60" : "text-zinc-400")}>Instrumental</p>
  } else if (synced && synced.length > 0) {
    body = (
      <ol className={cn("flex flex-col", full ? "gap-2 py-[30vh]" : "gap-1 py-[35%]")}>
        {synced.map((l, i) => (
          <li key={i}>
            <button
              type="button"
              data-line={i}
              onClick={() => onSeek(l.t)}
              className={cn(
                "w-full rounded-lg text-left transition-colors duration-300",
                full
                  ? "px-2 py-1 text-2xl font-extrabold leading-tight tracking-tight md:text-4xl lg:text-5xl"
                  : size === "side"
                    ? "px-2 py-1 text-lg font-bold leading-snug lg:text-xl"
                    : "px-3 py-1.5 text-lg font-semibold leading-snug md:text-xl",
                i === current
                  ? "text-white"
                  : tinted
                    ? "text-white/45 hover:text-white/80"
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
      <p
        className={cn(
          "whitespace-pre-wrap",
          full
            ? "px-2 py-[20vh] text-2xl font-extrabold leading-snug tracking-tight text-white/90 md:text-4xl"
            : size === "side"
              ? "px-2 py-6 text-lg font-bold leading-relaxed text-white/90"
              : "py-6 text-base leading-relaxed text-zinc-200 md:text-lg"
        )}
      >
        {lyrics.plain}
      </p>
    )
  }

  return (
    <div className={cn("flex min-h-0 flex-col", className)}>
      <div
        ref={scroller}
        onScroll={onScroll}
        className={cn(
          "min-h-0 flex-1 overflow-y-auto px-2 [mask-image:linear-gradient(to_bottom,transparent,black_12%,black_88%,transparent)]",
          full && "[scrollbar-width:none]"
        )}
      >
        {body}
      </div>
      {loaded && (
        <div className={cn("flex items-center justify-between gap-2 px-3 pt-1 text-[11px]", tinted ? "text-white/50" : "text-zinc-500")}>
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

function lyricsToLRC(l: Lyrics): string {
  if (!l.synced) return l.plain
  return l.synced.map((x) => lrcStamp(x.t) + x.line).join("\n")
}

function lrcStamp(t: number): string {
  const cs = Math.round(Math.max(0, t) * 100)
  const min = Math.floor(cs / 6000)
  const sec = ((cs % 6000) / 100).toFixed(2).padStart(5, "0")
  return `[${String(min).padStart(2, "0")}:${sec}]`
}

const LRC_STAMP = /\[(\d+):(\d{1,2}(?:\.\d+)?)\]/g

function shiftLRC(text: string, delta: number): string {
  return text.replace(LRC_STAMP, (_, m: string, sec: string) => lrcStamp(Number(m) * 60 + Number(sec) + delta))
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
  const [shiftBy, setShiftBy] = useState(1)
  const [shifted, setShifted] = useState(0)
  const synced = /\[\d+:\d{1,2}(?:\.\d+)?\]/.test(text)
  const step = Number.isFinite(shiftBy) && shiftBy > 0 ? shiftBy : 0
  const shift = (delta: number) => {
    setText((t) => shiftLRC(t, delta))
    setShifted((n) => Math.round((n + delta) * 100) / 100)
  }

  const save = async () => {
    setBusy(true)
    try {
      const saved = await saveLyrics(trackId, text)
      if (saved) {
        onSaved(saved)
        toast.success("Lyrics saved")
      } else {
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
        {synced ? (
          <div className="flex flex-wrap items-center gap-2 text-xs text-zinc-400">
            <span className="text-zinc-300">Shift timing</span>
            <input
              type="number"
              min={0.1}
              step={0.1}
              value={shiftBy}
              onChange={(e) => setShiftBy(parseFloat(e.target.value))}
              aria-label="Seconds to shift by"
              className="w-16 rounded-md border border-zinc-800 bg-zinc-900/60 px-2 py-1 text-zinc-100 outline-none focus:border-emerald-500/60"
            />
            <span>s</span>
            <Button
              variant="outline"
              size="sm"
              disabled={busy || step === 0}
              onClick={() => shift(-step)}
              title={`Every line ${step} s sooner`}
              className="h-7 border-zinc-800 bg-transparent text-zinc-200 hover:bg-zinc-800"
            >
              Earlier
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={busy || step === 0}
              onClick={() => shift(step)}
              title={`Every line ${step} s later`}
              className="h-7 border-zinc-800 bg-transparent text-zinc-200 hover:bg-zinc-800"
            >
              Later
            </Button>
            {shifted !== 0 ? (
              <span className="tabular-nums text-emerald-300">
                {shifted > 0 ? "+" : ""}
                {shifted.toFixed(1)} s
              </span>
            ) : null}
          </div>
        ) : null}
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
