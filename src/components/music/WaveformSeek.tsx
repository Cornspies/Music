"use client"

import { useEffect, useId, useRef, useState } from "react"
import { cn } from "@/lib/utils"
import { fetchWaveform } from "@/lib/api"
import type { Track } from "@/lib/types"

interface WaveformSeekProps {
  track: Track
  position: number
  duration: number
  onPreview: (seconds: number | null) => void
  onSeek: (seconds: number) => void
  disabled?: boolean
  className?: string
  /** bar height in px */
  height?: number
  /** rendered when the track has no waveform yet */
  fallback: React.ReactNode
}

/**
 * Waveform seek bar: the server's 200 peaks drawn as bars, the played part
 * tinted. Click or drag anywhere to seek. When a track hasn't been analysed
 * (no ffmpeg on the server, or not yet) it renders `fallback` — the plain
 * slider — so the layout never jumps.
 */
export function WaveformSeek({
  track,
  position,
  duration,
  onPreview,
  onSeek,
  disabled,
  className,
  height = 28,
  fallback,
}: WaveformSeekProps) {
  // Peaks are remembered together with the version of the track they
  // belong to, so a track change — or the same track re-analysed after a
  // trim — simply stops matching rather than needing a reset in the effect.
  const version = `${track.id}:${track.updatedAt}`
  const [loaded, setLoaded] = useState<{ version: string; peaks: number[] | null } | null>(null)
  const [dragging, setDragging] = useState<number | null>(null)
  const ref = useRef<HTMLDivElement>(null)
  // The player bar and the full-screen view can both show the same track.
  const clipId = useId()

  useEffect(() => {
    if (!track.hasWaveform) return
    let alive = true
    fetchWaveform(track.id, track.updatedAt).then((p) => {
      if (alive) setLoaded({ version, peaks: p })
    })
    return () => {
      alive = false
    }
  }, [track.id, track.updatedAt, track.hasWaveform, version])

  // While a re-analysis is pending the track has no waveform, and the old
  // one no longer matches the audio — the plain slider stands in.
  const peaks = track.hasWaveform && loaded?.version === version ? loaded.peaks : null
  if (!peaks || peaks.length === 0) return <>{fallback}</>

  const fraction = duration > 0 ? Math.min(1, Math.max(0, (dragging ?? position) / duration)) : 0

  const fractionAt = (clientX: number) => {
    const el = ref.current
    if (!el) return 0
    const rect = el.getBoundingClientRect()
    return Math.min(1, Math.max(0, (clientX - rect.left) / rect.width))
  }

  const onPointerDown = (e: React.PointerEvent<HTMLDivElement>) => {
    if (disabled || duration <= 0) return
    e.currentTarget.setPointerCapture(e.pointerId)
    const sec = fractionAt(e.clientX) * duration
    setDragging(sec)
    onPreview(sec)
  }
  const onPointerMove = (e: React.PointerEvent<HTMLDivElement>) => {
    if (dragging === null) return
    const sec = fractionAt(e.clientX) * duration
    setDragging(sec)
    onPreview(sec)
  }
  const onPointerUp = (e: React.PointerEvent<HTMLDivElement>) => {
    if (dragging === null) return
    const sec = fractionAt(e.clientX) * duration
    setDragging(null)
    onPreview(null)
    onSeek(sec)
  }

  const onKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    if (disabled || duration <= 0) return
    const step = e.shiftKey ? 30 : 5
    if (e.key === "ArrowRight") {
      e.preventDefault()
      onSeek(Math.min(duration, position + step))
    } else if (e.key === "ArrowLeft") {
      e.preventDefault()
      onSeek(Math.max(0, position - step))
    } else if (e.key === "Home") {
      e.preventDefault()
      onSeek(0)
    } else if (e.key === "End") {
      e.preventDefault()
      onSeek(duration)
    }
  }

  return (
    <div
      ref={ref}
      role="slider"
      tabIndex={disabled ? -1 : 0}
      aria-label="Seek"
      aria-valuemin={0}
      aria-valuemax={Math.round(duration)}
      aria-valuenow={Math.round(dragging ?? position)}
      aria-disabled={disabled}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={() => {
        setDragging(null)
        onPreview(null)
      }}
      onKeyDown={onKeyDown}
      style={{ height }}
      className={cn(
        "relative w-full touch-none select-none rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-emerald-500/50",
        disabled ? "cursor-default opacity-50" : "cursor-pointer",
        className
      )}
    >
      <svg
        viewBox={`0 0 ${peaks.length} 100`}
        preserveAspectRatio="none"
        className="absolute inset-0 size-full"
        aria-hidden="true"
      >
        <defs>
          <clipPath id={clipId}>
            <rect x="0" y="0" width={peaks.length * fraction} height="100" />
          </clipPath>
        </defs>
        {/* unplayed */}
        <g className="fill-zinc-600/70">
          {peaks.map((p, i) => {
            const h = Math.max(4, p)
            return <rect key={i} x={i + 0.15} y={(100 - h) / 2} width={0.7} height={h} rx={0.3} />
          })}
        </g>
        {/* played, clipped to the position */}
        <g className="fill-emerald-500" clipPath={`url(#${clipId})`}>
          {peaks.map((p, i) => {
            const h = Math.max(4, p)
            return <rect key={i} x={i + 0.15} y={(100 - h) / 2} width={0.7} height={h} rx={0.3} />
          })}
        </g>
      </svg>
      {/* playhead */}
      <div
        aria-hidden="true"
        className="pointer-events-none absolute top-0 bottom-0 w-px bg-white/80 shadow-[0_0_6px_rgba(255,255,255,0.6)]"
        style={{ left: `${fraction * 100}%` }}
      />
    </div>
  )
}
