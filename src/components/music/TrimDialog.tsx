"use client"

import { useEffect, useRef, useState } from "react"
import { Loader2, Play, RotateCcw, Scissors, Square, Wand2 } from "lucide-react"
import { toast } from "sonner"
import { cn } from "@/lib/utils"
import { fetchSilence, fetchWaveform, restoreTrack, streamUrl, trimTrack } from "@/lib/api"
import { formatDuration } from "@/lib/format"
import type { Track } from "@/lib/types"
import { useLibraryStore } from "@/stores/library"
import { usePlayerStore } from "@/stores/player"
import { useUIStore } from "@/stores/ui"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"

/**
 * Cut the start and end of a track — the seconds of silence a rip tends to
 * carry, or the applause on a live recording. The waveform shows what goes
 * (shaded) and what stays; "Detect silence" fills the cut points from the
 * server's analysis; each end can be previewed. The cut is a stream copy on
 * the server and the original is kept, so it can be undone.
 */
export function TrimDialog() {
  const track = useUIStore((s) => s.trimTrack)
  const close = () => useUIStore.getState().setTrimTrack(null)
  return (
    <Dialog open={track !== null} onOpenChange={(o) => !o && close()}>
      {track ? <TrimEditor key={track.id} track={track} onClose={close} /> : null}
    </Dialog>
  )
}

const PREVIEW_SECONDS = 5

function TrimEditor({ track, onClose }: { track: Track; onClose: () => void }) {
  const duration = track.duration
  const [start, setStart] = useState(0)
  const [end, setEnd] = useState(duration)
  const [peaks, setPeaks] = useState<number[] | null>(null)
  const [detecting, setDetecting] = useState(false)
  const [busy, setBusy] = useState(false)
  const [previewing, setPreviewing] = useState<"start" | "end" | null>(null)
  const audioRef = useRef<HTMLAudioElement | null>(null)

  useEffect(() => {
    let alive = true
    fetchWaveform(track.id).then((p) => {
      if (alive) setPeaks(p)
    })
    return () => {
      alive = false
    }
  }, [track.id])

  // One preview element, stopped when the dialog goes away.
  useEffect(() => {
    const el = new Audio()
    el.preload = "none"
    audioRef.current = el
    return () => {
      el.pause()
      el.removeAttribute("src")
    }
  }, [])

  const stopPreview = () => {
    audioRef.current?.pause()
    setPreviewing(null)
  }

  /** Plays a few seconds from the start cut, or the few seconds up to the end cut. */
  const preview = (which: "start" | "end") => {
    const el = audioRef.current
    if (!el) return
    if (previewing === which) {
      stopPreview()
      return
    }
    // The main player would fight for the audio output.
    const player = usePlayerStore.getState()
    if (player.isPlaying) player.togglePlay()
    const from = which === "start" ? start : Math.max(start, end - PREVIEW_SECONDS)
    const until = which === "start" ? Math.min(end, start + PREVIEW_SECONDS) : end
    if (!el.src) el.src = streamUrl(track)
    el.currentTime = from
    el.ontimeupdate = () => {
      if (el.currentTime >= until) {
        el.pause()
        setPreviewing(null)
      }
    }
    el.onended = () => setPreviewing(null)
    el.play()
      .then(() => setPreviewing(which))
      .catch(() => toast.error("Couldn't play the preview"))
  }

  const detect = async () => {
    setDetecting(true)
    try {
      const s = await fetchSilence(track.id)
      setStart(Math.max(0, Math.floor(s.leadingEnd * 100) / 100))
      setEnd(Math.min(duration, Math.ceil(s.trailingStart * 100) / 100))
      if (s.leadingEnd < 0.05 && s.trailingStart > duration - 0.15) toast("No silence at either end")
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setDetecting(false)
    }
  }

  const apply = async () => {
    stopPreview()
    setBusy(true)
    try {
      const updated = await trimTrack(track.id, start, end)
      toast.success(`Cut to ${formatDuration(updated.duration)}`)
      useLibraryStore.getState().fetchAll().catch(() => {})
      onClose()
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const restore = async () => {
    stopPreview()
    setBusy(true)
    try {
      const updated = await restoreTrack(track.id)
      toast.success(`Original restored (${formatDuration(updated.duration)})`)
      useLibraryStore.getState().fetchAll().catch(() => {})
      onClose()
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const removed = start + (duration - end)
  const unchanged = start < 0.05 && end > duration - 0.05
  const valid = end - start >= 1

  return (
    <DialogContent aria-describedby={undefined} className="border-zinc-800 bg-zinc-950 text-zinc-100 sm:max-w-2xl">
      <DialogHeader>
        <DialogTitle className="flex items-center gap-2 text-base">
          <Scissors className="size-4 text-emerald-400" aria-hidden="true" />
          Trim “{track.title}”
        </DialogTitle>
        <DialogDescription className="text-xs text-zinc-500">
          Drag the handles or set the times; the shaded parts are removed. This changes the file for everyone.
        </DialogDescription>
      </DialogHeader>

      {/* waveform with the cut regions shaded */}
      <div className="relative h-24 w-full overflow-hidden rounded-lg bg-zinc-900">
        {peaks ? (
          <svg viewBox={`0 0 ${peaks.length} 100`} preserveAspectRatio="none" className="absolute inset-0 size-full">
            {peaks.map((p, i) => {
              const t = (i / peaks.length) * duration
              const kept = t >= start && t <= end
              return (
                <rect
                  key={i}
                  x={i + 0.15}
                  y={50 - Math.max(2, p) / 2}
                  width={0.7}
                  height={Math.max(2, p)}
                  className={kept ? "fill-emerald-400" : "fill-zinc-700"}
                />
              )
            })}
          </svg>
        ) : (
          <div className="flex size-full items-center justify-center text-xs text-zinc-600">
            {track.hasWaveform ? <Loader2 className="size-4 animate-spin" /> : "No waveform yet"}
          </div>
        )}
        <div className="absolute inset-y-0 left-0 bg-zinc-950/60" style={{ width: `${(start / duration) * 100}%` }} />
        <div className="absolute inset-y-0 right-0 bg-zinc-950/60" style={{ width: `${((duration - end) / duration) * 100}%` }} />
        <div className="absolute inset-y-0 w-0.5 bg-white" style={{ left: `${(start / duration) * 100}%` }} />
        <div className="absolute inset-y-0 w-0.5 bg-white" style={{ left: `calc(${(end / duration) * 100}% - 2px)` }} />
      </div>

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <CutControl
          label="Start"
          value={start}
          max={Math.max(0, end - 1)}
          duration={duration}
          onChange={setStart}
          previewing={previewing === "start"}
          onPreview={() => preview("start")}
        />
        <CutControl
          label="End"
          value={end}
          min={Math.min(duration, start + 1)}
          duration={duration}
          onChange={setEnd}
          previewing={previewing === "end"}
          onPreview={() => preview("end")}
        />
      </div>

      <p className="text-xs text-zinc-500">
        Keeps {formatDuration(Math.max(0, end - start))} of {formatDuration(duration)}
        {removed > 0.05 ? ` · removes ${removed.toFixed(1)} s` : ""}
      </p>

      <DialogFooter className="gap-2 sm:justify-between">
        <div className="flex gap-2">
          <Button variant="ghost" size="sm" disabled={detecting || busy} onClick={detect} className="text-zinc-300">
            {detecting ? <Loader2 className="size-3.5 animate-spin" /> : <Wand2 className="size-3.5" />}
            Detect silence
          </Button>
          {track.trimmed && (
            <Button variant="ghost" size="sm" disabled={busy} onClick={restore} className="text-zinc-400 hover:text-zinc-100">
              <RotateCcw className="size-3.5" />
              Restore original
            </Button>
          )}
        </div>
        <div className="flex gap-2">
          <Button variant="ghost" size="sm" disabled={busy} onClick={onClose} className="text-zinc-300">
            Cancel
          </Button>
          <Button
            size="sm"
            disabled={busy || unchanged || !valid}
            onClick={apply}
            className="bg-emerald-500 text-zinc-950 hover:bg-emerald-400"
          >
            {busy ? <Loader2 className="size-3.5 animate-spin" /> : <Scissors className="size-3.5" />}
            Cut
          </Button>
        </div>
      </DialogFooter>
    </DialogContent>
  )
}

function CutControl({
  label,
  value,
  min = 0,
  max,
  duration,
  onChange,
  previewing,
  onPreview,
}: {
  label: string
  value: number
  min?: number
  max?: number
  duration: number
  onChange: (v: number) => void
  previewing: boolean
  onPreview: () => void
}) {
  const hi = max ?? duration
  const clamp = (v: number) => Math.min(hi, Math.max(min, Number.isFinite(v) ? v : min))
  return (
    <div className="rounded-lg border border-zinc-800/80 bg-zinc-900/50 p-3">
      <div className="flex items-center justify-between">
        <span className="text-xs font-medium text-zinc-300">{label}</span>
        <button
          type="button"
          onClick={onPreview}
          className={cn("inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px]", previewing ? "text-emerald-300" : "text-zinc-400 hover:text-zinc-100")}
        >
          {previewing ? <Square className="size-3" /> : <Play className="size-3" />}
          {previewing ? "Stop" : `Preview ${PREVIEW_SECONDS} s`}
        </button>
      </div>
      <input
        type="range"
        min={min}
        max={hi}
        step={0.05}
        value={value}
        onChange={(e) => onChange(clamp(parseFloat(e.target.value)))}
        aria-label={`${label} position`}
        className="mt-2 w-full accent-emerald-500"
      />
      <div className="mt-1.5 flex items-center gap-2">
        <input
          type="number"
          min={min}
          max={hi}
          step={0.1}
          value={Math.round(value * 100) / 100}
          onChange={(e) => onChange(clamp(parseFloat(e.target.value)))}
          aria-label={`${label} seconds`}
          className="w-24 rounded-md border border-zinc-800 bg-zinc-950/60 px-2 py-1 text-xs text-zinc-100 outline-none focus:border-emerald-500/60"
        />
        <span className="text-[11px] text-zinc-500">s · {formatDuration(value)}</span>
      </div>
    </div>
  )
}
